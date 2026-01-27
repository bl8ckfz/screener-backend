package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bl8ckfz/crypto-screener-backend/internal/alerts"
	"github.com/gorilla/websocket"
)

// Integration-style test: verifies REST and WS surfaces emit alerts from DB/NATS.
func TestAlertsGatewayRESTAndWS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Default local endpoints
	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("TIMESCALE_URL", "postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable")

	// Prepare test alert payload
	alert := alerts.Alert{
		ID:          "test-alert-1",
		Symbol:      "TESTUSDT",
		RuleType:    "test_rule",
		Description: "test alert description",
		Timestamp:   time.Now().UTC().Truncate(time.Second),
		Price:       123.45,
		Metadata: map[string]interface{}{
			"price_change_5m":  1.1,
			"price_change_15m": 2.2,
			"volume_5m":        1000,
			"vcp":              0.5,
		},
	}

	srv, err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer srv.shutdown()

	// Insert alert into alert_history for REST path
	insert := `INSERT INTO alert_history (time, symbol, rule_type, description, price, metadata) VALUES ($1,$2,$3,$4,$5,$6)`
	metaJSON, _ := json.Marshal(alert.Metadata)
	if _, err := srv.db.Exec(ctx, insert, alert.Timestamp, alert.Symbol, alert.RuleType, alert.Description, alert.Price, metaJSON); err != nil {
		t.Fatalf("failed to insert alert_history: %v", err)
	}
	t.Cleanup(func() {
		srv.db.Exec(context.Background(), "DELETE FROM alert_history WHERE symbol=$1 AND rule_type=$2 AND price=$3", alert.Symbol, alert.RuleType, alert.Price)
	})

	// Start HTTP test server
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// REST: fetch alerts
	restURL := ts.URL + "/api/alerts?symbol=" + url.QueryEscape(alert.Symbol)
	resp, err := http.Get(restURL)
	if err != nil {
		t.Fatalf("REST request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("REST status = %d", resp.StatusCode)
	}
	var got []alerts.Alert
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one alert in REST response")
	}

	// WS: connect and receive the published alert
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/alerts"
	d := websocket.Dialer{}
	conn, _, err := d.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS dial failed: %v", err)
	}
	defer conn.Close()

	// Publish after WS subscription is active
	payload, _ := json.Marshal(alert)
	if err := srv.nc.Publish("alerts.triggered", payload); err != nil {
		t.Fatalf("failed to publish alert: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read failed: %v", err)
	}
	var wsAlert alerts.Alert
	if err := json.Unmarshal(msg, &wsAlert); err != nil {
		t.Fatalf("unmarshal WS alert: %v", err)
	}
	if wsAlert.ID != alert.ID || wsAlert.Symbol != alert.Symbol || wsAlert.RuleType != alert.RuleType {
		t.Fatalf("unexpected WS alert: %+v", wsAlert)
	}
}