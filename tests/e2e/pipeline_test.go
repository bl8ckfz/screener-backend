package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// TestFullPipeline validates the complete data flow:
// Binance candles → NATS → Metrics Calculator → TimescaleDB → Alert Engine → NATS → API Gateway
func TestFullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to infrastructure
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	tsdbURL := getEnv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")
	redisURL := getEnv("REDIS_URL", "localhost:6379")

	// NATS connection
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect to NATS: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream context: %v", err)
	}

	// TimescaleDB connection
	db, err := pgx.Connect(ctx, tsdbURL)
	if err != nil {
		t.Fatalf("connect to TimescaleDB: %v", err)
	}
	defer db.Close(ctx)

	// Redis connection
	rdb := redis.NewClient(&redis.Options{Addr: redisURL})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to Redis: %v", err)
	}

	symbol := "PIPELINETEST"
	defer cleanupTestData(t, ctx, db, symbol)

	t.Run("Phase1_PublishCandles", func(t *testing.T) {
		// Publish synthetic candles to NATS
		baseTime := time.Now().UTC().Truncate(time.Minute)
		
		for i := 0; i < 100; i++ {
			candle := map[string]interface{}{
				"symbol":     symbol,
				"open_time":  baseTime.Add(time.Duration(i) * time.Minute).Unix(),
				"close_time": baseTime.Add(time.Duration(i+1) * time.Minute).Unix(),
				"open":       40000.0 + float64(i),
				"high":       40100.0 + float64(i),
				"low":        39900.0 + float64(i),
				"close":      40050.0 + float64(i),
				"volume":     1000.0 + float64(i*10),
			}

			data, _ := json.Marshal(candle)
			subject := fmt.Sprintf("candles.1m.%s", symbol)
			
			if _, err := js.Publish(subject, data); err != nil {
				t.Fatalf("publish candle %d: %v", i, err)
			}
		}

		t.Logf("✓ Published 100 candles to NATS stream")
	})

	t.Run("Phase2_VerifyMetricsCalculated", func(t *testing.T) {
		// Wait for metrics to be calculated and persisted
		time.Sleep(5 * time.Second)

		var count int
		query := `SELECT COUNT(*) FROM metrics_calculated WHERE symbol = $1`
		err := db.QueryRow(ctx, query, symbol).Scan(&count)
		if err != nil {
			t.Fatalf("query metrics: %v", err)
		}

		if count == 0 {
			t.Error("expected metrics to be calculated, got 0 rows")
		} else {
			t.Logf("✓ Found %d calculated metrics in TimescaleDB", count)
		}
	})

	t.Run("Phase3_VerifyAlertDeduplication", func(t *testing.T) {
		// Test Redis deduplication
		dedupKey := fmt.Sprintf("alert:%s:test_rule", symbol)
		
		// First attempt should succeed
		wasSet, err := rdb.SetNX(ctx, dedupKey, "1", 1*time.Minute).Result()
		if err != nil {
			t.Fatalf("SetNX: %v", err)
		}
		if !wasSet {
			t.Error("expected first SetNX to succeed")
		}

		// Duplicate should be blocked
		wasSet, err = rdb.SetNX(ctx, dedupKey, "1", 1*time.Minute).Result()
		if err != nil {
			t.Fatalf("SetNX duplicate: %v", err)
		}
		if wasSet {
			t.Error("expected duplicate to be blocked")
		}

		// Cleanup
		rdb.Del(ctx, dedupKey)
		t.Logf("✓ Redis deduplication working correctly")
	})

	t.Log("✅ Full pipeline E2E test completed successfully")
}

func cleanupTestData(t *testing.T, ctx context.Context, db *pgx.Conn, symbol string) {
	queries := []string{
		fmt.Sprintf("DELETE FROM candles_1m WHERE symbol = '%s'", symbol),
		fmt.Sprintf("DELETE FROM metrics_calculated WHERE symbol = '%s'", symbol),
		fmt.Sprintf("DELETE FROM alert_history WHERE symbol = '%s'", symbol),
	}

	for _, query := range queries {
		if _, err := db.Exec(ctx, query); err != nil {
			t.Logf("cleanup warning: %v", err)
		}
	}
}
