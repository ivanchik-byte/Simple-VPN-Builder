package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// RegisterUserTools registers user inspection, anti-abuse, ban, quota reset, and extension tools.
func RegisterUserTools(r *ToolRegistry, repos *store.Repositories) {
	// 1. user_find_bandwidth_hogs
	r.Register(ToolDefinition{
		Name:        "user_find_bandwidth_hogs",
		Description: "Identify subscribers with highest data usage or those approaching/exceeding their bandwidth quota.",
		Safety:      SafetyReadOnly,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "description": "Number of users to return (default 10, max 50)"},
				"over_quota_only": {"type": "boolean", "description": "If true, only return users who have consumed 100% or more of quota."}
			}
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Limit         int  `json:"limit"`
				OverQuotaOnly bool `json:"over_quota_only"`
			}
			_ = json.Unmarshal(args, &p)
			if p.Limit <= 0 {
				p.Limit = 10
			}
			if p.Limit > 50 {
				p.Limit = 50
			}

			users, _, err := repos.Users.List(ctx, store.UserFilter{Limit: 200})
			if err != nil {
				return nil, fmt.Errorf("list users: %w", err)
			}

			type HogEntry struct {
				ID           string `json:"id"`
				Username     string `json:"username"`
				TelegramID   string `json:"telegram_id"`
				TelegramUser string `json:"telegram_username"`
				Status       string `json:"status"`
				TrafficUsed  string `json:"traffic_used"`
				TrafficLimit string `json:"traffic_limit"`
				PercentUsed  string `json:"percent_used"`
				RawUsedBytes int64  `json:"raw_used_bytes"`
				IsBanned     bool   `json:"is_banned"`
			}

			entries := make([]HogEntry, 0, len(users))
			for _, u := range users {
				used := int64(0)
				if u.TrafficUsed.Valid {
					used = u.TrafficUsed.Int64
				}
				limit := int64(0)
				if u.TrafficLimit.Valid {
					limit = u.TrafficLimit.Int64
				}

				pct := 0.0
				if limit > 0 {
					pct = (float64(used) / float64(limit)) * 100.0
				}

				if p.OverQuotaOnly && limit > 0 && used < limit {
					continue
				}

				isBanned := false
				if u.IsBanned.Valid && u.IsBanned.Bool {
					isBanned = true
				}

				entries = append(entries, HogEntry{
					ID:           u.ID.String(),
					Username:     u.DisplayName(),
					TelegramID:   u.TelegramIDString(),
					TelegramUser: u.DisplayTelegram(),
					Status:       u.CRMStatus(),
					TrafficUsed:  formatBytes(used),
					TrafficLimit: formatBytes(limit),
					PercentUsed:  fmt.Sprintf("%.1f%%", pct),
					RawUsedBytes: used,
					IsBanned:     isBanned,
				})
			}

			// Sort by RawUsedBytes descending
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].RawUsedBytes > entries[j].RawUsedBytes
			})

			if len(entries) > p.Limit {
				entries = entries[:p.Limit]
			}

			return map[string]interface{}{
				"count": len(entries),
				"users": entries,
			}, nil
		},
	})

	// 2. user_ban
	r.Register(ToolDefinition{
		Name:        "user_ban",
		Description: "Ban a subscriber immediately revoking active VPN credentials and terminating egress traffic.",
		Safety:      SafetyMutating,
		RequiredPerm: "CanManageUsers",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"user_id": {"type": "string", "description": "UUID of the user to ban"},
				"reason": {"type": "string", "description": "Detailed reason for ban (e.g. 'Abnormal outbound port scanning', 'Account sharing', 'Chargeback')"},
				"dry_run": {"type": "boolean", "description": "If true, validates input and issues a 2-phase confirmation token without banning."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["user_id", "reason"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				UserID            string `json:"user_id"`
				Reason            string `json:"reason"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			userUUID, err := uuid.Parse(p.UserID)
			if err != nil {
				return nil, fmt.Errorf("invalid user_id: %w", err)
			}

			user, err := repos.Users.GetByID(ctx, userUUID)
			if err != nil {
				return nil, fmt.Errorf("user not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("user_ban:%s:%s", userUUID.String(), p.Reason)
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("user_ban", payloadHash, AdminIDFromContext(ctx))
				creds, _ := repos.Credentials.ListByUser(ctx, userUUID)
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "user_ban",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Ban User '%s' (%s, ID: %s)", user.DisplayName(), user.DisplayTelegram(), user.ID),
					ImpactSummary:     fmt.Sprintf("Blast radius: %d active credentials across nodes will be revoked.", len(creds)),
					Parameters:        args,
					WarningMessage:    fmt.Sprintf("User will be immediately disconnected across all nodes. Reason: %s", p.Reason),
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "user_ban", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			if err := repos.Users.SetBanStatus(ctx, userUUID, true, p.Reason); err != nil {
				return nil, fmt.Errorf("failed to set ban: %w", err)
			}

			// Record audit log
			_, _ = repos.AuditLogs.Create(ctx, store.CreateAuditLogParams{
				Action:       "UserBannedByCopilot",
				ResourceType: pgtype.Text{String: "user", Valid: true},
				ResourceID:   pgtype.UUID{Bytes: userUUID, Valid: true},
				Diff:         []byte(fmt.Sprintf(`{"reason":"%s"}`, p.Reason)),
			})

			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("User '%s' has been successfully banned.", user.DisplayName()),
				"user_id": userUUID.String(),
			}, nil
		},
	})

	// 3. user_unban
	r.Register(ToolDefinition{
		Name:        "user_unban",
		Description: "Unban a previously banned user and restore their tunnel connection.",
		Safety:      SafetyMutating,
		RequiredPerm: "CanManageUsers",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"user_id": {"type": "string", "description": "UUID of the user to unban"},
				"dry_run": {"type": "boolean", "description": "If true, generates confirmation token."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["user_id"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				UserID            string `json:"user_id"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			userUUID, err := uuid.Parse(p.UserID)
			if err != nil {
				return nil, fmt.Errorf("invalid user_id: %w", err)
			}

			user, err := repos.Users.GetByID(ctx, userUUID)
			if err != nil {
				return nil, fmt.Errorf("user not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("user_unban:%s", userUUID.String())
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("user_unban", payloadHash, AdminIDFromContext(ctx))
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "user_unban",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Unban User '%s' (%s, ID: %s)", user.DisplayName(), user.DisplayTelegram(), user.ID),
					Parameters:        args,
					WarningMessage:    "Access permissions and active credentials will be restored.",
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "user_unban", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			if err := repos.Users.SetBanStatus(ctx, userUUID, false, ""); err != nil {
				return nil, fmt.Errorf("failed to unban: %w", err)
			}

			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("User '%s' is now unbanned and active.", user.DisplayName()),
			}, nil
		},
	})

	// 4. subscription_extend
	r.Register(ToolDefinition{
		Name:        "subscription_extend",
		Description: "Add extra days and/or bandwidth quota to an existing subscriber's account (e.g. for downtime compensation or support resolution).",
		Safety:      SafetyMutating,
		RequiredPerm: "CanManageUsers",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"user_id": {"type": "string", "description": "UUID of the user"},
				"days": {"type": "integer", "description": "Number of days to add to current expiration"},
				"extra_traffic_gb": {"type": "integer", "description": "Additional gigabytes of traffic quota to grant"},
				"reason": {"type": "string", "description": "Explanation for granting bonus time/quota"},
				"dry_run": {"type": "boolean", "description": "If true, generates confirmation token."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["user_id", "days"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				UserID            string `json:"user_id"`
				Days              int    `json:"days"`
				ExtraTrafficGB    int64  `json:"extra_traffic_gb"`
				Reason            string `json:"reason"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			userUUID, err := uuid.Parse(p.UserID)
			if err != nil {
				return nil, fmt.Errorf("invalid user_id: %w", err)
			}

			user, err := repos.Users.GetByID(ctx, userUUID)
			if err != nil {
				return nil, fmt.Errorf("user not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("subscription_extend:%s:%d:%d", userUUID.String(), p.Days, p.ExtraTrafficGB)
			payloadHash := ComputePayloadHash(payloadStr)

			now := time.Now()
			baseExp := now
			if user.ExpiresAt.Valid && user.ExpiresAt.Time.After(now) {
				baseExp = user.ExpiresAt.Time
			}
			newExp := baseExp.Add(time.Duration(p.Days) * 24 * time.Hour)

			extraBytes := p.ExtraTrafficGB * 1024 * 1024 * 1024

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("subscription_extend", payloadHash, AdminIDFromContext(ctx))
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "subscription_extend",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Extend '%s' by %d days (New Expiry: %s, +%d GB)", user.DisplayName(), p.Days, newExp.Format("2006-01-02 15:04"), p.ExtraTrafficGB),
					Parameters:        args,
					WarningMessage:    fmt.Sprintf("Compensation/Adjustment: %s", p.Reason),
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "subscription_extend", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			_, err = repos.Users.ExtendSubscription(ctx, userUUID, newExp, extraBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to extend subscription: %w", err)
			}

			return map[string]interface{}{
				"success":    true,
				"message":    fmt.Sprintf("Subscription extended by %d days until %s.", p.Days, newExp.Format("2006-01-02")),
				"expires_at": newExp.Format(time.RFC3339),
			}, nil
		},
	})

	// 5. user_reset_traffic
	r.Register(ToolDefinition{
		Name:        "user_reset_traffic",
		Description: "Reset a user's consumed bandwidth counter back to zero.",
		Safety:      SafetyMutating,
		RequiredPerm: "CanManageUsers",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"user_id": {"type": "string", "description": "UUID of the user"},
				"dry_run": {"type": "boolean", "description": "If true, generates confirmation token."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["user_id"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				UserID            string `json:"user_id"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			userUUID, err := uuid.Parse(p.UserID)
			if err != nil {
				return nil, fmt.Errorf("invalid user_id: %w", err)
			}

			user, err := repos.Users.GetByID(ctx, userUUID)
			if err != nil {
				return nil, fmt.Errorf("user not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("user_reset_traffic:%s", userUUID.String())
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("user_reset_traffic", payloadHash, AdminIDFromContext(ctx))
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "user_reset_traffic",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Reset consumed traffic for '%s' (Currently %s used)", user.DisplayName(), formatBytes(user.TrafficUsed.Int64)),
					Parameters:        args,
					WarningMessage:    "Traffic counter will be reset to 0 bytes.",
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "user_reset_traffic", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			if err := repos.Users.ResetTraffic(ctx, userUUID); err != nil {
				return nil, fmt.Errorf("failed to reset traffic: %w", err)
			}

			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("Traffic for '%s' successfully reset to 0 bytes.", user.DisplayName()),
			}, nil
		},
	})
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
