package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

const (
	// FuturesWebSocketBase is the base URL for Binance Futures WebSocket
	FuturesWebSocketBase = "wss://fstream.binance.com/ws"
	
	// MaxReconnectAttempts is the maximum number of reconnection attempts
	MaxReconnectAttempts = 10
	
	// BaseReconnectDelay is the initial reconnection delay
	BaseReconnectDelay = 2 * time.Second
	
	// MaxReconnectDelay is the maximum reconnection delay
	MaxReconnectDelay = 30 * time.Second
)

// ConnectionManager manages WebSocket connections for multiple symbols
type ConnectionManager struct {
	symbols     []string
	connections map[string]*connection
	js          nats.JetStreamContext
	logger      zerolog.Logger
	wg          sync.WaitGroup
	mu          sync.RWMutex
}

// connection represents a single WebSocket connection
type connection struct {
	symbol          string
	conn            *websocket.Conn
	js              nats.JetStreamContext
	logger          zerolog.Logger
	reconnectCount  int
	stopCh          chan struct{}
	stoppedCh       chan struct{}
}

// NewConnectionManager creates a new WebSocket connection manager
func NewConnectionManager(symbols []string, js nats.JetStreamContext, logger zerolog.Logger) *ConnectionManager {
	return &ConnectionManager{
		symbols:     symbols,
		connections: make(map[string]*connection),
		js:          js,
		logger:      logger.With().Str("component", "ws-manager").Logger(),
	}
}

// Start begins managing WebSocket connections for all symbols
func (m *ConnectionManager) Start(ctx context.Context) error {
	m.logger.Info().Int("symbols", len(m.symbols)).Msg("starting connection manager")
	
	// Create connections for all symbols
	for _, symbol := range m.symbols {
		conn := &connection{
			symbol:    symbol,
			js:        m.js,
			logger:    m.logger.With().Str("symbol", symbol).Logger(),
			stopCh:    make(chan struct{}),
			stoppedCh: make(chan struct{}),
		}
		
		m.mu.Lock()
		m.connections[symbol] = conn
		m.mu.Unlock()
		
		m.wg.Add(1)
		go func(c *connection) {
			defer m.wg.Done()
			c.run(ctx)
		}(conn)
	}
	
	// Wait for context cancellation
	<-ctx.Done()
	
	// Stop all connections
	m.logger.Info().Msg("stopping all connections")
	m.mu.RLock()
	for _, conn := range m.connections {
		close(conn.stopCh)
	}
	m.mu.RUnlock()
	
	// Wait for all connections to stop
	m.wg.Wait()
	m.logger.Info().Msg("all connections stopped")
	
	return nil
}

// run manages the lifecycle of a single WebSocket connection
func (c *connection) run(ctx context.Context) {
	defer close(c.stoppedCh)
	
	for {
		select {
		case <-c.stopCh:
			if c.conn != nil {
				c.conn.Close()
			}
			return
		case <-ctx.Done():
			if c.conn != nil {
				c.conn.Close()
			}
			return
		default:
			if err := c.connect(); err != nil {
				c.logger.Error().Err(err).Msg("connection failed")
				
				// Check if max reconnect attempts reached
				if c.reconnectCount >= MaxReconnectAttempts {
					c.logger.Error().
						Int("attempts", c.reconnectCount).
						Msg("max reconnect attempts reached, stopping")
					return
				}
				
				// Calculate exponential backoff delay
				delay := c.calculateBackoff()
				c.logger.Info().
					Dur("delay", delay).
					Int("attempt", c.reconnectCount).
					Msg("reconnecting after delay")
				
				select {
				case <-time.After(delay):
					continue
				case <-c.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
			
			// Connection successful, reset reconnect count
			c.reconnectCount = 0
			
			// Handle messages
			if err := c.handleMessages(ctx); err != nil {
				c.logger.Error().Err(err).Msg("message handler error")
			}
			
			// Close connection before reconnecting
			if c.conn != nil {
				c.conn.Close()
			}
		}
	}
}

// connect establishes a WebSocket connection
func (c *connection) connect() error {
	url := fmt.Sprintf("%s/%s@kline_1m", FuturesWebSocketBase, strings.ToLower(c.symbol))
	
	c.logger.Debug().Str("url", url).Msg("connecting")
	
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		c.reconnectCount++
		return fmt.Errorf("dial: %w", err)
	}
	
	c.conn = conn
	c.logger.Info().Msg("connected")
	
	return nil
}

// handleMessages processes incoming WebSocket messages
// Note: Binance sends ping frames every 3 minutes, gorilla/websocket
// automatically responds with pong frames, so we don't need manual ping/pong
func (c *connection) handleMessages(ctx context.Context) error {
	// Read messages
	for {
		select {
		case <-c.stopCh:
			return nil
		case <-ctx.Done():
			return nil
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				return fmt.Errorf("read message: %w", err)
			}
			
			if err := c.processMessage(message); err != nil {
				c.logger.Error().Err(err).Msg("process message failed")
				continue
			}
		}
	}
}

// processMessage parses and publishes a kline event
func (c *connection) processMessage(data []byte) error {
	var event KlineEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	
	// Debug: log raw event occasionally for troubleshooting
	// Commented out to reduce log volume
	// c.logger.Debug().RawJSON("event", data).Msg("received kline event")
	
	// Only process closed candles (complete 1-minute periods)
	// Binance sends updates every second, we only want the final one
	if !event.Kline.IsClosed {
		return nil // Skip without error - this is normal
	}
	
	// Validate kline data (prices and volume present)
	if !event.Kline.ValidateFields() {
		c.logger.Warn().
			Str("open", event.Kline.OpenPrice).
			Str("close", event.Kline.ClosePrice).
			Str("high", event.Kline.HighPrice).
			Str("low", event.Kline.LowPrice).
			Str("volume", event.Kline.BaseAssetVolume).
			Str("quote_volume", event.Kline.QuoteAssetVolume).
			Bool("is_closed", event.Kline.IsClosed).
			RawJSON("raw_event", data).
			Msg("kline validation failed")
		return nil // Don't error, just skip
	}
	
	// Convert to internal Candle format
	candle, err := c.klineToCandle(&event.Kline)
	if err != nil {
		return fmt.Errorf("convert kline: %w", err)
	}
	
	// Publish to NATS
	subject := fmt.Sprintf("candles.1m.%s", event.Symbol)
	payload, err := json.Marshal(candle)
	if err != nil {
		return fmt.Errorf("marshal candle: %w", err)
	}
	
	if _, err := c.js.Publish(subject, payload); err != nil {
		return fmt.Errorf("publish to NATS: %w", err)
	}
	
	c.logger.Debug().
		Str("subject", subject).
		Float64("close", candle.Close).
		Float64("volume", candle.Volume).
		Msg("published candle")
	
	return nil
}

// klineToCandle converts KlineData to internal Candle format
func (c *connection) klineToCandle(k *KlineData) (*Candle, error) {
	open, err := strconv.ParseFloat(k.OpenPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("parse open price: %w", err)
	}
	
	high, err := strconv.ParseFloat(k.HighPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("parse high price: %w", err)
	}
	
	low, err := strconv.ParseFloat(k.LowPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("parse low price: %w", err)
	}
	
	close, err := strconv.ParseFloat(k.ClosePrice, 64)
	if err != nil {
		return nil, fmt.Errorf("parse close price: %w", err)
	}
	
	volume, err := strconv.ParseFloat(k.BaseAssetVolume, 64)
	if err != nil {
		return nil, fmt.Errorf("parse volume: %w", err)
	}
	
	quoteVolume, err := strconv.ParseFloat(k.QuoteAssetVolume, 64)
	if err != nil {
		return nil, fmt.Errorf("parse quote volume: %w", err)
	}
	
	return &Candle{
		Symbol:         k.Symbol,
		OpenTime:       time.UnixMilli(k.StartTime),
		CloseTime:      time.UnixMilli(k.CloseTime),
		Open:           open,
		High:           high,
		Low:            low,
		Close:          close,
		Volume:         volume,
		QuoteVolume:    quoteVolume,
		NumberOfTrades: k.NumberOfTrades,
	}, nil
}

// calculateBackoff returns the exponential backoff delay
func (c *connection) calculateBackoff() time.Duration {
	delay := BaseReconnectDelay * time.Duration(math.Pow(2, float64(c.reconnectCount)))
	if delay > MaxReconnectDelay {
		delay = MaxReconnectDelay
	}
	return delay
}
