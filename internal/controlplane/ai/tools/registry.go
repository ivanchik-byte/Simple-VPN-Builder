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
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Safety      ActionSafetyTier `json:"safety"`
	Parameters  json.RawMessage  `json:"parameters"`
	Handler     ToolHandler      `json:"-"`
}

// ActionProposalCard represents an interactive confirmation card rendered in the chat UI.
type ActionProposalCard struct {
	IsActionProposal  bool             `json:"is_action_proposal"`
	ActionName        string           `json:"action_name"`
	ConfirmationToken string           `json:"confirmation_token"`
	ExpiresInSeconds  int              `json:"expires_in_seconds"`
	TargetSummary     string           `json:"target_summary"`
	Parameters        json.RawMessage  `json:"parameters"`
	WarningMessage    string           `json:"warning_message"`
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
	specs := make([]provider.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
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

// GetTool retrieves a tool by name.
func (r *ToolRegistry) GetTool(name string) (ToolDefinition, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute invokes a tool with parameter validation.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (interface{}, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool '%s' not recognized", name)
	}
	return tool.Handler(ctx, args)
}
