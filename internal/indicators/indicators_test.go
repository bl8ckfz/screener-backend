package indicators

import (
	"math"
	"testing"
)

func TestCalculateVCP(t *testing.T) {
	tests := []struct {
		name              string
		lastPrice         float64
		weightedAvgPrice  float64
		highPrice         float64
		lowPrice          float64
		expected          float64
	}{
		{
			name:             "Normal case",
			lastPrice:        42350,
			weightedAvgPrice: 42300,
			highPrice:        42400,
			lowPrice:         42200,
			expected:         0.501, // (42350/42300) * ((42350-42200)-(42400-42350))/(42400-42200) = 1.0012 * 0.5 = 0.501
		},
		{
			name:             "Division by zero - same high and low",
			lastPrice:        100,
			weightedAvgPrice: 100,
			highPrice:        100,
			lowPrice:         100,
			expected:         0,
		},
		{
			name:             "Negative VCP",
			lastPrice:        42200,
			weightedAvgPrice: 42300,
			highPrice:        42400,
			lowPrice:         42200,
			expected:         -0.998, // (42200/42300) * ((42200-42200)-(42400-42200))/(42400-42200) = 0.998 * -1 = -0.998
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateVCP(tt.lastPrice, tt.weightedAvgPrice, tt.highPrice, tt.lowPrice)
			if math.Abs(result-tt.expected) > 0.001 {
				t.Errorf("CalculateVCP() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateFibonacciLevels(t *testing.T) {
	// Test case from TypeScript
	highPrice := 42400.0
	lowPrice := 42200.0
	lastPrice := 42350.0

	levels := CalculateFibonacciLevels(highPrice, lowPrice, lastPrice)

	// Expected values (manually calculated)
	pivot := (42400.0 + 42200.0 + 42350.0) / 3.0 // 42316.667
	priceRange := 42400.0 - 42200.0              // 200.0

	tests := []struct {
		name     string
		got      float64
		expected float64
	}{
		{"Pivot", levels.Pivot, round3(pivot)},
		{"Resistance1", levels.Resistance1, round3(pivot + 1.0*priceRange)},
		{"Resistance618", levels.Resistance618, round3(pivot + 0.618*priceRange)},
		{"Resistance382", levels.Resistance382, round3(pivot + 0.382*priceRange)},
		{"Support382", levels.Support382, round3(pivot - 0.382*priceRange)},
		{"Support618", levels.Support618, round3(pivot - 0.618*priceRange)},
		{"Support1", levels.Support1, round3(pivot - 1.0*priceRange)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if math.Abs(tt.got-tt.expected) > 0.001 {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestCalculateRSI(t *testing.T) {
	// Sample price data with known RSI
	prices := []float64{
		44.34, 44.09, 43.61, 44.33, 44.83,
		45.10, 45.42, 45.84, 46.08, 45.89,
		46.03, 45.61, 46.28, 46.28, 46.00,
	}

	rsi := CalculateRSI(prices, 14)

	// RSI for this data should be around 66-67
	if rsi < 65 || rsi > 68 {
		t.Errorf("CalculateRSI() = %v, expected around 66-67", rsi)
	}

	// Test edge case: not enough data
	shortPrices := []float64{100, 101, 102}
	rsi = CalculateRSI(shortPrices, 14)
	if rsi != 0 {
		t.Errorf("CalculateRSI() with insufficient data = %v, expected 0", rsi)
	}
}

func TestCalculateMACD(t *testing.T) {
	// Generate sample price data
	prices := make([]float64, 50)
	for i := range prices {
		prices[i] = 100 + float64(i)*0.5 // Uptrend
	}

	macd := CalculateMACD(prices, 12, 26, 9)

	// In an uptrend, MACD should be positive
	if macd.MACD <= 0 {
		t.Errorf("CalculateMACD().MACD = %v, expected positive value in uptrend", macd.MACD)
	}

	// Test edge case: not enough data
	shortPrices := []float64{100, 101, 102}
	macd = CalculateMACD(shortPrices, 12, 26, 9)
	if macd.MACD != 0 {
		t.Errorf("CalculateMACD() with insufficient data = %v, expected zero values", macd.MACD)
	}
}

func TestRound3(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{1.2345, 1.235},
		{1.2344, 1.234},
		{-1.2345, -1.235},
		{0.0001, 0.0},
		{42316.666666, 42316.667},
	}

	for _, tt := range tests {
		result := round3(tt.input)
		if result != tt.expected {
			t.Errorf("round3(%v) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}
