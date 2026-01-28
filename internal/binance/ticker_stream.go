package binance

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const tickerStreamURL = "wss://fstream.binance.com/ws/!ticker@arr"

// TickerStreamEvent represents a 24hr ticker event from Binance WS
// Docs: https://binance-docs.github.io/apidocs/futures/en/#individual-symbol-ticker-streams
// Stream returns an array of these events.
type TickerStreamEvent struct {
	EventType          string `json:"e"`
	EventTime          int64  `json:"E"`
	Symbol             string `json:"s"`
	PriceChange        string `json:"p"`
	PriceChangePercent string `json:"P"`
	WeightedAvgPrice   string `json:"w"`
	LastPrice          string `json:"c"`
	LastQty            string `json:"Q"`
	OpenPrice          string `json:"o"`
	HighPrice          string `json:"h"`
	LowPrice           string `json:"l"`
	Volume             string `json:"v"`
	QuoteVolume        string `json:"q"`
	OpenTime           int64  `json:"O"`
	CloseTime          int64  `json:"C"`
	FirstID            int64  `json:"F"`
	LastID             int64  `json:"L"`
	Count              int64  `json:"n"`
}

// StartTickerStream connects to Binance 24hr ticker stream and writes latest tickers to Redis.
func StartTickerStream(ctx context.Context, rdb *redis.Client, logger zerolog.Logger) {
	if rdb == nil {
		logger.Warn().Msg("Redis not configured; ticker stream disabled")
		return
	}

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(tickerStreamURL, nil)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to connect to ticker stream")
			sleepWithContext(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		logger.Info().Msg("Connected to Binance ticker stream")
		backoff = time.Second

		if err := readTickerLoop(ctx, conn, rdb, logger); err != nil {
			logger.Error().Err(err).Msg("Ticker stream read error")
		}

		_ = conn.Close()
	}
}

func readTickerLoop(ctx context.Context, conn *websocket.Conn, rdb *redis.Client, logger zerolog.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				return err
			}

			var events []TickerStreamEvent
			if err := json.Unmarshal(message, &events); err != nil {
				logger.Error().Err(err).Msg("Failed to decode ticker stream payload")
				continue
			}

			pipe := rdb.Pipeline()
			for _, t := range events {
				payload := map[string]interface{}{
					"symbol":             t.Symbol,
					"priceChange":        t.PriceChange,
					"priceChangePercent": t.PriceChangePercent,
					"weightedAvgPrice":   t.WeightedAvgPrice,
					"lastPrice":          t.LastPrice,
					"lastQty":            t.LastQty,
					"openPrice":          t.OpenPrice,
					"highPrice":          t.HighPrice,
					"lowPrice":           t.LowPrice,
					"volume":             t.Volume,
					"quoteVolume":        t.QuoteVolume,
					"openTime":           t.OpenTime,
					"closeTime":          t.CloseTime,
					"count":              t.Count,
				}

				buf, err := json.Marshal(payload)
				if err != nil {
					continue
				}
				pipe.HSet(ctx, "tickers", t.Symbol, string(buf))
			}
			pipe.Expire(ctx, "tickers", 2*time.Minute)
			_, _ = pipe.Exec(ctx)
		}
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * 2)
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return time.Duration(math.Max(float64(next), float64(time.Second)))
}
