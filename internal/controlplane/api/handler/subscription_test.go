package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type handlerMockUserRepo struct {
	store.UserRepository
	user store.User
}

func (m *handlerMockUserRepo) GetBySubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	return m.user, nil
}

type handlerMockCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
}

func (m *handlerMockCredRepo) ListByUser(_ context.Context, _ uuid.UUID) ([]store.Credential, error) {
	return m.creds, nil
}

type handlerMockNodeRepo struct {
	store.NodeRepository
	node store.Node
}

func (m *handlerMockNodeRepo) GetByID(_ context.Context, _ uuid.UUID) (store.Node, error) {
	return m.node, nil
}

func TestSubscriptionHandler_GetSubscription(t *testing.T) {
	userID := uuid.New()
	token := uuid.New()
	nodeID := uuid.New()
	vlessUUID := uuid.New()

	uRepo := &handlerMockUserRepo{
		user: store.User{
			ID:                userID,
			SubscriptionToken: token,
			Status:            pgtype.Text{String: "active", Valid: true},
			TrafficLimit:      pgtype.Int8{Int64: 50 * 1024 * 1024 * 1024, Valid: true},
			TrafficUsed:       pgtype.Int8{Int64: 10 * 1024 * 1024 * 1024, Valid: true},
		},
	}

	nRepo := &handlerMockNodeRepo{
		node: store.Node{
			ID:       nodeID,
			Name:     "Amsterdam-Node",
			Endpoint: "203.0.113.1",
		},
	}

	cRepo := &handlerMockCredRepo{
		creds: []store.Credential{
			{
				ID:        uuid.New(),
				UserID:    userID,
				NodeID:    nodeID,
				Protocol:  "vless",
				Uuid:      pgtype.UUID{Bytes: vlessUUID, Valid: true},
				Flow:      pgtype.Text{String: "xtls-rprx-vision", Valid: true},
				PublicKey: pgtype.Text{String: "pub-reality-key", Valid: true},
				Status:    pgtype.Text{String: "active", Valid: true},
			},
		},
	}

	subService := service.NewSubscriptionService(uRepo, cRepo, nRepo)
	subHandler := NewSubscriptionHandler(subService)

	r := chi.NewRouter()
	r.Get("/sub/{token}", subHandler.GetSubscription)

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/sub/"+token.String()+"?format=base64", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Subscription-Userinfo"), "total=53687091200")
	assert.Equal(t, "24", rec.Header().Get("Profile-Update-Interval"))
	assert.NotEmpty(t, rec.Body.String())
}
