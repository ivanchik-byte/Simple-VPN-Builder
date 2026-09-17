package service_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockCredRepo struct {
	mock.Mock
}

func (m *mockCredRepo) Create(ctx context.Context, params store.CreateCredentialParams) (store.Credential, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.Credential), args.Error(1)
}

func (m *mockCredRepo) GetByID(ctx context.Context, id uuid.UUID) (store.Credential, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(store.Credential), args.Error(1)
}

func (m *mockCredRepo) GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, proto string) (store.Credential, error) {
	args := m.Called(ctx, userID, nodeID, proto)
	return args.Get(0).(store.Credential), args.Error(1)
}

func (m *mockCredRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]store.Credential, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]store.Credential), args.Error(1)
}

func (m *mockCredRepo) ListByNode(ctx context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	args := m.Called(ctx, nodeID)
	return args.Get(0).([]store.Credential), args.Error(1)
}

func (m *mockCredRepo) ListActiveByNode(ctx context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	args := m.Called(ctx, nodeID)
	return args.Get(0).([]store.Credential), args.Error(1)
}

func (m *mockCredRepo) Update(ctx context.Context, params store.UpdateCredentialParams) (store.Credential, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.Credential), args.Error(1)
}

func (m *mockCredRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockCredRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *mockCredRepo) ListAll(ctx context.Context) ([]store.Credential, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]store.Credential), args.Error(1)
}

type mockNodeRepo struct {
	mock.Mock
}

func (m *mockNodeRepo) Create(ctx context.Context, params store.CreateNodeParams) (store.Node, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.Node), args.Error(1)
}

func (m *mockNodeRepo) GetByID(ctx context.Context, id uuid.UUID) (store.Node, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(store.Node), args.Error(1)
}

func (m *mockNodeRepo) GetByName(ctx context.Context, name string) (store.Node, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(store.Node), args.Error(1)
}

func (m *mockNodeRepo) List(ctx context.Context, filter store.NodeFilter) ([]store.Node, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]store.Node), args.Get(1).(int64), args.Error(2)
}

func (m *mockNodeRepo) ListActive(ctx context.Context) ([]store.Node, error) {
	args := m.Called(ctx)
	return args.Get(0).([]store.Node), args.Error(1)
}

func (m *mockNodeRepo) Update(ctx context.Context, params store.UpdateNodeParams) (store.Node, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.Node), args.Error(1)
}

func (m *mockNodeRepo) UpdateHeartbeat(ctx context.Context, id uuid.UUID, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *mockNodeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) Create(ctx context.Context, params store.CreateUserParams) (store.User, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (store.User, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]store.User, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]store.User), args.Error(1)
}

func (m *mockUserRepo) GetByUsername(ctx context.Context, username string) (store.User, error) {
	args := m.Called(ctx, username)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (store.User, error) {
	args := m.Called(ctx, email)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) GetBySubscriptionToken(ctx context.Context, token uuid.UUID) (store.User, error) {
	args := m.Called(ctx, token)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) RotateSubscriptionToken(ctx context.Context, id uuid.UUID) (store.User, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) List(ctx context.Context, filter store.UserFilter) ([]store.User, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]store.User), args.Get(1).(int64), args.Error(2)
}

func (m *mockUserRepo) Update(ctx context.Context, params store.UpdateUserParams) (store.User, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) UpdateTraffic(ctx context.Context, id uuid.UUID, bytes int64) error {
	args := m.Called(ctx, id, bytes)
	return args.Error(0)
}

func (m *mockUserRepo) ResetTraffic(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockUserRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockUserRepo) GetByTelegramID(ctx context.Context, tgID int64) (store.User, error) {
	args := m.Called(ctx, tgID)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) GetByReferralCode(ctx context.Context, code string) (store.User, error) {
	args := m.Called(ctx, code)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) ExtendSubscription(ctx context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (store.User, error) {
	args := m.Called(ctx, id, expiresAt, extraTrafficBytes)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) SetBanStatus(ctx context.Context, id uuid.UUID, isBanned bool, reason string) error {
	args := m.Called(ctx, id, isBanned, reason)
	return args.Error(0)
}

func (m *mockUserRepo) UpdateTelegramMetadata(ctx context.Context, id uuid.UUID, tgID int64, tgUsername string, trialUsed bool, referrerID *uuid.UUID, refCode string) (store.User, error) {
	args := m.Called(ctx, id, tgID, tgUsername, trialUsed, referrerID, refCode)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) CountReferrals(ctx context.Context, referrerID uuid.UUID) (int64, error) {
	args := m.Called(ctx, referrerID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockUserRepo) ListTelegramIDsForBroadcast(ctx context.Context, segment string) ([]int64, error) {
	args := m.Called(ctx, segment)
	return args.Get(0).([]int64), args.Error(1)
}

func (m *mockUserRepo) UpsertTelegramLead(ctx context.Context, params store.TelegramLeadParams) (store.User, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) UpdateEmail(ctx context.Context, id uuid.UUID, email string) (store.User, error) {
	args := m.Called(ctx, id, email)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) LinkTelegramEmail(ctx context.Context, tgID int64, email string) (store.User, error) {
	args := m.Called(ctx, tgID, email)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) RebindTelegramUser(ctx context.Context, email string, tgID int64, tgUsername, firstName, lastName string) (store.User, error) {
	args := m.Called(ctx, email, tgID, tgUsername, firstName, lastName)
	return args.Get(0).(store.User), args.Error(1)
}

func (m *mockUserRepo) CreateEmailVerification(ctx context.Context, tgID int64, email, otpHash, purpose string, ttl time.Duration) (store.UserEmailVerification, error) {
	args := m.Called(ctx, tgID, email, otpHash, purpose, ttl)
	return args.Get(0).(store.UserEmailVerification), args.Error(1)
}

func (m *mockUserRepo) GetActiveEmailVerification(ctx context.Context, tgID int64, purpose string) (*store.UserEmailVerification, error) {
	args := m.Called(ctx, tgID, purpose)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.UserEmailVerification), args.Error(1)
}

func (m *mockUserRepo) RecordVerificationAttempt(ctx context.Context, id uuid.UUID, success bool) error {
	args := m.Called(ctx, id, success)
	return args.Error(0)
}

func TestCredentialProvisioner_ProvisionAndRotate(t *testing.T) {
	credRepo := new(mockCredRepo)
	nodeRepo := new(mockNodeRepo)
	userRepo := new(mockUserRepo)

	provisioner := service.NewCredentialProvisioner(credRepo, nodeRepo, userRepo)
	ctx := context.Background()

	userID := uuid.New()
	nodeID := uuid.New()

	nodeRepo.On("ListActive", ctx).Return([]store.Node{
		{
			ID:     nodeID,
			Name:   "frankfurt-01",
			Status: pgtype.Text{String: "online", Valid: true},
		},
	}, nil)

	credRepo.On("ListActiveByNode", ctx, nodeID).Return([]store.Credential{}, nil)
	credRepo.On("Create", ctx, mock.Anything).Return(store.Credential{}, nil)

	err := provisioner.ProvisionUser(ctx, userID)
	require.NoError(t, err)

	userRepo.On("RotateSubscriptionToken", ctx, userID).Return(store.User{
		ID:                userID,
		SubscriptionToken: uuid.New(),
	}, nil)

	credRepo.On("DeleteByUser", ctx, userID).Return(nil)

	rotatedUser, err := provisioner.RotateUserCredentials(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, userID, rotatedUser.ID)
}

func TestCredentialProvisioner_IPAllocation_NoCollisions(t *testing.T) {
	credRepo := new(mockCredRepo)
	nodeRepo := new(mockNodeRepo)
	userRepo := new(mockUserRepo)

	provisioner := service.NewCredentialProvisioner(credRepo, nodeRepo, userRepo)
	ctx := context.Background()

	userID := uuid.New()
	nodeID := uuid.New()

	nodeRepo.On("ListActive", ctx).Return([]store.Node{
		{
			ID:     nodeID,
			Name:   "london-01",
			Status: pgtype.Text{String: "online", Valid: true},
		},
	}, nil)

	// Pre-occupy 10.8.0.2 and 10.8.0.3 on this node
	ip2 := netip.MustParseAddr("10.8.0.2")
	ip3 := netip.MustParseAddr("10.8.0.3")
	credRepo.On("ListActiveByNode", ctx, nodeID).Return([]store.Credential{
		{ID: uuid.New(), NodeID: nodeID, Ipv4: &ip2},
		{ID: uuid.New(), NodeID: nodeID, Ipv4: &ip3},
	}, nil)

	var allocatedIP *netip.Addr
	credRepo.On("Create", ctx, mock.MatchedBy(func(params store.CreateCredentialParams) bool {
		if params.Protocol == "amneziawg" {
			allocatedIP = params.Ipv4
		}
		return true
	})).Return(store.Credential{}, nil)

	err := provisioner.ProvisionUser(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, allocatedIP)

	// Must allocate next free IP: 10.8.0.4
	assert.Equal(t, "10.8.0.4", allocatedIP.String())
}

func TestProvisionerRevokePushesNodes(t *testing.T) {
	userID := uuid.New()
	nodeAID := uuid.New()
	nodeBID := uuid.New()
	credID := uuid.New()

	credRepo := &mockCredRepo{}
	credRepo.On("GetByID", mock.Anything, credID).Return(
		store.Credential{ID: credID, UserID: userID, NodeID: nodeAID}, nil)
	credRepo.On("Delete", mock.Anything, credID).Return(nil)
	credRepo.On("ListByUser", mock.Anything, userID).Return([]store.Credential{
		{ID: credID, UserID: userID, NodeID: nodeAID},
		{ID: uuid.New(), UserID: userID, NodeID: nodeBID},
	}, nil)
	credRepo.On("DeleteByUser", mock.Anything, userID).Return(nil)

	provisioner := service.NewCredentialProvisioner(credRepo, &mockNodeRepo{}, &mockUserRepo{})
	var pushed []uuid.UUID
	provisioner.SetConfigPusher(func(_ context.Context, id uuid.UUID) error {
		pushed = append(pushed, id)
		return nil
	})

	require.NoError(t, provisioner.RevokeCredential(context.Background(), credID))
	require.Len(t, pushed, 1)
	require.Equal(t, nodeAID, pushed[0])

	pushed = nil
	require.NoError(t, provisioner.RevokeUser(context.Background(), userID))
	require.Len(t, pushed, 2)
}
