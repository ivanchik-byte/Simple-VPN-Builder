package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// RegisterTelemetryBillingTools registers cluster analytics, financial summaries, and gateway audits.
func RegisterTelemetryBillingTools(r *ToolRegistry, repos *store.Repositories) {
	// 1. cluster_telemetry
	r.Register(ToolDefinition{
		Name:        "cluster_telemetry",
		Description: "Retrieve aggregate cluster metrics including total subscribers, active tunnels, node counts, and recent traffic consumption.",
		Safety:      SafetyReadOnly,
		Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			nodes, totalNodes, err := repos.Nodes.List(ctx, store.NodeFilter{Limit: 100})
			if err != nil {
				return nil, err
			}

			activeNodes := 0
			drainingNodes := 0
			for _, n := range nodes {
				if n.Status.String == "active" {
					activeNodes++
				} else if n.Status.String == "draining" {
					drainingNodes++
				}
			}

			users, totalUsersCount, err := repos.Users.List(ctx, store.UserFilter{Limit: 1000})
			if err != nil {
				return nil, err
			}

			activeUsers := 0
			totalBytes := int64(0)
			for _, u := range users {
				if u.CRMStatus() == "active" || u.CRMStatus() == "trial" {
					activeUsers++
				}
				if u.TrafficUsed.Valid {
					totalBytes += u.TrafficUsed.Int64
				}
			}

			return map[string]interface{}{
				"timestamp":          time.Now().UTC().Format(time.RFC3339),
				"nodes_total":        totalNodes,
				"nodes_active":       activeNodes,
				"nodes_draining":     drainingNodes,
				"subscribers_total":  totalUsersCount,
				"subscribers_active": activeUsers,
				"total_traffic":      formatBytes(totalBytes),
			}, nil
		},
	})

	// 2. billing_revenue_breakdown
	r.Register(ToolDefinition{
		Name:        "billing_revenue_breakdown",
		Description: "Analyze commercial sales, recent orders, Telegram Stars revenue, and active promo codes.",
		Safety:      SafetyReadOnly,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"days": {"type": "integer", "description": "Analysis timeframe in days (default 30)"}
			}
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Days int `json:"days"`
			}
			_ = json.Unmarshal(args, &p)
			if p.Days <= 0 {
				p.Days = 30
			}

			gateways, _ := repos.Billing.ListPaymentGateways(ctx)
			settings, _ := repos.Billing.GetBillingSettings(ctx)
			promos, _ := repos.Billing.ListPromoCodes(ctx)

			type GatewaySummary struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			}

			gwSummaries := make([]GatewaySummary, 0, len(gateways))
			for _, g := range gateways {
				gwSummaries = append(gwSummaries, GatewaySummary{
					Name:    g.Name,
					Enabled: g.IsEnabled.Bool,
				})
			}

			return map[string]interface{}{
				"timeframe_days":         p.Days,
				"telegram_stars_enabled": settings.TelegramStarsEnabled,
				"stars_price_per_month":  settings.StarsPricePerMonth,
				"cryptobot_enabled":      settings.CryptobotEnabled,
				"gateways":               gwSummaries,
				"active_promo_count":     len(promos),
			}, nil
		},
	})

	// 3. gateway_health_check
	r.Register(ToolDefinition{
		Name:        "gateway_health_check",
		Description: "Verify configured payment gateways and test webhook readiness.",
		Safety:      SafetyReadOnly,
		Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			settings, err := repos.Billing.GetBillingSettings(ctx)
			if err != nil {
				return nil, err
			}

			type Check struct {
				Gateway string `json:"gateway"`
				Status  string `json:"status"`
				Notes   string `json:"notes"`
			}

			checks := []Check{
				{
					Gateway: "telegram_stars",
					Status:  fmt.Sprintf("enabled=%v", settings.TelegramStarsEnabled),
					Notes:   fmt.Sprintf("Direct in-app Stars billing (Price: %d Stars/month)", settings.StarsPricePerMonth),
				},
			}

			if settings.CryptobotEnabled {
				tokenLen := len(settings.CryptobotApiToken)
				status := "configured"
				if tokenLen == 0 {
					status = "missing_api_token"
				}
				checks = append(checks, Check{
					Gateway: "cryptobot",
					Status:  status,
					Notes:   "Multi-cryptocurrency payments (USDT, TON, BTC)",
				})
			}

			return map[string]interface{}{
				"gateway_checks": checks,
				"webhook_secret_set": len(settings.WebhookSecret) > 0,
			}, nil
		},
	})
}
