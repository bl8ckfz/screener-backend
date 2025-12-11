package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

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

	// TODO: Initialize ring buffers for all symbols
	// TODO: Subscribe to NATS candles.1m.{symbol}
	// TODO: Initialize TimescaleDB connection
	// TODO: Start metrics calculation workers

	log.Info().Msg("Metrics Calculator service started")

	// Wait for shutdown
	<-ctx.Done()
	log.Info().Msg("Metrics Calculator service stopped")
}
