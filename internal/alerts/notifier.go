package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Notifier handles sending alert notifications to external services
type Notifier struct {
	httpClient  *http.Client
	webhookURLs []string
	enabled     bool
	logger      zerolog.Logger
}

// NewNotifier creates a new webhook notifier
func NewNotifier(webhookURLs []string, logger zerolog.Logger) *Notifier {
	return &Notifier{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		webhookURLs: webhookURLs,
		enabled:     len(webhookURLs) > 0,
		logger:      logger,
	}
}

// SendAlert sends an alert to all configured webhooks
func (n *Notifier) SendAlert(alert *Alert) error {
	if !n.enabled {
		return nil
	}

	for _, webhookURL := range n.webhookURLs {
		if err := n.sendWebhook(webhookURL, alert); err != nil {
			n.logger.Error().
				Err(err).
				Str("webhook", webhookURL).
				Str("symbol", alert.Symbol).
				Str("rule", alert.RuleType).
				Msg("Failed to send webhook")
			continue
		}

		n.logger.Debug().
			Str("webhook", webhookURL).
			Str("symbol", alert.Symbol).
			Str("rule", alert.RuleType).
			Msg("Webhook sent successfully")
	}

	return nil
}

// sendWebhook sends alert to a specific webhook URL
func (n *Notifier) sendWebhook(webhookURL string, alert *Alert) error {
	// Format the alert for Discord/Telegram
	payload := n.formatPayload(alert)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	return nil
}

// formatPayload formats alert for Discord/Telegram webhooks
func (n *Notifier) formatPayload(alert *Alert) map[string]interface{} {
	// Determine color based on alert type
	color := n.getAlertColor(alert.RuleType)
	emoji := n.getAlertEmoji(alert.RuleType)

	// Format the alert message
	title := fmt.Sprintf("%s %s", emoji, alert.Symbol)

	// Build fields with alert details
	fields := []map[string]interface{}{
		{
			"name":   "Price",
			"value":  fmt.Sprintf("$%.6f", alert.Price),
			"inline": true,
		},
		{
			"name":   "Time",
			"value":  alert.Timestamp.Format("15:04:05 UTC"),
			"inline": true,
		},
	}

	// Add metadata fields if available
	if alert.Metadata != nil {
		if change5m, ok := alert.Metadata["change_5m"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "5m Change",
				"value":  fmt.Sprintf("%.2f%%", change5m),
				"inline": true,
			})
		}

		if change15m, ok := alert.Metadata["change_15m"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "15m Change",
				"value":  fmt.Sprintf("%.2f%%", change15m),
				"inline": true,
			})
		}

		if change1h, ok := alert.Metadata["change_1h"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "1h Change",
				"value":  fmt.Sprintf("%.2f%%", change1h),
				"inline": true,
			})
		}

		if change8h, ok := alert.Metadata["change_8h"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "8h Change",
				"value":  fmt.Sprintf("%.2f%%", change8h),
				"inline": true,
			})
		}

		if volume1h, ok := alert.Metadata["volume_1h"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "1h Volume",
				"value":  fmt.Sprintf("%.0f", volume1h),
				"inline": true,
			})
		}

		if vcp, ok := alert.Metadata["vcp"].(float64); ok {
			fields = append(fields, map[string]interface{}{
				"name":   "VCP",
				"value":  fmt.Sprintf("%.3f", vcp),
				"inline": true,
			})
		}
	}

	// Discord embed format (also works for many Telegram bots)
	return map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": alert.Description,
				"color":       color,
				"fields":      fields,
				"timestamp":   alert.Timestamp.Format(time.RFC3339),
				"footer": map[string]interface{}{
					"text": "Crypto Screener Alert",
				},
			},
		},
	}
}

// getAlertColor returns embed color based on alert type
func (n *Notifier) getAlertColor(ruleType string) int {
	switch ruleType {
	case "futures_big_bull_60", "futures_pioneer_bull", "futures_5_big_bull",
		"futures_15_big_bull", "futures_bottom_hunter":
		return 0x00FF00 // Green for bullish
	case "futures_big_bear_60", "futures_pioneer_bear", "futures_5_big_bear",
		"futures_15_big_bear", "futures_top_hunter":
		return 0xFF0000 // Red for bearish
	default:
		return 0x0099FF // Blue for others
	}
}

// getAlertEmoji returns emoji based on alert type
func (n *Notifier) getAlertEmoji(ruleType string) string {
	switch ruleType {
	case "futures_big_bull_60":
		return "🚨 Big Bull 60m"
	case "futures_big_bear_60":
		return "🚨 Big Bear 60m"
	case "futures_pioneer_bull":
		return "🔔 Pioneer Bull"
	case "futures_pioneer_bear":
		return "🔔 Pioneer Bear"
	case "futures_5_big_bull":
		return "⚡ 5m Big Bull"
	case "futures_5_big_bear":
		return "⚡ 5m Big Bear"
	case "futures_15_big_bull":
		return "📈 15m Big Bull"
	case "futures_15_big_bear":
		return "📉 15m Big Bear"
	case "futures_bottom_hunter":
		return "🎯 Bottom Hunter"
	case "futures_top_hunter":
		return "🎯 Top Hunter"
	default:
		return "🔔"
	}
}
