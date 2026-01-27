package calculator

import (
	"sync"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/indicators"
	"github.com/bl8ckfz/crypto-screener-backend/internal/ringbuffer"
	"github.com/rs/zerolog"
)

// TimeframeCandle represents an aggregated candle for a specific timeframe
type TimeframeCandle struct {
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// SymbolMetrics holds calculated metrics for a symbol with multi-timeframe data
type SymbolMetrics struct {
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	LastPrice float64   `json:"last_price"`

	// Aggregated candles for each timeframe (sliding window)
	Candle1m  TimeframeCandle `json:"candle_1m"`
	Candle5m  TimeframeCandle `json:"candle_5m"`
	Candle15m TimeframeCandle `json:"candle_15m"`
	Candle1h  TimeframeCandle `json:"candle_1h"`
	Candle4h  TimeframeCandle `json:"candle_4h"`
	Candle8h  TimeframeCandle `json:"candle_8h"`
	Candle1d  TimeframeCandle `json:"candle_1d"`

	// Price changes (calculated from aggregated candles)
	PriceChange5m  float64 `json:"price_change_5m"`
	PriceChange15m float64 `json:"price_change_15m"`
	PriceChange1h  float64 `json:"price_change_1h"`
	PriceChange4h  float64 `json:"price_change_4h"`
	PriceChange8h  float64 `json:"price_change_8h"`
	PriceChange1d  float64 `json:"price_change_1d"`

	// Volume ratios (current vs previous period)
	VolumeRatio5m  float64 `json:"volume_ratio_5m"`
	VolumeRatio15m float64 `json:"volume_ratio_15m"`
	VolumeRatio1h  float64 `json:"volume_ratio_1h"`
	VolumeRatio4h  float64 `json:"volume_ratio_4h"`
	VolumeRatio8h  float64 `json:"volume_ratio_8h"`

	// Technical indicators
	VCP       float64                    `json:"vcp"`
	Fibonacci indicators.FibonacciLevels `json:"fibonacci"`
	RSI       float64                    `json:"rsi"`
	MACD      indicators.MACD            `json:"macd"`
}

// MetricsCalculator manages ring buffers and calculates metrics for multiple symbols
type MetricsCalculator struct {
	buffers map[string]*ringbuffer.RingBuffer
	mu      sync.RWMutex
	logger  zerolog.Logger
}

// NewMetricsCalculator creates a new metrics calculator
func NewMetricsCalculator(logger zerolog.Logger) *MetricsCalculator {
	return &MetricsCalculator{
		buffers: make(map[string]*ringbuffer.RingBuffer),
		logger:  logger.With().Str("component", "metrics-calculator").Logger(),
	}
}

// AddCandle adds a candle to the ring buffer and calculates metrics
func (mc *MetricsCalculator) AddCandle(candle ringbuffer.Candle) (*SymbolMetrics, error) {
	mc.mu.Lock()

	// Get or create ring buffer for symbol
	buffer, exists := mc.buffers[candle.Symbol]
	if !exists {
		buffer = ringbuffer.NewRingBuffer()
		mc.buffers[candle.Symbol] = buffer
		mc.logger.Debug().Str("symbol", candle.Symbol).Msg("created new ring buffer")
	}

	// Add candle to buffer
	buffer.Append(candle)
	mc.mu.Unlock()

	// Calculate metrics if we have enough data
	return mc.CalculateMetrics(candle.Symbol)
}

// CalculateMetrics calculates all metrics for a symbol using sliding window aggregation
func (mc *MetricsCalculator) CalculateMetrics(symbol string) (*SymbolMetrics, error) {
	mc.mu.RLock()
	buffer, exists := mc.buffers[symbol]
	mc.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	// Get latest candle
	latest := buffer.GetLatest()
	if latest == nil {
		return nil, nil
	}

	// Use current time rounded to the minute as timestamp
	// Note: Candle timestamps from websocket are having JSON marshaling issues
	// For MVP, using current time is acceptable as metrics are calculated in real-time
	timestamp := time.Now().Truncate(time.Minute)

	metrics := &SymbolMetrics{
		Symbol:    symbol,
		Timestamp: timestamp,
		LastPrice: latest.Close,
	}

	// Aggregate candles for each timeframe (sliding window)
	metrics.Candle1m = mc.aggregateTimeframeCandle(buffer, 1)
	metrics.Candle5m = mc.aggregateTimeframeCandle(buffer, 5)
	metrics.Candle15m = mc.aggregateTimeframeCandle(buffer, 15)
	metrics.Candle1h = mc.aggregateTimeframeCandle(buffer, 60)
	metrics.Candle4h = mc.aggregateTimeframeCandle(buffer, 240)
	metrics.Candle8h = mc.aggregateTimeframeCandle(buffer, 480)
	metrics.Candle1d = mc.aggregateTimeframeCandle(buffer, 1440)

	// Calculate VCP from 1m candle
	if metrics.Candle1m.Close > 0 {
		weightedAvg := (metrics.Candle1m.Open + metrics.Candle1m.Close) / 2
		metrics.VCP = indicators.CalculateVCP(
			metrics.Candle1m.Close,
			weightedAvg,
			metrics.Candle1m.High,
			metrics.Candle1m.Low,
		)
	}

	// Calculate Fibonacci levels from 1d candle
	if metrics.Candle1d.Close > 0 {
		metrics.Fibonacci = indicators.CalculateFibonacciLevels(
			metrics.Candle1d.High,
			metrics.Candle1d.Low,
			metrics.Candle1d.Close,
		)
	}

	// Calculate price changes (% change from open to close of aggregated candle)
	metrics.PriceChange5m = mc.calculatePriceChangeFromCandle(metrics.Candle5m)
	metrics.PriceChange15m = mc.calculatePriceChangeFromCandle(metrics.Candle15m)
	metrics.PriceChange1h = mc.calculatePriceChangeFromCandle(metrics.Candle1h)
	metrics.PriceChange4h = mc.calculatePriceChangeFromCandle(metrics.Candle4h)
	metrics.PriceChange8h = mc.calculatePriceChangeFromCandle(metrics.Candle8h)
	metrics.PriceChange1d = mc.calculatePriceChangeFromCandle(metrics.Candle1d)

	// Calculate volume ratios (current period vs previous period)
	metrics.VolumeRatio5m = mc.calculateVolumeRatio(buffer, 5)
	metrics.VolumeRatio15m = mc.calculateVolumeRatio(buffer, 15)
	metrics.VolumeRatio1h = mc.calculateVolumeRatio(buffer, 60)
	metrics.VolumeRatio4h = mc.calculateVolumeRatio(buffer, 240)
	metrics.VolumeRatio8h = mc.calculateVolumeRatio(buffer, 480)

	// Calculate RSI if we have enough data (need at least 15 candles)
	if buffer.Size() >= 15 {
		prices := mc.extractClosePrices(buffer, 15)
		metrics.RSI = indicators.CalculateRSI(prices, 14)
	}

	// Calculate MACD if we have enough data (need at least 26 candles)
	if buffer.Size() >= 26 {
		prices := mc.extractClosePrices(buffer, 26)
		metrics.MACD = indicators.CalculateMACD(prices, 12, 26, 9)
	}

	return metrics, nil
}

// aggregateTimeframeCandle aggregates last N 1-minute candles into a single timeframe candle
func (mc *MetricsCalculator) aggregateTimeframeCandle(buffer *ringbuffer.RingBuffer, minutes int) TimeframeCandle {
	if buffer.Size() < minutes {
		// If not enough data, use what we have
		minutes = buffer.Size()
	}

	if minutes == 0 {
		return TimeframeCandle{}
	}

	candles := buffer.GetLast(minutes)
	if len(candles) == 0 {
		return TimeframeCandle{}
	}

	// Aggregate using ringbuffer's helper
	aggregated := ringbuffer.AggregateTimeframe(candles)
	if aggregated == nil {
		return TimeframeCandle{}
	}

	return TimeframeCandle{
		Open:   aggregated.Open,
		High:   aggregated.High,
		Low:    aggregated.Low,
		Close:  aggregated.Close,
		Volume: aggregated.QuoteVolume,
	}
}

// calculatePriceChangeFromCandle calculates percentage price change from open to close
func (mc *MetricsCalculator) calculatePriceChangeFromCandle(candle TimeframeCandle) float64 {
	if candle.Open == 0 {
		return 0
	}

	return ((candle.Close - candle.Open) / candle.Open) * 100
}

// calculateVolumeRatio calculates the ratio of current period volume to previous period volume
func (mc *MetricsCalculator) calculateVolumeRatio(buffer *ringbuffer.RingBuffer, minutes int) float64 {
	// Need at least 2x the period to compare
	if buffer.Size() < minutes*2 {
		return 0
	}

	// Get current period volume (last N minutes)
	currentCandles := buffer.GetLast(minutes)
	currentVolume := 0.0
	for _, c := range currentCandles {
		currentVolume += c.QuoteVolume
	}

	// Get previous period volume (N minutes before that)
	allCandles := buffer.GetLast(minutes * 2)
	previousVolume := 0.0
	for i := 0; i < minutes && i < len(allCandles); i++ {
		previousVolume += allCandles[i].QuoteVolume
	}

	if previousVolume == 0 {
		return 0
	}

	return currentVolume / previousVolume
}

// extractClosePrices extracts closing prices from the last N candles
func (mc *MetricsCalculator) extractClosePrices(buffer *ringbuffer.RingBuffer, count int) []float64 {
	candles := buffer.GetLast(count)
	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}
	return prices
}

// GetBufferSize returns the current size of a symbol's buffer
func (mc *MetricsCalculator) GetBufferSize(symbol string) int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	buffer, exists := mc.buffers[symbol]
	if !exists {
		return 0
	}

	return buffer.Size()
}

// ClearBuffer clears the ring buffer for a symbol
func (mc *MetricsCalculator) ClearBuffer(symbol string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	delete(mc.buffers, symbol)
}
