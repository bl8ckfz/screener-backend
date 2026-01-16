package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/alerts"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting Alert Engine service")

	// Context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info().Msg("Shutdown signal received")
		cancel()
	}()

	// Get environment variables
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	pgURL := getEnv("POSTGRES_URL", "postgres://crypto_user:crypto_pass@localhost:5433/crypto_metadata")
	redisURL := getEnv("REDIS_URL", "localhost:6379")
	tsdbURL := getEnv("TIMESCALE_URL", "postgres://crypto_user:crypto_pass@localhost:5432/crypto_timeseries")
	webhookURLs := getEnvSlice("WEBHOOK_URLS", "")

	// Connect to PostgreSQL
	log.Info().Msg("Connecting to PostgreSQL")
	poolConfig, err := pgxpool.ParseConfig(pgURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse PostgreSQL URL")
	}

	poolConfig.MaxConns = 10
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to PostgreSQL")
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping PostgreSQL")
	}

	// Connect to Redis
	log.Info().Str("url", redisURL).Msg("Connecting to Redis")
	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})
	defer rdb.Close()

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}

	// Connect to TimescaleDB
	log.Info().Msg("Connecting to TimescaleDB")
	tsdbConfig, err := pgxpool.ParseConfig(tsdbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse TimescaleDB URL")
	}

	tsdbConfig.MaxConns = 10
	tsdbConfig.MaxConnLifetime = 1 * time.Hour
	tsdbConfig.MaxConnIdleTime = 30 * time.Minute

	tsdb, err := pgxpool.NewWithConfig(ctx, tsdbConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to TimescaleDB")
	}
	defer tsdb.Close()

	// Verify connection
	if err := tsdb.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping TimescaleDB")
	}

	// Connect to NATS
	log.Info().Str("url", natsURL).Msg("Connecting to NATS")
	nc, err := messaging.NewNATSConn(messaging.Config{
		URL:             natsURL,
		MaxReconnects:   -1,
		ReconnectWait:   2 * time.Second,
		EnableJetStream: true,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to NATS")
	}
	defer nc.Close()

	// Create JetStream context
	js, err := messaging.NewJetStream(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create JetStream context")
	}

	// Ensure ALERTS stream exists
	if err := messaging.CreateStream(js, "ALERTS", []string{"alerts.>"}, 1*time.Hour); err != nil {
		log.Fatal().Err(err).Msg("Failed to create ALERTS stream")
	}

	// Initialize alert engine
	engine := alerts.NewEngine(db, rdb, log.Logger)

	// Load alert rules from database
	log.Info().Msg("Loading alert rules")
	if err := engine.LoadRules(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to load alert rules")
	}

	// Initialize notifier
	notifier := alerts.NewNotifier(webhookURLs, log.Logger)
	log.Info().Int("webhooks", len(webhookURLs)).Msg("Initialized notifier")

	// Initialize persister
	persister := alerts.NewAlertPersister(tsdb, log.Logger)
	defer persister.Close()
	log.Info().Msg("Initialized alert persister")

	// Subscribe to metrics
	log.Info().Msg("Subscribing to metrics.calculated")
	sub, err := js.Subscribe("metrics.calculated", func(msg *nats.Msg) {
		// Parse metrics from message
		var metrics alerts.Metrics
		if err := json.Unmarshal(msg.Data, &metrics); err != nil {
			log.Error().Err(err).Msg("Failed to unmarshal metrics")
			return
		}

		// Evaluate all rules
		triggeredAlerts, err := engine.Evaluate(ctx, &metrics)
		if err != nil {
			log.Error().Err(err).Str("symbol", metrics.Symbol).Msg("Failed to evaluate rules")
			return
		}

		// Process triggered alerts
		for _, alert := range triggeredAlerts {
			// Persist to database
			persister.SaveAlert(alert)

			// Send webhook notifications
			if err := notifier.SendAlert(alert); err != nil {
				log.Error().Err(err).Str("symbol", alert.Symbol).Msg("Failed to send webhook")
			}

			// Publish to NATS for API Gateway
			payload, err := json.Marshal(alert)
			if err != nil {
				log.Error().Err(err).Msg("Failed to marshal alert")
				continue
			}

			subject := "alerts.triggered"
			if _, err := js.Publish(subject, payload); err != nil {
				log.Error().Err(err).Msg("Failed to publish alert")
				continue
			}
		}
	}, nats.Durable("alert-engine"), nats.DeliverAll())

	if err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to metrics")
	}
	defer func() {
		if err := sub.Unsubscribe(); err != nil {
			log.Error().Err(err).Msg("Failed to unsubscribe")
		}
	}()

	log.Info().Msg("Alert Engine service started")

	// Wait for shutdown
	<-ctx.Done()

	// Give time for final messages to process
	time.Sleep(1 * time.Second)

	log.Info().Msg("Alert Engine service stopped")
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
