package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/calculator"
	"github.com/bl8ckfz/crypto-screener-backend/internal/ringbuffer"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/database"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting Metrics Calculator service")

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

	// Connect to TimescaleDB
	timescaleURL := os.Getenv("TIMESCALEDB_URL")
	if timescaleURL == "" {
		timescaleURL = "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable"
	}

	log.Info().Str("url", timescaleURL).Msg("Connecting to TimescaleDB")
	dbPool, err := database.NewPostgresPool(ctx, timescaleURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to TimescaleDB")
	}
	defer dbPool.Close()

	// Get NATS URL from environment
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
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

	// Ensure METRICS stream exists
	if err := messaging.CreateStream(js, "METRICS", []string{"metrics.>"}, 1*time.Hour); err != nil {
		log.Fatal().Err(err).Msg("Failed to create METRICS stream")
	}

	// Initialize metrics calculator
	calc := calculator.NewMetricsCalculator(log.Logger)

	// Initialize metrics persister with batch writing (batch size: 50)
	persister := calculator.NewMetricsPersister(dbPool, log.Logger, 50)
	defer persister.Close()

	// Subscribe to all candle messages
	log.Info().Msg("Subscribing to candles.1m.>")
	sub, err := js.Subscribe("candles.1m.>", func(msg *nats.Msg) {
		// Parse candle from message
		var candle ringbuffer.Candle
		if err := json.Unmarshal(msg.Data, &candle); err != nil {
			log.Error().Err(err).Msg("Failed to unmarshal candle")
			return
		}

		// Add candle and calculate metrics
		metrics, err := calc.AddCandle(candle)
		if err != nil {
			log.Error().Err(err).Str("symbol", candle.Symbol).Msg("Failed to calculate metrics")
			return
		}

		// Only publish if we have meaningful metrics (buffer has enough data)
		if metrics == nil || calc.GetBufferSize(candle.Symbol) < 15 {
			return
		}

		// Persist metrics to TimescaleDB (async batch write)
		persister.Enqueue(metrics)

		// Publish metrics to NATS
		payload, err := json.Marshal(metrics)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal metrics")
			return
		}

		subject := "metrics.calculated"
		if _, err := js.Publish(subject, payload); err != nil {
			log.Error().Err(err).Msg("Failed to publish metrics")
			return
		}

		log.Debug().
			Str("symbol", candle.Symbol).
			Float64("vcp", metrics.VCP).
			Float64("rsi", metrics.RSI).
			Msg("Published metrics")
	}, nats.Durable("metrics-calculator"), nats.DeliverAll())

	if err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to candles")
	}
	defer sub.Unsubscribe()

	log.Info().Msg("Metrics Calculator service started")

	// Wait for shutdown
	<-ctx.Done()

	// Give time for final messages to process
	time.Sleep(1 * time.Second)

	log.Info().Msg("Metrics Calculator service stopped")
}
