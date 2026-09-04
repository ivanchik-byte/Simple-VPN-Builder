package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// AgentServiceServer implements the agentv1.AgentServiceServer gRPC interface.
type AgentServiceServer struct {
	agentv1.UnimplementedAgentServiceServer

	nodeRepo      store.NodeRepository
	userRepo      store.UserRepository
	credRepo      store.CredentialRepository
	trafficRepo   store.TrafficRepository
	configBuilder *service.ConfigBuilder
	sessionMgr    *SessionManager
}

// NewAgentServiceServer instantiates an AgentServiceServer.
func NewAgentServiceServer(
	nodeRepo store.NodeRepository,
	userRepo store.UserRepository,
	credRepo store.CredentialRepository,
	trafficRepo store.TrafficRepository,
	configBuilder *service.ConfigBuilder,
	sessionMgr *SessionManager,
) *AgentServiceServer {
	return &AgentServiceServer{
		nodeRepo:      nodeRepo,
		userRepo:      userRepo,
		credRepo:      credRepo,
		trafficRepo:   trafficRepo,
		configBuilder: configBuilder,
		sessionMgr:    sessionMgr,
	}
}

// Connect handles long-lived bidirectional gRPC streaming with a connected node agent.
func (s *AgentServiceServer) Connect(stream agentv1.AgentService_ConnectServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	var (
		session     *AgentSession
		sessionOnce sync.Once
		outboundWg  sync.WaitGroup
	)

	defer func() {
		if session != nil {
			nodeID := session.NodeID
			s.sessionMgr.Unregister(nodeID)
			// Mark node offline in database upon disconnection
			_ = s.nodeRepo.UpdateHeartbeat(context.Background(), nodeID, "offline")
			session.Close()
		}
		outboundWg.Wait()
	}()

	startOutbound := func(sess *AgentSession) {
		outboundWg.Add(1)
		go func() {
			defer outboundWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-sess.SendCh:
					if !ok {
						return
					}
					if err := stream.Send(msg); err != nil {
						cancel()
						return
					}
				}
			}
		}()
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if msg == nil || msg.Payload == nil {
			continue
		}

		switch payload := msg.Payload.(type) {
		case *agentv1.AgentMessage_Register:
			reg := payload.Register
			node, regErr := s.handleRegister(ctx, reg)
			if regErr != nil {
				logger.ErrorContext(ctx, "failed to register agent node", "error", regErr)
				return regErr
			}

			sessionOnce.Do(func() {
				session = NewAgentSession(node.ID, node.Name)
				s.sessionMgr.Register(session)
				startOutbound(session)
			})

			// Push initial full configuration
			cfgUpdate, cfgErr := s.configBuilder.BuildConfig(ctx, node.ID, 1, true)
			if cfgErr != nil {
				logger.ErrorContext(ctx, "failed to build initial node config", "node_id", node.ID, "error", cfgErr)
			} else {
				_ = session.Send(&agentv1.ControlMessage{
					Payload: &agentv1.ControlMessage_Config{
						Config: cfgUpdate,
					},
				})
			}

		case *agentv1.AgentMessage_Heartbeat:
			if session == nil {
				continue
			}
			s.handleHeartbeat(ctx, session, payload.Heartbeat)

		case *agentv1.AgentMessage_Metrics:
			if session == nil {
				continue
			}
			s.handleMetrics(ctx, session, payload.Metrics)

		case *agentv1.AgentMessage_ConfigAck:
			if session == nil {
				continue
			}
			ack := payload.ConfigAck
			if ack.Success {
				session.SetConfigVersion(ack.ConfigVersion)
				logger.DebugContext(ctx, "agent acknowledged config", "node_id", session.NodeID, "version", ack.ConfigVersion)
			} else {
				logger.WarnContext(ctx, "agent failed to apply config", "node_id", session.NodeID, "version", ack.ConfigVersion, "error", ack.Error)
			}

		case *agentv1.AgentMessage_CommandResult:
			if session != nil {
				session.ResolveCommand(payload.CommandResult)
			}

		case *agentv1.AgentMessage_Log:
			if session == nil {
				continue
			}
			entry := payload.Log
			logger.InfoContext(ctx, "agent log entry", "node_name", session.NodeName, "message", entry.Message, "level", entry.Level)
		}
	}
}

func (s *AgentServiceServer) handleRegister(ctx context.Context, reg *agentv1.RegisterRequest) (*store.Node, error) {
	if reg == nil || reg.NodeName == "" {
		return nil, errors.New("node_name is required in RegisterRequest")
	}

	node, err := s.nodeRepo.GetByName(ctx, reg.NodeName)
	if err != nil {
		// Auto-register node record if not already present
		created, createErr := s.nodeRepo.Create(ctx, store.CreateNodeParams{
			Name:         reg.NodeName,
			PublicKey:    reg.WireguardPublicKey,
			Status:       pgtype.Text{String: "online", Valid: true},
			Endpoint:     "",
			GrpcEndpoint: "",
		})
		if createErr != nil {
			return nil, fmt.Errorf("auto-create node: %w", createErr)
		}
		return &created, nil
	}

	// Update existing node status and heartbeat
	_ = s.nodeRepo.UpdateHeartbeat(ctx, node.ID, "online")
	return &node, nil
}

func (s *AgentServiceServer) handleHeartbeat(ctx context.Context, session *AgentSession, hb *agentv1.Heartbeat) {
	if hb == nil {
		return
	}

	now := time.Now()
	if hb.Timestamp > 0 {
		now = time.Unix(hb.Timestamp, 0)
	}
	session.SetLastHeartbeat(now)

	statusStr := "online"
	switch hb.Status {
	case agentv1.NodeStatus_NODE_STATUS_DEGRADED:
		statusStr = "degraded"
	case agentv1.NodeStatus_NODE_STATUS_DRAINING:
		statusStr = "draining"
	}

	_ = s.nodeRepo.UpdateHeartbeat(ctx, session.NodeID, statusStr)
}

func (s *AgentServiceServer) handleMetrics(ctx context.Context, session *AgentSession, report *agentv1.MetricsReport) {
	if report == nil {
		return
	}

	t := time.Now()
	if report.Timestamp > 0 {
		t = time.Unix(report.Timestamp, 0)
	}
	hourBucket := t.Truncate(time.Hour)

	for _, protoMetrics := range report.Protocols {
		protocol := protoMetrics.Protocol
		for _, peer := range protoMetrics.Peers {
			peerUUID, err := uuid.Parse(peer.PeerId)
			if err != nil {
				continue
			}

			// Record traffic in hourly aggregation stats
			_, _ = s.trafficRepo.Upsert(ctx, store.UpsertTrafficStatsParams{
				UserID:     peerUUID,
				NodeID:     session.NodeID,
				Protocol:   protocol,
				HourBucket: hourBucket,
				RxBytes:    pgtype.Int8{Int64: peer.RxBytes, Valid: true},
				TxBytes:    pgtype.Int8{Int64: peer.TxBytes, Valid: true},
			})

			// Increment user account traffic consumption
			totalBytes := peer.RxBytes + peer.TxBytes
			if totalBytes > 0 {
				_ = s.userRepo.UpdateTraffic(ctx, peerUUID, totalBytes)
			}
		}
	}
}

// PushConfigUpdate generates a config update and dispatches it to the connected node.
func (s *AgentServiceServer) PushConfigUpdate(ctx context.Context, nodeID uuid.UUID, version int64, isFull bool) error {
	session, found := s.sessionMgr.Get(nodeID)
	if !found {
		return ErrSessionNotFound
	}

	cfgUpdate, err := s.configBuilder.BuildConfig(ctx, nodeID, version, isFull)
	if err != nil {
		return fmt.Errorf("build config update: %w", err)
	}

	return session.Send(&agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Config{
			Config: cfgUpdate,
		},
	})
}
