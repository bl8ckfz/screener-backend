package messaging

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Config holds NATS configuration
type Config struct {
	URL             string
	MaxReconnects   int
	ReconnectWait   time.Duration
	EnableJetStream bool
}

// NewNATSConn creates a new NATS connection
func NewNATSConn(cfg Config) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("crypto-screener"),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				log.Warn().Err(err).Msg("NATS disconnected")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("NATS reconnected")
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Info().Msg("NATS connection closed")
		}),
	}

	// Set default values if not provided
	if cfg.MaxReconnects == 0 {
		cfg.MaxReconnects = -1 // Infinite retries
	}
	if cfg.ReconnectWait == 0 {
		cfg.ReconnectWait = 2 * time.Second
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	log.Info().
		Str("url", cfg.URL).
		Str("server", nc.ConnectedUrl()).
		Bool("jetstream", cfg.EnableJetStream).
		Msg("Connected to NATS")

	return nc, nil
}

// NewJetStream creates a JetStream context
func NewJetStream(nc *nats.Conn) (nats.JetStreamContext, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	log.Info().Msg("JetStream context created")
	return js, nil
}

// CreateStream creates a JetStream stream if it doesn't exist
func CreateStream(js nats.JetStreamContext, name string, subjects []string, maxAge time.Duration) error {
	// Check if stream exists
	_, err := js.StreamInfo(name)
	if err == nil {
		log.Info().Str("stream", name).Msg("Stream already exists")
		return nil
	}

	// Create stream
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: nats.WorkQueuePolicy,
		MaxAge:    maxAge,
		Storage:   nats.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("failed to create stream %s: %w", name, err)
	}

	log.Info().
		Str("stream", name).
		Strs("subjects", subjects).
		Dur("max_age", maxAge).
		Msg("Created JetStream stream")

	return nil
}

// Close gracefully closes the NATS connection
func Close(nc *nats.Conn) {
	if nc != nil && !nc.IsClosed() {
		nc.Close()
		log.Info().Msg("NATS connection closed")
	}
}
