package middleware

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type AuditService struct {
	repo store.AuditLogRepository
}

func NewAuditService(repo store.AuditLogRepository) *AuditService {
	return &AuditService{repo: repo}
}

func (s *AuditService) Log(r *http.Request, action, resourceType string, resourceID *uuid.UUID, diff []byte) error {
	if s.repo == nil || r == nil {
		return nil
	}

	var adminID pgtype.UUID
	var apiKeyID pgtype.UUID

	authCtx := GetAuth(r.Context())
	if authCtx != nil {
		if authCtx.AuthType == "jwt" {
			adminID = pgtype.UUID{Bytes: authCtx.UserID, Valid: true}
		} else if authCtx.AuthType == "apikey" {
			apiKeyID = pgtype.UUID{Bytes: authCtx.UserID, Valid: true}
		}
	}

	var resID pgtype.UUID
	if resourceID != nil {
		resID = pgtype.UUID{Bytes: *resourceID, Valid: true}
	}

	ipStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ipStr = r.RemoteAddr
	}

	var parsedIP *netip.Addr
	if addr, parseErr := netip.ParseAddr(ipStr); parseErr == nil {
		parsedIP = &addr
	}

	_, err = s.repo.Create(r.Context(), store.CreateAuditLogParams{
		AdminID:      adminID,
		ApiKeyID:     apiKeyID,
		Action:       action,
		ResourceType: pgtype.Text{String: resourceType, Valid: resourceType != ""},
		ResourceID:   resID,
		Diff:         diff,
		IpAddress:    parsedIP,
		UserAgent:    pgtype.Text{String: r.UserAgent(), Valid: true},
	})
	return err
}
