package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/alerts"
	"github.com/bl8ckfz/crypto-screener-backend/internal/calculator"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// convertCandle converts calculator.TimeframeCandle to alerts.TimeframeCandle
func convertCandle(c calculator.TimeframeCandle) alerts.TimeframeCandle {
	return alerts.TimeframeCandle{
		Open:   c.Open,
		High:   c.High,
		Low:    c.Low,
		Close:  c.Close,
		Volume: c.Volume,
	}
}

func main() {
	// Setup observability with LOG_LEVEL from environment
	logLevel := observability.LevelInfo
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "debug":
			logLevel = observability.LevelDebug
		case "info":
			logLevel = observability.LevelInfo
		case "warn", "warning":
			logLevel = observability.LevelWarn
		case "error":
			logLevel = observability.LevelError
		}
	}
	
	logger := observability.NewLogger("alert-engine", logLevel)
	metrics := observability.GetCollector()
	health := observability.NewHealthChecker()

	logger.Info("Starting Alert Engine service")

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

	// Get environment variables
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	pgURL := getEnv("POSTGRES_URL", "postgres://crypto_user:crypto_pass@localhost:5433/crypto_metadata")
	redisURL := getEnv("REDIS_URL", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	tsdbURL := getEnv("TIMESCALE_URL", "postgres://crypto_user:crypto_pass@localhost:5432/crypto_timeseries")
	webhookURLs := getEnvSlice("WEBHOOK_URLS", "")

	// Connect to PostgreSQL
	logger.Info("Connecting to PostgreSQL")
	poolConfig, err := pgxpool.ParseConfig(pgURL)
	if err != nil {
		logger.Fatal("Failed to parse PostgreSQL URL", err)
	}

	poolConfig.MaxConns = 10
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		logger.Fatal("Failed to connect to PostgreSQL", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(ctx); err != nil {
		logger.Fatal("Failed to ping PostgreSQL", err)
	}

	// Add PostgreSQL health check
	health.AddCheck("postgres", func(ctx context.Context) error {
		return db.Ping(ctx)
	})

	// Connect to Redis (optional - use in-memory if not available)
	var rdb *redis.Client
	if redisURL != "" && redisURL != "disabled" {
		logger.WithField("url", redisURL).Info("Connecting to Redis")
		rdb = redis.NewClient(&redis.Options{
			Addr:     redisURL,
			Password: redisPassword,
		})
		
		// Test Redis connection
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.WithField("error", err.Error()).Warn("Failed to connect to Redis, using in-memory deduplication")
			rdb.Close()
			rdb = nil
		} else {
			defer rdb.Close()
			// Add Redis health check
			health.AddCheck("redis", func(ctx context.Context) error {
				return rdb.Ping(ctx).Err()
			})
			logger.Info("Connected to Redis for alert deduplication")
		}
	} else {
		logger.Info("Redis disabled, using in-memory deduplication")
	}

	// Connect to TimescaleDB
	logger.Info("Connecting to TimescaleDB")
	tsdbConfig, err := pgxpool.ParseConfig(tsdbURL)
	if err != nil {
		logger.Fatal("Failed to parse TimescaleDB URL", err)
	}

	tsdbConfig.MaxConns = 10
	tsdbConfig.MaxConnLifetime = 1 * time.Hour
	tsdbConfig.MaxConnIdleTime = 30 * time.Minute

	tsdb, err := pgxpool.NewWithConfig(ctx, tsdbConfig)
	if err != nil {
		logger.Fatal("Failed to connect to TimescaleDB", err)
	}
	defer tsdb.Close()

	// Verify connection
	if err := tsdb.Ping(ctx); err != nil {
		logger.Fatal("Failed to ping TimescaleDB", err)
	}

	// Add TimescaleDB health check
	health.AddCheck("timescaledb", func(ctx context.Context) error {
		return tsdb.Ping(ctx)
	})

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

	// Ensure ALERTS stream exists
	if err := messaging.CreateStream(js, "ALERTS", []string{"alerts.>"}, 1*time.Hour); err != nil {
		logger.Fatal("Failed to create ALERTS stream", err)
	}

	// Initialize alert engine
	engine := alerts.NewEngine(db, rdb, logger.Zerolog())

	// Load alert rules from database
	logger.Info("Loading alert rules")
	if err := engine.LoadRules(ctx); err != nil {
		logger.Fatal("Failed to load alert rules", err)
	}

	// Initialize notifier
	notifier := alerts.NewNotifier(webhookURLs, logger.Zerolog())
	logger.WithField("webhooks", len(webhookURLs)).Info("Initialized notifier")

	// Initialize persister
	persister := alerts.NewAlertPersister(tsdb, logger.Zerolog())
	defer persister.Close()
	logger.Info("Initialized alert persister")

	// Subscribe to metrics
	logger.Info("Subscribing to metrics.calculated")
	sub, err := js.Subscribe("metrics.calculated", func(msg *nats.Msg) {
		// Parse metrics from message using calculator.SymbolMetrics
		var metricsData calculator.SymbolMetrics
		if err := json.Unmarshal(msg.Data, &metricsData); err != nil {
			logger.Error("Failed to unmarshal metrics", err)
			return
		}

		// Convert to alerts.Metrics format
		alertMetrics := &alerts.Metrics{
			Symbol:         metricsData.Symbol,
			Timestamp:      metricsData.Timestamp,
			LastPrice:      metricsData.LastPrice,
			Candle1m:       convertCandle(metricsData.Candle1m),
			Candle5m:       convertCandle(metricsData.Candle5m),
			Candle15m:      convertCandle(metricsData.Candle15m),
			Candle1h:       convertCandle(metricsData.Candle1h),
			Candle8h:       convertCandle(metricsData.Candle8h),
			Candle1d:       convertCandle(metricsData.Candle1d),
			PriceChange5m:  metricsData.PriceChange5m,
			PriceChange15m: metricsData.PriceChange15m,
			PriceChange1h:  metricsData.PriceChange1h,
			PriceChange8h:  metricsData.PriceChange8h,
			PriceChange1d:  metricsData.PriceChange1d,
			VolumeRatio5m:  metricsData.VolumeRatio5m,
			VolumeRatio15m: metricsData.VolumeRatio15m,
			VolumeRatio1h:  metricsData.VolumeRatio1h,
			VolumeRatio8h:  metricsData.VolumeRatio8h,
			VCP:            metricsData.VCP,
			RSI:            metricsData.RSI,
		}

		// DEBUG: Log metrics to identify data issues
		logger.WithFields(map[string]interface{}{
			"symbol":       alertMetrics.Symbol,
			"price":        alertMetrics.LastPrice,
			"change_5m":    alertMetrics.PriceChange5m,
			"change_15m":   alertMetrics.PriceChange15m,
			"change_1h":    alertMetrics.PriceChange1h,
			"change_8h":    alertMetrics.PriceChange8h,
			"change_1d":    alertMetrics.PriceChange1d,
			"volume_5m":    alertMetrics.Candle5m.Volume,
			"volume_1h":    alertMetrics.Candle1h.Volume,
		}).Debug("Received metrics for evaluation")

		metrics.Counter(observability.MetricNATSMessagesReceived).Inc()

		// Measure evaluation time
		defer metrics.Timer(observability.MetricEvaluationDuration)()

		// Evaluate all rules
		triggeredAlerts, err := engine.Evaluate(ctx, alertMetrics)
		if err != nil {
			logger.WithField("symbol", alertMetrics.Symbol).Error("Failed to evaluate rules", err)
			return
		}

		metrics.Counter(observability.MetricAlertsEvaluated).Inc()

		// Process triggered alerts
		for _, alert := range triggeredAlerts {
			metrics.Counter(observability.MetricAlertsTriggered).Inc()

			// Persist to database
			persister.SaveAlert(alert)

			// Send webhook notifications
			if err := notifier.SendAlert(alert); err != nil {
				logger.WithField("symbol", alert.Symbol).Error("Failed to send webhook", err)
				metrics.Counter(observability.MetricWebhooksFailed).Inc()
			} else {
				metrics.Counter(observability.MetricWebhooksSent).Inc()
			}

			// Publish to NATS for API Gateway
			payload, err := json.Marshal(alert)
			if err != nil {
				logger.Error("Failed to marshal alert", err)
				continue
			}

			subject := "alerts.triggered"
			if _, err := js.Publish(subject, payload); err != nil {
				logger.Error("Failed to publish alert", err)
				metrics.Counter(observability.MetricNATSPublishErrors).Inc()
				continue
			}

			metrics.Counter(observability.MetricNATSMessagesPublished).Inc()
		}
	}, nats.Durable("alert-engine"), nats.DeliverAll())

	if err != nil {
		logger.Fatal("Failed to subscribe to metrics", err)
	}
	defer func() {
		if err := sub.Unsubscribe(); err != nil {
			logger.Error("Failed to unsubscribe", err)
		}
	}()

	// Start metrics server
	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9092"
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

	logger.Info("Alert Engine service started")

	// Wait for shutdown
	<-ctx.Done()

	// Give time for final messages to process
	time.Sleep(1 * time.Second)

	logger.Info("Alert Engine service stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvSlice(key, defaultValue string) []string {
	value := getEnv(key, defaultValue)
	if value == "" {
		return []string{}
	}

	// Split by comma and trim spaces
	parts := strings.Split(value, ",")
	var result []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
