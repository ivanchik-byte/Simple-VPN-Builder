package knowledge

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed playbook.md
var basePlaybookContent string

// DynamicClusterContext captures real-time facts about the cluster to ground the LLM.
type DynamicClusterContext struct {
	OnlineNodes   int
	DrainingNodes int
	OfflineNodes  int
	ActiveUsers   int
	TotalUsers    int
	ActiveAlerts  []string
	Timestamp     time.Time
}

// SystemPromptBuilder composes the static playbook with real-time cluster state.
type SystemPromptBuilder struct {
	basePlaybook string
}

// NewSystemPromptBuilder initializes the prompt builder with the embedded playbook.
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		basePlaybook: basePlaybookContent,
	}
}

// Build compiles the full system prompt with dynamic facts.
func (b *SystemPromptBuilder) Build(facts DynamicClusterContext) string {
	var sb strings.Builder
	sb.WriteString(b.basePlaybook)
	sb.WriteString("\n\n---\n")
	sb.WriteString("## CURRENT REAL-TIME INFRASTRUCTURE STATE\n")
	sb.WriteString(fmt.Sprintf("- Active Exit Nodes: %d online, %d draining, %d offline\n",
		facts.OnlineNodes, facts.DrainingNodes, facts.OfflineNodes))
	sb.WriteString(fmt.Sprintf("- Total Users: %d (Active: %d)\n", facts.TotalUsers, facts.ActiveUsers))
	if len(facts.ActiveAlerts) > 0 {
		sb.WriteString(fmt.Sprintf("- Active Alerts (%d firing):\n", len(facts.ActiveAlerts)))
		for _, a := range facts.ActiveAlerts {
			sb.WriteString(fmt.Sprintf("  * [ALERT] %s\n", a))
		}
	} else {
		sb.WriteString("- Active Alerts: None (Cluster healthy)\n")
	}
	ts := facts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	sb.WriteString(fmt.Sprintf("- Server UTC Time: %s\n", ts.Format(time.RFC3339)))
	return sb.String()
}
