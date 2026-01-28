package binance

import "time"

// ExchangeInfo represents the response from /fapi/v1/exchangeInfo
type ExchangeInfo struct {
	Timezone   string       `json:"timezone"`
	ServerTime int64        `json:"serverTime"`
	Symbols    []SymbolInfo `json:"symbols"`
}

// SymbolInfo contains metadata for a trading pair
type SymbolInfo struct {
	Symbol             string `json:"symbol"`
	Pair               string `json:"pair"`
	ContractType       string `json:"contractType"`
	Status             string `json:"status"`
	BaseAsset          string `json:"baseAsset"`
	QuoteAsset         string `json:"quoteAsset"`
	MarginAsset        string `json:"marginAsset"`
	PricePrecision     int    `json:"pricePrecision"`
	QuantityPrecision  int    `json:"quantityPrecision"`
	BaseAssetPrecision int    `json:"baseAssetPrecision"`
}

// IsActive returns true if the symbol is actively trading
func (s SymbolInfo) IsActive() bool {
	return s.Status == "TRADING" && s.ContractType == "PERPETUAL" && s.QuoteAsset == "USDT"
}

// KlineEvent represents a WebSocket kline (candlestick) event
type KlineEvent struct {
	EventType string    `json:"e"` // Event type (kline)
	EventTime int64     `json:"E"` // Event time (ms)
	Symbol    string    `json:"s"` // Symbol
	Kline     KlineData `json:"k"` // Kline data
}

// KlineData contains the actual candlestick data
type KlineData struct {
	StartTime           int64  `json:"t"` // Kline start time (ms)
	CloseTime           int64  `json:"T"` // Kline close time (ms)
	Symbol              string `json:"s"` // Symbol
	Interval            string `json:"i"` // Interval (1m, 5m, etc.)
	FirstTradeID        int64  `json:"f"` // First trade ID
	LastTradeID         int64  `json:"L"` // Last trade ID
	OpenPrice           string `json:"o"` // Open price
	ClosePrice          string `json:"c"` // Close price
	HighPrice           string `json:"h"` // High price
	LowPrice            string `json:"l"` // Low price
	BaseAssetVolume     string `json:"v"` // Base asset volume
	NumberOfTrades      int64  `json:"n"` // Number of trades
	IsClosed            bool   `json:"x"` // Is this kline closed?
	QuoteAssetVolume    string `json:"q"` // Quote asset volume
	TakerBuyBaseVolume  string `json:"V"` // Taker buy base asset volume
	TakerBuyQuoteVolume string `json:"Q"` // Taker buy quote asset volume
}

// ValidateFields checks if the kline data has all required fields
func (k *KlineData) ValidateFields() bool {
	// Check for non-empty prices
	if k.OpenPrice == "" || k.ClosePrice == "" || k.HighPrice == "" || k.LowPrice == "" {
		return false
	}

	// Check for non-empty volume
	if k.BaseAssetVolume == "" || k.QuoteAssetVolume == "" {
		return false
	}

	return true
}

// Candle represents a processed candlestick for internal use
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

// Ticker24h represents 24-hour ticker statistics
type Ticker24h struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	WeightedAvgPrice   string `json:"weightedAvgPrice"`
	LastPrice          string `json:"lastPrice"`
	LastQty            string `json:"lastQty"`
	OpenPrice          string `json:"openPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	OpenTime           int64  `json:"openTime"`
	CloseTime          int64  `json:"closeTime"`
	Count              int64  `json:"count"`
}
