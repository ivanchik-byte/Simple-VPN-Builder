package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
)

type SubscriptionHandler struct {
	subService *service.SubscriptionService
}

func NewSubscriptionHandler(subService *service.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService: subService,
	}
}

func (h *SubscriptionHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid subscription token", nil)
		return
	}

	ctx := r.Context()
	info, err := h.subService.GetUserSubscriptionInfo(ctx, token)
	if err != nil {
		response.RespondNotFound(w, r, "Subscription not found")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		ua := strings.ToLower(r.Header.Get("User-Agent"))
		accept := strings.ToLower(r.Header.Get("Accept"))

		if strings.Contains(ua, "clash") || strings.Contains(ua, "meta") || strings.Contains(ua, "mihomo") {
			format = "clash"
		} else if strings.Contains(ua, "sing-box") || strings.Contains(ua, "happ") || strings.Contains(ua, "hiddify") || strings.Contains(ua, "karing") || strings.Contains(ua, "sfi") || strings.Contains(ua, "sfm") || strings.Contains(ua, "sfa") {
			format = "singbox"
		} else if strings.Contains(ua, "amnezia") || strings.Contains(ua, "awg") {
			format = "amneziawg"
		} else if strings.Contains(ua, "wireguard") || strings.Contains(ua, "wg-quick") {
			format = "wireguard"
		} else if strings.Contains(accept, "application/json") {
			format = "json"
		} else {
			format = "base64"
		}
	}

	content, contentType, err := h.subService.GenerateSubscriptionContent(ctx, token, format)
	if err != nil {
		response.RespondForbidden(w, r, err.Error())
		return
	}

	ext := "txt"
	switch format {
	case "singbox", "json":
		ext = "json"
	case "clash", "clash-meta", "mihomo":
		ext = "yaml"
	case "wireguard", "wg", "amneziawg", "awg", "amnezia":
		ext = "conf"
	}

	// Inject standard telecom/subscription headers
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d",
		info.UploadBytes, info.DownloadBytes, info.TotalLimit, info.ExpireEpoch))
	w.Header().Set("Profile-Update-Interval", "24")
	w.Header().Set("Profile-Title", "Simple-VPN-Network")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"subscription.%s\"", ext))
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
