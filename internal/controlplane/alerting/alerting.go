package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// AlertTopic defines categories for alert routing.
type AlertTopic string

const (
	TopicInfra   AlertTopic = "infra"
	TopicAudit   AlertTopic = "audit"
	TopicBilling AlertTopic = "billing"
)

// AlertConfig holds Telegram alerting settings.
type AlertConfig struct {
	BotToken     string `json:"bot_token"`
	ChatID       int64  `json:"chat_id"`
	TopicInfra   int    `json:"topic_infra"`
	TopicAudit   int    `json:"topic_audit"`
	TopicBilling int    `json:"topic_billing"`
	Enabled      bool   `json:"enabled"`
}

// AlertEvent represents a single event to be dispatched.
type AlertEvent struct {
	Topic   AlertTopic
	Title   string
	Message string
}

// AlertDispatcher handles asynchronous Telegram alerts with forum topic routing.
type AlertDispatcher struct {
	mu         sync.RWMutex
	config     AlertConfig
	httpClient *http.Client
	eventCh    chan AlertEvent
	stopCh     chan struct{}
	baseURL    string
}

// NewAlertDispatcher creates and starts an AlertDispatcher.
func NewAlertDispatcher(cfg AlertConfig) *AlertDispatcher {
	d := &AlertDispatcher{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		eventCh: make(chan AlertEvent, 256),
		stopCh:  make(chan struct{}),
		baseURL: "https://api.telegram.org",
	}
	go d.worker()
	return d
}

// SetBaseURL allows overriding the Telegram API base URL (useful for testing).
func (d *AlertDispatcher) SetBaseURL(url string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baseURL = url
}

// UpdateConfig dynamically updates the alerting configuration.
func (d *AlertDispatcher) UpdateConfig(cfg AlertConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.config = cfg
}

// GetConfig returns the current alerting configuration copy.
func (d *AlertDispatcher) GetConfig() AlertConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.config
}

// Stop shuts down the background worker.
func (d *AlertDispatcher) Stop() {
	close(d.stopCh)
}

// Dispatch queues an alert event without blocking caller.
func (d *AlertDispatcher) Dispatch(event AlertEvent) {
	d.mu.RLock()
	enabled := d.config.Enabled && d.config.BotToken != "" && d.config.ChatID != 0
	d.mu.RUnlock()

	if !enabled {
		return
	}

	select {
	case d.eventCh <- event:
	default:
		logger.WarnContext(context.Background(), "alert dispatcher queue full, dropping alert", "title", event.Title)
	}
}

// SendInfraAlert dispatches an infrastructure alert (node status, heartbeat).
func (d *AlertDispatcher) SendInfraAlert(nodeName string, status string, details string) {
	badge := "[ALERT]"
	if status == "online" {
		badge = "[RECOVERY]"
	} else if status == "degraded" {
		badge = "[WARNING]"
	}

	text := fmt.Sprintf("%s Node: %s\nStatus: %s\nDetails: %s\nTimestamp: %s",
		badge, nodeName, status, details, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	d.Dispatch(AlertEvent{
		Topic:   TopicInfra,
		Title:   fmt.Sprintf("%s Node %s %s", badge, nodeName, status),
		Message: text,
	})
}

// SendAuditAlert dispatches an admin action audit alert.
func (d *AlertDispatcher) SendAuditAlert(actor string, action string, target string, details string) {
	text := fmt.Sprintf("[AUDIT] Action: %s\nActor: %s\nTarget: %s\nDetails: %s\nTimestamp: %s",
		action, actor, target, details, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	d.Dispatch(AlertEvent{
		Topic:   TopicAudit,
		Title:   fmt.Sprintf("[AUDIT] %s by %s", action, actor),
		Message: text,
	})
}

// RBACDiffItem represents a single permission change item for alerting.
type RBACDiffItem struct {
	Field       string
	OldValue    bool
	NewValue    bool
	Granted     bool
	Severity    string
	Description string
}

// SendRBACAlert dispatches a high-priority security alert for administrator permission modifications or attempts.
func (d *AlertDispatcher) SendRBACAlert(actor, actorRole, target, targetRole, ip string, status string, changes []RBACDiffItem, reason string) {
	badge := "[RBAC PERMISSION CHANGED]"
	if status == "DENIED" {
		badge = "[SECURITY ALERT - PERMISSION ESCALATION DENIED]"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n", html.EscapeString(badge)))
	sb.WriteString(fmt.Sprintf("<b>Status:</b> %s\n", html.EscapeString(status)))
	sb.WriteString(fmt.Sprintf("<b>Actor:</b> <code>%s</code> (%s)\n", html.EscapeString(actor), html.EscapeString(actorRole)))
	sb.WriteString(fmt.Sprintf("<b>Target:</b> <code>%s</code> (%s)\n", html.EscapeString(target), html.EscapeString(targetRole)))
	if ip != "" {
		sb.WriteString(fmt.Sprintf("<b>IP Address:</b> <code>%s</code>\n", html.EscapeString(ip)))
	}
	sb.WriteString(fmt.Sprintf("<b>Timestamp:</b> %s\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC")))

	if reason != "" {
		sb.WriteString(fmt.Sprintf("<b>Details:</b> %s\n", html.EscapeString(reason)))
	}

	if len(changes) > 0 {
		sb.WriteString("\n<b>Permission Deltas:</b>\n<pre><code class=\"language-diff\">")
		for _, ch := range changes {
			if ch.Granted {
				sb.WriteString(fmt.Sprintf("+ [GRANTED] %s\n", ch.Field))
			} else {
				sb.WriteString(fmt.Sprintf("- [REVOKED] %s\n", ch.Field))
			}
		}
		sb.WriteString("</code></pre>")
	}

	d.Dispatch(AlertEvent{
		Topic:   TopicAudit,
		Title:   fmt.Sprintf("%s Target: %s", badge, target),
		Message: sb.String(),
	})
}

// SendBillingAlert dispatches a payment/billing alert.
func (d *AlertDispatcher) SendBillingAlert(gateway string, amount string, user string, invoiceID string) {
	text := fmt.Sprintf("[BILLING] Payment Received\nGateway: %s\nUser: %s\nAmount: %s\nInvoice: %s\nTimestamp: %s",
		gateway, user, amount, invoiceID, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	d.Dispatch(AlertEvent{
		Topic:   TopicBilling,
		Title:   fmt.Sprintf("[BILLING] Payment %s via %s", amount, gateway),
		Message: text,
	})
}

func (d *AlertDispatcher) worker() {
	for {
		select {
		case <-d.stopCh:
			return
		case ev := <-d.eventCh:
			d.sendTelegram(ev)
		}
	}
}

func (d *AlertDispatcher) sendTelegram(ev AlertEvent) {
	d.mu.RLock()
	cfg := d.config
	baseURL := d.baseURL
	d.mu.RUnlock()

	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == 0 {
		return
	}

	threadID := 0
	switch ev.Topic {
	case TopicInfra:
		threadID = cfg.TopicInfra
	case TopicAudit:
		threadID = cfg.TopicAudit
	case TopicBilling:
		threadID = cfg.TopicBilling
	}

	payload := map[string]any{
		"chat_id":    cfg.ChatID,
		"text":       ev.Message,
		"parse_mode": "HTML",
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", baseURL, cfg.BotToken)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		logger.ErrorContext(ctx, "failed to send telegram alert", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		logger.WarnContext(ctx, "telegram alert response error", "status", resp.StatusCode)
	}
}
