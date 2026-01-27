package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestMetricsEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")

	srv, err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer srv.shutdown()

	// Insert test metrics
	now := time.Now().UTC().Truncate(time.Second)
	insert := `INSERT INTO metrics_calculated (
		time, symbol, timeframe, open, high, low, close, volume,
		vcp, rsi_14, macd, macd_signal,
		bb_upper, bb_middle, bb_lower,
		fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`

	_, err = srv.db.Exec(ctx, insert,
		now, "BTCUSDT", "5m", 42000.0, 42100.0, 41900.0, 42050.0, 100000.0,
		0.75, 62.5, 15.3, 12.1,
		42200.0, 42000.0, 41800.0,
		42500.0, 42300.0, 42150.0, 42000.0, 41850.0, 41700.0, 41500.0)
	if err != nil {
		t.Fatalf("failed to insert metrics: %v", err)
	}
	t.Cleanup(func() {
		srv.db.Exec(context.Background(), "DELETE FROM metrics_calculated WHERE symbol='BTCUSDT' AND time=$1", now)
	})

	// Test GET /api/metrics/{symbol}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/metrics/BTCUSDT")
	if err != nil {
		t.Fatalf("GET metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if result["symbol"] != "BTCUSDT" {
		t.Fatalf("expected symbol BTCUSDT, got %v", result["symbol"])
	}

	timeframes, ok := result["timeframes"].(map[string]interface{})
	if !ok || len(timeframes) == 0 {
		t.Fatalf("expected timeframes data, got: %v", result["timeframes"])
	}
}

func TestSettingsEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")
	os.Setenv("POSTGRES_URL", "postgres://crypto_user:crypto_password@localhost:5433/crypto_metadata?sslmode=disable")

	srv, err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer srv.shutdown()

	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// Test POST /api/settings (requires auth)
	settings := map[string]interface{}{
		"selected_alerts":      []string{"big_bull_60m", "pioneer_bull"},
		"webhook_url":          "https://discord.com/webhooks/test",
		"notification_enabled": true,
	}
	body, _ := json.Marshal(settings)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/settings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST settings failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if result["success"] != true {
		t.Fatalf("expected success=true, got %v", result)
	}

	// Cleanup
	srv.metadataDB.Exec(ctx, "DELETE FROM user_settings WHERE user_id='test-user-id'")
}

func TestRateLimiting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")

	srv, err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer srv.shutdown()

	// Override rate limiter for testing (2 requests per minute)
	srv.rateLimiter = newRateLimiter(2, time.Minute)

	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// First two requests should succeed
	for i := 0; i < 2; i++ {
		resp, err := http.Get(ts.URL + "/api/alerts")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	// Third request should be rate limited
	resp, err := http.Get(ts.URL + "/api/alerts")
	if err != nil {
		t.Fatalf("rate limit test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
}

func TestCORS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")

	srv, err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer srv.shutdown()

	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// Test OPTIONS request
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/alerts", nil)
	req.Header.Set("Origin", "https://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("missing CORS headers")
	}
}
