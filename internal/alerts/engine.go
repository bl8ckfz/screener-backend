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
func (e *Engine) evaluateBigBull60(c *AlertCriteria, m *Metrics) bool {
	if c.Change1hMin != nil && m.PriceChange1h < *c.Change1hMin {
		return false
	}
	if c.Change1dMax != nil && m.PriceChange1d > *c.Change1dMax {
		return false
	}
	if c.Change8hGt1h && m.PriceChange8h <= m.PriceChange1h {
		return false
	}
	if c.Change1dGt8h && m.PriceChange1d <= m.PriceChange8h {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	if c.Volume8hMin != nil && m.Candle8h.Volume < *c.Volume8hMin {
		return false
	}
	if c.VolumeRatio1h8h != nil && m.VolumeRatio1h < *c.VolumeRatio1h8h {
		return false
	}
	if c.VolumeRatio1h1d != nil && m.VolumeRatio1h < *c.VolumeRatio1h1d {
		return false
	}
	return true
}

// evaluateBigBear60: Sustained downward momentum
func (e *Engine) evaluateBigBear60(c *AlertCriteria, m *Metrics) bool {
	if c.Change1hMax != nil && m.PriceChange1h > *c.Change1hMax {
		return false
	}
	if c.Change1dMin != nil && m.PriceChange1d < *c.Change1dMin {
		return false
	}
	if c.Change8hLt1h && m.PriceChange8h >= m.PriceChange1h {
		return false
	}
	if c.Change1dLt8h && m.PriceChange1d >= m.PriceChange8h {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	if c.Volume8hMin != nil && m.Candle8h.Volume < *c.Volume8hMin {
		return false
	}
	if c.VolumeRatio1h8h != nil && m.VolumeRatio1h < *c.VolumeRatio1h8h {
		return false
	}
	if c.VolumeRatio1h1d != nil && m.VolumeRatio1h < *c.VolumeRatio1h1d {
		return false
	}
	return true
}

// evaluatePioneerBull: Early bullish momentum detection
func (e *Engine) evaluatePioneerBull(c *AlertCriteria, m *Metrics) bool {
	if c.Change5mMin != nil && m.PriceChange5m < *c.Change5mMin {
		return false
	}
	if c.Change15mMin != nil && m.PriceChange15m < *c.Change15mMin {
		return false
	}
	if c.ChangeAcceleration != nil && m.PriceChange15m > 0 && 
		(*c.ChangeAcceleration)*m.PriceChange5m < m.PriceChange15m {
		return false
	}
	if c.VolumeRatio5m15m != nil && m.Candle15m.Volume > 0 && 
		(*c.VolumeRatio5m15m)*m.Candle5m.Volume < m.Candle15m.Volume {
		return false
	}
	return true
}

// evaluatePioneerBear: Early bearish momentum detection
func (e *Engine) evaluatePioneerBear(c *AlertCriteria, m *Metrics) bool {
	if c.Change5mMax != nil && m.PriceChange5m > *c.Change5mMax {
		return false
	}
	if c.Change15mMax != nil && m.PriceChange15m > *c.Change15mMax {
		return false
	}
	if c.ChangeAcceleration != nil && m.PriceChange15m < 0 && 
		(*c.ChangeAcceleration)*m.PriceChange5m > m.PriceChange15m {
		return false
	}
	if c.VolumeRatio5m15m != nil && m.Candle15m.Volume > 0 && 
		(*c.VolumeRatio5m15m)*m.Candle5m.Volume < m.Candle15m.Volume {
		return false
	}
	return true
}

// evaluate5BigBull: Explosive bullish moves starting from 5m
func (e *Engine) evaluate5BigBull(c *AlertCriteria, m *Metrics) bool {
	if c.Change5mMin != nil && m.PriceChange5m < *c.Change5mMin {
		return false
	}
	if c.Change1dMax != nil && m.PriceChange1d > *c.Change1dMax {
		return false
	}
	if c.Change15mGt5m && m.PriceChange15m <= m.PriceChange5m {
		return false
	}
	if c.Change1hGt15m && m.PriceChange1h <= m.PriceChange15m {
		return false
	}
	if c.Volume5mMin != nil && m.Candle5m.Volume < *c.Volume5mMin {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	return e.checkVolumeRatios5m(c, m)
}

// evaluate5BigBear: Explosive bearish moves starting from 5m
func (e *Engine) evaluate5BigBear(c *AlertCriteria, m *Metrics) bool {
	if c.Change5mMax != nil && m.PriceChange5m > *c.Change5mMax {
		return false
	}
	if c.Change1dMin != nil && m.PriceChange1d < *c.Change1dMin {
		return false
	}
	if c.Change15mLt5m && m.PriceChange15m >= m.PriceChange5m {
		return false
	}
	if c.Change1hLt15m && m.PriceChange1h >= m.PriceChange15m {
		return false
	}
	if c.Volume5mMin != nil && m.Candle5m.Volume < *c.Volume5mMin {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	return e.checkVolumeRatios5m(c, m)
}

// evaluate15BigBull: Strong bullish trending from 15m
func (e *Engine) evaluate15BigBull(c *AlertCriteria, m *Metrics) bool {
	if c.Change15mMin != nil && m.PriceChange15m < *c.Change15mMin {
		return false
	}
	if c.Change1dMax != nil && m.PriceChange1d > *c.Change1dMax {
		return false
	}
	if c.Change1hGt15m && m.PriceChange1h <= m.PriceChange15m {
		return false
	}
	if c.Change8hGt1hAlt && m.PriceChange8h <= m.PriceChange1h {
		return false
	}
	if c.Volume15mMin != nil && m.Candle15m.Volume < *c.Volume15mMin {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	return e.checkVolumeRatios15m(c, m)
}

// evaluate15BigBear: Strong bearish trending from 15m
func (e *Engine) evaluate15BigBear(c *AlertCriteria, m *Metrics) bool {
	if c.Change15mMax != nil && m.PriceChange15m > *c.Change15mMax {
		return false
	}
	if c.Change1dMin != nil && m.PriceChange1d < *c.Change1dMin {
		return false
	}
	if c.Change1hLt15mAlt && m.PriceChange1h >= m.PriceChange15m {
		return false
	}
	if c.Change8hLt1h && m.PriceChange8h >= m.PriceChange1h {
		return false
	}
	if c.Volume15mMin != nil && m.Candle15m.Volume < *c.Volume15mMin {
		return false
	}
	if c.Volume1hMin != nil && m.Candle1h.Volume < *c.Volume1hMin {
		return false
	}
	return e.checkVolumeRatios15m(c, m)
}

// evaluateBottomHunter: Detect reversal from bottom
func (e *Engine) evaluateBottomHunter(c *AlertCriteria, m *Metrics) bool {
	if c.Change1hMax != nil && m.PriceChange1h > *c.Change1hMax {
		return false
	}
	if c.Change15mMax != nil && m.PriceChange15m > *c.Change15mMax {
		return false
	}
	if c.Volume5mMin != nil && m.Candle5m.Volume < *c.Volume5mMin {
		return false
	}
	if c.VolumeRatio5m15m != nil && m.Candle15m.Volume > 0 && 
		(*c.VolumeRatio5m15m)*m.Candle5m.Volume < m.Candle15m.Volume {
		return false
	}
	if c.VolumeRatio5m1h != nil && m.Candle1h.Volume > 0 && 
		(*c.VolumeRatio5m1h)*m.Candle5m.Volume < m.Candle1h.Volume {
		return false
	}
	return true
}

// evaluateTopHunter: Detect reversal from top
func (e *Engine) evaluateTopHunter(c *AlertCriteria, m *Metrics) bool {
	if c.Change1hMin != nil && m.PriceChange1h < *c.Change1hMin {
		return false
	}
	if c.Change15mMin != nil && m.PriceChange15m < *c.Change15mMin {
		return false
	}
	if c.Change5mMax != nil && m.PriceChange5m > *c.Change5mMax {
		return false
	}
	if c.VolumeRatio5m15m != nil && m.Candle15m.Volume > 0 && 
		(*c.VolumeRatio5m15m)*m.Candle5m.Volume < m.Candle15m.Volume {
		return false
	}
	if c.VolumeRatio5m1h != nil && m.Candle1h.Volume > 0 && 
		(*c.VolumeRatio5m1h)*m.Candle5m.Volume < m.Candle1h.Volume {
		return false
	}
	return true
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
func (e *Engine) isDuplicate(ctx context.Context, symbol, ruleType string, ts time.Time) bool {
	key := e.dedupKey(symbol, ruleType, ts)
	exists, err := e.redis.Exists(ctx, key).Result()
	if err != nil {
		e.logger.Error().Err(err).Msg("redis exists check failed")
		return false
	}
	return exists > 0
}

// setDeduplicationKey sets the deduplication key for a specific candle/window.
// TTL slightly exceeds the source timeframe to filter only exact-duplicate publishes.
func (e *Engine) setDeduplicationKey(ctx context.Context, symbol, ruleType string, ts time.Time) {
	key := e.dedupKey(symbol, ruleType, ts)
	if err := e.redis.Set(ctx, key, "1", 2*time.Minute).Err(); err != nil {
		e.logger.Error().Err(err).Msg("failed to set deduplication key")
	}
}

// dedupKey builds a per-candle deduplication key so each closed window can re-trigger naturally.
func (e *Engine) dedupKey(symbol, ruleType string, ts time.Time) string {
	return fmt.Sprintf("alert:%s:%s:%d", symbol, ruleType, ts.Unix())
}
