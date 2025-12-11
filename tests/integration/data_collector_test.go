package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/binance"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// TestDataCollectorIntegration tests the full flow from Binance API to NATS
func TestDataCollectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := zerolog.Nop() // Silent logger for tests
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Connect to NATS
	nc, err := messaging.NewNATSConn(messaging.Config{
		URL:             "nats://localhost:4222",
		MaxReconnects:   3,
		ReconnectWait:   1 * time.Second,
		EnableJetStream: true,
	})
	if err != nil {
		t.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Create JetStream context
	js, err := messaging.NewJetStream(nc)
	if err != nil {
		t.Fatalf("Failed to create JetStream: %v", err)
	}

	// Ensure CANDLES stream exists
	if err := messaging.CreateStream(js, "CANDLES", []string{"candles.>"}, 1*time.Hour); err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}

	// Initialize Binance client
	client := binance.NewClient(logger)

	// Fetch active symbols (just use first 3 for testing)
	symbols, err := client.GetActiveSymbols(ctx)
	if err != nil {
		t.Fatalf("Failed to fetch symbols: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("No symbols returned")
	}

	t.Logf("Fetched %d symbols, testing with first 3", len(symbols))
	testSymbols := symbols[:3]

	// Subscribe to candle messages
	receivedCh := make(chan *binance.Candle, 3)
	subscriptions := make([]*nats.Subscription, 0)

	for _, symbol := range testSymbols {
		subject := "candles.1m." + symbol
		sub, err := js.Subscribe(subject, func(msg *nats.Msg) {
			var candle binance.Candle
			if err := json.Unmarshal(msg.Data, &candle); err != nil {
				t.Errorf("Failed to unmarshal candle: %v", err)
				return
			}
			receivedCh <- &candle
		})
		if err != nil {
			t.Fatalf("Failed to subscribe to %s: %v", subject, err)
		}
		subscriptions = append(subscriptions, sub)
		defer sub.Unsubscribe()
	}

	// Start WebSocket manager
	wsManager := binance.NewConnectionManager(testSymbols, js, logger)

	go func() {
		if err := wsManager.Start(ctx); err != nil {
			t.Logf("WebSocket manager error: %v", err)
		}
	}()

	// Wait for messages (max 2 minutes for 3 symbols to complete 1 candle each)
	t.Log("Waiting for candle messages...")
	received := 0
	timeout := time.After(2 * time.Minute)

	for received < 3 {
		select {
		case candle := <-receivedCh:
			t.Logf("Received candle: %s @ %f (volume: %f)", candle.Symbol, candle.Close, candle.Volume)
			received++
		case <-timeout:
			t.Fatalf("Timeout waiting for candles (received %d/3)", received)
		case <-ctx.Done():
			t.Fatalf("Context cancelled (received %d/3)", received)
		}
	}

	t.Logf("Successfully received %d candles", received)
}
