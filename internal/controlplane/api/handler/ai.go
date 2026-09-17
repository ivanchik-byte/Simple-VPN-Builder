package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
)

// AIHandler serves REST and SSE streaming endpoints for the AI Infrastructure Copilot.
type AIHandler struct {
	copilot *ai.CopilotService
	repos   *store.Repositories
}

// NewAIHandler initializes an AIHandler.
func NewAIHandler(copilot *ai.CopilotService, repos *store.Repositories) *AIHandler {
	return &AIHandler{copilot: copilot, repos: repos}
}

// canAccessCopilot determines if the authenticated caller has permission to use AI Copilot.
// By default, only the system owner has access. Other admins must be granted CanAccessAICopilot.
func (h *AIHandler) canAccessCopilot(r *http.Request) bool {
	ctx := r.Context()

	// 1. Web session admin context
	if adminCtx := web.GetAdminContext(ctx); adminCtx != nil {
		if adminCtx.Role == "owner" {
			return true
		}
		if h.repos != nil && h.repos.Admins != nil {
			callerAdmin, err := h.repos.Admins.GetByID(ctx, adminCtx.AdminID)
			if err == nil {
				return callerAdmin.ParsedPermissions().CanAccessAICopilot
			}
		}
		return false
	}

	// 2. API auth context (JWT or scoped API Key)
	if authCtx := middleware.GetAuth(ctx); authCtx != nil {
		if authCtx.Role == "owner" {
			return true
		}
		if authCtx.AuthType == "jwt" {
			if h.repos != nil && h.repos.Admins != nil {
				callerAdmin, err := h.repos.Admins.GetByID(ctx, authCtx.UserID)
				if err == nil {
					return callerAdmin.ParsedPermissions().CanAccessAICopilot
				}
			}
			return false
		}
		if authCtx.AuthType == "apikey" {
			return authCtx.HasScope("ai") || authCtx.HasScope("*")
		}
	}

	return false
}

func getFlusher(w http.ResponseWriter) http.Flusher {
	curr := w
	for curr != nil {
		if f, ok := curr.(http.Flusher); ok {
			return f
		}
		if u, ok := curr.(interface{ Unwrap() http.ResponseWriter }); ok {
			curr = u.Unwrap()
		} else {
			break
		}
	}
	return nil
}

// Chat handles the streaming conversation endpoint: POST /api/v1/ai/chat
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	if !h.canAccessCopilot(r) {
		http.Error(w, `{"error":"Access denied: AI Infrastructure Copilot is restricted to owner or authorized administrators"}`, http.StatusForbidden)
		return
	}

	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	var req ai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request JSON"}`, http.StatusBadRequest)
		return
	}

	// Server-Sent Events header setup
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher := getFlusher(w)
	ctx := r.Context()

	_ = h.copilot.ProcessChat(ctx, req, func(event ai.ChatEvent) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	})
}

// ExecuteAction executes a user-confirmed proposal action: POST /api/v1/ai/actions/{token}/execute
func (h *AIHandler) ExecuteAction(w http.ResponseWriter, r *http.Request) {
	if !h.canAccessCopilot(r) {
		http.Error(w, `{"error":"Access denied: AI Infrastructure Copilot is restricted to owner or authorized administrators"}`, http.StatusForbidden)
		return
	}

	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	token := chi.URLParam(r, "token")

	var body struct {
		ActionName string          `json:"action_name"`
		Parameters json.RawMessage `json:"parameters"`
		Token      string          `json:"confirmation_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if token == "" {
		token = body.Token
	}
	if token == "" {
		http.Error(w, `{"error":"Token is required"}`, http.StatusBadRequest)
		return
	}

	res, err := h.copilot.ExecuteConfirmationAction(r.Context(), body.ActionName, token, body.Parameters)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"result":  res,
	})
}

// GetSettings retrieves current AI endpoint config: GET /api/v1/ai/settings
func (h *AIHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if !h.canAccessCopilot(r) {
		http.Error(w, `{"error":"Access denied: AI Infrastructure Copilot is restricted to owner or authorized administrators"}`, http.StatusForbidden)
		return
	}

	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	settings := h.copilot.GetSettings()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(settings)
}

// UpdateSettings saves updated LLM endpoint credentials: POST /api/v1/ai/settings
func (h *AIHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !h.canAccessCopilot(r) {
		http.Error(w, `{"error":"Access denied: AI Infrastructure Copilot is restricted to owner or authorized administrators"}`, http.StatusForbidden)
		return
	}

	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	var s ai.AgentSettings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if err := h.copilot.UpdateSettings(r.Context(), s); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"settings": h.copilot.GetSettings(),
	})
}

// TestConnection verifies credentials against the LLM provider: POST /api/v1/ai/test
func (h *AIHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	if !h.canAccessCopilot(r) {
		http.Error(w, `{"error":"Access denied: AI Infrastructure Copilot is restricted to owner or authorized administrators"}`, http.StatusForbidden)
		return
	}

	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	var s ai.AgentSettings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	duration, reply, err := h.copilot.TestConnection(r.Context(), s)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"duration_ms": duration.Milliseconds(),
		"reply":       reply,
	})
}

