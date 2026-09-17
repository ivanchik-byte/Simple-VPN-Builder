package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// auditMutation records a Copilot-executed mutation in the audit log.
// Best-effort by design: audit failure must never fail the action itself.
// All mutating tools route through this helper so the trail is uniform.
func auditMutation(ctx context.Context, repos *store.Repositories, action, resourceType string, resourceID uuid.UUID, details map[string]any) {
	if repos == nil || repos.AuditLogs == nil {
		return
	}
	var diff []byte
	if details != nil {
		diff, _ = json.Marshal(details)
	}
	params := store.CreateAuditLogParams{
		Action:       action,
		ResourceType: pgtype.Text{String: resourceType, Valid: resourceType != ""},
		Diff:         diff,
	}
	if resourceID != uuid.Nil {
		params.ResourceID = pgtype.UUID{Bytes: resourceID, Valid: true}
	}
	if adminID, err := uuid.Parse(AdminIDFromContext(ctx)); err == nil {
		params.AdminID = pgtype.UUID{Bytes: adminID, Valid: true}
	}
	_, _ = repos.AuditLogs.Create(ctx, params)
}

// maskTelegramHandle hides a telegram username for UI/LLM-facing surfaces
// (proposal cards, summaries, hog lists): "@abcdef" -> "@abc***".
// Raw UUIDs/numeric IDs are intentionally left untouched — they are required
// to execute follow-up actions (ban/extend/reset by user_id).
func maskTelegramHandle(h string) string {
	if !strings.HasPrefix(h, "@") {
		return h
	}
	name := strings.TrimPrefix(h, "@")
	if len(name) <= 3 {
		return "@***"
	}
	return "@" + name[:3] + "***"
}
