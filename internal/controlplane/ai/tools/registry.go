package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/provider"
)

// ToolHandler is a function that executes the tool logic.
type ToolHandler func(ctx context.Context, args json.RawMessage) (interface{}, error)

// ToolDefinition defines an operational capability exposed to the AI agent.
type ToolDefinition struct {
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Safety       ActionSafetyTier `json:"safety"`
	Parameters   json.RawMessage  `json:"parameters"`
	Handler      ToolHandler      `json:"-"`
	RequiredPerm string           `json:"required_perm"`
}

// ActionProposalCard represents an interactive confirmation card rendered in the chat UI.
type ActionProposalCard struct {
	IsActionProposal  bool            `json:"is_action_proposal"`
	ActionName        string          `json:"action_name"`
	ConfirmationToken string          `json:"confirmation_token"`
	ExpiresInSeconds  int             `json:"expires_in_seconds"`
	TargetSummary     string          `json:"target_summary"`
	ImpactSummary     string          `json:"impact_summary"`
	Parameters        json.RawMessage `json:"parameters"`
	WarningMessage    string          `json:"warning_message"`
}

// ToolRegistry manages registered tools and converts them to LLM specs.
type ToolRegistry struct {
	tools  map[string]ToolDefinition
	safety *SafetyManager
}

// NewToolRegistry creates an empty registry with a safety manager.
func NewToolRegistry(safety *SafetyManager) *ToolRegistry {
	return &ToolRegistry{
		tools:  make(map[string]ToolDefinition),
		safety: safety,
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool ToolDefinition) {
	r.tools[tool.Name] = tool
}

// GetSpecs converts registered tools to OpenAI-compatible ToolSpec slices.
func (r *ToolRegistry) GetSpecs() []provider.ToolSpec {
	return r.GetSpecsFor(nil)
}

// GetSpecsFor exposes only the tools the caller is permitted to use.
func (r *ToolRegistry) GetSpecsFor(perms map[string]bool) []provider.ToolSpec {
	specs := make([]provider.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		if t.RequiredPerm != "" && (perms == nil || !perms[t.RequiredPerm]) {
			continue
		}
		specs = append(specs, provider.ToolSpec{
			Type: "function",
			Function: provider.FunctionSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return specs
}

// Permitted reports whether the tool may run under the given permissions.
func (r *ToolRegistry) Permitted(name string, perms map[string]bool) bool {
	tool, ok := r.tools[name]
	if !ok {
		return false
	}
	return tool.RequiredPerm == "" || (perms != nil && perms[tool.RequiredPerm])
}

// GetTool retrieves a tool by name.
func (r *ToolRegistry) GetTool(name string) (ToolDefinition, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute invokes a tool with parameter validation.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (interface{}, error) {
	return r.ExecuteChecked(ctx, name, args, nil)
}

// ExecuteChecked enforces the tool permission gate before running.
func (r *ToolRegistry) ExecuteChecked(ctx context.Context, name string, args json.RawMessage, perms map[string]bool) (interface{}, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool '%s' not recognized", name)
	}
	if tool.RequiredPerm != "" && (perms == nil || !perms[tool.RequiredPerm]) {
		return nil, fmt.Errorf("tool '%s' is not permitted for this administrator", name)
	}
	return tool.Handler(ctx, args)
}
