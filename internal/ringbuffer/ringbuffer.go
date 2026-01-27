package ringbuffer

import (
	"sync"
	"time"
)

// Candle represents a single candlestick data point
type Candle struct {
	Symbol         string    `json:"symbol"`
	OpenTime       time.Time `json:"open_time"`
	CloseTime      time.Time `json:"close_time"`
	Open           float64   `json:"open"`
	High           float64   `json:"high"`
	Low            float64   `json:"low"`
	Close          float64   `json:"close"`
	Volume         float64   `json:"volume"`
	QuoteVolume    float64   `json:"quote_volume"`
	NumberOfTrades int64     `json:"number_of_trades"`
}

// RingBuffer is a fixed-size circular buffer for storing candles
// Optimized for O(1) append and O(1) range queries for sliding windows
type RingBuffer struct {
	candles [1440]Candle // 24 hours of 1-minute candles
	head    int           // Write position (next insertion point)
	size    int           // Current number of elements (0 to 1440)
	mu      sync.RWMutex  // Thread-safe access
}

// NewRingBuffer creates a new ring buffer for candle storage
func NewRingBuffer() *RingBuffer {
	return &RingBuffer{
		head: 0,
		size: 0,
	}
}

// Append adds a new candle to the buffer
// O(1) complexity - overwrites oldest candle when full
func (rb *RingBuffer) Append(candle Candle) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.candles[rb.head] = candle
	rb.head = (rb.head + 1) % 1440

	if rb.size < 1440 {
		rb.size++
	}
}

// Size returns the current number of candles in the buffer
func (rb *RingBuffer) Size() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// GetLast returns the last N candles (most recent)
// O(N) complexity where N is the requested count
func (rb *RingBuffer) GetLast(count int) []Candle {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if count <= 0 || rb.size == 0 {
		return nil
	}

	if count > rb.size {
		count = rb.size
	}

	result := make([]Candle, count)
	
	// Calculate start position (count candles before head)
	start := rb.head - count
	if start < 0 {
		start += 1440
	}

	// Copy candles in chronological order
	for i := 0; i < count; i++ {
		idx := (start + i) % 1440
		result[i] = rb.candles[idx]
	}

	return result
}

// GetRange returns candles for a specific time window
// minutes: 5, 15, 60, 480, 1440 for 5m, 15m, 1h, 8h, 1d
func (rb *RingBuffer) GetRange(minutes int) []Candle {
	return rb.GetLast(minutes)
}

// GetLatest returns the most recent candle
func (rb *RingBuffer) GetLatest() *Candle {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return nil
	}

	// Head points to next insertion, so latest is head-1
	idx := rb.head - 1
	if idx < 0 {
		idx = 1439
	}

	candle := rb.candles[idx]
	return &candle
}

// Clear resets the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.head = 0
	rb.size = 0
}

// AggregateTimeframe aggregates 1-minute candles into a larger timeframe
// For example: 5 one-minute candles → 1 five-minute candle
func AggregateTimeframe(candles []Candle) *Candle {
	if len(candles) == 0 {
		return nil
	}

	agg := &Candle{
		Symbol:    candles[0].Symbol,
		OpenTime:  candles[0].OpenTime,
		CloseTime: candles[len(candles)-1].CloseTime,
		Open:      candles[0].Open,
		Close:     candles[len(candles)-1].Close,
		High:      candles[0].High,
		Low:       candles[0].Low,
	}

	// Find highest high and lowest low
	for _, c := range candles {
		if c.High > agg.High {
			agg.High = c.High
		}
		if c.Low < agg.Low {
			agg.Low = c.Low
		}
		agg.Volume += c.Volume
		agg.QuoteVolume += c.QuoteVolume
		agg.NumberOfTrades += c.NumberOfTrades
	}

	return agg
}
