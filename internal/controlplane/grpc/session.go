package grpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

var (
	ErrSessionClosed   = errors.New("agent session is closed")
	ErrSessionNotFound = errors.New("agent session not found")
	ErrBufferFull      = errors.New("agent send buffer is full")
	ErrCommandTimeout  = errors.New("command execution timed out")
)

const (
	defaultSendBufferSize = 64
	defaultCommandTimeout = 30 * time.Second
)

// AgentSession represents an active bidirectional gRPC streaming session with a Node Agent.
type AgentSession struct {
	NodeID          uuid.UUID
	NodeName        string
	ConnectedAt     time.Time
	SendCh          chan *agentv1.ControlMessage
	closed          atomic.Bool
	configVersion   atomic.Int64
	lastHeartbeatMu sync.RWMutex
	lastHeartbeat   time.Time

	cmdMu           sync.Mutex
	pendingCommands map[string]chan *agentv1.CommandResult
}

// NewAgentSession initializes a new AgentSession.
func NewAgentSession(nodeID uuid.UUID, nodeName string) *AgentSession {
	return &AgentSession{
		NodeID:          nodeID,
		NodeName:        nodeName,
		ConnectedAt:     time.Now(),
		lastHeartbeat:   time.Now(),
		SendCh:          make(chan *agentv1.ControlMessage, defaultSendBufferSize),
		pendingCommands: make(map[string]chan *agentv1.CommandResult),
	}
}

// Send enqueues a ControlMessage to be transmitted to the agent.
func (s *AgentSession) Send(msg *agentv1.ControlMessage) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	select {
	case s.SendCh <- msg:
		return nil
	default:
		return ErrBufferFull
	}
}

// Close gracefully closes the send channel and clears pending commands.
func (s *AgentSession) Close() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.SendCh)

		s.cmdMu.Lock()
		defer s.cmdMu.Unlock()
		for id, ch := range s.pendingCommands {
			close(ch)
			delete(s.pendingCommands, id)
		}
	}
}

// IsClosed returns whether the session stream is terminated.
func (s *AgentSession) IsClosed() bool {
	return s.closed.Load()
}

// SetLastHeartbeat updates the timestamp of the most recent heartbeat.
func (s *AgentSession) SetLastHeartbeat(t time.Time) {
	s.lastHeartbeatMu.Lock()
	defer s.lastHeartbeatMu.Unlock()
	s.lastHeartbeat = t
}

// GetLastHeartbeat returns the timestamp of the most recent heartbeat.
func (s *AgentSession) GetLastHeartbeat() time.Time {
	s.lastHeartbeatMu.RLock()
	defer s.lastHeartbeatMu.RUnlock()
	return s.lastHeartbeat
}

// SetConfigVersion atomically updates the applied config version.
func (s *AgentSession) SetConfigVersion(version int64) {
	s.configVersion.Store(version)
}

// GetConfigVersion atomically reads the currently applied config version.
func (s *AgentSession) GetConfigVersion() int64 {
	return s.configVersion.Load()
}

// RegisterCommand creates a promise channel for an asynchronous command result.
func (s *AgentSession) RegisterCommand(cmdID string) <-chan *agentv1.CommandResult {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	ch := make(chan *agentv1.CommandResult, 1)
	s.pendingCommands[cmdID] = ch
	return ch
}

// ResolveCommand notifies the promise channel waiting for the command result.
func (s *AgentSession) ResolveCommand(res *agentv1.CommandResult) {
	if res == nil {
		return
	}

	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	if ch, ok := s.pendingCommands[res.CommandId]; ok {
		ch <- res
		close(ch)
		delete(s.pendingCommands, res.CommandId)
	}
}

// SessionManager coordinates all active Node Agent gRPC connections.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*AgentSession
	byName   map[string]uuid.UUID
}

// NewSessionManager creates a thread-safe SessionManager instance.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[uuid.UUID]*AgentSession),
		byName:   make(map[string]uuid.UUID),
	}
}

// Register adds or replaces an active AgentSession in the manager.
// If an older session exists for the same node ID, it is terminated.
func (m *SessionManager) Register(session *AgentSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if old, exists := m.sessions[session.NodeID]; exists {
		old.Close()
		delete(m.byName, old.NodeName)
	}

	m.sessions[session.NodeID] = session
	m.byName[session.NodeName] = session.NodeID
}

// Unregister evicts and closes a session for the specified node ID.
func (m *SessionManager) Unregister(nodeID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[nodeID]; exists {
		session.Close()
		delete(m.byName, session.NodeName)
		delete(m.sessions, nodeID)
	}
}

// Get retrieves a session by node ID.
func (m *SessionManager) Get(nodeID uuid.UUID) (*AgentSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[nodeID]
	return session, exists
}

// GetByName retrieves a session by human-readable node name.
func (m *SessionManager) GetByName(name string) (*AgentSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodeID, exists := m.byName[name]
	if !exists {
		return nil, false
	}
	session, ok := m.sessions[nodeID]
	return session, ok
}

// List returns a snapshot slice of all active sessions.
func (m *SessionManager) List() []*AgentSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*AgentSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

// Count returns the number of currently connected sessions.
func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Broadcast dispatches a ControlMessage to all connected nodes.
func (m *SessionManager) Broadcast(msg *agentv1.ControlMessage) {
	sessions := m.List()
	for _, s := range sessions {
		_ = s.Send(msg)
	}
}

// SendCommand dispatches an administrative command to a node and waits for its execution result.
func (m *SessionManager) SendCommand(ctx context.Context, nodeID uuid.UUID, cmd *agentv1.Command) (*agentv1.CommandResult, error) {
	session, exists := m.Get(nodeID)
	if !exists {
		return nil, ErrSessionNotFound
	}

	resultCh := session.RegisterCommand(cmd.CommandId)

	msg := &agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Command{
			Command: cmd,
		},
	}

	if err := session.Send(msg); err != nil {
		return nil, fmt.Errorf("dispatch command: %w", err)
	}

	timeout := defaultCommandTimeout
	if cmd.TimeoutSeconds > 0 {
		timeout = time.Duration(cmd.TimeoutSeconds) * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrCommandTimeout
	case res, ok := <-resultCh:
		if !ok {
			return nil, ErrSessionClosed
		}
		return res, nil
	}
}
