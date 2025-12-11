package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

const (
	// FuturesAPIBase is the base URL for Binance Futures API
	FuturesAPIBase = "https://fapi.binance.com"
	
	// ExchangeInfoEndpoint provides trading pair metadata
	ExchangeInfoEndpoint = "/fapi/v1/exchangeInfo"
)

// Client handles HTTP requests to Binance Futures API
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     zerolog.Logger
}

// NewClient creates a new Binance API client
func NewClient(logger zerolog.Logger) *Client {
	return &Client{
		baseURL: FuturesAPIBase,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger.With().Str("component", "binance-client").Logger(),
	}
}

// GetActiveSymbols fetches active USDT-margined perpetual futures pairs
// For development/testing, it returns only the top 50 most liquid pairs
func (c *Client) GetActiveSymbols(ctx context.Context) ([]string, error) {
	url := c.baseURL + ExchangeInfoEndpoint
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	
	c.logger.Debug().Str("url", url).Msg("fetching exchange info")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	
	var exchangeInfo ExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&exchangeInfo); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	
	// Filter for active USDT perpetual futures
	var activeSymbols []string
	for _, symbol := range exchangeInfo.Symbols {
		if symbol.IsActive() {
			activeSymbols = append(activeSymbols, symbol.Symbol)
		}
	}
	c.logger.Info().
		Int("total", len(exchangeInfo.Symbols)).
		Int("active", len(activeSymbols)).
		Msg("fetched exchange info")
	
	// For development/testing: limit to top 50 most liquid pairs
	// These are the most actively traded perpetual futures on Binance
	top50 := []string{
		"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
		"ADAUSDT", "DOGEUSDT", "MATICUSDT", "DOTUSDT", "SHIBUSDT",
		"AVAXUSDT", "LINKUSDT", "UNIUSDT", "ATOMUSDT", "LTCUSDT",
		"NEARUSDT", "APTUSDT", "ARBUSDT", "OPUSDT", "FILUSDT",
		"LDOUSDT", "INJUSDT", "STXUSDT", "SUIUSDT", "RNDRUSDT",
		"ICPUSDT", "WLDUSDT", "TAOUSDT", "FETUSDT", "IMXUSDT",
		"HBARUSDT", "GMXUSDT", "GRTUSDT", "MKRUSDT", "SANDUSDT",
		"FTMUSDT", "AAVEUSDT", "RUNEUSDT", "TIAUSDT", "ALGOUSDT",
		"VETUSDT", "RENDERUSDT", "ENAUSDT", "ARUSDT", "AXSUSDT",
		"PEPEUSDT", "SEIUSDT", "PENDLEUSDT", "MANAUSDT", "FLOKIUSDT",
	}
	
	// Filter activeSymbols to only include top50
	filteredSymbols := make([]string, 0, len(top50))
	symbolSet := make(map[string]bool)
	for _, s := range activeSymbols {
		symbolSet[s] = true
	}
	
	for _, symbol := range top50 {
		if symbolSet[symbol] {
			filteredSymbols = append(filteredSymbols, symbol)
		}
	}
	
	c.logger.Info().
		Int("filtered", len(filteredSymbols)).
		Msg("limited to top 50 most liquid pairs for development")
	
	return filteredSymbols, nil
}
