package web

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"net/http"
	"net/url"
	"strings"
)

func (h *Handler) ClientPortal(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func isClientJSONRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.Contains(contentType, "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// POST /client/{token}/rotate and /client/{token}/reset

func (h *Handler) RotateClientCredentials(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		if isClientJSONRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid subscription token"})
			return
		}
		http.Error(w, "invalid subscription token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.repos.Users.GetBySubscriptionToken(ctx, token)
	if err != nil {
		if isClientJSONRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "subscription not found"})
			return
		}
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	var newToken string
	if h.provisioner != nil {
		updated, rotErr := h.provisioner.RotateUserCredentials(ctx, user.ID)
		if rotErr != nil {
			logger.ErrorContext(ctx, "failed to rotate user credentials", "error", rotErr)
			if isClientJSONRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to rotate credentials"})
				return
			}
			http.Error(w, "failed to rotate credentials", http.StatusInternalServerError)
			return
		}
		newToken = updated.SubscriptionToken.String()
	} else {
		updated, rotErr := h.repos.Users.RotateSubscriptionToken(ctx, user.ID)
		if rotErr != nil {
			logger.ErrorContext(ctx, "failed to rotate user subscription token", "error", rotErr)
			if isClientJSONRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to rotate subscription token"})
				return
			}
			http.Error(w, "failed to rotate subscription token", http.StatusInternalServerError)
			return
		}
		newToken = updated.SubscriptionToken.String()
	}

	redirectURL := fmt.Sprintf("/sub/%s", newToken)
	if isClientJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"new_token":    newToken,
			"redirect_url": redirectURL,
			"message":      "Credentials rotated successfully",
		})
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// GET /client/{token}/connect

func (h *Handler) ConnectDeepLink(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		http.Error(w, "invalid subscription token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err = h.repos.Users.GetBySubscriptionToken(ctx, token)
	if err != nil {
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	subURL := fmt.Sprintf("%s://%s/sub/%s", scheme, host, token.String())
	clientApp := strings.ToLower(r.URL.Query().Get("client"))
	if clientApp == "" {
		clientApp = strings.ToLower(r.URL.Query().Get("app"))
	}

	var deepLink string
	switch clientApp {
	case "happ":
		deepLink = fmt.Sprintf("happ://add/%s", url.QueryEscape(subURL))
	case "v2rayng":
		deepLink = fmt.Sprintf("v2rayng://install-config?url=%s", url.QueryEscape(subURL))
	case "streisand":
		deepLink = fmt.Sprintf("streisand://import/%s", url.QueryEscape(subURL))
	case "singbox", "sing-box":
		deepLink = fmt.Sprintf("sing-box://import-remote-profile?url=%s#Simple-VPN", url.QueryEscape(subURL))
	case "clash", "mihomo":
		deepLink = fmt.Sprintf("clash://install-config?url=%s&name=Simple-VPN", url.QueryEscape(subURL))
	case "hiddify":
		deepLink = fmt.Sprintf("hiddify://install-sub?url=%s#Simple-VPN", url.QueryEscape(subURL))
	default:
		http.Redirect(w, r, fmt.Sprintf("/client/%s", token.String()), http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Launching VPN Profile</title>
  <meta http-equiv="refresh" content="0; url=%s">
  <script>window.location.href = %q;</script>
  <style>
    body { background: #0c0c0e; color: #f4f4f6; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
    .card { background: #131316; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; text-align: center; max-width: 360px; }
    .btn { display: inline-block; margin-top: 16px; padding: 10px 18px; background: #00bb7f; color: #000; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 14px; }
    .subtext { font-size: 12px; color: #71717a; margin-top: 12px; }
  </style>
</head>
<body>
  <div class="card">
    <h3>Launching VPN Profile</h3>
    <p>Opening client application automatically...</p>
    <a href="%s" class="btn">Open App Directly</a>
    <p class="subtext"><a href="/client/%s" style="color:#71717a;">Back to Web Portal</a></p>
  </div>
</body>
</html>`, deepLink, deepLink, deepLink, token.String())
}

// GET /admin/broadcast
