package web

import (
	"encoding/json"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"strings"
)

func (h *Handler) CreateBroadcast(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanBroadcast {
		http.Redirect(w, r, "/admin/dashboard?error=Access+denied:+Telegram+broadcast+permission+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/broadcast?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	title := strings.TrimSpace(r.FormValue("title"))
	segment := strings.TrimSpace(r.FormValue("segment"))
	messageText := strings.TrimSpace(r.FormValue("message_text"))
	btnText := strings.TrimSpace(r.FormValue("button_text"))
	btnURL := strings.TrimSpace(r.FormValue("button_url"))

	if title == "" || messageText == "" {
		http.Redirect(w, r, "/admin/broadcast?error=missing_required_fields", http.StatusSeeOther)
		return
	}
	if segment == "" {
		segment = "all"
	}

	var buttons []service.BroadcastButton
	if btnText != "" && btnURL != "" {
		buttons = append(buttons, service.BroadcastButton{
			Text: btnText,
			URL:  btnURL,
		})
	}

	if h.broadcastService != nil {
		_, err := h.broadcastService.CreateAndDispatch(ctx, title, segment, messageText, buttons)
		if err != nil {
			logger.ErrorContext(ctx, "failed to dispatch broadcast", "error", err)
			http.Redirect(w, r, "/admin/broadcast?error=dispatch_failed", http.StatusSeeOther)
			return
		}
	} else {
		// Fallback without active broadcast service
		btnBytes, _ := json.Marshal(buttons)
		_, _ = h.repos.Billing.CreateBroadcastCampaign(ctx, store.CreateBroadcastCampaignParams{
			Title:           title,
			TargetSegment:   segment,
			MessageText:     messageText,
			InlineButtons:   btnBytes,
			TotalRecipients: pgtype.Int4{Int32: 0, Valid: true},
			Status:          pgtype.Text{String: "pending", Valid: true},
		})
	}

	http.Redirect(w, r, "/admin/broadcast?sent=true", http.StatusSeeOther)
}
