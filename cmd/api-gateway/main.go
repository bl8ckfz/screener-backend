package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/alerts"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/database"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/observability"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type server struct {
	logger      *observability.Logger
	metrics     *observability.MetricsCollector
	health      *observability.HealthChecker
	db          *pgxpool.Pool
	metadataDB  *pgxpool.Pool
	redis       *redis.Client
	nc          *nats.Conn
	upgrader    websocket.Upgrader
	authSecret  string
	rateLimiter *rateLimiter
}

func main() {
	logger := observability.NewLogger("api-gateway", observability.LevelInfo)
	logger.Info("Starting API Gateway service")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Shutdown signal received")
		cancel()
	}()

	srv, err := bootstrap(ctx, logger)
	if err != nil {
		logger.Fatal("Failed to bootstrap API Gateway", err)
	}
	defer srv.shutdown()

	addr := getenv("HTTP_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Infof("API Gateway listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server error", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Graceful shutdown failed", err)
	}
	logger.Info("API Gateway service stopped")
}

func bootstrap(ctx context.Context, logger *observability.Logger) (*server, error) {
	metrics := observability.GetCollector()
	health := observability.NewHealthChecker()
	natsURL := getenv("NATS_URL", "nats://localhost:4222")
	dbURL := getenv("TIMESCALEDB_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")
	authSecret := os.Getenv("SUPABASE_JWT_SECRET")
	redisURL := getenv("REDIS_URL", "")
	redisPassword := getenv("REDIS_PASSWORD", "")

	nc, err := messaging.NewNATSConn(messaging.Config{URL: natsURL, MaxReconnects: -1, ReconnectWait: 2 * time.Second, EnableJetStream: true})
	if err != nil {
		return nil, err
	}

	// Add NATS health check
	health.AddCheck("nats", func(ctx context.Context) error {
		if nc.IsClosed() {
			return fmt.Errorf("NATS connection closed")
		}
		return nil
	})

	db, err := database.NewPostgresPool(ctx, dbURL)
	if err != nil {
		nc.Close()
		return nil, err
	}

	// Add TimescaleDB health check
	health.AddCheck("timescaledb", func(ctx context.Context) error {
		return db.Ping(ctx)
	})

	// Optional Redis connection for ticker cache
	var rdb *redis.Client
	if redisURL != "" && redisURL != "disabled" {
		logger.WithField("url", redisURL).Info("Connecting to Redis")
		rdb = redis.NewClient(&redis.Options{
			Addr:     redisURL,
			Password: redisPassword,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.WithField("error", err.Error()).Warn("Failed to connect to Redis, ticker cache disabled")
			rdb.Close()
			rdb = nil
		} else {
			health.AddCheck("redis", func(ctx context.Context) error {
				return rdb.Ping(ctx).Err()
			})
		}
	}

	return &server{
		logger:     logger,
		metrics:    metrics,
		health:     health,
		db:         db,
		metadataDB: db,
		redis:      rdb,
		nc:         nc,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		authSecret:  authSecret,
		rateLimiter: newRateLimiter(100, time.Minute),
	}, nil
}

func (s *server) shutdown() {
	if s.db != nil {
		s.db.Close()
	}
	if s.metadataDB != nil {
		s.metadataDB.Close()
	}
	if s.nc != nil {
		s.nc.Close()
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// Observability endpoints
	mux.HandleFunc("/metrics", s.metrics.Handler())
	mux.HandleFunc("/health/live", s.health.LivenessHandler())
	mux.HandleFunc("/health/ready", s.health.ReadinessHandler())
	// Health endpoints (both with and without /api prefix for compatibility)
	mux.HandleFunc("/health", s.cors(s.handleHealth))
	mux.HandleFunc("/api/health", s.cors(s.handleHealth))
	// API endpoints
	mux.HandleFunc("/api/alerts", s.cors(s.rateLimit(s.authOptional(s.handleAlerts))))
	mux.HandleFunc("/api/metrics/", s.cors(s.rateLimit(s.authOptional(s.handleMetrics))))
	mux.HandleFunc("/api/klines", s.cors(s.rateLimit(s.authOptional(s.handleKlines))))
	mux.HandleFunc("/api/tickers", s.cors(s.rateLimit(s.authOptional(s.handleTickers))))
	mux.HandleFunc("/api/settings", s.cors(s.rateLimit(s.authRequired(s.handleSettings))))
	mux.HandleFunc("/ws/alerts", s.cors(s.authOptional(s.handleAlertsWS)))
	return mux
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbErr := s.db.Ping(ctx)
	natsOK := s.nc.Status() == nats.CONNECTED

	resp := map[string]interface{}{
		"status":  "ok",
		"db":      dbErr == nil,
		"nats":    natsOK,
		"version": "alerts-first",
	}
	if dbErr != nil {
		resp["db_error"] = dbErr.Error()
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	symbol := strings.TrimSpace(q.Get("symbol"))
	ruleType := strings.TrimSpace(q.Get("rule_type"))
	sinceStr := strings.TrimSpace(q.Get("since"))
	limit := clamp(toInt(q.Get("limit"), 100), 1, 500)

	filters := []string{"1=1"}
	args := []interface{}{}
	idx := 1

	if symbol != "" {
		filters = append(filters, "symbol = $"+strconv.Itoa(idx))
		args = append(args, symbol)
		idx++
	}
	if ruleType != "" {
		filters = append(filters, "rule_type = $"+strconv.Itoa(idx))
		args = append(args, ruleType)
		idx++
	}
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			filters = append(filters, "time >= $"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
	}

	query := "SELECT time, symbol, rule_type, description, price, metadata FROM alert_history WHERE " + strings.Join(filters, " AND ") + " ORDER BY time DESC LIMIT $" + strconv.Itoa(idx)
	args = append(args, limit)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	defer rows.Close()

	var results []alerts.Alert
	for rows.Next() {
		var a alerts.Alert
		var metadata []byte
		if err := rows.Scan(&a.Timestamp, &a.Symbol, &a.RuleType, &a.Description, &a.Price, &metadata); err != nil {
			s.writeError(w, http.StatusInternalServerError, "scan_failed", err.Error())
			return
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &a.Metadata)
		}
		results = append(results, a)
	}

	s.writeJSON(w, http.StatusOK, results)
}

func (s *server) handleAlertsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", err)
		return
	}

	const pongWait = 30 * time.Second
	const pingPeriod = 20 * time.Second
	const writeWait = 10 * time.Second

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub, err := s.nc.SubscribeSync("alerts.triggered")
	if err != nil {
		s.logger.Error("NATS subscribe failed", err)
		_ = conn.Close()
		return
	}
	defer sub.Unsubscribe()

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	var writeMu sync.Mutex
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait))
				writeMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read pump to detect client disconnects and handle pong frames
	// This goroutine must exist for SetPongHandler to work
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			// Client sent a message (could be keepalive JSON) - reset deadline
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		}
	}()

	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			break
		}
		writeMu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		err = conn.WriteMessage(websocket.TextMessage, msg.Data)
		writeMu.Unlock()
		if err != nil {
			break
		}
	}

	_ = conn.Close()
}

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Extract symbol from path: /api/metrics/{symbol}
	path := strings.TrimPrefix(r.URL.Path, "/api/metrics/")
	symbol := strings.ToUpper(strings.TrimSpace(path))

	// If no symbol, return all symbols' metrics
	if symbol == "" {
		s.handleAllMetrics(w, r)
		return
	}

	// Query latest metrics for all timeframes
	query := `
		SELECT 
			timeframe, time, open, high, low, close, volume,
			vcp, rsi_14, macd, macd_signal,
			bb_upper, bb_middle, bb_lower,
			fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3
		FROM metrics_calculated
		WHERE symbol = $1
			AND time >= NOW() - INTERVAL '1 hour'
		ORDER BY time DESC, timeframe
		LIMIT 6
	`

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, query, symbol)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	defer rows.Close()

	type MetricsData struct {
		Open       float64                `json:"open"`
		High       float64                `json:"high"`
		Low        float64                `json:"low"`
		Close      float64                `json:"close"`
		Volume     float64                `json:"volume"`
		VCP        *float64               `json:"vcp,omitempty"`
		RSI14      *float64               `json:"rsi_14,omitempty"`
		MACD       *float64               `json:"macd,omitempty"`
		MACDSignal *float64               `json:"macd_signal,omitempty"`
		BBUpper    *float64               `json:"bb_upper,omitempty"`
		BBMiddle   *float64               `json:"bb_middle,omitempty"`
		BBLower    *float64               `json:"bb_lower,omitempty"`
		Fibonacci  map[string]interface{} `json:"fibonacci,omitempty"`
	}

	timeframes := make(map[string]MetricsData)
	var latestTime time.Time

	for rows.Next() {
		var tf string
		var t time.Time
		var m MetricsData
		var fibR3, fibR2, fibR1, fibPivot, fibS1, fibS2, fibS3 *float64

		if err := rows.Scan(&tf, &t, &m.Open, &m.High, &m.Low, &m.Close, &m.Volume,
			&m.VCP, &m.RSI14, &m.MACD, &m.MACDSignal,
			&m.BBUpper, &m.BBMiddle, &m.BBLower,
			&fibR3, &fibR2, &fibR1, &fibPivot, &fibS1, &fibS2, &fibS3); err != nil {
			s.writeError(w, http.StatusInternalServerError, "scan_failed", err.Error())
			return
		}

		if fibPivot != nil {
			m.Fibonacci = map[string]interface{}{
				"r3":    fibR3,
				"r2":    fibR2,
				"r1":    fibR1,
				"pivot": fibPivot,
				"s1":    fibS1,
				"s2":    fibS2,
				"s3":    fibS3,
			}
		}

		timeframes[tf] = m
		if t.After(latestTime) {
			latestTime = t
		}
	}

	if len(timeframes) == 0 {
		s.writeError(w, http.StatusNotFound, "not_found", "no metrics found for symbol")
		return
	}

	response := map[string]interface{}{
		"symbol":     symbol,
		"timestamp":  latestTime,
		"timeframes": timeframes,
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *server) handleAllMetrics(w http.ResponseWriter, r *http.Request) {
	// Query latest metrics for all symbols across all timeframes
	query := `
		WITH latest_metrics AS (
			SELECT DISTINCT ON (symbol, timeframe)
				symbol, timeframe, time, open, high, low, close, volume,
				price_change, volume_ratio,
				vcp, rsi_14, macd, macd_signal,
				bb_upper, bb_middle, bb_lower,
				fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3
			FROM metrics_calculated
			WHERE time >= NOW() - INTERVAL '1 hour'
			ORDER BY symbol, timeframe, time DESC
		)
		SELECT * FROM latest_metrics ORDER BY symbol, timeframe
	`

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	defer rows.Close()

	type MetricsData struct {
		Time        time.Time              `json:"time"`
		Open        float64                `json:"open"`
		High        float64                `json:"high"`
		Low         float64                `json:"low"`
		Close       float64                `json:"close"`
		Volume      float64                `json:"volume"`
		PriceChange *float64               `json:"price_change,omitempty"`
		VolumeRatio *float64               `json:"volume_ratio,omitempty"`
		VCP         *float64               `json:"vcp,omitempty"`
		RSI14       *float64               `json:"rsi_14,omitempty"`
		MACD        *float64               `json:"macd,omitempty"`
		MACDSignal  *float64               `json:"macd_signal,omitempty"`
		BBUpper     *float64               `json:"bb_upper,omitempty"`
		BBMiddle    *float64               `json:"bb_middle,omitempty"`
		BBLower     *float64               `json:"bb_lower,omitempty"`
		Fibonacci   map[string]interface{} `json:"fibonacci,omitempty"`
	}

	type SymbolMetrics struct {
		Symbol     string                 `json:"symbol"`
		Timeframes map[string]MetricsData `json:"timeframes"`
	}

	symbolsMap := make(map[string]*SymbolMetrics)

	for rows.Next() {
		var symbol, tf string
		var t time.Time
		var m MetricsData
		var fibR3, fibR2, fibR1, fibPivot, fibS1, fibS2, fibS3 *float64

		if err := rows.Scan(&symbol, &tf, &t, &m.Open, &m.High, &m.Low, &m.Close, &m.Volume,
			&m.PriceChange, &m.VolumeRatio,
			&m.VCP, &m.RSI14, &m.MACD, &m.MACDSignal,
			&m.BBUpper, &m.BBMiddle, &m.BBLower,
			&fibR3, &fibR2, &fibR1, &fibPivot, &fibS1, &fibS2, &fibS3); err != nil {
			s.writeError(w, http.StatusInternalServerError, "scan_failed", err.Error())
			return
		}

		m.Time = t
		if fibPivot != nil {
			m.Fibonacci = map[string]interface{}{
				"r3":    fibR3,
				"r2":    fibR2,
				"r1":    fibR1,
				"pivot": fibPivot,
				"s1":    fibS1,
				"s2":    fibS2,
				"s3":    fibS3,
			}
		}

		if _, exists := symbolsMap[symbol]; !exists {
			symbolsMap[symbol] = &SymbolMetrics{
				Symbol:     symbol,
				Timeframes: make(map[string]MetricsData),
			}
		}
		symbolsMap[symbol].Timeframes[tf] = m
	}

	// Convert map to array
	result := make([]SymbolMetrics, 0, len(symbolsMap))
	for _, sm := range symbolsMap {
		result = append(result, *sm)
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *server) handleKlines(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	symbol := strings.ToUpper(strings.TrimSpace(q.Get("symbol")))
	interval := strings.TrimSpace(q.Get("interval"))
	limit := clamp(toInt(q.Get("limit"), 500), 1, 1500)

	if symbol == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "symbol is required")
		return
	}
	if interval == "" || !isValidKlineInterval(interval) {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "invalid interval")
		return
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	binanceURL := "https://fapi.binance.com/fapi/v1/klines?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, binanceURL, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "request_failed", err.Error())
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "upstream_failed", err.Error())
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *server) handleTickers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	requested := strings.TrimSpace(q.Get("symbols"))
	var symbolSet map[string]struct{}
	if requested != "" {
		symbolSet = make(map[string]struct{})
		for _, raw := range strings.Split(requested, ",") {
			symbol := strings.ToUpper(strings.TrimSpace(raw))
			if symbol != "" {
				symbolSet[symbol] = struct{}{}
			}
		}
	}

	// Serve from Redis cache when available
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		var payload []map[string]interface{}
		if symbolSet != nil {
			symbols := make([]string, 0, len(symbolSet))
			for symbol := range symbolSet {
				symbols = append(symbols, symbol)
			}
			values, err := s.redis.HMGet(ctx, "tickers", symbols...).Result()
			if err == nil {
				for _, value := range values {
					if value == nil {
						continue
					}
					str, ok := value.(string)
					if !ok {
						continue
					}
					var item map[string]interface{}
					if err := json.Unmarshal([]byte(str), &item); err == nil {
						payload = append(payload, item)
					}
				}
			}
		} else {
			values, err := s.redis.HGetAll(ctx, "tickers").Result()
			if err == nil {
				payload = make([]map[string]interface{}, 0, len(values))
				for _, str := range values {
					var item map[string]interface{}
					if err := json.Unmarshal([]byte(str), &item); err == nil {
						payload = append(payload, item)
					}
				}
			}
		}

		if len(payload) > 0 {
			s.writeJSON(w, http.StatusOK, payload)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	binanceURL := "https://fapi.binance.com/fapi/v1/ticker/24hr"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, binanceURL, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "request_failed", err.Error())
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "upstream_failed", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}

	var payload []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		s.writeError(w, http.StatusBadGateway, "decode_failed", err.Error())
		return
	}

	if symbolSet != nil {
		filtered := make([]map[string]interface{}, 0, len(symbolSet))
		for _, item := range payload {
			symbolValue, ok := item["symbol"].(string)
			if !ok {
				continue
			}
			if _, exists := symbolSet[symbolValue]; exists {
				filtered = append(filtered, item)
			}
		}
		payload = filtered
	}

	s.writeJSON(w, http.StatusOK, payload)
}

func isValidKlineInterval(interval string) bool {
	switch interval {
	case "1m", "3m", "5m", "15m", "30m",
		"1h", "2h", "4h", "6h", "8h", "12h",
		"1d", "3d", "1w", "1M":
		return true
	default:
		return false
	}
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.handleGetSettings(w, r)
	} else if r.Method == http.MethodPost {
		s.handleSaveSettings(w, r)
	} else {
		s.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST allowed")
	}
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id")
	if userID == nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "user_id not found")
		return
	}

	query := `
		SELECT selected_alerts, webhook_url, notification_enabled
		FROM user_settings
		WHERE user_id = $1
	`

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var selectedAlerts []string
	var webhookURL *string
	var notificationEnabled bool

	err := s.metadataDB.QueryRow(ctx, query, userID).Scan(&selectedAlerts, &webhookURL, &notificationEnabled)
	if err != nil {
		// No settings found, return defaults
		response := map[string]interface{}{
			"selected_alerts":      []string{},
			"webhook_url":          nil,
			"notification_enabled": true,
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}

	response := map[string]interface{}{
		"selected_alerts":      selectedAlerts,
		"webhook_url":          webhookURL,
		"notification_enabled": notificationEnabled,
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id")
	if userID == nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "user_id not found")
		return
	}

	var req struct {
		SelectedAlerts      []string `json:"selected_alerts"`
		WebhookURL          *string  `json:"webhook_url"`
		NotificationEnabled bool     `json:"notification_enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	query := `
		INSERT INTO user_settings (user_id, selected_alerts, webhook_url, notification_enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			selected_alerts = EXCLUDED.selected_alerts,
			webhook_url = EXCLUDED.webhook_url,
			notification_enabled = EXCLUDED.notification_enabled,
			updated_at = NOW()
	`

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if _, err := s.metadataDB.Exec(ctx, query, userID, req.SelectedAlerts, req.WebhookURL, req.NotificationEnabled); err != nil {
		s.writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}

	response := map[string]interface{}{
		"success":              true,
		"selected_alerts":      req.SelectedAlerts,
		"webhook_url":          req.WebhookURL,
		"notification_enabled": req.NotificationEnabled,
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *server) authOptional(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authSecret == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := extractBearer(r.Header.Get("Authorization"))
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Placeholder: we only check presence when secret configured. Full JWT verify can be added later.
		// In production, decode JWT and extract user_id
		next.ServeHTTP(w, r)
	}
}

func (s *server) authRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r.Header.Get("Authorization"))
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		// Placeholder: In production, decode JWT, verify signature, and extract user_id
		// For now, use a dummy user_id for testing
		ctx := context.WithValue(r.Context(), "user_id", "test-user-id")
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func (s *server) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		if !s.rateLimiter.Allow(ip) {
			s.writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	}
}

func (s *server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func toInt(val string, def int) int {
	v, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return v
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func extractBearer(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func extractIP(r *http.Request) string {
	// Check X-Forwarded-For header first (proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to remote address
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// rateLimiter implements a simple token bucket rate limiter per IP
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	maxRate  int
	interval time.Duration
}

type bucket struct {
	tokens     int
	lastRefill time.Time
}

func newRateLimiter(maxRate int, interval time.Duration) *rateLimiter {
	rl := &rateLimiter{
		buckets:  make(map[string]*bucket),
		maxRate:  maxRate,
		interval: interval,
	}
	// Cleanup old buckets every 10 minutes
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *rateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[ip]
	if !exists {
		b = &bucket{
			tokens:     rl.maxRate,
			lastRefill: time.Now(),
		}
		rl.buckets[ip] = b
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	if elapsed >= rl.interval {
		b.tokens = rl.maxRate
		b.lastRefill = now
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, b := range rl.buckets {
		if now.Sub(b.lastRefill) > 1*time.Hour {
			delete(rl.buckets, ip)
		}
	}
}
