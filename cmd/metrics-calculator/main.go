package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/calculator"
	"github.com/bl8ckfz/crypto-screener-backend/internal/ringbuffer"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/database"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/observability"
	"github.com/nats-io/nats.go"
)

func main() {
	// Setup observability
	logger := observability.NewLogger("metrics-calculator", observability.LevelInfo)
	metrics := observability.GetCollector()
	health := observability.NewHealthChecker()

	logger.Info("Starting Metrics Calculator service")

	// Context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutdown signal received")
		cancel()
	}()

	// Connect to TimescaleDB
	timescaleURL := os.Getenv("TIMESCALEDB_URL")
	if timescaleURL == "" {
		timescaleURL = "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable"
	}

	logger.Infof("Connecting to TimescaleDB: %s", timescaleURL)
	dbPool, err := database.NewPostgresPool(ctx, timescaleURL)
	if err != nil {
		logger.Fatal("Failed to connect to TimescaleDB", err)
	}
	defer dbPool.Close()

	// Add database health check
	health.AddCheck("timescaledb", func(ctx context.Context) error {
		return dbPool.Ping(ctx)
	})

	// Track connection pool size
	metrics.Gauge(observability.MetricDBConnectionPool).Set(float64(dbPool.Stat().TotalConns()))

	// Get NATS URL from environment
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	// Connect to NATS
	logger.Infof("Connecting to NATS: %s", natsURL)
	nc, err := messaging.NewNATSConn(messaging.Config{
		URL:             natsURL,
		MaxReconnects:   -1,
		ReconnectWait:   2 * time.Second,
		EnableJetStream: true,
	})
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}
	defer nc.Close()

	// Add NATS health check
	health.AddCheck("nats", func(ctx context.Context) error {
		if nc.IsClosed() {
			return fmt.Errorf("NATS connection closed")
		}
		return nil
	})

	// Create JetStream context
	js, err := messaging.NewJetStream(nc)
	if err != nil {
		logger.Fatal("Failed to create JetStream context", err)
	}

	// Ensure METRICS stream exists
	if err := messaging.CreateStream(js, "METRICS", []string{"metrics.>"}, 1*time.Hour); err != nil {
		logger.Fatal("Failed to create METRICS stream", err)
	}

	// Initialize metrics calculator
	calc := calculator.NewMetricsCalculator(logger.Zerolog())

	// Initialize metrics persister with batch writing (batch size: 50)
	persister := calculator.NewMetricsPersister(dbPool, logger.Zerolog(), 50)
	defer persister.Close()

	// Subscribe to all candle messages
	// Use unique consumer name to allow multiple deployments/replicas
	hostname, err := os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("metrics-calculator-%d", time.Now().Unix())
	}
	consumerName := fmt.Sprintf("metrics-calculator-%s", hostname)
	
	logger.WithField("consumer", consumerName).Info("Subscribing to candles.1m.>")
	sub, err := js.Subscribe("candles.1m.>", func(msg *nats.Msg) {
		defer msg.Ack() // Acknowledge message after processing
		// Parse candle from message
		var candle ringbuffer.Candle
		if err := json.Unmarshal(msg.Data, &candle); err != nil {
			logger.Error("Failed to unmarshal candle", err)
			return
		}

		// DEBUG: Log received candle to verify volume data
		logger.WithFields(map[string]interface{}{
			"symbol":       candle.Symbol,
			"volume":       candle.Volume,
			"quote_volume": candle.QuoteVolume,
			"close":        candle.Close,
		}).Debug("Received candle from NATS")

		metrics.Counter(observability.MetricCandlesProcessed).Inc()

		// Persist candle to TimescaleDB (synchronous to ensure historical data)
		// This is critical for ring buffer initialization on restart
		candleCtx, candleCancel := context.WithTimeout(ctx, 5*time.Second)
		defer candleCancel()
		if err := persister.PersistCandle(candleCtx, candle); err != nil {
			logger.WithField("symbol", candle.Symbol).Error("Failed to persist candle", err)
			// Don't return - continue processing even if DB write fails
		}

		// Measure calculation time
		defer metrics.Timer(observability.MetricCalculationDuration)()

		// Add candle and calculate metrics
		metricsData, err := calc.AddCandle(candle)
		if err != nil {
			logger.WithField("symbol", candle.Symbol).Error("Failed to calculate metrics", err)
			return
		}

		// Only publish if we have meaningful metrics (buffer has enough data)
		if metricsData == nil || calc.GetBufferSize(candle.Symbol) < 15 {
			return
		}

		metrics.Counter(observability.MetricMetricsCalculated).Inc()

		// Persist metrics to TimescaleDB (async batch write)
		persister.Enqueue(metricsData)

		// Publish metrics to NATS
		payload, err := json.Marshal(metricsData)
		if err != nil {
			logger.Error("Failed to marshal metrics", err)
			return
		}

		subject := "metrics.calculated"
		if _, err := js.Publish(subject, payload); err != nil {
			logger.Error("Failed to publish metrics", err)
			metrics.Counter(observability.MetricNATSPublishErrors).Inc()
			return
		}

		metrics.Counter(observability.MetricNATSMessagesPublished).Inc()

		logger.WithFields(map[string]interface{}{
			"symbol":       candle.Symbol,
			"vcp":          metricsData.VCP,
			"rsi":          metricsData.RSI,
			"volume_1h":    metricsData.Candle1h.Volume,
			"volume_5m":    metricsData.Candle5m.Volume,
			"quote_vol_1h": metricsData.Candle1h.Volume,
		}).Debug("Published metrics")
	}, nats.Durable(consumerName), nats.DeliverAll(), nats.AckExplicit())

	if err != nil {
		logger.Fatal("Failed to subscribe to candles", err)
	}
	defer func() {
		if err := sub.Unsubscribe(); err != nil {
			logger.Error("Failed to unsubscribe", err)
		}
	}()

	// Start metrics server
	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9091"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metrics.Handler())
	mux.HandleFunc("/health/live", health.LivenessHandler())
	mux.HandleFunc("/health/ready", health.ReadinessHandler())

	metricsServer := &http.Server{
		Addr:    ":" + metricsPort,
		Handler: mux,
	}

	go func() {
		logger.Infof("Metrics server listening on :%s", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Metrics server error", err)
		}
	}()
	defer metricsServer.Shutdown(context.Background())

	logger.Info("Metrics Calculator service started")

	// Wait for shutdown
	<-ctx.Done()

	// Give time for final messages to process
	time.Sleep(1 * time.Second)

	logger.Info("Metrics Calculator service stopped")
}
