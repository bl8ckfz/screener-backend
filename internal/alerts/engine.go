package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Engine evaluates metrics against alert rules
type Engine struct {
	db     *pgxpool.Pool
	redis  *redis.Client
	rules  map[string]*AlertRule
	logger zerolog.Logger
}

// NewEngine creates a new alert engine
func NewEngine(db *pgxpool.Pool, redis *redis.Client, logger zerolog.Logger) *Engine {
	return &Engine{
		db:     db,
		redis:  redis,
		rules:  make(map[string]*AlertRule),
		logger: logger.With().Str("component", "alert-engine").Logger(),
	}
}

// LoadRules loads alert rules from PostgreSQL
func (e *Engine) LoadRules(ctx context.Context) error {
	query := `SELECT rule_type, config, description FROM alert_rules`

	rows, err := e.db.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var rule AlertRule
		var configJSON []byte

		if err := rows.Scan(&rule.RuleType, &configJSON, &rule.Description); err != nil {
			return fmt.Errorf("scan rule: %w", err)
		}

		if err := json.Unmarshal(configJSON, &rule.Config); err != nil {
			return fmt.Errorf("unmarshal config: %w", err)
		}

		e.rules[rule.RuleType] = &rule
		count++
	}

	e.logger.Info().Int("count", count).Msg("loaded alert rules")
	return nil
}

// Evaluate checks if metrics trigger any alert rules
func (e *Engine) Evaluate(ctx context.Context, metrics *Metrics) ([]*Alert, error) {
	var alerts []*Alert

	for ruleType, rule := range e.rules {
		// Check deduplication first
		if e.isDuplicate(ctx, metrics.Symbol, ruleType, metrics.Timestamp) {
			continue
		}

		// Extract criteria from config
		criteria, err := e.extractCriteria(rule.Config)
		if err != nil {
			e.logger.Error().Err(err).Str("rule", ruleType).Msg("failed to extract criteria")
			continue
		}

		// Evaluate rule
		if e.evaluateRule(ruleType, criteria, metrics) {
			alert := &Alert{
				ID:          uuid.New().String(),
				Symbol:      metrics.Symbol,
				RuleType:    ruleType,
				Description: rule.Description,
				Timestamp:   metrics.Timestamp,
				Price:       metrics.LastPrice,
				Metadata: map[string]interface{}{
					"vcp":              metrics.VCP,
					"price_change_5m":  metrics.PriceChange5m,
					"price_change_15m": metrics.PriceChange15m,
					"price_change_1h":  metrics.PriceChange1h,
					"price_change_8h":  metrics.PriceChange8h,
					"price_change_1d":  metrics.PriceChange1d,
					"volume_5m":        metrics.Candle5m.Volume,
					"volume_15m":       metrics.Candle15m.Volume,
					"volume_1h":        metrics.Candle1h.Volume,
					"volume_8h":        metrics.Candle8h.Volume,
					"volume_ratio_5m":  metrics.VolumeRatio5m,
					"volume_ratio_15m": metrics.VolumeRatio15m,
					"volume_ratio_1h":  metrics.VolumeRatio1h,
					"volume_ratio_8h":  metrics.VolumeRatio8h,
				},
			}

			alerts = append(alerts, alert)

			// Set deduplication key scoped to this candle/window
			e.setDeduplicationKey(ctx, metrics.Symbol, ruleType, metrics.Timestamp)

			e.logger.Info().
				Str("symbol", metrics.Symbol).
				Str("rule", ruleType).
				Float64("price", metrics.LastPrice).
				Msg("alert triggered")
		}
	}

	return alerts, nil
}

// evaluateRule evaluates a specific rule against metrics
func (e *Engine) evaluateRule(ruleType string, criteria *AlertCriteria, metrics *Metrics) bool {
	// Market cap filter (if we had market cap data, we'd check it here)
	// For now, assuming all symbols pass market cap filter

	switch ruleType {
	case "futures_big_bull_60":
		return e.evaluateBigBull60(criteria, metrics)
	case "futures_big_bear_60":
		return e.evaluateBigBear60(criteria, metrics)
	case "futures_pioneer_bull":
		return e.evaluatePioneerBull(criteria, metrics)
	case "futures_pioneer_bear":
		return e.evaluatePioneerBear(criteria, metrics)
	case "futures_5_big_bull":
		return e.evaluate5BigBull(criteria, metrics)
	case "futures_5_big_bear":
		return e.evaluate5BigBear(criteria, metrics)
	case "futures_15_big_bull":
		return e.evaluate15BigBull(criteria, metrics)
	case "futures_15_big_bear":
		return e.evaluate15BigBear(criteria, metrics)
	case "futures_bottom_hunter":
		return e.evaluateBottomHunter(criteria, metrics)
	case "futures_top_hunter":
		return e.evaluateTopHunter(criteria, metrics)
	default:
		return false
	}
}

// evaluateBigBull60: Sustained momentum over multiple timeframes
// Frontend logic: change_1h > 1.6 && change_1d < 15 && change_8h > change_1h &&
// change_1d > change_8h && volume_1h > 500_000 && volume_8h > 5_000_000 &&
// 6 * volume_1h > volume_8h && 16 * volume_1h > volume_1d
func (e *Engine) evaluateBigBull60(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange1h > 1.6 &&
		m.PriceChange1d < 15 &&
		m.PriceChange8h > m.PriceChange1h &&
		m.PriceChange1d > m.PriceChange8h &&
		m.Candle1h.Volume > 500_000 &&
		m.Candle8h.Volume > 5_000_000 &&
		6*m.Candle1h.Volume > m.Candle8h.Volume &&
		16*m.Candle1h.Volume > m.Candle1d.Volume
}

// evaluateBigBear60: Sustained downward momentum
// Frontend logic: change_1h < -1.6 && change_1d > -15 && change_8h < change_1h &&
// change_1d < change_8h && volume_1h > 500_000 && volume_8h > 5_000_000 &&
// 6 * volume_1h > volume_8h && 16 * volume_1h > volume_1d
func (e *Engine) evaluateBigBear60(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange1h < -1.6 &&
		m.PriceChange1d > -15 &&
		m.PriceChange8h < m.PriceChange1h &&
		m.PriceChange1d < m.PriceChange8h &&
		m.Candle1h.Volume > 500_000 &&
		m.Candle8h.Volume > 5_000_000 &&
		6*m.Candle1h.Volume > m.Candle8h.Volume &&
		16*m.Candle1h.Volume > m.Candle1d.Volume
}

// evaluatePioneerBull: Early bullish momentum detection
// Frontend logic: change_5m > 1 && change_15m > 1 && 3 * change_5m > change_15m && 2 * volume_5m > volume_15m
func (e *Engine) evaluatePioneerBull(c *AlertCriteria, m *Metrics) bool {
	result := m.PriceChange5m > 1 &&
		m.PriceChange15m > 1 &&
		3*m.PriceChange5m > m.PriceChange15m &&
		2*m.Candle5m.Volume > m.Candle15m.Volume
	
	// DEBUG: Log evaluation for all symbols that are close to triggering
	if (m.PriceChange5m > 0.8 || m.PriceChange15m > 0.8) && result == false {
		e.logger.Debug().
			Str("symbol", m.Symbol).
			Float64("change_5m", m.PriceChange5m).
			Float64("change_15m", m.PriceChange15m).
			Float64("volume_5m", m.Candle5m.Volume).
			Float64("volume_15m", m.Candle15m.Volume).
			Bool("cond1_change_5m>1", m.PriceChange5m > 1).
			Bool("cond2_change_15m>1", m.PriceChange15m > 1).
			Bool("cond3_3x5m>15m", 3*m.PriceChange5m > m.PriceChange15m).
			Bool("cond4_2xvol5m>vol15m", 2*m.Candle5m.Volume > m.Candle15m.Volume).
			Msg("Pioneer Bull FAILED - close to triggering")
	} else if result {
		e.logger.Info().
			Str("symbol", m.Symbol).
			Float64("change_5m", m.PriceChange5m).
			Float64("change_15m", m.PriceChange15m).
			Msg("Pioneer Bull TRIGGERED")
	}
	
	return result
}

// evaluatePioneerBear: Early bearish momentum detection
// Frontend logic: change_5m < -1 && change_15m < -1 && 3 * change_5m < change_15m && 2 * volume_5m > volume_15m
func (e *Engine) evaluatePioneerBear(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange5m < -1 &&
		m.PriceChange15m < -1 &&
		3*m.PriceChange5m < m.PriceChange15m &&
		2*m.Candle5m.Volume > m.Candle15m.Volume
}

// evaluate5BigBull: Explosive bullish moves starting from 5m
// Frontend logic: change_5m > 0.6 && change_1d < 15 && change_15m > change_5m && change_1h > change_15m &&
// volume_5m > 100_000 && volume_1h > 1_000_000 && volume_5m > volume_15m / 3 &&
// volume_5m > volume_1h / 6 && volume_5m > volume_8h / 66
func (e *Engine) evaluate5BigBull(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange5m > 0.6 &&
		m.PriceChange1d < 15 &&
		m.PriceChange15m > m.PriceChange5m &&
		m.PriceChange1h > m.PriceChange15m &&
		m.Candle5m.Volume > 100_000 &&
		m.Candle1h.Volume > 1_000_000 &&
		m.Candle5m.Volume > m.Candle15m.Volume/3 &&
		m.Candle5m.Volume > m.Candle1h.Volume/6 &&
		m.Candle5m.Volume > m.Candle8h.Volume/66
}

// evaluate5BigBear: Explosive bearish moves starting from 5m
// Frontend logic: change_5m < -0.6 && change_1d > -15 && change_15m < change_5m && change_1h < change_15m &&
// volume_5m > 100_000 && volume_1h > 1_000_000 && volume_5m > volume_15m / 3 &&
// volume_5m > volume_1h / 6 && volume_5m > volume_8h / 66
func (e *Engine) evaluate5BigBear(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange5m < -0.6 &&
		m.PriceChange1d > -15 &&
		m.PriceChange15m < m.PriceChange5m &&
		m.PriceChange1h < m.PriceChange15m &&
		m.Candle5m.Volume > 100_000 &&
		m.Candle1h.Volume > 1_000_000 &&
		m.Candle5m.Volume > m.Candle15m.Volume/3 &&
		m.Candle5m.Volume > m.Candle1h.Volume/6 &&
		m.Candle5m.Volume > m.Candle8h.Volume/66
}

// evaluate15BigBull: Strong bullish trending from 15m
// Frontend logic: change_15m > 1 && change_1d < 15 && change_1h > change_15m && change_8h > change_1h &&
// volume_15m > 400_000 && volume_1h > 1_000_000 && volume_15m > volume_1h / 3 && volume_15m > volume_8h / 26
func (e *Engine) evaluate15BigBull(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange15m > 1 &&
		m.PriceChange1d < 15 &&
		m.PriceChange1h > m.PriceChange15m &&
		m.PriceChange8h > m.PriceChange1h &&
		m.Candle15m.Volume > 400_000 &&
		m.Candle1h.Volume > 1_000_000 &&
		m.Candle15m.Volume > m.Candle1h.Volume/3 &&
		m.Candle15m.Volume > m.Candle8h.Volume/26
}

// evaluate15BigBear: Strong bearish trending from 15m
// Frontend logic: change_15m < -1 && change_1d > -15 && change_1h < change_15m && change_8h < change_1h &&
// volume_15m > 400_000 && volume_1h > 1_000_000 && volume_15m > volume_1h / 3 && volume_15m > volume_8h / 26
func (e *Engine) evaluate15BigBear(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange15m < -1 &&
		m.PriceChange1d > -15 &&
		m.PriceChange1h < m.PriceChange15m &&
		m.PriceChange8h < m.PriceChange1h &&
		m.Candle15m.Volume > 400_000 &&
		m.Candle1h.Volume > 1_000_000 &&
		m.Candle15m.Volume > m.Candle1h.Volume/3 &&
		m.Candle15m.Volume > m.Candle8h.Volume/26
}

// evaluateBottomHunter: Detect reversal from bottom
// Frontend logic: change_1h < -0.7 && change_15m < -0.6 && change_5m > 0.5 &&
// volume_5m > volume_15m / 2 && volume_5m > volume_1h / 8
func (e *Engine) evaluateBottomHunter(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange1h < -0.7 &&
		m.PriceChange15m < -0.6 &&
		m.PriceChange5m > 0.5 &&
		m.Candle5m.Volume > m.Candle15m.Volume/2 &&
		m.Candle5m.Volume > m.Candle1h.Volume/8
}

// evaluateTopHunter: Detect reversal from top
// Frontend logic: change_1h > 0.7 && change_15m > 0.6 && change_5m < -0.5 &&
// volume_5m > volume_15m / 2 && volume_5m > volume_1h / 8
func (e *Engine) evaluateTopHunter(c *AlertCriteria, m *Metrics) bool {
	return m.PriceChange1h > 0.7 &&
		m.PriceChange15m > 0.6 &&
		m.PriceChange5m < -0.5 &&
		m.Candle5m.Volume > m.Candle15m.Volume/2 &&
		m.Candle5m.Volume > m.Candle1h.Volume/8
}

// Helper functions
func (e *Engine) checkVolumeRatios5m(c *AlertCriteria, m *Metrics) bool {
	// VolumeRatio5m is already calculated as current 5m volume / previous 5m volume
	// For 5m alerts, we just check if the ratio meets the threshold
	if c.VolumeRatio5m15m != nil && m.VolumeRatio5m < *c.VolumeRatio5m15m {
		return false
	}
	if c.VolumeRatio5m1h != nil && m.VolumeRatio5m < *c.VolumeRatio5m1h {
		return false
	}
	if c.VolumeRatio5m8h != nil && m.VolumeRatio5m < *c.VolumeRatio5m8h {
		return false
	}
	return true
}

func (e *Engine) checkVolumeRatios15m(c *AlertCriteria, m *Metrics) bool {
	// VolumeRatio15m is already calculated as current 15m volume / previous 15m volume
	if c.VolumeRatio15m1h != nil && m.VolumeRatio15m < *c.VolumeRatio15m1h {
		return false
	}
	if c.VolumeRatio15m8h != nil && m.VolumeRatio15m < *c.VolumeRatio15m8h {
		return false
	}
	return true
}

// extractCriteria extracts AlertCriteria from rule config
func (e *Engine) extractCriteria(config map[string]interface{}) (*AlertCriteria, error) {
	criteriaMap, ok := config["criteria"].(map[string]interface{})
	if !ok {
		return &AlertCriteria{}, nil
	}

	criteriaJSON, err := json.Marshal(criteriaMap)
	if err != nil {
		return nil, err
	}

	var criteria AlertCriteria
	if err := json.Unmarshal(criteriaJSON, &criteria); err != nil {
		return nil, err
	}

	return &criteria, nil
}

// isDuplicate checks if alert was recently triggered
// Always returns false to match TypeScript behavior (no deduplication)
func (e *Engine) isDuplicate(ctx context.Context, symbol, ruleType string, ts time.Time) bool {
	return false
}

// setDeduplicationKey sets the deduplication key for a specific candle/window.
// No TTL - each new minute's metrics will have a different timestamp, so no deduplication needed
// This matches TypeScript behavior where alerts fire every time conditions are met
func (e *Engine) setDeduplicationKey(ctx context.Context, symbol, ruleType string, ts time.Time) {
	// Deduplication disabled to match TypeScript version behavior
	// TypeScript fires alerts every evaluation cycle when conditions are met
	return
}

// dedupKey builds a per-candle deduplication key so each closed window can re-trigger naturally.
func (e *Engine) dedupKey(symbol, ruleType string, ts time.Time) string {
	return fmt.Sprintf("alert:%s:%s:%d", symbol, ruleType, ts.Unix())
}
