package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

const (
	// FuturesAPIBase is the base URL for Binance Futures API
	FuturesAPIBase = "https://fapi.binance.com"
	
	// ExchangeInfoEndpoint provides trading pair metadata
	ExchangeInfoEndpoint = "/fapi/v1/exchangeInfo"
	
	// Ticker24hEndpoint provides 24-hour ticker statistics
	Ticker24hEndpoint = "/fapi/v1/ticker/24hr"
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
// Returns top 150 symbols sorted by 24-hour quote volume (USDT volume)
func (c *Client) GetActiveSymbols(ctx context.Context) ([]string, error) {
	// Step 1: Get exchange info to filter for active perpetual USDT futures
	exchangeInfoURL := c.baseURL + ExchangeInfoEndpoint
	
	req, err := http.NewRequestWithContext(ctx, "GET", exchangeInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create exchange info request: %w", err)
	}
	
	c.logger.Debug().Str("url", exchangeInfoURL).Msg("fetching exchange info")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange info request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	
	var exchangeInfo ExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&exchangeInfo); err != nil {
		return nil, fmt.Errorf("decode exchange info: %w", err)
	}
	
	// Filter for active USDT perpetual futures
	activeSymbolSet := make(map[string]bool)
	for _, symbol := range exchangeInfo.Symbols {
		if symbol.IsActive() {
			activeSymbolSet[symbol.Symbol] = true
		}
	}
	
	c.logger.Info().
		Int("total", len(exchangeInfo.Symbols)).
		Int("active_usdt_perpetuals", len(activeSymbolSet)).
		Msg("fetched exchange info")
	
	// Step 2: Get 24h ticker data for all symbols
	ticker24hURL := c.baseURL + Ticker24hEndpoint
	
	req, err = http.NewRequestWithContext(ctx, "GET", ticker24hURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create ticker request: %w", err)
	}
	
	c.logger.Debug().Str("url", ticker24hURL).Msg("fetching 24h ticker data")
	
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ticker request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected ticker status %d: %s", resp.StatusCode, string(body))
	}
	
	var tickers []Ticker24h
	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return nil, fmt.Errorf("decode ticker data: %w", err)
	}
	
	c.logger.Info().Int("tickers", len(tickers)).Msg("fetched 24h ticker data")
	
	// Step 3: Filter tickers to only active USDT perpetuals and parse quote volume
	type symbolVolume struct {
		symbol string
		volume float64
	}
	
	var symbolVolumes []symbolVolume
	for _, ticker := range tickers {
		// Only include symbols that are active USDT perpetuals
		if !activeSymbolSet[ticker.Symbol] {
			continue
		}
		
		// Parse quote volume (volume in USDT)
		quoteVol, err := strconv.ParseFloat(ticker.QuoteVolume, 64)
		if err != nil {
			c.logger.Warn().
				Str("symbol", ticker.Symbol).
				Str("quote_volume", ticker.QuoteVolume).
				Err(err).
				Msg("failed to parse quote volume, skipping")
			continue
		}
		
		symbolVolumes = append(symbolVolumes, symbolVolume{
			symbol: ticker.Symbol,
			volume: quoteVol,
		})
	}
	
	// Step 4: Sort by volume (descending) and take top 150
	sort.Slice(symbolVolumes, func(i, j int) bool {
		return symbolVolumes[i].volume > symbolVolumes[j].volume
	})
	
	// Limit to top 150
	limit := 150
	if len(symbolVolumes) < limit {
		limit = len(symbolVolumes)
	}
	
	topSymbols := make([]string, limit)
	for i := 0; i < limit; i++ {
		topSymbols[i] = symbolVolumes[i].symbol
	}
	
	c.logger.Info().
		Int("selected", len(topSymbols)).
		Strs("top_10", topSymbols[:min(10, len(topSymbols))]).
		Msg("selected top symbols by 24h volume")
	
	return topSymbols, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
