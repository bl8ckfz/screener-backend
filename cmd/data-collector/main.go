package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bl8ckfz/crypto-screener-backend/internal/binance"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
)

func main() {
	// Setup structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting Data Collector service")

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

	// Get NATS URL from environment (default to localhost)
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

	// Ensure CANDLES stream exists
	if err := messaging.CreateStream(js, "CANDLES", []string{"candles.>"}, 1*time.Hour); err != nil {
		log.Fatal().Err(err).Msg("Failed to create CANDLES stream")
	}

	// Initialize Binance API client
	client := binance.NewClient(log.Logger)

	// Fetch active symbols
	log.Info().Msg("Fetching active symbols from Binance")
	symbols, err := client.GetActiveSymbols(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to fetch active symbols")
	}

	log.Info().Int("count", len(symbols)).Msg("Fetched active symbols")

	// Create WebSocket connection manager
	wsManager := binance.NewConnectionManager(symbols, js, log.Logger)

	log.Info().Msg("Data Collector service started")

	// Start collecting data in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- wsManager.Start(ctx)
	}()

	// Wait for either error or shutdown signal
	select {
	case err := <-errCh:
		if err != nil {
			log.Error().Err(err).Msg("Connection manager error")
		}
	case <-ctx.Done():
		log.Info().Msg("Context cancelled, shutting down")
	}

	// Give connections time to close gracefully
	time.Sleep(2 * time.Second)

	log.Info().Msg("Data Collector service stopped")
}
