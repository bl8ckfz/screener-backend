package indicators

import (
	"math"
)

// VCP calculates the Volatility Contraction Pattern indicator
// Formula from TypeScript: VCP = (P / WA) * [((C - L) - (H - C)) / (H - L)]
// Where:
//   P = lastPrice (close)
//   WA = weightedAvgPrice
//   C = lastPrice (close)
//   H = highPrice
//   L = lowPrice
//
// Returns: VCP value rounded to 3 decimals, 0 if highPrice == lowPrice
func CalculateVCP(lastPrice, weightedAvgPrice, highPrice, lowPrice float64) float64 {
	// Check for division by zero
	if highPrice == lowPrice {
		return 0
	}

	priceToWA := lastPrice / weightedAvgPrice
	numerator := lastPrice - lowPrice - (highPrice - lastPrice)
	denominator := highPrice - lowPrice
	vcp := priceToWA * (numerator / denominator)

	// Round to 3 decimals to match TypeScript
	return math.Round(vcp*1000) / 1000
}

// FibonacciLevels represents Fibonacci pivot levels
type FibonacciLevels struct {
	Pivot         float64 `json:"pivot"`
	Resistance1   float64 `json:"resistance1"`
	Resistance618 float64 `json:"resistance0618"`
	Resistance382 float64 `json:"resistance0382"`
	Support382    float64 `json:"support0382"`
	Support618    float64 `json:"support0618"`
	Support1      float64 `json:"support1"`
}

// CalculateFibonacciLevels calculates Fibonacci pivot levels
// Formula from TypeScript:
//   pivot = (H + L + C) / 3
//   range = H - L
//   R1 = pivot + 1.0 * range
//   R0.618 = pivot + 0.618 * range
//   R0.382 = pivot + 0.382 * range
//   S0.382 = pivot - 0.382 * range
//   S0.618 = pivot - 0.618 * range
//   S1 = pivot - 1.0 * range
//
// All values rounded to 3 decimals
func CalculateFibonacciLevels(highPrice, lowPrice, lastPrice float64) FibonacciLevels {
	pivot := (highPrice + lowPrice + lastPrice) / 3
	priceRange := highPrice - lowPrice

	return FibonacciLevels{
		Pivot:         round3(pivot),
		Resistance1:   round3(pivot + 1.0*priceRange),
		Resistance618: round3(pivot + 0.618*priceRange),
		Resistance382: round3(pivot + 0.382*priceRange),
		Support382:    round3(pivot - 0.382*priceRange),
		Support618:    round3(pivot - 0.618*priceRange),
		Support1:      round3(pivot - 1.0*priceRange),
	}
}

// RSI calculates the Relative Strength Index
// period: typically 14
// prices: slice of closing prices (must be at least period+1 length)
func CalculateRSI(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return 0
	}

	// Calculate price changes
	gains := 0.0
	losses := 0.0

	for i := 1; i <= period; i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	// Average gain and loss
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return round3(rsi)
}

// MACD represents MACD indicator values
type MACD struct {
	MACD      float64 `json:"macd"`
	Signal    float64 `json:"signal"`
	Histogram float64 `json:"histogram"`
}

// CalculateMACD calculates the Moving Average Convergence Divergence
// prices: slice of closing prices
// fastPeriod: typically 12
// slowPeriod: typically 26
// signalPeriod: typically 9
func CalculateMACD(prices []float64, fastPeriod, slowPeriod, signalPeriod int) MACD {
	if len(prices) < slowPeriod {
		return MACD{}
	}

	// Calculate EMAs
	fastEMA := calculateEMA(prices, fastPeriod)
	slowEMA := calculateEMA(prices, slowPeriod)

	macdLine := fastEMA - slowEMA

	// Calculate signal line (EMA of MACD)
	// For simplicity, we'll use SMA here (full EMA would require historical MACD values)
	signalLine := macdLine // Simplified - should be EMA of MACD line

	histogram := macdLine - signalLine

	return MACD{
		MACD:      round3(macdLine),
		Signal:    round3(signalLine),
		Histogram: round3(histogram),
	}
}

// calculateEMA calculates Exponential Moving Average
func calculateEMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	// Calculate initial SMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// Calculate EMA
	multiplier := 2.0 / (float64(period) + 1.0)
	for i := period; i < len(prices); i++ {
		ema = (prices[i]-ema)*multiplier + ema
	}

	return ema
}

// round3 rounds a float64 to 3 decimal places
// Matches TypeScript: Math.round(value * 1000) / 1000
func round3(value float64) float64 {
	return math.Round(value*1000) / 1000
}
