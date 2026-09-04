package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

func TestSession_SendAndClose(t *testing.T) {
	nodeID := uuid.New()
	session := NewAgentSession(nodeID, "node-ams-01")
	assert.False(t, session.IsClosed())

	msg := &agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Ping{
			Ping: &agentv1.Ping{Timestamp: time.Now().Unix()},
		},
	}

	err := session.Send(msg)
	require.NoError(t, err)

	received := <-session.SendCh
	assert.NotNil(t, received.GetPing())

	session.Close()
	assert.True(t, session.IsClosed())

	// Sending to closed session must error
	err = session.Send(msg)
	assert.ErrorIs(t, err, ErrSessionClosed)
}

func TestSession_HeartbeatAndVersion(t *testing.T) {
	nodeID := uuid.New()
	session := NewAgentSession(nodeID, "node-fra-01")

	now := time.Now().Add(-5 * time.Minute)
	session.SetLastHeartbeat(now)
	assert.Equal(t, now, session.GetLastHeartbeat())

	session.SetConfigVersion(10)
	assert.Equal(t, int64(10), session.GetConfigVersion())
}

func TestSession_CommandResolution(t *testing.T) {
	nodeID := uuid.New()
	session := NewAgentSession(nodeID, "node-cmd-01")

	cmdID := "cmd-123"
	resCh := session.RegisterCommand(cmdID)

	go func() {
		time.Sleep(10 * time.Millisecond)
		session.ResolveCommand(&agentv1.CommandResult{
			CommandId: cmdID,
			Success:   true,
			Output:    "restarted successfully",
			ExitCode:  0,
		})
	}()

	select {
	case res := <-resCh:
		require.NotNil(t, res)
		assert.True(t, res.Success)
		assert.Equal(t, "restarted successfully", res.Output)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for command resolution")
	}
}

func TestSessionManager_Lifecycle(t *testing.T) {
	mgr := NewSessionManager()
	assert.Equal(t, 0, mgr.Count())

	node1ID := uuid.New()
	session1 := NewAgentSession(node1ID, "node-1")
	mgr.Register(session1)

	assert.Equal(t, 1, mgr.Count())
	s, found := mgr.Get(node1ID)
	assert.True(t, found)
	assert.Equal(t, "node-1", s.NodeName)

	sByName, foundByName := mgr.GetByName("node-1")
	assert.True(t, foundByName)
	assert.Equal(t, node1ID, sByName.NodeID)

	// Re-registering with same ID evicts old session
	session1New := NewAgentSession(node1ID, "node-1-new")
	mgr.Register(session1New)

	assert.Equal(t, 1, mgr.Count())
	assert.True(t, session1.IsClosed())

	sUpdated, _ := mgr.Get(node1ID)
	assert.Equal(t, "node-1-new", sUpdated.NodeName)

	// List
	list := mgr.List()
	assert.Len(t, list, 1)

	// Unregister
	mgr.Unregister(node1ID)
	assert.Equal(t, 0, mgr.Count())
	assert.True(t, session1New.IsClosed())
}

func TestSessionManager_Broadcast(t *testing.T) {
	mgr := NewSessionManager()
	s1 := NewAgentSession(uuid.New(), "n1")
	s2 := NewAgentSession(uuid.New(), "n2")
	mgr.Register(s1)
	mgr.Register(s2)

	ping := &agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Ping{
			Ping: &agentv1.Ping{Timestamp: 12345},
		},
	}
	mgr.Broadcast(ping)

	msg1 := <-s1.SendCh
	msg2 := <-s2.SendCh
	assert.Equal(t, int64(12345), msg1.GetPing().Timestamp)
	assert.Equal(t, int64(12345), msg2.GetPing().Timestamp)
}

func TestSessionManager_SendCommand_SuccessAndTimeout(t *testing.T) {
	mgr := NewSessionManager()
	nodeID := uuid.New()
	session := NewAgentSession(nodeID, "node-cmd")
	mgr.Register(session)

	// Success case
	go func() {
		msg := <-session.SendCh
		cmd := msg.GetCommand()
		session.ResolveCommand(&agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   true,
			Output:    "ok",
		})
	}()

	res, err := mgr.SendCommand(context.Background(), nodeID, &agentv1.Command{
		CommandId:      "cmd-1",
		Type:           "reload_config",
		TimeoutSeconds: 2,
	})
	require.NoError(t, err)
	assert.True(t, res.Success)

	// Timeout case
	_, err = mgr.SendCommand(context.Background(), nodeID, &agentv1.Command{
		CommandId:      "cmd-timeout",
		Type:           "reboot",
		TimeoutSeconds: 1,
	})
	assert.ErrorIs(t, err, ErrCommandTimeout)

	// Not found case
	_, err = mgr.SendCommand(context.Background(), uuid.New(), &agentv1.Command{CommandId: "cmd-none"})
	assert.ErrorIs(t, err, ErrSessionNotFound)
}
