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

	systemInfoMu sync.RWMutex
	systemInfo   *agentv1.SystemInfo

	sendMu sync.RWMutex

	cmdMu           sync.Mutex
	pendingCommands map[string]chan *agentv1.CommandResult

	peerCacheMu     sync.RWMutex
	peerUserCache   map[uuid.UUID]uuid.UUID
	pubKeyToCredMap map[string]uuid.UUID
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
		peerUserCache:   make(map[uuid.UUID]uuid.UUID),
		pubKeyToCredMap: make(map[string]uuid.UUID),
	}
}

// ResolveCachedUser looks up a peer's UserID from the session cache.
func (s *AgentSession) ResolveCachedUser(peerID uuid.UUID) (uuid.UUID, bool) {
	s.peerCacheMu.RLock()
	defer s.peerCacheMu.RUnlock()
	userID, found := s.peerUserCache[peerID]
	return userID, found
}

// CachePeerUser stores a peer to user mapping in the session cache.
func (s *AgentSession) CachePeerUser(peerID, userID uuid.UUID) {
	s.peerCacheMu.Lock()
	defer s.peerCacheMu.Unlock()
	s.peerUserCache[peerID] = userID
}

// ResolveCachedCredByPubKey looks up a credential UUID by peer public key or identifier.
func (s *AgentSession) ResolveCachedCredByPubKey(pubKey string) (uuid.UUID, bool) {
	s.peerCacheMu.RLock()
	defer s.peerCacheMu.RUnlock()
	credID, found := s.pubKeyToCredMap[pubKey]
	return credID, found
}

// CachePubKeyCred associates a peer public key with a credential UUID in the session cache.
func (s *AgentSession) CachePubKeyCred(pubKey string, credID uuid.UUID) {
	s.peerCacheMu.Lock()
	defer s.peerCacheMu.Unlock()
	if s.pubKeyToCredMap == nil {
		s.pubKeyToCredMap = make(map[string]uuid.UUID)
	}
	s.pubKeyToCredMap[pubKey] = credID
}

// InvalidateCaches drops peer/user and pubkey/credential mappings.
// Must be called on every PushConfigUpdate: after a config change the
// previous credential resolution may be stale (N9).
func (s *AgentSession) InvalidateCaches() {
	s.peerCacheMu.Lock()
	defer s.peerCacheMu.Unlock()
	s.peerUserCache = make(map[uuid.UUID]uuid.UUID)
	s.pubKeyToCredMap = make(map[string]uuid.UUID)
}

// Send enqueues a ControlMessage to be transmitted to the agent.
func (s *AgentSession) Send(msg *agentv1.ControlMessage) error {
	s.sendMu.RLock()
	defer s.sendMu.RUnlock()

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
		s.sendMu.Lock()
		close(s.SendCh)
		s.sendMu.Unlock()

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

// SetSystemInfo updates the reported hardware and network telemetry from the node agent.
func (s *AgentSession) SetSystemInfo(info *agentv1.SystemInfo) {
	s.systemInfoMu.Lock()
	defer s.systemInfoMu.Unlock()
	s.systemInfo = info
}

// GetSystemInfo returns the most recent hardware and network telemetry from the node agent.
func (s *AgentSession) GetSystemInfo() *agentv1.SystemInfo {
	s.systemInfoMu.RLock()
	defer s.systemInfoMu.RUnlock()
	return s.systemInfo
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

// UnregisterCommand cleanly purges a pending command channel upon timeout or cancellation.
func (s *AgentSession) UnregisterCommand(cmdID string) {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if ch, ok := s.pendingCommands[cmdID]; ok {
		close(ch)
		delete(s.pendingCommands, cmdID)
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

// UnregisterSession evicts and closes a specific session instance.
// Returns true if the session was the currently registered session and was removed.
func (m *SessionManager) UnregisterSession(session *AgentSession) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session == nil {
		return false
	}
	current, exists := m.sessions[session.NodeID]
	if exists && current == session {
		session.Close()
		delete(m.byName, session.NodeName)
		delete(m.sessions, session.NodeID)
		return true
	}
	return false
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

// SweepInactive evicts sessions that have not sent a heartbeat within timeout.
// It returns the node IDs of evicted sessions so callers can mark them offline.
func (m *SessionManager) SweepInactive(timeout time.Duration) []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var evicted []uuid.UUID
	for id, s := range m.sessions {
		if now.Sub(s.GetLastHeartbeat()) < timeout {
			continue
		}
		s.Close()
		delete(m.byName, s.NodeName)
		delete(m.sessions, id)
		evicted = append(evicted, id)
	}
	return evicted
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
	defer session.UnregisterCommand(cmd.CommandId)

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
