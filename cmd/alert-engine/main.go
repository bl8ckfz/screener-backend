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
	dbURL := getEnv("TIMESCALEDB_URL", "postgres://crypto_user:crypto_pass@localhost:5432/crypto")
	redisURL := getEnv("REDIS_URL", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	webhookURLs := getEnvSlice("WEBHOOK_URLS", "")

	// Connect to TimescaleDB
	logger.Info("Connecting to TimescaleDB")
	poolConfig, err := pgxpool.ParseConfig(dbURL)
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
	persister := alerts.NewAlertPersister(db, logger.Zerolog())
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
			"symbol":      alertMetrics.Symbol,
			"price":       alertMetrics.LastPrice,
			"change_5m":   alertMetrics.PriceChange5m,
			"change_15m":  alertMetrics.PriceChange15m,
			"change_1h":   alertMetrics.PriceChange1h,
			"change_8h":   alertMetrics.PriceChange8h,
			"change_1d":   alertMetrics.PriceChange1d,
			"volume_5m":   alertMetrics.Candle5m.Volume,
			"volume_15m":  alertMetrics.Candle15m.Volume,
			"volume_1h":   alertMetrics.Candle1h.Volume,
			"vcp":         alertMetrics.VCP,
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

	// Start periodic evaluation (every 5 seconds to catch intra-minute spikes)
	logger.Info("Starting periodic evaluation (5s interval)")
	go runPeriodicEvaluation(ctx, engine, db, persister, notifier, js, metrics, logger)

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

// runPeriodicEvaluation queries the latest metrics from the database every 5 seconds
// This catches intra-minute price spikes that the NATS stream evaluation might miss
func runPeriodicEvaluation(
	ctx context.Context,
	engine *alerts.Engine,
	db *pgxpool.Pool,
	persister *alerts.AlertPersister,
	notifier *alerts.Notifier,
	js nats.JetStreamContext,
	metrics *observability.MetricsCollector,
	logger *observability.Logger,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Query latest 1m metrics for all symbols
			metricsSlice, err := queryLatestMetrics(ctx, db)
			if err != nil {
				logger.Error("Failed to query latest metrics", err)
				continue
			}

			// Evaluate each symbol
			evaluationCount := 0
			alertCount := 0
			for _, m := range metricsSlice {
				triggeredAlerts, err := engine.Evaluate(ctx, m)
				if err != nil {
					continue
				}

				evaluationCount++

				// Process triggered alerts
				for _, alert := range triggeredAlerts {
					alertCount++
					metrics.Counter(observability.MetricAlertsTriggered).Inc()

					// Persist to database
					persister.SaveAlert(alert)

					// Send webhook notifications
					if err := notifier.SendAlert(alert); err != nil {
						metrics.Counter(observability.MetricWebhooksFailed).Inc()
					} else {
						metrics.Counter(observability.MetricWebhooksSent).Inc()
					}

					// Publish to NATS for API Gateway
					payload, err := json.Marshal(alert)
					if err != nil {
						continue
					}

					if _, err := js.Publish("alerts.triggered", payload); err != nil {
						metrics.Counter(observability.MetricNATSPublishErrors).Inc()
						continue
					}

					metrics.Counter(observability.MetricNATSMessagesPublished).Inc()
				}
			}

			// Log periodic evaluation stats
			if alertCount > 0 {
				logger.WithFields(map[string]interface{}{
					"symbols_evaluated": evaluationCount,
					"alerts_triggered":  alertCount,
				}).Info("Periodic evaluation completed")
			}

		case <-ctx.Done():
			logger.Info("Stopping periodic evaluation")
			return
		}
	}
}

// queryLatestMetrics retrieves the latest metrics for all symbols from the database
// This is used by periodic evaluation to get fresh data every 5 seconds
// NOTE: Each timeframe is stored as a separate row in metrics_calculated
func queryLatestMetrics(ctx context.Context, db *pgxpool.Pool) ([]*alerts.Metrics, error) {
	// Query to get latest metrics for each symbol across all timeframes
	// We pivot the timeframe rows into columns using MAX and CASE
	query := `
		WITH latest_metrics AS (
			SELECT DISTINCT ON (symbol, timeframe)
				symbol,
				timeframe,
				time,
				open, high, low, close, volume,
				price_change,
				volume_ratio,
				vcp,
				rsi_14
			FROM metrics_calculated
			WHERE time > NOW() - INTERVAL '5 minutes'
			ORDER BY symbol, timeframe, time DESC
		)
		SELECT
			symbol,
			MAX(CASE WHEN timeframe = '5m' THEN time END) as time_5m,
			MAX(CASE WHEN timeframe = '5m' THEN open END) as open_5m,
			MAX(CASE WHEN timeframe = '5m' THEN high END) as high_5m,
			MAX(CASE WHEN timeframe = '5m' THEN low END) as low_5m,
			MAX(CASE WHEN timeframe = '5m' THEN close END) as close_5m,
			MAX(CASE WHEN timeframe = '5m' THEN volume END) as volume_5m,
			MAX(CASE WHEN timeframe = '5m' THEN price_change END) as price_change_5m,
			MAX(CASE WHEN timeframe = '15m' THEN open END) as open_15m,
			MAX(CASE WHEN timeframe = '15m' THEN high END) as high_15m,
			MAX(CASE WHEN timeframe = '15m' THEN low END) as low_15m,
			MAX(CASE WHEN timeframe = '15m' THEN close END) as close_15m,
			MAX(CASE WHEN timeframe = '15m' THEN volume END) as volume_15m,
			MAX(CASE WHEN timeframe = '15m' THEN price_change END) as price_change_15m,
			MAX(CASE WHEN timeframe = '1h' THEN open END) as open_1h,
			MAX(CASE WHEN timeframe = '1h' THEN high END) as high_1h,
			MAX(CASE WHEN timeframe = '1h' THEN low END) as low_1h,
			MAX(CASE WHEN timeframe = '1h' THEN close END) as close_1h,
			MAX(CASE WHEN timeframe = '1h' THEN volume END) as volume_1h,
			MAX(CASE WHEN timeframe = '1h' THEN price_change END) as price_change_1h,
			MAX(CASE WHEN timeframe = '8h' THEN open END) as open_8h,
			MAX(CASE WHEN timeframe = '8h' THEN high END) as high_8h,
			MAX(CASE WHEN timeframe = '8h' THEN low END) as low_8h,
			MAX(CASE WHEN timeframe = '8h' THEN close END) as close_8h,
			MAX(CASE WHEN timeframe = '8h' THEN volume END) as volume_8h,
			MAX(CASE WHEN timeframe = '8h' THEN price_change END) as price_change_8h,
			MAX(CASE WHEN timeframe = '1d' THEN open END) as open_1d,
			MAX(CASE WHEN timeframe = '1d' THEN high END) as high_1d,
			MAX(CASE WHEN timeframe = '1d' THEN low END) as low_1d,
			MAX(CASE WHEN timeframe = '1d' THEN close END) as close_1d,
			MAX(CASE WHEN timeframe = '1d' THEN volume END) as volume_1d,
			MAX(CASE WHEN timeframe = '1d' THEN price_change END) as price_change_1d,
			MAX(CASE WHEN timeframe = '5m' THEN vcp END) as vcp,
			MAX(CASE WHEN timeframe = '5m' THEN rsi_14 END) as rsi
		FROM latest_metrics
		GROUP BY symbol
		HAVING MAX(CASE WHEN timeframe = '5m' THEN close END) IS NOT NULL
	`

	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metricsSlice []*alerts.Metrics
	for rows.Next() {
		var m alerts.Metrics
		var c5m, c15m, c1h, c8h, c1d alerts.TimeframeCandle
		var time5m time.Time
		var priceChange5m, priceChange15m, priceChange1h, priceChange8h, priceChange1d *float64
		var vcp, rsi *float64

		err := rows.Scan(
			&m.Symbol,
			&time5m,
			&c5m.Open, &c5m.High, &c5m.Low, &c5m.Close, &c5m.Volume,
			&priceChange5m,
			&c15m.Open, &c15m.High, &c15m.Low, &c15m.Close, &c15m.Volume,
			&priceChange15m,
			&c1h.Open, &c1h.High, &c1h.Low, &c1h.Close, &c1h.Volume,
			&priceChange1h,
			&c8h.Open, &c8h.High, &c8h.Low, &c8h.Close, &c8h.Volume,
			&priceChange8h,
			&c1d.Open, &c1d.High, &c1d.Low, &c1d.Close, &c1d.Volume,
			&priceChange1d,
			&vcp,
			&rsi,
		)
		if err != nil {
			continue
		}

		// Convert nullable floats
		if priceChange5m != nil {
			m.PriceChange5m = *priceChange5m
		}
		if priceChange15m != nil {
			m.PriceChange15m = *priceChange15m
		}
		if priceChange1h != nil {
			m.PriceChange1h = *priceChange1h
		}
		if priceChange8h != nil {
			m.PriceChange8h = *priceChange8h
		}
		if priceChange1d != nil {
			m.PriceChange1d = *priceChange1d
		}
		if vcp != nil {
			m.VCP = *vcp
		}
		if rsi != nil {
			m.RSI = *rsi
		}

		m.Timestamp = time5m
		m.LastPrice = c5m.Close
		m.Candle5m = c5m
		m.Candle15m = c15m
		m.Candle1h = c1h
		m.Candle8h = c8h
		m.Candle1d = c1d

		metricsSlice = append(metricsSlice, &m)
	}

	return metricsSlice, rows.Err()
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
