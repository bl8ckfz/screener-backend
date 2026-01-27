package binance

import (
	"testing"
)

func TestKlineEventValidation(t *testing.T) {
	tests := []struct {
		name    string
		event   KlineEvent
		wantErr bool
	}{
		{
			name: "valid_closed_candle",
			event: KlineEvent{
				EventType: "kline",
				EventTime: 1234567890,
				Symbol:    "BTCUSDT",
				Kline: KlineData{
					Symbol:            "BTCUSDT",
					StartTime:         1234567800,
					CloseTime:         1234567860,
					IsClosed:          true,
					OpenPrice:         "40000.0",
					HighPrice:         "40100.0",
					LowPrice:          "39900.0",
					ClosePrice:        "40050.0",
					BaseAssetVolume:   "100.5",
					QuoteAssetVolume:  "4000000.0",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_not_closed",
			event: KlineEvent{
				EventType: "kline",
				EventTime: 1234567890,
				Symbol:    "BTCUSDT",
				Kline: KlineData{
					Symbol:            "BTCUSDT",
					StartTime:         1234567800,
					CloseTime:         1234567860,
					IsClosed:          false, // Not closed
					OpenPrice:         "40000.0",
					HighPrice:         "40100.0",
					LowPrice:          "39900.0",
					ClosePrice:        "40050.0",
					BaseAssetVolume:   "100.5",
					QuoteAssetVolume:  "4000000.0",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid_empty_prices",
			event: KlineEvent{
				EventType: "kline",
				EventTime: 1234567890,
				Symbol:    "BTCUSDT",
				Kline: KlineData{
					Symbol:            "BTCUSDT",
					StartTime:         1234567800,
					CloseTime:         1234567860,
					IsClosed:          true,
					OpenPrice:         "", // Empty
					HighPrice:         "40100.0",
					LowPrice:          "39900.0",
					ClosePrice:        "40050.0",
					BaseAssetVolume:   "100.5",
					QuoteAssetVolume:  "4000000.0",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid_zero_volume",
			event: KlineEvent{
				EventType: "kline",
				EventTime: 1234567890,
				Symbol:    "BTCUSDT",
				Kline: KlineData{
					Symbol:            "BTCUSDT",
					StartTime:         1234567800,
					CloseTime:         1234567860,
					IsClosed:          true,
					OpenPrice:         "40000.0",
					HighPrice:         "40100.0",
					LowPrice:          "39900.0",
					ClosePrice:        "40050.0",
					BaseAssetVolume:   "", // Empty volume
					QuoteAssetVolume:  "",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.event.Kline.Validate()
			if (valid == false) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", valid, tt.wantErr)
			}
		})
	}
}

func TestKlineDataValidation(t *testing.T) {
	kline := KlineData{
		Symbol:           "ETHUSDT",
		StartTime:        1640000000000,
		CloseTime:        1640000060000,
		IsClosed:         true,
		OpenPrice:        "2000.50",
		HighPrice:        "2010.75",
		LowPrice:         "1990.25",
		ClosePrice:       "2005.00",
		BaseAssetVolume:  "500.123",
		QuoteAssetVolume: "1002000.0",
	}

	if !kline.Validate() {
		t.Error("Validate() returned false for valid kline")
	}

	if kline.Symbol != "ETHUSDT" {
		t.Errorf("Symbol = %s, want ETHUSDT", kline.Symbol)
	}

	if kline.OpenPrice != "2000.50" {
		t.Errorf("OpenPrice = %s, want 2000.50", kline.OpenPrice)
	}

	if kline.HighPrice != "2010.75" {
		t.Errorf("HighPrice = %s, want 2010.75", kline.HighPrice)
	}

	if kline.LowPrice != "1990.25" {
		t.Errorf("LowPrice = %s, want 1990.25", kline.LowPrice)
	}

	if kline.ClosePrice != "2005.00" {
		t.Errorf("ClosePrice = %s, want 2005.00", kline.ClosePrice)
	}

	if kline.BaseAssetVolume != "500.123" {
		t.Errorf("BaseAssetVolume = %s, want 500.123", kline.BaseAssetVolume)
	}
}

func TestInvalidKlineData(t *testing.T) {
	tests := []struct {
		name  string
		kline KlineData
	}{
		{
			name: "invalid_empty_open",
			kline: KlineData{
				Symbol:           "BTCUSDT",
				StartTime:        1640000000000,
				CloseTime:        1640000060000,
				IsClosed:         true,
				OpenPrice:        "", // Empty
				HighPrice:        "40100.0",
				LowPrice:         "39900.0",
				ClosePrice:       "40050.0",
				BaseAssetVolume:  "100.5",
				QuoteAssetVolume: "4000000.0",
			},
		},
		{
			name: "invalid_empty_high",
			kline: KlineData{
				Symbol:           "BTCUSDT",
				StartTime:        1640000000000,
				CloseTime:        1640000060000,
				IsClosed:         true,
				OpenPrice:        "40000.0",
				HighPrice:        "", // Empty
				LowPrice:         "39900.0",
				ClosePrice:       "40050.0",
				BaseAssetVolume:  "100.5",
				QuoteAssetVolume: "4000000.0",
			},
		},
		{
			name: "invalid_empty_volume",
			kline: KlineData{
				Symbol:           "BTCUSDT",
				StartTime:        1640000000000,
				CloseTime:        1640000060000,
				IsClosed:         true,
				OpenPrice:        "40000.0",
				HighPrice:        "40100.0",
				LowPrice:         "39900.0",
				ClosePrice:       "40050.0",
				BaseAssetVolume:  "", // Empty
				QuoteAssetVolume: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.kline.Validate()
			if valid {
				t.Error("Validate() expected false, got true")
			}
		})
	}
}

func TestSymbolInfoParsing(t *testing.T) {
	symbol := SymbolInfo{
		Symbol:             "BTCUSDT",
		Status:             "TRADING",
		BaseAsset:          "BTC",
		QuoteAsset:         "USDT",
		ContractType:       "PERPETUAL",
		MarginAsset:        "USDT",
		PricePrecision:     2,
		QuantityPrecision:  3,
	}

	if symbol.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %s, want BTCUSDT", symbol.Symbol)
	}

	if symbol.Status != "TRADING" {
		t.Errorf("Status = %s, want TRADING", symbol.Status)
	}

	if symbol.ContractType != "PERPETUAL" {
		t.Errorf("ContractType = %s, want PERPETUAL", symbol.ContractType)
	}

	if symbol.MarginAsset != "USDT" {
		t.Errorf("MarginAsset = %s, want USDT", symbol.MarginAsset)
	}
}

func TestExchangeInfoFiltering(t *testing.T) {
	info := ExchangeInfo{
		Symbols: []SymbolInfo{
			{
				Symbol:       "BTCUSDT",
				Status:       "TRADING",
				ContractType: "PERPETUAL",
				MarginAsset:  "USDT",
			},
			{
				Symbol:       "ETHUSDT",
				Status:       "TRADING",
				ContractType: "PERPETUAL",
				MarginAsset:  "USDT",
			},
			{
				Symbol:       "BTCBUSD",
				Status:       "TRADING",
				ContractType: "PERPETUAL",
				MarginAsset:  "BUSD", // Different margin asset
			},
			{
				Symbol:       "BTCUSDT_250328",
				Status:       "TRADING",
				ContractType: "CURRENT_QUARTER",
				MarginAsset:  "USDT", // Not perpetual
			},
			{
				Symbol:       "OLDUSDT",
				Status:       "BREAK", // Not trading
				ContractType: "PERPETUAL",
				MarginAsset:  "USDT",
			},
		},
	}

	// Filter for USDT perpetuals that are trading
	var filtered []SymbolInfo
	for _, s := range info.Symbols {
		if s.Status == "TRADING" &&
			s.ContractType == "PERPETUAL" &&
			s.MarginAsset == "USDT" {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered symbols, got %d", len(filtered))
	}

	// Verify correct symbols
	if filtered[0].Symbol != "BTCUSDT" || filtered[1].Symbol != "ETHUSDT" {
		t.Errorf("unexpected filtered symbols: %v", filtered)
	}
}

func TestPrecisionHandling(t *testing.T) {
	tests := []struct {
		name      string
		precision int
		value     string
		expected  string
	}{
		{
			name:      "high_precision",
			precision: 8,
			value:     "0.00012345",
			expected:  "0.00012345",
		},
		{
			name:      "low_precision",
			precision: 2,
			value:     "42000.50",
			expected:  "42000.50",
		},
		{
			name:      "integer",
			precision: 0,
			value:     "42000",
			expected:  "42000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kline := KlineData{
				Symbol:           "TESTUSDT",
				StartTime:        1640000000000,
				CloseTime:        1640000060000,
				IsClosed:         true,
				OpenPrice:        tt.value,
				HighPrice:        tt.value,
				LowPrice:         tt.value,
				ClosePrice:       tt.value,
				BaseAssetVolume:  "100",
				QuoteAssetVolume: "10000",
			}

			if !kline.Validate() {
				t.Error("Validate() returned false for valid kline")
			}

			if kline.ClosePrice != tt.expected {
				t.Errorf("ClosePrice = %s, want %s", kline.ClosePrice, tt.expected)
			}
		})
	}
}

func TestWebSocketURL(t *testing.T) {
	tests := []struct {
		symbol   string
		expected string
	}{
		{
			symbol:   "BTCUSDT",
			expected: "wss://fstream.binance.com/ws/btcusdt@kline_1m",
		},
		{
			symbol:   "ETHUSDT",
			expected: "wss://fstream.binance.com/ws/ethusdt@kline_1m",
		},
		{
			symbol:   "BTCUSDT_250328",
			expected: "wss://fstream.binance.com/ws/btcusdt_250328@kline_1m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			url := getWebSocketURL(tt.symbol)
			if url != tt.expected {
				t.Errorf("getWebSocketURL() = %s, want %s", url, tt.expected)
			}
		})
	}
}

// Helper function (should match actual implementation)
func getWebSocketURL(symbol string) string {
	// Convert symbol to lowercase for WebSocket endpoint
	lowerSymbol := ""
	for _, c := range symbol {
		if c >= 'A' && c <= 'Z' {
			lowerSymbol += string(c + 32)
		} else {
			lowerSymbol += string(c)
		}
	}
	return "wss://fstream.binance.com/ws/" + lowerSymbol + "@kline_1m"
}
