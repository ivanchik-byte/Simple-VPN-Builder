package web

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"net/http"
)

func (h *Handler) RotateCredential(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/credentials?error=Forbidden:+permission+to+manage+credentials+is+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	credIDStr := chi.URLParam(r, "id")
	credID, err := uuid.Parse(credIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
		return
	}

	// Fetch the existing credential so we can decide which type to rotate.
	existing, err := h.repos.Credentials.GetByID(ctx, credID)
	if err != nil {
		http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
		return
	}

	params := store.UpdateCredentialParams{
		ID:     credID,
		Status: pgtype.Text{String: "active", Valid: true},
	}

	switch existing.Protocol {
	case "wireguard", "amneziawg":
		// Generate a real new WireGuard private/public key pair.
		newPriv, keyErr := wgtypes.GeneratePrivateKey()
		if keyErr != nil {
			logger.ErrorContext(ctx, "failed to generate WireGuard key on rotate", "error", keyErr)
			http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
			return
		}
		params.PrivateKey = pgtype.Text{String: newPriv.String(), Valid: true}
		params.PublicKey = pgtype.Text{String: newPriv.PublicKey().String(), Valid: true}
	case "vless":
		// Rotate VLESS UUID.
		newUUID := uuid.New()
		params.Uuid = pgtype.UUID{Bytes: newUUID, Valid: true}
	}

	if _, err := h.repos.Credentials.Update(ctx, params); err != nil {
		logger.ErrorContext(ctx, "failed to rotate credential", "id", credID, "error", err)
	} else {
		logger.InfoContext(ctx, "rotated credential", "id", credID, "protocol", existing.Protocol)
		if h.provisioner != nil {
			h.provisioner.PushNode(ctx, existing.NodeID)
		}
	}
	http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
}

// POST /admin/credentials/{id}/delete

func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/credentials?error=Forbidden:+permission+to+manage+credentials+is+required", http.StatusSeeOther)
		return
	}

	credIDStr := chi.URLParam(r, "id")
	if credID, err := uuid.Parse(credIDStr); err == nil {
		if h.provisioner != nil {
			_ = h.provisioner.RevokeCredential(r.Context(), credID)
		} else {
			_ = h.repos.Credentials.Delete(r.Context(), credID)
		}
	}
	http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
}

// GET /admin/analytics
