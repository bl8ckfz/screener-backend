package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/alerts"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/database"
	"github.com/bl8ckfz/crypto-screener-backend/pkg/messaging"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type server struct {
	logger      zerolog.Logger
	db          *pgxpool.Pool
	nc          *nats.Conn
	upgrader    websocket.Upgrader
	authSecret  string
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	log.Info().Msg("Starting API Gateway service")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Info().Msg("Shutdown signal received")
		cancel()
	}()

	srv, err := bootstrap(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to bootstrap API Gateway")
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
		log.Info().Str("addr", addr).Msg("API Gateway listening")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Graceful shutdown failed")
	}
	log.Info().Msg("API Gateway service stopped")
}

func bootstrap(ctx context.Context) (*server, error) {
	natsURL := getenv("NATS_URL", "nats://localhost:4222")
	tsdbURL := getenv("TIMESCALE_URL", "postgres://crypto_user:crypto_pass@localhost:5432/crypto_timeseries")
	authSecret := os.Getenv("SUPABASE_JWT_SECRET")

	nc, err := messaging.NewNATSConn(messaging.Config{URL: natsURL, MaxReconnects: -1, ReconnectWait: 2 * time.Second, EnableJetStream: true})
	if err != nil {
		return nil, err
	}

	db, err := database.NewPostgresPool(ctx, tsdbURL)
	if err != nil {
		nc.Close()
		return nil, err
	}

	return &server{
		logger: log.Logger,
		db:     db,
		nc:     nc,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		authSecret: authSecret,
	}, nil
}

func (s *server) shutdown() {
	if s.db != nil {
		s.db.Close()
	}
	if s.nc != nil {
		s.nc.Close()
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/alerts", s.authOptional(s.handleAlerts))
	mux.HandleFunc("/ws/alerts", s.authOptional(s.handleAlertsWS))
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
		s.logger.Error().Err(err).Msg("websocket upgrade failed")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub, err := s.nc.SubscribeSync("alerts.triggered")
	if err != nil {
		s.logger.Error().Err(err).Msg("NATS subscribe failed")
		_ = conn.Close()
		return
	}
	defer sub.Unsubscribe()

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			break
		}
		if err := conn.WriteMessage(websocket.TextMessage, msg.Data); err != nil {
			break
		}
	}

	_ = conn.Close()
}

func (s *server) authOptional(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authSecret == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := extractBearer(r.Header.Get("Authorization"))
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		// Placeholder: we only check presence when secret configured. Full JWT verify can be added later.
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
