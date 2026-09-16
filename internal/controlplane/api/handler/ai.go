package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai"
)

// AIHandler serves REST and SSE streaming endpoints for the AI Infrastructure Copilot.
type AIHandler struct {
	copilot *ai.CopilotService
}

// NewAIHandler initializes an AIHandler.
func NewAIHandler(copilot *ai.CopilotService) *AIHandler {
	return &AIHandler{copilot: copilot}
}

// Chat handles the streaming conversation endpoint: POST /api/v1/ai/chat
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"Streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	_ = h.copilot.ProcessChat(ctx, req, func(event ai.ChatEvent) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	})
}

// ExecuteAction executes a user-confirmed proposal action: POST /api/v1/ai/actions/{token}/execute
func (h *AIHandler) ExecuteAction(w http.ResponseWriter, r *http.Request) {
	if h.copilot == nil {
		http.Error(w, `{"error":"AI Copilot not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, `{"error":"Token is required"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		ActionName string          `json:"action_name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
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
