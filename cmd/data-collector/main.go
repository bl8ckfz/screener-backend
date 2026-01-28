package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/binance"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/observability"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Setup observability
	logger := observability.NewLogger("data-collector", observability.LevelInfo)
	metrics := observability.GetCollector()
	health := observability.NewHealthChecker()

	logger.Info("Starting Data Collector service")

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

	// Get NATS URL from environment (default to localhost)
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	// Optional Redis for ticker cache
	redisURL := os.Getenv("REDIS_URL")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	var rdb *redis.Client
	if redisURL != "" && redisURL != "disabled" {
		logger.WithField("url", redisURL).Info("Connecting to Redis")
		rdb = redis.NewClient(&redis.Options{
			Addr:     redisURL,
			Password: redisPassword,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.WithField("error", err.Error()).Warn("Failed to connect to Redis, ticker cache disabled")
			rdb.Close()
			rdb = nil
		} else {
			defer rdb.Close()
		}
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

	// Ensure CANDLES stream exists
	if err := messaging.CreateStream(js, "CANDLES", []string{"candles.>"}, 1*time.Hour); err != nil {
		logger.Fatal("Failed to create CANDLES stream", err)
	}

	// Initialize Binance API client
	client := binance.NewClient(logger.Zerolog())

	// Fetch active symbols
	logger.Info("Fetching active symbols from Binance")
	symbols, err := client.GetActiveSymbols(ctx)
	if err != nil {
		logger.Fatal("Failed to fetch active symbols", err)
	}

	logger.WithField("count", len(symbols)).Info("Fetched active symbols")

	// Track active connections
	metrics.Gauge(observability.MetricWSConnections).Set(float64(len(symbols)))

	// Create WebSocket connection manager
	wsManager := binance.NewConnectionManager(symbols, js, logger.Zerolog())

	// Start metrics server
	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9090"
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

	logger.Info("Data Collector service started")

	// Start Binance ticker stream to populate Redis cache
	if rdb != nil {
		go binance.StartTickerStream(ctx, rdb, logger.Zerolog())
	}

	// Start collecting data in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- wsManager.Start(ctx)
	}()

	// Wait for either error or shutdown signal
	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("Connection manager error", err)
		}
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down")
	}

	// Give connections time to close gracefully
	time.Sleep(2 * time.Second)

	logger.Info("Data Collector service stopped")
}
