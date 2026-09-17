package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/knowledge"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/provider"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/tools"
	apimw "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

// AgentSettings contains the LLM endpoint connection settings.
type AgentSettings struct {
	BaseURL string `json:"base_url"` // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	APIKey  string `json:"api_key"`
	Model   string `json:"model"` // e.g. gpt-4o, gpt-4o-mini, qwen2.5:14b, llama3.1
	Enabled bool   `json:"enabled"`
}

// NormalizeBaseURL cleans up LLM base URLs.
func NormalizeBaseURL(urlStr string) string {
	return provider.NormalizeBaseURL(urlStr)
}

// CopilotService orchestrates AI multi-turn loops, knowledge injection, and tool execution.
type CopilotService struct {
	repos         *store.Repositories
	sessionMgr    *cpgrpc.SessionManager
	toolsRegistry *tools.ToolRegistry
	safetyMgr     *tools.SafetyManager
	promptBuilder *knowledge.SystemPromptBuilder

	mu       sync.RWMutex
	settings AgentSettings
	client   *provider.Client
}

// NewCopilotService initializes the AI Copilot subsystem.
// Returns an error when the HMAC safety key is empty (fail-closed).
func NewCopilotService(
	repos *store.Repositories,
	sessionMgr *cpgrpc.SessionManager,
	broadcastSvc *service.BroadcastService,
	safetyKey []byte,
) (*CopilotService, error) {
	safety, err := tools.NewSafetyManager(safetyKey)
	if err != nil {
		return nil, err
	}
	registry := tools.NewToolRegistry(safety)

	// Register all tool suites
	tools.RegisterNodeTools(registry, repos, sessionMgr)
	tools.RegisterUserTools(registry, repos)
	tools.RegisterTelemetryBillingTools(registry, repos)
	tools.RegisterBotTools(registry, repos, broadcastSvc)

	svc := &CopilotService{
		repos:         repos,
		sessionMgr:    sessionMgr,
		toolsRegistry: registry,
		safetyMgr:     safety,
		promptBuilder: knowledge.NewSystemPromptBuilder(),
		settings: AgentSettings{
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o",
			Enabled: false,
		},
	}

	// Load settings from persistent bot_replies or DB if present
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if replies, err := repos.Billing.GetBotReplies(ctx); err == nil {
		if u := replies["ai_base_url"]; u != "" {
			svc.settings.BaseURL = provider.NormalizeBaseURL(u)
		}
		if k := replies["ai_api_key"]; k != "" {
			svc.settings.APIKey = k
		}
		if m := replies["ai_model"]; m != "" {
			svc.settings.Model = strings.TrimSpace(m)
		}
		if e := replies["ai_enabled"]; e == "true" {
			svc.settings.Enabled = true
		}
	}

	svc.client = provider.NewClient(provider.Config{
		BaseURL: svc.settings.BaseURL,
		APIKey:  svc.settings.APIKey,
		Model:   svc.settings.Model,
	})

	return svc, nil
}

// GetSettings retrieves current AI endpoint config (sanitizing API key).
func (s *CopilotService) GetSettings() AgentSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	safe := s.settings
	if len(safe.APIKey) > 8 {
		safe.APIKey = safe.APIKey[:4] + "..." + safe.APIKey[len(safe.APIKey)-4:]
	} else if len(safe.APIKey) > 0 {
		safe.APIKey = "***"
	}
	return safe
}

// UpdateSettings updates the LLM endpoint and instantiates a new LLMClient.
func (s *CopilotService) UpdateSettings(ctx context.Context, settings AgentSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings.BaseURL = provider.NormalizeBaseURL(settings.BaseURL)
	if settings.BaseURL == "" {
		settings.BaseURL = "https://api.openai.com/v1"
	}
	settings.Model = strings.TrimSpace(settings.Model)
	if settings.Model == "" {
		settings.Model = "gpt-4o"
	}

	// If API key is masked or unchanged, keep existing key
	if settings.APIKey == "" || settings.APIKey == "***" || (len(settings.APIKey) > 8 && settings.APIKey[4:7] == "...") {
		settings.APIKey = s.settings.APIKey
	} else {
		settings.APIKey = strings.TrimSpace(settings.APIKey)
	}

	s.settings = settings
	s.client = provider.NewClient(provider.Config{
		BaseURL: settings.BaseURL,
		APIKey:  settings.APIKey,
		Model:   settings.Model,
	})

	// Persist
	_ = s.repos.Billing.UpsertBotReply(ctx, "ai_base_url", settings.BaseURL)
	_ = s.repos.Billing.UpsertBotReply(ctx, "ai_api_key", settings.APIKey)
	_ = s.repos.Billing.UpsertBotReply(ctx, "ai_model", settings.Model)
	enabledStr := "false"
	if settings.Enabled {
		enabledStr = "true"
	}
	_ = s.repos.Billing.UpsertBotReply(ctx, "ai_enabled", enabledStr)

	return nil
}

// TestConnection verifies an LLM endpoint and model using a lightweight ping.
func (s *CopilotService) TestConnection(ctx context.Context, settings AgentSettings) (time.Duration, string, error) {
	apiKey := strings.TrimSpace(settings.APIKey)
	if apiKey == "" || apiKey == "***" || (len(apiKey) > 8 && apiKey[4:7] == "...") {
		s.mu.RLock()
		apiKey = s.settings.APIKey
		s.mu.RUnlock()
	}

	baseURL := provider.NormalizeBaseURL(settings.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(settings.Model)
	if model == "" {
		model = "gpt-4o"
	}

	testClient := provider.NewClient(provider.Config{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Timeout: 15 * time.Second,
	})

	return testClient.TestPing(ctx)
}

// ChatRequest incoming user message and history.
type ChatRequest struct {
	Messages []provider.ChatMessage `json:"messages"`
	Screen   ScreenContext          `json:"screen"`
}

// ScreenContext carries the operator's current view for grounded answers.
type ScreenContext struct {
	Route      string `json:"route"`
	SelectedID string `json:"selected_id"`
}

// ChatEvent represents a SSE or response event chunk.
type ChatEvent struct {
	Type       string      `json:"type"` // "token", "tool_start", "tool_result", "proposal", "error", "done"
	Content    string      `json:"content,omitempty"`
	ToolName   string      `json:"tool_name,omitempty"`
	ToolArgs   string      `json:"tool_args,omitempty"`
	ToolResult interface{} `json:"tool_result,omitempty"`
	Proposal   interface{} `json:"proposal,omitempty"`
}

// ProcessChat handles the conversation loop with multi-step tool execution.
func (s *CopilotService) ProcessChat(ctx context.Context, req ChatRequest, onEvent func(ChatEvent)) error {
	s.mu.RLock()
	client := s.client
	enabled := s.settings.Enabled
	s.mu.RUnlock()

	if !enabled {
		onEvent(ChatEvent{
			Type:    "error",
			Content: "AI Infra Copilot is disabled. Configure and enable it in Settings -> AI Copilot.",
		})
		onEvent(ChatEvent{Type: "done"})
		return nil
	}

	if client == nil {
		onEvent(ChatEvent{
			Type:    "error",
			Content: "AI Copilot client is not initialized. Please verify your API key and Base URL in Settings.",
		})
		onEvent(ChatEvent{Type: "done"})
		return nil
	}

	// 1. Collect dynamic cluster facts to inject into the system prompt
	nodes, _, _ := s.repos.Nodes.List(ctx, store.NodeFilter{Limit: 100})
	online := 0
	draining := 0
	offline := 0
	for _, n := range nodes {
		if n.Status.String == "active" {
			online++
		} else if n.Status.String == "draining" {
			draining++
		} else {
			offline++
		}
	}

	users, _, _ := s.repos.Users.List(ctx, store.UserFilter{Limit: 500})
	activeUsers := 0
	for _, u := range users {
		if u.CRMStatus() == "active" || u.CRMStatus() == "trial" {
			activeUsers++
		}
	}

	facts := knowledge.DynamicClusterContext{
		OnlineNodes:   online,
		DrainingNodes: draining,
		OfflineNodes:  offline,
		ActiveUsers:   activeUsers,
		TotalUsers:    len(users),
		Timestamp:     time.Now().UTC(),
	}

	systemPrompt := s.promptBuilder.Build(facts)

	// Caller permissions gate the mutating tool schema (prompt-injection defense).
	perms := s.callerPerms(ctx)

	// 2. Prepare conversation messages
	messages := []provider.ChatMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
	}
	if req.Screen.Route != "" || req.Screen.SelectedID != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: fmt.Sprintf("Operator screen context: route=%s selected_id=%s. Ground pronouns like 'it' or 'this node' to the selection.", req.Screen.Route, req.Screen.SelectedID),
		})
	}
	messages = append(messages, req.Messages...)

	toolSpecs := s.toolsRegistry.GetSpecsFor(perms)

	// 3. Multi-turn execution loop (up to 6 tool iterations)
	maxIterations := 6
	for iter := 0; iter < maxIterations; iter++ {
		resp, err := client.Complete(ctx, messages, toolSpecs)
		if err != nil {
			onEvent(ChatEvent{
				Type:    "error",
				Content: fmt.Sprintf("LLM request failed: %v", err),
			})
			onEvent(ChatEvent{Type: "done"})
			return err
		}

		// If no tool calls, emit the final textual response
		if len(resp.ToolCalls) == 0 {
			content := resp.Content
			if strings.TrimSpace(content) == "" {
				content = "Model completed request without generating output. Verify model capabilities or check provider logs."
			}
			onEvent(ChatEvent{
				Type:    "token",
				Content: content,
			})
			onEvent(ChatEvent{Type: "done"})
			return nil
		}

		// Assistant called one or more tools
		messages = append(messages, provider.ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			onEvent(ChatEvent{
				Type:     "tool_start",
				ToolName: tc.Function.Name,
				ToolArgs: tc.Function.Arguments,
			})

			result, execErr := s.toolsRegistry.ExecuteChecked(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), perms)
			var resJSON []byte
			if execErr != nil {
				resJSON = []byte(fmt.Sprintf(`{"error":"%s"}`, execErr.Error()))
			} else {
				resJSON, _ = json.Marshal(result)
			}

			// Check if the tool returned an interactive action proposal card
			if card, ok := result.(tools.ActionProposalCard); ok && card.IsActionProposal {
				onEvent(ChatEvent{
					Type:     "proposal",
					Proposal: card,
				})
			} else {
				onEvent(ChatEvent{
					Type:       "tool_result",
					ToolName:   tc.Function.Name,
					ToolResult: result,
				})
			}

			messages = append(messages, provider.ChatMessage{
				Role:       "tool",
				Name:       tc.Function.Name,
				ToolCallID: tc.ID,
				Content:    string(resJSON),
			})
		}
	}

	onEvent(ChatEvent{Type: "done"})
	return nil
}

// ExecuteConfirmationAction directly verifies and applies a user-approved proposal.
func (s *CopilotService) ExecuteConfirmationAction(ctx context.Context, actionName string, token string, params json.RawMessage) (interface{}, error) {
	// Re-route with dry_run = false and confirmation_token set
	var paramMap map[string]interface{}
	if err := json.Unmarshal(params, &paramMap); err != nil {
		paramMap = make(map[string]interface{})
	}
	paramMap["dry_run"] = false
	paramMap["confirmation_token"] = token
	modifiedParams, err := json.Marshal(paramMap)
	if err != nil {
		return nil, err
	}

	return s.toolsRegistry.ExecuteChecked(ctx, actionName, modifiedParams, s.callerPerms(ctx))
}

// callerPerms resolves the tool permission set for the request identity.
func (s *CopilotService) callerPerms(ctx context.Context) map[string]bool {
	authCtx := apimw.GetAuth(ctx)
	if authCtx == nil {
		return nil
	}
	if authCtx.Role == "owner" {
		return map[string]bool{
			"CanManageNodes": true, "CanManageUsers": true, "CanBroadcast": true,
			"CanDeleteUsers": true, "CanViewAudit": true, "CanEditBotReplies": true,
		}
	}
	if authCtx.AuthType == "apikey" {
		if authCtx.HasScope("ai") || authCtx.HasScope("*") {
			return map[string]bool{
				"CanManageNodes": true, "CanManageUsers": true, "CanBroadcast": true,
			}
		}
		return nil
	}
	if s.repos != nil {
		if admin, err := s.repos.Admins.GetByID(ctx, authCtx.UserID); err == nil {
			p := admin.ParsedPermissions()
			return map[string]bool{
				"CanManageNodes": p.CanManageNodes, "CanManageUsers": p.CanManageUsers,
				"CanBroadcast": p.CanBroadcast, "CanDeleteUsers": p.CanDeleteUsers,
				"CanViewAudit": p.CanViewAudit, "CanEditBotReplies": p.CanEditBotReplies,
			}
		}
	}
	return nil
}
