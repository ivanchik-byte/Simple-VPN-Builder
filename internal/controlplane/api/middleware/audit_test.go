package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAuditRepo struct {
	logs []store.CreateAuditLogParams
}

func (m *mockAuditRepo) Create(_ context.Context, params store.CreateAuditLogParams) (store.AuditLog, error) {
	m.logs = append(m.logs, params)
	return store.AuditLog{ID: int64(len(m.logs))}, nil
}
func (m *mockAuditRepo) List(_ context.Context, _ store.ListAuditLogsParams) ([]store.AuditLog, error) {
	return nil, nil
}
func (m *mockAuditRepo) Count(_ context.Context, _ store.CountAuditLogsParams) (int64, error) {
	return int64(len(m.logs)), nil
}

func TestAuditService_Log(t *testing.T) {
	repo := &mockAuditRepo{}
	audit := NewAuditService(repo)

	adminID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes", nil)
	req.RemoteAddr = "198.51.100.1:44321"
	req.Header.Set("User-Agent", "Mozilla/5.0")

	authCtx := &AuthContext{
		UserID:   adminID,
		Email:    "admin@test.local",
		Role:     "admin",
		AuthType: "jwt",
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthCtxKey, authCtx))

	nodeID := uuid.New()
	err := audit.Log(req, "create", "node", &nodeID, []byte(`{"name":"test-node"}`))
	require.NoError(t, err)

	require.Len(t, repo.logs, 1)
	log := repo.logs[0]
	assert.Equal(t, "create", log.Action)
	assert.Equal(t, "node", log.ResourceType.String)
	assert.Equal(t, nodeID, uuid.UUID(log.ResourceID.Bytes))
	assert.Equal(t, adminID, uuid.UUID(log.AdminID.Bytes))
	assert.Equal(t, "198.51.100.1", log.IpAddress.String())
	assert.Equal(t, "Mozilla/5.0", log.UserAgent.String)
}
