package ringbuffer

import (
	"testing"
)

func TestRingBuffer_AppendAndSize(t *testing.T) {
	rb := NewRingBuffer()

	if rb.Size() != 0 {
		t.Errorf("Expected size 0, got %d", rb.Size())
	}

	// Add 10 candles
	for i := 0; i < 10; i++ {
		rb.Append(Candle{
			Symbol: "BTCUSDT",
			Close:  float64(40000 + i),
		})
	}

	if rb.Size() != 10 {
		t.Errorf("Expected size 10, got %d", rb.Size())
	}
}

func TestRingBuffer_GetLast(t *testing.T) {
	rb := NewRingBuffer()

	// Add 100 candles with sequential close prices
	for i := 0; i < 100; i++ {
		rb.Append(Candle{
			Symbol: "ETHUSDT",
			Close:  float64(3000 + i),
		})
	}

	// Get last 5 candles
	last5 := rb.GetLast(5)
	if len(last5) != 5 {
		t.Errorf("Expected 5 candles, got %d", len(last5))
	}

	// Verify they're the most recent ones (3095, 3096, 3097, 3098, 3099)
	for i, c := range last5 {
		expected := float64(3095 + i)
		if c.Close != expected {
			t.Errorf("Candle %d: expected close %f, got %f", i, expected, c.Close)
		}
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	rb := NewRingBuffer()

	// Fill buffer completely (1440 candles)
	for i := 0; i < 1440; i++ {
		rb.Append(Candle{
			Symbol: "BTCUSDT",
			Close:  float64(i),
		})
	}

	if rb.Size() != 1440 {
		t.Errorf("Expected size 1440, got %d", rb.Size())
	}

	// Add 10 more candles (should overwrite oldest)
	for i := 0; i < 10; i++ {
		rb.Append(Candle{
			Symbol: "BTCUSDT",
			Close:  float64(1440 + i),
		})
	}

	// Size should still be 1440
	if rb.Size() != 1440 {
		t.Errorf("Expected size 1440 after wraparound, got %d", rb.Size())
	}

	// Latest candle should be 1449
	latest := rb.GetLatest()
	if latest.Close != 1449 {
		t.Errorf("Expected latest close 1449, got %f", latest.Close)
	}

	// First candle should now be 10 (oldest 0-9 were overwritten)
	first := rb.GetLast(1440)[0]
	if first.Close != 10 {
		t.Errorf("Expected first close 10 after wraparound, got %f", first.Close)
	}
}

func TestRingBuffer_GetLatest(t *testing.T) {
	rb := NewRingBuffer()

	// Empty buffer
	if latest := rb.GetLatest(); latest != nil {
		t.Error("Expected nil for empty buffer")
	}

	// Add one candle
	rb.Append(Candle{
		Symbol: "SOLUSDT",
		Close:  100.5,
	})

	latest := rb.GetLatest()
	if latest == nil {
		t.Fatal("Expected candle, got nil")
	}
	if latest.Close != 100.5 {
		t.Errorf("Expected close 100.5, got %f", latest.Close)
	}
}

func TestAggregateTimeframe(t *testing.T) {
	// Create 5 one-minute candles
	candles := []Candle{
		{Symbol: "BTCUSDT", Open: 40000, High: 40100, Low: 39900, Close: 40050, Volume: 10},
		{Symbol: "BTCUSDT", Open: 40050, High: 40200, Low: 40000, Close: 40150, Volume: 15},
		{Symbol: "BTCUSDT", Open: 40150, High: 40250, Low: 40100, Close: 40200, Volume: 12},
		{Symbol: "BTCUSDT", Open: 40200, High: 40300, Low: 40150, Close: 40250, Volume: 18},
		{Symbol: "BTCUSDT", Open: 40250, High: 40400, Low: 40200, Close: 40350, Volume: 20},
	}

	agg := AggregateTimeframe(candles)
	if agg == nil {
		t.Fatal("Expected aggregated candle, got nil")
	}

	// Verify aggregated values
	if agg.Open != 40000 {
		t.Errorf("Expected open 40000, got %f", agg.Open)
	}
	if agg.Close != 40350 {
		t.Errorf("Expected close 40350, got %f", agg.Close)
	}
	if agg.High != 40400 {
		t.Errorf("Expected high 40400, got %f", agg.High)
	}
	if agg.Low != 39900 {
		t.Errorf("Expected low 39900, got %f", agg.Low)
	}
	if agg.Volume != 75 {
		t.Errorf("Expected volume 75, got %f", agg.Volume)
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer()

	// Simulate concurrent writes and reads
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			rb.Append(Candle{
				Symbol: "BTCUSDT",
				Close:  float64(i),
			})
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			rb.GetLast(10)
			rb.GetLatest()
			rb.Size()
		}
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done

	// Verify final size
	if rb.Size() != 1000 {
		t.Errorf("Expected size 1000, got %d", rb.Size())
	}
}
