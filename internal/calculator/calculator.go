package calculator

import (
	"sync"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/indicators"
	"github.com/bl8ckfz/crypto-screener-backend/internal/ringbuffer"
	"github.com/rs/zerolog"
)

// SymbolMetrics holds calculated metrics for a symbol
type SymbolMetrics struct {
	Symbol         string                    `json:"symbol"`
	Timestamp      time.Time                 `json:"timestamp"`
	LastPrice      float64                   `json:"last_price"`
	VCP            float64                   `json:"vcp"`
	Fibonacci      indicators.FibonacciLevels `json:"fibonacci"`
	RSI            float64                   `json:"rsi"`
	MACD           indicators.MACD           `json:"macd"`
	Volume24h      float64                   `json:"volume_24h"`
	PriceChange5m  float64                   `json:"price_change_5m"`
	PriceChange15m float64                   `json:"price_change_15m"`
	PriceChange1h  float64                   `json:"price_change_1h"`
	PriceChange8h  float64                   `json:"price_change_8h"`
	PriceChange1d  float64                   `json:"price_change_1d"`
	Volume5m       float64                   `json:"volume_5m"`
	Volume15m      float64                   `json:"volume_15m"`
	Volume1h       float64                   `json:"volume_1h"`
	Volume8h       float64                   `json:"volume_8h"`
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

// CalculateMetrics calculates all metrics for a symbol
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
	
	metrics := &SymbolMetrics{
		Symbol:    symbol,
		Timestamp: latest.CloseTime,
		LastPrice: latest.Close,
	}
	
	// Calculate VCP (requires current candle data)
	// Using close price as both last price and weighted average (simplified)
	metrics.VCP = indicators.CalculateVCP(
		latest.Close,
		(latest.Open + latest.Close) / 2, // Simplified weighted average
		latest.High,
		latest.Low,
	)
	
	// Calculate Fibonacci levels
	metrics.Fibonacci = indicators.CalculateFibonacciLevels(
		latest.High,
		latest.Low,
		latest.Close,
	)
	
	// Calculate price changes for different timeframes
	metrics.PriceChange5m = mc.calculatePriceChange(buffer, 5)
	metrics.PriceChange15m = mc.calculatePriceChange(buffer, 15)
	metrics.PriceChange1h = mc.calculatePriceChange(buffer, 60)
	metrics.PriceChange8h = mc.calculatePriceChange(buffer, 480)
	metrics.PriceChange1d = mc.calculatePriceChange(buffer, 1440)
	
	// Calculate volume for different timeframes
	metrics.Volume5m = mc.calculateVolume(buffer, 5)
	metrics.Volume15m = mc.calculateVolume(buffer, 15)
	metrics.Volume1h = mc.calculateVolume(buffer, 60)
	metrics.Volume8h = mc.calculateVolume(buffer, 480)
	metrics.Volume24h = mc.calculateVolume(buffer, 1440)
	
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

// calculatePriceChange calculates percentage price change over N minutes
func (mc *MetricsCalculator) calculatePriceChange(buffer *ringbuffer.RingBuffer, minutes int) float64 {
	if buffer.Size() < minutes {
		return 0
	}
	
	candles := buffer.GetLast(minutes)
	if len(candles) < 2 {
		return 0
	}
	
	oldPrice := candles[0].Open
	newPrice := candles[len(candles)-1].Close
	
	if oldPrice == 0 {
		return 0
	}
	
	return ((newPrice - oldPrice) / oldPrice) * 100
}

// calculateVolume sums volume over N minutes
func (mc *MetricsCalculator) calculateVolume(buffer *ringbuffer.RingBuffer, minutes int) float64 {
	if buffer.Size() < minutes {
		minutes = buffer.Size()
	}
	
	if minutes == 0 {
		return 0
	}
	
	candles := buffer.GetLast(minutes)
	volume := 0.0
	for _, c := range candles {
		volume += c.QuoteVolume
	}
	
	return volume
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
