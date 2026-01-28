package calculator

import (
	"context"
	"fmt"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/ringbuffer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// MetricsPersister handles batch writing of metrics to TimescaleDB
type MetricsPersister struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
	queue  chan *SymbolMetrics
	ctx    context.Context
	cancel context.CancelFunc
}

// NewMetricsPersister creates a new metrics persister with batch writing
func NewMetricsPersister(pool *pgxpool.Pool, logger zerolog.Logger, batchSize int) *MetricsPersister {
	ctx, cancel := context.WithCancel(context.Background())

	mp := &MetricsPersister{
		pool:   pool,
		logger: logger.With().Str("component", "metrics-persister").Logger(),
		queue:  make(chan *SymbolMetrics, batchSize*2), // Buffer 2x batch size
		ctx:    ctx,
		cancel: cancel,
	}

	// Start batch writer goroutine
	go mp.batchWriter(batchSize)

	return mp
}

// Enqueue adds metrics to the write queue (non-blocking)
func (mp *MetricsPersister) Enqueue(metrics *SymbolMetrics) {
	select {
	case mp.queue <- metrics:
	default:
		mp.logger.Warn().Str("symbol", metrics.Symbol).Msg("metrics queue full, dropping")
	}
}

// batchWriter processes metrics in batches
func (mp *MetricsPersister) batchWriter(batchSize int) {
	ticker := time.NewTicker(5 * time.Second) // Write every 5 seconds
	defer ticker.Stop()

	batch := make([]*SymbolMetrics, 0, batchSize)

	for {
		select {
		case <-mp.ctx.Done():
			// Flush remaining metrics
			if len(batch) > 0 {
				mp.writeBatch(batch)
			}
			return

		case metrics := <-mp.queue:
			batch = append(batch, metrics)
			if len(batch) >= batchSize {
				mp.writeBatch(batch)
				batch = batch[:0] // Clear batch
			}

		case <-ticker.C:
			if len(batch) > 0 {
				mp.writeBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// writeBatch writes a batch of metrics to TimescaleDB
func (mp *MetricsPersister) writeBatch(batch []*SymbolMetrics) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use COPY for efficient bulk insert
	// We'll insert one row per timeframe (5m, 15m, 1h, 4h, 8h, 1d)
	query := `
		INSERT INTO metrics_calculated (
			time, symbol, timeframe,
			open, high, low, close, volume,
			price_change, volume_ratio,
			vcp, rsi_14, macd, macd_signal,
			fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (time, symbol, timeframe) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			price_change = EXCLUDED.price_change,
			volume_ratio = EXCLUDED.volume_ratio,
			vcp = EXCLUDED.vcp,
			rsi_14 = EXCLUDED.rsi_14,
			macd = EXCLUDED.macd,
			macd_signal = EXCLUDED.macd_signal,
			fib_r3 = EXCLUDED.fib_r3,
			fib_r2 = EXCLUDED.fib_r2,
			fib_r1 = EXCLUDED.fib_r1,
			fib_pivot = EXCLUDED.fib_pivot,
			fib_s1 = EXCLUDED.fib_s1,
			fib_s2 = EXCLUDED.fib_s2,
			fib_s3 = EXCLUDED.fib_s3
	`

	// Begin transaction
	tx, err := mp.pool.Begin(ctx)
	if err != nil {
		mp.logger.Error().Err(err).Msg("failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	inserted := 0

	for _, metrics := range batch {
		// Insert each timeframe as a separate row
		timeframes := []struct {
			name   string
			candle TimeframeCandle
		}{
			{"5m", metrics.Candle5m},
			{"15m", metrics.Candle15m},
			{"1h", metrics.Candle1h},
			{"4h", metrics.Candle4h},
			{"8h", metrics.Candle8h},
			{"1d", metrics.Candle1d},
		}

		for _, tf := range timeframes {
			if tf.candle.Close == 0 {
				continue // Skip empty candles
			}

			// Get price_change and volume_ratio for this timeframe
			var priceChange, volumeRatio float64
			switch tf.name {
			case "5m":
				priceChange = metrics.PriceChange5m
				volumeRatio = metrics.VolumeRatio5m
			case "15m":
				priceChange = metrics.PriceChange15m
				volumeRatio = metrics.VolumeRatio15m
			case "1h":
				priceChange = metrics.PriceChange1h
				volumeRatio = metrics.VolumeRatio1h
			case "4h":
				priceChange = metrics.PriceChange4h
				volumeRatio = metrics.VolumeRatio4h
			case "8h":
				priceChange = metrics.PriceChange8h
				volumeRatio = metrics.VolumeRatio8h
			case "1d":
				priceChange = metrics.PriceChange1d
			}

			_, err := tx.Exec(ctx, query,
				metrics.Timestamp,
				metrics.Symbol,
				tf.name,
				tf.candle.Open,
				tf.candle.High,
				tf.candle.Low,
				tf.candle.Close,
				tf.candle.Volume,
				priceChange,
				volumeRatio,
				metrics.VCP,
				metrics.RSI,
				metrics.MACD.MACD,
				metrics.MACD.Signal,
				metrics.Fibonacci.Resistance1,
				metrics.Fibonacci.Resistance618,
				metrics.Fibonacci.Resistance382,
				metrics.Fibonacci.Pivot,
				metrics.Fibonacci.Support382,
				metrics.Fibonacci.Support618,
				metrics.Fibonacci.Support1,
			)

			if err != nil {
				mp.logger.Error().
					Err(err).
					Str("symbol", metrics.Symbol).
					Str("timeframe", tf.name).
					Msg("failed to insert metrics")
				continue
			}

			inserted++
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		mp.logger.Error().Err(err).Msg("failed to commit transaction")
		return
	}

	mp.logger.Info().
		Int("batch_size", len(batch)).
		Int("rows_inserted", inserted).
		Msg("persisted metrics batch")
}

// Close stops the persister and flushes remaining metrics
func (mp *MetricsPersister) Close() error {
	mp.cancel()
	close(mp.queue)

	// Give time for final batch to write
	time.Sleep(1 * time.Second)

	return nil
}

// PersistCandle writes a single 1m candle to TimescaleDB
// This is called synchronously for each candle to ensure we have historical data
func (mp *MetricsPersister) PersistCandle(ctx context.Context, candle ringbuffer.Candle) error {
	query := `
		INSERT INTO candles_1m (
			time, symbol,
			open, high, low, close,
			volume, quote_volume,
			number_of_trades
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (time, symbol) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			quote_volume = EXCLUDED.quote_volume,
			number_of_trades = EXCLUDED.number_of_trades
	`

	_, err := mp.pool.Exec(ctx, query,
		candle.OpenTime,
		candle.Symbol,
		candle.Open,
		candle.High,
		candle.Low,
		candle.Close,
		candle.Volume,
		candle.QuoteVolume,
		candle.NumberOfTrades,
	)

	if err != nil {
		return fmt.Errorf("insert candle: %w", err)
	}

	return nil
}

// PersistMetrics is a helper function for single metric persistence (mainly for testing)
func PersistMetrics(ctx context.Context, pool *pgxpool.Pool, metrics *SymbolMetrics) error {
	query := `
		INSERT INTO metrics_calculated (
			time, symbol, timeframe,
			open, high, low, close, volume,
			price_change, volume_ratio,
			vcp, rsi_14, macd, macd_signal,
			fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (time, symbol, timeframe) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			price_change = EXCLUDED.price_change,
			volume_ratio = EXCLUDED.volume_ratio,
			vcp = EXCLUDED.vcp,
			rsi_14 = EXCLUDED.rsi_14,
			macd = EXCLUDED.macd,
			macd_signal = EXCLUDED.macd_signal
	`

	timeframes := []struct {
		name   string
		candle TimeframeCandle
	}{
		{"5m", metrics.Candle5m},
		{"15m", metrics.Candle15m},
		{"1h", metrics.Candle1h},
		{"4h", metrics.Candle4h},
		{"8h", metrics.Candle8h},
		{"1d", metrics.Candle1d},
	}

	for _, tf := range timeframes {
		if tf.candle.Close == 0 {
			continue
		}

		// Get price_change and volume_ratio for this timeframe
		var priceChange, volumeRatio float64
		switch tf.name {
		case "5m":
			priceChange = metrics.PriceChange5m
			volumeRatio = metrics.VolumeRatio5m
		case "15m":
			priceChange = metrics.PriceChange15m
			volumeRatio = metrics.VolumeRatio15m
		case "1h":
			priceChange = metrics.PriceChange1h
			volumeRatio = metrics.VolumeRatio1h
		case "4h":
			priceChange = metrics.PriceChange4h
			volumeRatio = metrics.VolumeRatio4h
		case "8h":
			priceChange = metrics.PriceChange8h
			volumeRatio = metrics.VolumeRatio8h
		case "1d":
			priceChange = metrics.PriceChange1d
		}

		_, err := pool.Exec(ctx, query,
			metrics.Timestamp,
			metrics.Symbol,
			tf.name,
			tf.candle.Open,
			tf.candle.High,
			tf.candle.Low,
			tf.candle.Close,
			tf.candle.Volume,
			priceChange,
			volumeRatio,
			metrics.VCP,
			metrics.RSI,
			metrics.MACD.MACD,
			metrics.MACD.Signal,
			metrics.Fibonacci.Resistance1,
			metrics.Fibonacci.Resistance618,
			metrics.Fibonacci.Resistance382,
			metrics.Fibonacci.Pivot,
			metrics.Fibonacci.Support382,
			metrics.Fibonacci.Support618,
			metrics.Fibonacci.Support1,
		)

		if err != nil {
			return fmt.Errorf("insert timeframe %s: %w", tf.name, err)
		}
	}

	return nil
}
