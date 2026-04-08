package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// WebhookNotifier dispatches alerts to webhook endpoints.
// Supports Slack, Teams, Discord, and generic JSON.
type WebhookNotifier struct {
	httpClient *http.Client
}

// NewWebhookNotifier creates a notifier with a default HTTP client.
func NewWebhookNotifier() *WebhookNotifier {
	return &WebhookNotifier{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Notify sends an alert to a single webhook.
func (n *WebhookNotifier) Notify(ctx context.Context, wh model.WebhookConfig, alert model.Alert) error {
	var body []byte
	var contentType = "application/json"

	switch strings.ToLower(wh.WebhookType) {
	case "slack":
		body = buildSlackPayload(alert)
	case "teams":
		body = buildTeamsPayload(alert)
	case "discord":
		body = buildDiscordPayload(alert)
	case "generic", "":
		body = buildGenericPayload(alert)
	default:
		return fmt.Errorf("unsupported webhook type: %s", wh.WebhookType)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "as-stats-alerting/1.0")

	// Custom headers from webhook config (JSON)
	if wh.Headers != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(wh.Headers), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// -----------------------------------------------------------------------------
// Payload builders
// -----------------------------------------------------------------------------

func severityEmoji(sev string) string {
	switch sev {
	case "critical":
		return "🚨"
	case "warning":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

func severityColor(sev string) string {
	switch sev {
	case "critical":
		return "#e74c3c" // red
	case "warning":
		return "#f39c12" // orange
	default:
		return "#3498db" // blue
	}
}

func formatMetric(metricType string, value float64) string {
	switch metricType {
	case "bps":
		return formatBps(value)
	case "pps":
		return formatPps(value)
	case "count":
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

func formatBps(v float64) string {
	units := []string{"bps", "Kbps", "Mbps", "Gbps", "Tbps"}
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func formatPps(v float64) string {
	units := []string{"pps", "Kpps", "Mpps", "Gpps"}
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

// Slack incoming webhook format (Block Kit)
func buildSlackPayload(a model.Alert) []byte {
	title := fmt.Sprintf("%s %s — %s", severityEmoji(a.Severity), strings.ToUpper(a.Severity), a.RuleName)
	metric := formatMetric(a.MetricType, a.MetricValue)
	threshold := formatMetric(a.MetricType, a.Threshold)

	payload := map[string]any{
		"text": title,
		"attachments": []map[string]any{
			{
				"color": severityColor(a.Severity),
				"fields": []map[string]any{
					{"title": "Target", "value": a.TargetIP, "short": true},
					{"title": "Rule", "value": a.RuleName, "short": true},
					{"title": "Value", "value": metric, "short": true},
					{"title": "Threshold", "value": threshold, "short": true},
					{"title": "Triggered", "value": a.TriggeredAt.Format(time.RFC3339), "short": false},
				},
				"footer":    "AS-Stats Alerts",
				"timestamp": a.TriggeredAt.Unix(),
			},
		},
	}

	b, _ := json.Marshal(payload)
	return b
}

// Microsoft Teams MessageCard format (legacy but widely supported)
func buildTeamsPayload(a model.Alert) []byte {
	title := fmt.Sprintf("%s %s: %s", severityEmoji(a.Severity), strings.ToUpper(a.Severity), a.RuleName)
	metric := formatMetric(a.MetricType, a.MetricValue)
	threshold := formatMetric(a.MetricType, a.Threshold)

	payload := map[string]any{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": strings.TrimPrefix(severityColor(a.Severity), "#"),
		"summary":    title,
		"title":      title,
		"sections": []map[string]any{
			{
				"facts": []map[string]string{
					{"name": "Target", "value": a.TargetIP},
					{"name": "Rule", "value": a.RuleName},
					{"name": "Value", "value": metric},
					{"name": "Threshold", "value": threshold},
					{"name": "Triggered", "value": a.TriggeredAt.Format(time.RFC3339)},
				},
				"markdown": true,
			},
		},
	}

	b, _ := json.Marshal(payload)
	return b
}

// Discord webhook format (embeds)
func buildDiscordPayload(a model.Alert) []byte {
	title := fmt.Sprintf("%s %s — %s", severityEmoji(a.Severity), strings.ToUpper(a.Severity), a.RuleName)
	metric := formatMetric(a.MetricType, a.MetricValue)
	threshold := formatMetric(a.MetricType, a.Threshold)

	colorInt := 0x3498db
	switch a.Severity {
	case "critical":
		colorInt = 0xe74c3c
	case "warning":
		colorInt = 0xf39c12
	}

	payload := map[string]any{
		"content": title,
		"embeds": []map[string]any{
			{
				"title": a.RuleName,
				"color": colorInt,
				"fields": []map[string]any{
					{"name": "Target", "value": a.TargetIP, "inline": true},
					{"name": "Severity", "value": a.Severity, "inline": true},
					{"name": "Value", "value": metric, "inline": true},
					{"name": "Threshold", "value": threshold, "inline": true},
				},
				"timestamp": a.TriggeredAt.Format(time.RFC3339),
				"footer":    map[string]string{"text": "AS-Stats Alerts"},
			},
		},
	}

	b, _ := json.Marshal(payload)
	return b
}

// Generic JSON: just dumps the alert
func buildGenericPayload(a model.Alert) []byte {
	b, _ := json.Marshal(a)
	return b
}
