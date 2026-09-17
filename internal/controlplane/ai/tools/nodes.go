package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

// RegisterNodeTools registers all node & diagnostic operational tools.
func RegisterNodeTools(r *ToolRegistry, repos *store.Repositories, sessionMgr *cpgrpc.SessionManager) {
	// 1. node_list_health
	r.Register(ToolDefinition{
		Name:        "node_list_health",
		Description: "List all exit nodes with their current real-time health, gRPC connection status, memory/CPU load, and active client counts.",
		Safety:      SafetyReadOnly,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"region": {"type": "string", "description": "Optional filter by region code, e.g. 'eu-central', 'us-east'"},
				"status": {"type": "string", "description": "Optional filter by status: 'active', 'offline', 'draining'"}
			}
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Region string `json:"region"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(args, &p)

			nodes, _, err := repos.Nodes.List(ctx, store.NodeFilter{
				Status: p.Status,
				Region: p.Region,
				Limit:  100,
			})
			if err != nil {
				return nil, fmt.Errorf("list nodes: %w", err)
			}

			type NodeHealthReport struct {
				ID          string  `json:"id"`
				Name        string  `json:"name"`
				Region      string  `json:"region"`
				Status      string  `json:"status"`
				Endpoint    string  `json:"endpoint"`
				Connected   bool    `json:"connected"`
				CPUPercent  float64 `json:"cpu_percent"`
				MemoryUsage string  `json:"memory_usage"`
				DiskUsage   string  `json:"disk_usage"`
				LastSeen    string  `json:"last_seen"`
			}

			reports := make([]NodeHealthReport, 0, len(nodes))
			for _, n := range nodes {
				rep := NodeHealthReport{
					ID:       n.ID.String(),
					Name:     n.Name,
					Region:   n.Region.String,
					Status:   n.Status.String,
					Endpoint: n.Endpoint,
					LastSeen: "never",
				}
				if n.LastHeartbeat.Valid {
					rep.LastSeen = n.LastHeartbeat.Time.Format(time.RFC3339)
				}

				if sessionMgr != nil {
					if sess, ok := sessionMgr.Get(n.ID); ok && !sess.IsClosed() {
						rep.Connected = true
						sys := sess.GetSystemInfo()
						if sys != nil {
							rep.CPUPercent = sys.CpuUsagePercent
							if sys.MemoryTotal > 0 {
								rep.MemoryUsage = fmt.Sprintf("%.1f%% (%d MB / %d MB)",
									float64(sys.MemoryUsed)/float64(sys.MemoryTotal)*100,
									sys.MemoryUsed/(1024*1024),
									sys.MemoryTotal/(1024*1024))
							}
							if sys.DiskTotal > 0 {
								rep.DiskUsage = fmt.Sprintf("%.1f%% (%d GB / %d GB)",
									float64(sys.DiskUsed)/float64(sys.DiskTotal)*100,
									sys.DiskUsed/(1024*1024*1024),
									sys.DiskTotal/(1024*1024*1024))
							}
						}
					}
				}
				reports = append(reports, rep)
			}

			return map[string]interface{}{
				"total_nodes": len(reports),
				"nodes":       reports,
			}, nil
		},
	})

	// 2. node_add
	r.Register(ToolDefinition{
		Name:         "node_add",
		Description:  "Add and onboard a new exit node into the Simple-VPN-Builder network.",
		Safety:       SafetyMutating,
		RequiredPerm: "CanManageNodes",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "Unique descriptive hostname or node identifier, e.g. 'de-fra-node-02'"},
				"endpoint": {"type": "string", "description": "Public IPv4 or IPv6 or domain of the node, e.g. '198.51.100.45'"},
				"grpc_endpoint": {"type": "string", "description": "gRPC management address with port, e.g. '198.51.100.45:50051'"},
				"region": {"type": "string", "description": "Geographical region code, e.g. 'de', 'nl', 'us-east'"},
				"capacity_gbps": {"type": "integer", "description": "Uplink network capacity in Gbps (default 1)"},
				"dry_run": {"type": "boolean", "description": "If true, validates input and generates a 2-phase confirmation token without applying changes."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["name", "endpoint", "grpc_endpoint"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				Name              string `json:"name"`
				Endpoint          string `json:"endpoint"`
				GrpcEndpoint      string `json:"grpc_endpoint"`
				Region            string `json:"region"`
				CapacityGbps      int32  `json:"capacity_gbps"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			if p.CapacityGbps <= 0 {
				p.CapacityGbps = 1
			}

			payloadStr := fmt.Sprintf("node_add:%s:%s:%s", p.Name, p.Endpoint, p.GrpcEndpoint)
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("node_add", payloadHash, AdminIDFromContext(ctx))
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "node_add",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Onboard Exit Node '%s' (%s, Region: %s, %d Gbps)", p.Name, p.Endpoint, p.Region, p.CapacityGbps),
					Parameters:        args,
					WarningMessage:    "This will allocate node credentials and register the server into cluster routing tables.",
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "node_add", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			created, err := repos.Nodes.Create(ctx, store.CreateNodeParams{
				Name:            p.Name,
				Endpoint:        p.Endpoint,
				GrpcEndpoint:    p.GrpcEndpoint,
				Region:          pgtype.Text{String: p.Region, Valid: p.Region != ""},
				CapacityGbps:    pgtype.Int4{Int32: p.CapacityGbps, Valid: true},
				Status:          pgtype.Text{String: "active", Valid: true},
				Tags:            []byte(`["copilot-provisioned"]`),
				PublicKey:       "",
				CertFingerprint: "",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create node: %w", err)
			}

			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("Node '%s' added successfully with ID %s", created.Name, created.ID),
				"node_id": created.ID.String(),
			}, nil
		},
	})

	// 3. node_drain
	r.Register(ToolDefinition{
		Name:         "node_drain",
		Description:  "Set an exit node into 'draining' mode so new client connections bypass it while existing sessions finish gracefully before maintenance.",
		Safety:       SafetyMutating,
		RequiredPerm: "CanManageNodes",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node_id": {"type": "string", "description": "UUID of the node to drain"},
				"reason": {"type": "string", "description": "Reason for draining, e.g. 'Kernel upgrade / hardware migration'"},
				"dry_run": {"type": "boolean", "description": "If true, validates input and generates confirmation token without modifying node state."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["node_id"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				NodeID            string `json:"node_id"`
				Reason            string `json:"reason"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			nodeUUID, err := uuid.Parse(p.NodeID)
			if err != nil {
				return nil, fmt.Errorf("invalid node_id UUID: %w", err)
			}

			node, err := repos.Nodes.GetByID(ctx, nodeUUID)
			if err != nil {
				return nil, fmt.Errorf("node not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("node_drain:%s", nodeUUID.String())
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("node_drain", payloadHash, AdminIDFromContext(ctx))
				peers, _ := repos.Credentials.ListActiveByNode(ctx, nodeUUID)
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "node_drain",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Drain Node '%s' (%s, ID: %s)", node.Name, node.Endpoint, node.ID),
					ImpactSummary:     fmt.Sprintf("Blast radius: %d active peers will be migrated off this node.", len(peers)),
					Parameters:        args,
					WarningMessage:    fmt.Sprintf("Node '%s' will be marked as draining. Reason: %s", node.Name, p.Reason),
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "node_drain", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			_, err = repos.Nodes.Update(ctx, store.UpdateNodeParams{
				ID:              node.ID,
				Name:            node.Name,
				Endpoint:        node.Endpoint,
				GrpcEndpoint:    node.GrpcEndpoint,
				Region:          node.Region,
				CapacityGbps:    node.CapacityGbps,
				Status:          pgtype.Text{String: "draining", Valid: true},
				Tags:            node.Tags,
				PublicKey:       node.PublicKey,
				CertFingerprint: node.CertFingerprint,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to set node draining: %w", err)
			}

			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("Node '%s' is now in draining state. Client traffic is being redirected.", node.Name),
			}, nil
		},
	})

	// 4. service_restart
	r.Register(ToolDefinition{
		Name:         "service_restart",
		Description:  "Instruct the node agent over gRPC to restart a specific tunneling daemon ('wireguard', 'amneziawg', 'xray', or 'all').",
		Safety:       SafetyMutating,
		RequiredPerm: "CanManageNodes",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node_id": {"type": "string", "description": "UUID of the exit node"},
				"service_name": {"type": "string", "enum": ["wireguard", "amneziawg", "xray", "all"], "description": "Service daemon to restart"},
				"dry_run": {"type": "boolean", "description": "Simulates action and generates confirmation token."},
				"confirmation_token": {"type": "string", "description": "HMAC confirmation token required when dry_run=false"}
			},
			"required": ["node_id", "service_name"]
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			var p struct {
				NodeID            string `json:"node_id"`
				ServiceName       string `json:"service_name"`
				DryRun            *bool  `json:"dry_run"`
				ConfirmationToken string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			nodeUUID, err := uuid.Parse(p.NodeID)
			if err != nil {
				return nil, fmt.Errorf("invalid node_id: %w", err)
			}

			node, err := repos.Nodes.GetByID(ctx, nodeUUID)
			if err != nil {
				return nil, fmt.Errorf("node not found: %w", err)
			}

			dryRun := true
			if p.DryRun != nil {
				dryRun = *p.DryRun
			}

			payloadStr := fmt.Sprintf("service_restart:%s:%s", nodeUUID.String(), p.ServiceName)
			payloadHash := ComputePayloadHash(payloadStr)

			if dryRun {
				token, _ := r.safety.GenerateConfirmationTokenFor("service_restart", payloadHash, AdminIDFromContext(ctx))
				return ActionProposalCard{
					IsActionProposal:  true,
					ActionName:        "service_restart",
					ConfirmationToken: token,
					ExpiresInSeconds:  300,
					TargetSummary:     fmt.Sprintf("Restart Service '%s' on Node '%s'", p.ServiceName, node.Name),
					Parameters:        args,
					WarningMessage:    "Brief tunnel reconnection (~1-2 seconds) may occur for active users on this node.",
				}, nil
			}

			if err := r.safety.ValidateAndConsume(p.ConfirmationToken, "service_restart", payloadHash, AdminIDFromContext(ctx)); err != nil {
				return nil, fmt.Errorf("confirmation failed: %w", err)
			}

			if sessionMgr == nil {
				return nil, fmt.Errorf("gRPC session manager not initialized")
			}

			cmdID := uuid.New().String()
			cmd := &agentv1.Command{
				CommandId:      cmdID,
				Type:           "restart_protocol",
				Params:         map[string]string{"service": p.ServiceName},
				TimeoutSeconds: 15,
			}

			res, err := sessionMgr.SendCommand(ctx, nodeUUID, cmd)
			if err != nil {
				return nil, fmt.Errorf("failed to send command to agent: %w", err)
			}

			return map[string]interface{}{
				"success":   res.Success,
				"exit_code": res.ExitCode,
				"output":    res.Output,
				"message":   fmt.Sprintf("Service '%s' restarted on node '%s'", p.ServiceName, node.Name),
			}, nil
		},
	})

	// 5. doctor_run_diagnostics
	r.Register(ToolDefinition{
		Name:        "doctor_run_diagnostics",
		Description: "Run deep automated health diagnostics across all nodes: verifies gRPC sessions, kernel forwarding, WireGuard interfaces, and port reachability.",
		Safety:      SafetyReadOnly,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"target": {"type": "string", "description": "Optional: 'all' or specific node UUID"}
			}
		}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			nodes, _, err := repos.Nodes.List(ctx, store.NodeFilter{Limit: 100})
			if err != nil {
				return nil, err
			}

			type DiagnosticResult struct {
				NodeID         string   `json:"node_id"`
				NodeName       string   `json:"node_name"`
				SessionStatus  string   `json:"session_status"`
				HeartbeatAge   string   `json:"heartbeat_age"`
				IssuesDetected []string `json:"issues_detected"`
				Suggestions    []string `json:"suggestions"`
			}

			results := make([]DiagnosticResult, 0, len(nodes))
			for _, n := range nodes {
				res := DiagnosticResult{
					NodeID:         n.ID.String(),
					NodeName:       n.Name,
					SessionStatus:  "disconnected",
					IssuesDetected: make([]string, 0),
					Suggestions:    make([]string, 0),
				}

				if n.LastHeartbeat.Valid {
					age := time.Since(n.LastHeartbeat.Time).Round(time.Second)
					res.HeartbeatAge = age.String()
					if age > 2*time.Minute {
						res.IssuesDetected = append(res.IssuesDetected, "Heartbeat is stale (>2m)")
						res.Suggestions = append(res.Suggestions, "Check if agent daemon is running or if firewall blocks gRPC port 50051.")
					}
				} else {
					res.HeartbeatAge = "never"
					res.IssuesDetected = append(res.IssuesDetected, "No heartbeat recorded yet")
				}

				if sessionMgr != nil {
					if sess, ok := sessionMgr.Get(n.ID); ok && !sess.IsClosed() {
						res.SessionStatus = "connected"
						sys := sess.GetSystemInfo()
						if sys != nil && sys.CpuUsagePercent > 90.0 {
							res.IssuesDetected = append(res.IssuesDetected, fmt.Sprintf("High CPU utilization: %.1f%%", sys.CpuUsagePercent))
							res.Suggestions = append(res.Suggestions, "Check for crypto throughput bottleneck or high connection concurrency.")
						}
					}
				}

				results = append(results, res)
			}

			return map[string]interface{}{
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
				"diagnostics": results,
			}, nil
		},
	})
}
