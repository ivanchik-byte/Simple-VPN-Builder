package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// RegisterBotTools registers telegram bot validation, partner tenant checks, and broadcast tools.
func RegisterBotTools(r *ToolRegistry, repos *store.Repositories, broadcastService *service.BroadcastService) {
	// 1. bot_status_check
	r.Register(ToolDefinition{
		Name:        "bot_status_check",
		Description: "Check the Telegram bot connection status, username, and token validity with Telegram API.",
		Safety:      SafetyReadOnly,
		Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			replies, err := repos.Billing.GetBotReplies(ctx)
			if err != nil {
				return nil, err
			}

			token := replies["bot_token"]
			if token == "" {
				return map[string]interface{}{
					"status":  "not_configured",
					"message": "Telegram Bot Token is not set in Admin Settings.",
				}, nil
			}

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token))
			if err != nil {
				return map[string]interface{}{
					"status":  "unreachable",
					"message": fmt.Sprintf("Error connecting to Telegram API: %v", err),
				}, nil
			}
			defer resp.Body.Close()

			var tgResp struct {
				Ok     bool `json:"ok"`
				Result struct {
					ID        int64  `json:"id"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				} `json:"result"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
				return nil, fmt.Errorf("failed to decode telegram response: %w", err)
			}

			if !tgResp.Ok {
				return map[string]interface{}{
					"status":  "invalid_token",
					"message": tgResp.Description,
				}, nil
			}

			return map[string]interface{}{
				"status":       "online",
				"bot_id":       tgResp.Result.ID,
				"bot_name":     tgResp.Result.FirstName,
				"bot_username": "@" + tgResp.Result.Username,
				"webhook_mode": "long_polling_or_webhook",
			}, nil
		},
	})

	// 2. broadcast_announcement
	r.Register(ToolDefinition{
		Name:        "broadcast_announcement",
		Description: "Broadcast an urgent maintenance notice, protocol update, or announcement to subscribers.",
		Safety:      SafetyMutating,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"target_segment": {"type": "string", "enum": ["all", "active", "trial", "expired"], "description": "Target audience segment"},
				"title": {"type": "string", "description": "Title of the campaign"},
				"message": {"type": "string", "description": "Message body text (supports HTML tags)"},
				"dry_run": {"type": "boolean", "description": "If true, simulates delivery and generates confirmation token without sending messages."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["target_segment", "title", "message"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				TargetSegment     string `json:"target_segment"`
				Title             string `json:"title"`
				Message           string `json:"message"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			tgIDs, err := repos.Users.ListTelegramIDsForBroadcast(ctx, p.TargetSegment)
			if err != nil {
				return nil, fmt.Errorf("fetch broadcast targets: %w", err)
			}

			payloadStr := fmt.Sprintf("broadcast:%s:%s", p.TargetSegment, p.Title)
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationToken("broadcast_announcement", payloadHash)
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "broadcast_announcement",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Broadcast '%s' to segment '%s' (%d recipients)", p.Title, p.TargetSegment, len(tgIDs)),
					Parameters:        args,
					WarningMessage:    fmt.Sprintf("This will immediately send Telegram messages to %d users.", len(tgIDs)),
				}, nil
			}

			if err := r.safety.ValidateConfirmationToken(p.ConfirmationToken, "broadcast_announcement", payloadHash); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			if broadcastService == nil {
				return nil, fmt.Errorf("broadcast service not initialized")
			}

			camp, err := broadcastService.CreateAndDispatch(ctx, p.Title, p.TargetSegment, p.Message, nil)
			if err != nil {
				return nil, fmt.Errorf("dispatch broadcast: %w", err)
			}

			return map[string]interface{}{
				"success":          true,
				"campaign_id":      camp.ID.String(),
				"total_recipients": len(tgIDs),
				"message":          fmt.Sprintf("Broadcast campaign '%s' queued for %d users.", p.Title, len(tgIDs)),
			}, nil
		},
	})
}
