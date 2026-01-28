package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer pool.Close()

	log.Println("Connected to database, running migrations...")

	migrations := []string{
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS price_change DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS volume_ratio DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS bb_upper DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS bb_middle DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS bb_lower DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_r3 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_r2 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_r1 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_pivot DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_s1 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_s2 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS fib_s3 DOUBLE PRECISION",
		"ALTER TABLE metrics_calculated ADD COLUMN IF NOT EXISTS rsi_14 DOUBLE PRECISION",
	}

	for _, migration := range migrations {
		log.Printf("Running: %s", migration)
		_, err := pool.Exec(ctx, migration)
		if err != nil {
			log.Printf("WARNING: Migration failed: %v", err)
		} else {
			log.Println("✓ Success")
		}
	}

	log.Println("All migrations completed")
}
