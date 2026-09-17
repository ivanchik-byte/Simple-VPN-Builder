package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// auditMutation records a Copilot mutation in the audit log.
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

// maskTelegramHandle obfuscates a telegram username: "@abcdef" -> "@abc***".
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
