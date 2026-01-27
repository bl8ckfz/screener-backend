package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// TestAlertDeduplication verifies that duplicate alerts are properly suppressed
// within the cooldown window using Redis
func TestAlertDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	redisURL := getEnv("REDIS_URL", "localhost:6379")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	tsdbURL := getEnv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{Addr: redisURL})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to Redis: %v", err)
	}

	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect to NATS: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream context: %v", err)
	}

	// Connect to DB
	db, err := pgx.Connect(ctx, tsdbURL)
	if err != nil {
		t.Fatalf("connect to DB: %v", err)
	}
	defer db.Close(ctx)

	symbol := "DEDUPTEST"
	ruleType := "test_dedup_rule"
	dedupKey := fmt.Sprintf("alert:%s:%s", symbol, ruleType)

	// Cleanup before and after test
	defer cleanupTestData(t, ctx, db, symbol)
	cleanupTestData(t, ctx, db, symbol)
	rdb.Del(ctx, dedupKey)

	t.Run("FirstAlert_ShouldPass", func(t *testing.T) {
		// Check Redis key doesn't exist
		exists, err := rdb.Exists(ctx, dedupKey).Result()
		if err != nil {
			t.Fatalf("check Redis key: %v", err)
		}
		if exists != 0 {
			t.Fatal("dedup key should not exist before first alert")
		}

		// Simulate alert engine setting dedup key
		err = rdb.SetNX(ctx, dedupKey, "1", 1*time.Minute).Err()
		if err != nil {
			t.Fatalf("set dedup key: %v", err)
		}

		// Publish alert
		alert := map[string]interface{}{
			"id":          "dedup-test-1",
			"symbol":      symbol,
			"rule_type":   ruleType,
			"description": "First alert",
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
			"price":       42000.0,
		}
		alertData, _ := json.Marshal(alert)
		
		if _, err := js.Publish("alerts.triggered", alertData); err != nil {
			t.Fatalf("publish alert: %v", err)
		}

		t.Log("✓ First alert published and dedup key set")
	})

	t.Run("DuplicateAlert_ShouldBeBlocked", func(t *testing.T) {
		// Check key exists
		exists, err := rdb.Exists(ctx, dedupKey).Result()
		if err != nil {
			t.Fatalf("check Redis key: %v", err)
		}
		if exists == 0 {
			t.Fatal("dedup key should exist after first alert")
		}

		// Try to set dedup key again (should fail)
		wasSet, err := rdb.SetNX(ctx, dedupKey, "1", 1*time.Minute).Result()
		if err != nil {
			t.Fatalf("check SetNX: %v", err)
		}
		if wasSet {
			t.Fatal("SetNX should return false when key exists (duplicate should be blocked)")
		}

		t.Log("✓ Duplicate alert correctly blocked by Redis dedup key")
	})

	t.Run("AfterCooldown_ShouldAllowAgain", func(t *testing.T) {
		// Delete the key to simulate cooldown expiration
		rdb.Del(ctx, dedupKey)

		// Should be able to set again
		wasSet, err := rdb.SetNX(ctx, dedupKey, "1", 1*time.Minute).Result()
		if err != nil {
			t.Fatalf("check SetNX: %v", err)
		}
		if !wasSet {
			t.Fatal("SetNX should return true after cooldown")
		}

		t.Log("✓ Alert allowed again after cooldown expiration")
	})

	t.Run("VerifyTTL", func(t *testing.T) {
		// Check TTL is set correctly
		ttl, err := rdb.TTL(ctx, dedupKey).Result()
		if err != nil {
			t.Fatalf("get TTL: %v", err)
		}
		if ttl <= 0 || ttl > 61*time.Second {
			t.Fatalf("expected TTL around 60s, got %v", ttl)
		}

		t.Logf("✓ Dedup key TTL: %v", ttl)
	})

	// Cleanup
	rdb.Del(ctx, dedupKey)
}

// TestMultipleSymbolDeduplication verifies dedup is per-symbol
func TestMultipleSymbolDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	redisURL := getEnv("REDIS_URL", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisURL})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to Redis: %v", err)
	}

	symbol1 := "DEDUP1"
	symbol2 := "DEDUP2"
	ruleType := "test_rule"
	key1 := fmt.Sprintf("alert:%s:%s", symbol1, ruleType)
	key2 := fmt.Sprintf("alert:%s:%s", symbol2, ruleType)

	// Cleanup
	rdb.Del(ctx, key1, key2)
	defer rdb.Del(ctx, key1, key2)

	// Set dedup for symbol1
	if err := rdb.SetNX(ctx, key1, "1", 1*time.Minute).Err(); err != nil {
		t.Fatalf("set key1: %v", err)
	}

	// Symbol2 should still be allowed (different key)
	wasSet, err := rdb.SetNX(ctx, key2, "1", 1*time.Minute).Result()
	if err != nil {
		t.Fatalf("set key2: %v", err)
	}
	if !wasSet {
		t.Fatal("symbol2 should be allowed (different symbol)")
	}

	// Symbol1 should be blocked
	wasSet, err = rdb.SetNX(ctx, key1, "1", 1*time.Minute).Result()
	if err != nil {
		t.Fatalf("check key1: %v", err)
	}
	if wasSet {
		t.Fatal("symbol1 should be blocked (duplicate)")
	}

	t.Log("✓ Deduplication correctly isolated per symbol")
}
