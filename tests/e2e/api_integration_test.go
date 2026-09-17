package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In-memory repositories for complete isolated end-to-end integration tests

type e2eAdminRepo struct {
	admins map[uuid.UUID]store.Admin
}

func (r *e2eAdminRepo) Create(_ context.Context, p store.CreateAdminParams) (store.Admin, error) {
	id := uuid.New()
	a := store.Admin{
		ID:           id,
		Email:        p.Email,
		PasswordHash: p.PasswordHash,
		Role:         p.Role,
	}
	r.admins[id] = a
	return a, nil
}
func (r *e2eAdminRepo) GetByID(_ context.Context, id uuid.UUID) (store.Admin, error) {
	if a, ok := r.admins[id]; ok {
		return a, nil
	}
	return store.Admin{}, assert.AnError
}
func (r *e2eAdminRepo) GetByEmail(_ context.Context, email string) (store.Admin, error) {
	for _, a := range r.admins {
		if a.Email == email {
			return a, nil
		}
	}
	return store.Admin{}, assert.AnError
}
func (r *e2eAdminRepo) List(_ context.Context) ([]store.Admin, error) {
	var list []store.Admin
	for _, a := range r.admins {
		list = append(list, a)
	}
	return list, nil
}
func (r *e2eAdminRepo) Update(_ context.Context, _ store.UpdateAdminParams) (store.Admin, error) {
	return store.Admin{}, nil
}
func (r *e2eAdminRepo) UpdateLastLogin(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (r *e2eAdminRepo) UpdatePermissions(_ context.Context, id uuid.UUID, permissions []byte) error {
	if a, ok := r.admins[id]; ok {
		a.Permissions = permissions
		r.admins[id] = a
		return nil
	}
	return assert.AnError
}
func (r *e2eAdminRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.admins, id)
	return nil
}

func (r *e2eAdminRepo) SetMustChangePassword(_ context.Context, id uuid.UUID, must bool) error {
	if a, ok := r.admins[id]; ok {
		a.MustChangePassword = must
		r.admins[id] = a
	}
	return nil
}

func (r *e2eAdminRepo) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	if a, ok := r.admins[id]; ok {
		a.PasswordHash = hash
		a.MustChangePassword = false
		r.admins[id] = a
	}
	return nil
}

type e2eAPIKeyRepo struct {
	keys map[uuid.UUID]store.ApiKey
}

func (r *e2eAPIKeyRepo) Create(_ context.Context, p store.CreateAPIKeyParams) (store.ApiKey, error) {
	id := uuid.New()
	k := store.ApiKey{
		ID:        id,
		Name:      p.Name,
		KeyHash:   p.KeyHash,
		Prefix:    p.Prefix,
		Scopes:    p.Scopes,
		CreatedBy: p.CreatedBy,
	}
	r.keys[id] = k
	return k, nil
}
func (r *e2eAPIKeyRepo) GetByPrefix(_ context.Context, prefix string) (store.ApiKey, error) {
	for _, k := range r.keys {
		if k.Prefix == prefix {
			return k, nil
		}
	}
	return store.ApiKey{}, assert.AnError
}
func (r *e2eAPIKeyRepo) List(_ context.Context) ([]store.ApiKey, error) {
	var list []store.ApiKey
	for _, k := range r.keys {
		list = append(list, k)
	}
	return list, nil
}
func (r *e2eAPIKeyRepo) Update(_ context.Context, _ store.UpdateAPIKeyParams) (store.ApiKey, error) {
	return store.ApiKey{}, nil
}
func (r *e2eAPIKeyRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.keys, id)
	return nil
}
func (r *e2eAPIKeyRepo) GetAPIKeyByPrefix(ctx context.Context, prefix string) (store.ApiKey, error) {
	return r.GetByPrefix(ctx, prefix)
}

type e2eNodeRepo struct {
	nodes map[uuid.UUID]store.Node
}

func (r *e2eNodeRepo) Create(_ context.Context, p store.CreateNodeParams) (store.Node, error) {
	id := uuid.New()
	n := store.Node{
		ID:           id,
		Name:         p.Name,
		Endpoint:     p.Endpoint,
		GrpcEndpoint: p.GrpcEndpoint,
		Region:       p.Region,
		CapacityGbps: p.CapacityGbps,
		Status:       p.Status,
		Tags:         p.Tags,
	}
	r.nodes[id] = n
	return n, nil
}
func (r *e2eNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, assert.AnError
}
func (r *e2eNodeRepo) GetByName(_ context.Context, name string) (store.Node, error) {
	for _, n := range r.nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return store.Node{}, assert.AnError
}
func (r *e2eNodeRepo) List(_ context.Context, f store.NodeFilter) ([]store.Node, int64, error) {
	var list []store.Node
	for _, n := range r.nodes {
		if f.Status != "" && n.Status.String != f.Status {
			continue
		}
		if f.Region != "" && n.Region.String != f.Region {
			continue
		}
		list = append(list, n)
	}
	return list, int64(len(list)), nil
}
func (r *e2eNodeRepo) ListActive(_ context.Context) ([]store.Node, error) {
	return nil, nil
}
func (r *e2eNodeRepo) Update(_ context.Context, p store.UpdateNodeParams) (store.Node, error) {
	n, ok := r.nodes[p.ID]
	if !ok {
		return store.Node{}, assert.AnError
	}
	n.Name = p.Name
	n.Status = p.Status
	n.CapacityGbps = p.CapacityGbps
	r.nodes[p.ID] = n
	return n, nil
}
func (r *e2eNodeRepo) UpdateHeartbeat(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (r *e2eNodeRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.nodes, id)
	return nil
}

type e2ePlanRepo struct {
	plans map[uuid.UUID]store.Plan
}

func (r *e2ePlanRepo) Create(_ context.Context, p store.CreatePlanParams) (store.Plan, error) {
	id := uuid.New()
	pl := store.Plan{
		ID:           id,
		Name:         p.Name,
		MonthlyPrice: p.MonthlyPrice,
		TrafficLimit: p.TrafficLimit,
		DeviceLimit:  p.DeviceLimit,
		Protocols:    p.Protocols,
		Features:     p.Features,
		IsActive:     p.IsActive,
	}
	r.plans[id] = pl
	return pl, nil
}
func (r *e2ePlanRepo) GetByID(_ context.Context, id uuid.UUID) (store.Plan, error) {
	if p, ok := r.plans[id]; ok {
		return p, nil
	}
	return store.Plan{}, assert.AnError
}
func (r *e2ePlanRepo) GetByName(_ context.Context, name string) (store.Plan, error) {
	for _, p := range r.plans {
		if p.Name == name {
			return p, nil
		}
	}
	return store.Plan{}, assert.AnError
}
func (r *e2ePlanRepo) List(_ context.Context) ([]store.Plan, error) {
	var list []store.Plan
	for _, p := range r.plans {
		list = append(list, p)
	}
	return list, nil
}
func (r *e2ePlanRepo) Update(_ context.Context, p store.UpdatePlanParams) (store.Plan, error) {
	pl, ok := r.plans[p.ID]
	if !ok {
		return store.Plan{}, assert.AnError
	}
	pl.Name = p.Name
	pl.MonthlyPrice = p.MonthlyPrice
	r.plans[p.ID] = pl
	return pl, nil
}
func (r *e2ePlanRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.plans, id)
	return nil
}
func (r *e2ePlanRepo) GetTrial(_ context.Context) (store.Plan, error) {
	for _, p := range r.plans {
		if p.IsTrial.Bool {
			return p, nil
		}
	}
	return store.Plan{}, assert.AnError
}

type e2eUserRepo struct {
	users map[uuid.UUID]store.User
}

func (r *e2eUserRepo) Create(_ context.Context, p store.CreateUserParams) (store.User, error) {
	id := uuid.New()
	subToken := uuid.New()
	u := store.User{
		ID:                id,
		Email:             p.Email,
		Username:          p.Username,
		Status:            p.Status,
		PlanID:            p.PlanID,
		TrafficLimit:      p.TrafficLimit,
		TrafficUsed:       p.TrafficUsed,
		SubscriptionToken: subToken,
	}
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) GetByUsername(_ context.Context, _ string) (store.User, error) {
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) GetByEmail(_ context.Context, _ string) (store.User, error) {
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) GetBySubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) RotateSubscriptionToken(_ context.Context, id uuid.UUID) (store.User, error) {
	u, ok := r.users[id]
	if !ok {
		return store.User{}, assert.AnError
	}
	u.SubscriptionToken = uuid.New()
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) List(_ context.Context, f store.UserFilter) ([]store.User, int64, error) {
	var list []store.User
	for _, u := range r.users {
		if f.Status != "" && u.Status.String != f.Status {
			continue
		}
		list = append(list, u)
	}
	return list, int64(len(list)), nil
}
func (r *e2eUserRepo) Update(_ context.Context, p store.UpdateUserParams) (store.User, error) {
	u, ok := r.users[p.ID]
	if !ok {
		return store.User{}, assert.AnError
	}
	u.Status = p.Status
	r.users[p.ID] = u
	return u, nil
}
func (r *e2eUserRepo) UpdateTraffic(_ context.Context, _ uuid.UUID, _ int64) error {
	return nil
}
func (r *e2eUserRepo) ResetTraffic(_ context.Context, id uuid.UUID) error {
	u, ok := r.users[id]
	if !ok {
		return assert.AnError
	}
	u.TrafficUsed.Int64 = 0
	r.users[id] = u
	return nil
}
func (r *e2eUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.users, id)
	return nil
}
func (r *e2eUserRepo) GetByTelegramID(_ context.Context, tgID int64) (store.User, error) {
	for _, u := range r.users {
		if u.TelegramID.Valid && u.TelegramID.Int64 == tgID {
			return u, nil
		}
	}
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) GetByReferralCode(_ context.Context, code string) (store.User, error) {
	for _, u := range r.users {
		if u.ReferralCode.Valid && u.ReferralCode.String == code {
			return u, nil
		}
	}
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) ExtendSubscription(_ context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (store.User, error) {
	u, ok := r.users[id]
	if !ok {
		return store.User{}, assert.AnError
	}
	u.ExpiresAt = pgtype.Timestamptz{Time: expiresAt, Valid: true}
	u.TrafficLimit.Int64 += extraTrafficBytes
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) SetBanStatus(_ context.Context, id uuid.UUID, isBanned bool, reason string) error {
	u, ok := r.users[id]
	if !ok {
		return assert.AnError
	}
	u.IsBanned = pgtype.Bool{Bool: isBanned, Valid: true}
	u.BanReason = pgtype.Text{String: reason, Valid: true}
	r.users[id] = u
	return nil
}
func (r *e2eUserRepo) UpdateTelegramMetadata(_ context.Context, id uuid.UUID, tgID int64, tgUsername string, trialUsed bool, referrerID *uuid.UUID, refCode string) (store.User, error) {
	u, ok := r.users[id]
	if !ok {
		return store.User{}, assert.AnError
	}
	u.TelegramID = pgtype.Int8{Int64: tgID, Valid: tgID > 0}
	u.TelegramUsername = pgtype.Text{String: tgUsername, Valid: tgUsername != ""}
	u.TrialUsed = pgtype.Bool{Bool: trialUsed, Valid: true}
	if referrerID != nil {
		u.ReferrerID = pgtype.UUID{Bytes: *referrerID, Valid: true}
	}
	u.ReferralCode = pgtype.Text{String: refCode, Valid: refCode != ""}
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) CountReferrals(_ context.Context, referrerID uuid.UUID) (int64, error) {
	var count int64
	for _, u := range r.users {
		if u.ReferrerID.Valid && u.ReferrerID.Bytes == referrerID {
			count++
		}
	}
	return count, nil
}
func (r *e2eUserRepo) ListTelegramIDsForBroadcast(_ context.Context, _ string) ([]int64, error) {
	var ids []int64
	for _, u := range r.users {
		if u.TelegramID.Valid {
			ids = append(ids, u.TelegramID.Int64)
		}
	}
	return ids, nil
}
func (r *e2eUserRepo) UpsertTelegramLead(_ context.Context, p store.TelegramLeadParams) (store.User, error) {
	for id, u := range r.users {
		if u.TelegramID.Valid && u.TelegramID.Int64 == p.TelegramID {
			if p.TelegramUsername != "" {
				u.TelegramUsername = pgtype.Text{String: p.TelegramUsername, Valid: true}
			}
			r.users[id] = u
			return u, nil
		}
	}
	id := uuid.New()
	u := store.User{
		ID:               id,
		Username:         p.TelegramUsername,
		TelegramID:       pgtype.Int8{Int64: p.TelegramID, Valid: true},
		TelegramUsername: pgtype.Text{String: p.TelegramUsername, Valid: p.TelegramUsername != ""},
		Status:           pgtype.Text{String: "lead", Valid: true},
	}
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.User, error) {
	result := make([]store.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := r.users[id]; ok {
			result = append(result, u)
		}
	}
	return result, nil
}
func (r *e2eUserRepo) UpdateEmail(_ context.Context, id uuid.UUID, email string) (store.User, error) {
	u, ok := r.users[id]
	if !ok {
		return store.User{}, assert.AnError
	}
	u.Email = pgtype.Text{String: email, Valid: email != ""}
	r.users[id] = u
	return u, nil
}
func (r *e2eUserRepo) LinkTelegramEmail(_ context.Context, tgID int64, email string) (store.User, error) {
	for id, u := range r.users {
		if u.TelegramID.Valid && u.TelegramID.Int64 == tgID {
			u.Email = pgtype.Text{String: email, Valid: email != ""}
			r.users[id] = u
			return u, nil
		}
	}
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) RebindTelegramUser(_ context.Context, email string, tgID int64, tgUsername, firstName, lastName string) (store.User, error) {
	for id, u := range r.users {
		if u.Email.Valid && u.Email.String == email {
			u.TelegramID = pgtype.Int8{Int64: tgID, Valid: true}
			u.TelegramUsername = pgtype.Text{String: tgUsername, Valid: tgUsername != ""}
			u.TelegramFirstName = pgtype.Text{String: firstName, Valid: firstName != ""}
			u.TelegramLastName = pgtype.Text{String: lastName, Valid: lastName != ""}
			r.users[id] = u
			return u, nil
		}
	}
	return store.User{}, assert.AnError
}
func (r *e2eUserRepo) CreateEmailVerification(_ context.Context, tgID int64, email, otpHash, purpose string, ttl time.Duration) (store.UserEmailVerification, error) {
	return store.UserEmailVerification{
		ID:                uuid.New(),
		TelegramID:        tgID,
		Email:             email,
		OTPHash:           otpHash,
		Purpose:           purpose,
		AttemptsRemaining: 3,
		ExpiresAt:         time.Now().Add(ttl),
		CreatedAt:         time.Now(),
	}, nil
}
func (r *e2eUserRepo) GetActiveEmailVerification(_ context.Context, _ int64, _ string) (*store.UserEmailVerification, error) {
	return nil, assert.AnError
}
func (r *e2eUserRepo) RecordVerificationAttempt(_ context.Context, _ uuid.UUID, _ bool) error {
	return nil
}

type e2eCredRepo struct {
	creds map[uuid.UUID]store.Credential
}

func (r *e2eCredRepo) Create(_ context.Context, p store.CreateCredentialParams) (store.Credential, error) {
	id := uuid.New()
	c := store.Credential{
		ID:           id,
		UserID:       p.UserID,
		NodeID:       p.NodeID,
		Protocol:     p.Protocol,
		PublicKey:    p.PublicKey,
		PresharedKey: p.PresharedKey,
		Ipv4:         p.Ipv4,
		Ipv6:         p.Ipv6,
	}
	r.creds[id] = c
	return c, nil
}
func (r *e2eCredRepo) GetByID(_ context.Context, id uuid.UUID) (store.Credential, error) {
	if c, ok := r.creds[id]; ok {
		return c, nil
	}
	return store.Credential{}, assert.AnError
}
func (r *e2eCredRepo) GetByUserNodeProtocol(_ context.Context, _, _ uuid.UUID, _ string) (store.Credential, error) {
	return store.Credential{}, assert.AnError
}
func (r *e2eCredRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]store.Credential, error) {
	var list []store.Credential
	for _, c := range r.creds {
		if c.UserID == userID {
			list = append(list, c)
		}
	}
	return list, nil
}
func (r *e2eCredRepo) ListByNode(_ context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	var list []store.Credential
	for _, c := range r.creds {
		if c.NodeID == nodeID {
			list = append(list, c)
		}
	}
	return list, nil
}
func (r *e2eCredRepo) ListActiveByNode(_ context.Context, _ uuid.UUID) ([]store.Credential, error) {
	return nil, nil
}
func (r *e2eCredRepo) ListAll(_ context.Context) ([]store.Credential, error) {
	result := make([]store.Credential, 0, len(r.creds))
	for _, c := range r.creds {
		result = append(result, c)
	}
	return result, nil
}
func (r *e2eCredRepo) Update(_ context.Context, p store.UpdateCredentialParams) (store.Credential, error) {
	c, ok := r.creds[p.ID]
	if !ok {
		return store.Credential{}, assert.AnError
	}
	c.PublicKey = p.PublicKey
	r.creds[p.ID] = c
	return c, nil
}
func (r *e2eCredRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.creds, id)
	return nil
}
func (r *e2eCredRepo) DeleteByUser(_ context.Context, userID uuid.UUID) error {
	for id, c := range r.creds {
		if c.UserID == userID {
			delete(r.creds, id)
		}
	}
	return nil
}

type e2eTrafficRepo struct{}

func (r *e2eTrafficRepo) Upsert(_ context.Context, _ store.UpsertTrafficStatsParams) (store.TrafficStat, error) {
	return store.TrafficStat{}, nil
}
func (r *e2eTrafficRepo) GetByUserHour(_ context.Context, _, _ uuid.UUID, _ string, _ time.Time) (store.TrafficStat, error) {
	return store.TrafficStat{}, nil
}
func (r *e2eTrafficRepo) ListByUser(_ context.Context, _ uuid.UUID, _, _ time.Time) ([]store.TrafficStat, error) {
	return nil, nil
}
func (r *e2eTrafficRepo) GetAggregateByUser(_ context.Context, _ uuid.UUID, _, _ time.Time) (store.GetTrafficAggregateByUserRow, error) {
	return store.GetTrafficAggregateByUserRow{}, nil
}
func (r *e2eTrafficRepo) GetAggregateByNode(_ context.Context, _, _ time.Time) ([]store.GetTrafficAggregateByNodeRow, error) {
	return []store.GetTrafficAggregateByNodeRow{
		{NodeID: uuid.New(), TotalRx: 104857600, TotalTx: 524288000},
	}, nil
}

func TestE2E_FullResourceLifecycle(t *testing.T) {
	adminRepo := &e2eAdminRepo{admins: make(map[uuid.UUID]store.Admin)}
	apiKeyRepo := &e2eAPIKeyRepo{keys: make(map[uuid.UUID]store.ApiKey)}
	nodeRepo := &e2eNodeRepo{nodes: make(map[uuid.UUID]store.Node)}
	planRepo := &e2ePlanRepo{plans: make(map[uuid.UUID]store.Plan)}
	userRepo := &e2eUserRepo{users: make(map[uuid.UUID]store.User)}
	credRepo := &e2eCredRepo{creds: make(map[uuid.UUID]store.Credential)}
	trafficRepo := &e2eTrafficRepo{}

	jwtMgr := auth.NewJWTManager("test-jwt-super-secret-key-32-chars-long", 15*time.Minute, 24*time.Hour)
	apiKeyMgr := auth.NewAPIKeyManager(apiKeyRepo)
	pwdMgr := auth.NewPasswordManager(4)
	totpMgr := auth.NewTOTPManager("VPN-E2E")
	blacklist := auth.NewMemoryBlacklist()

	authenticator := middleware.NewAuthenticator(jwtMgr, apiKeyMgr)
	rateLimiter := middleware.NewRateLimiter(nil, 500, time.Minute)

	authH := handler.NewAuthHandler(adminRepo, jwtMgr, pwdMgr, totpMgr, blacklist)
	nodeH := handler.NewNodeHandler(nodeRepo, nil)
	userH := handler.NewUserHandler(userRepo, planRepo, nil)
	planH := handler.NewPlanHandler(planRepo, nil)
	credH := handler.NewCredentialHandler(credRepo, userRepo, nodeRepo, nil)
	analyticsH := handler.NewAnalyticsHandler(trafficRepo)
	adminH := handler.NewAdminHandler(adminRepo, apiKeyRepo, apiKeyMgr, pwdMgr, nil)

	handlers := api.Handlers{
		Auth:       authH,
		Node:       nodeH,
		User:       userH,
		Plan:       planH,
		Credential: credH,
		Analytics:  analyticsH,
		Admin:      adminH,
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			CORSAllowedOrigins: []string{"http://localhost:3000"},
		},
	}
	router := api.NewRouter(cfg, handlers, authenticator, rateLimiter, nil, nil)
	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()

	// 1. Seed initial SuperAdmin
	adminHash, err := pwdMgr.Hash("RootPassword2026!")
	require.NoError(t, err)
	seedAdmin, err := adminRepo.Create(context.Background(), store.CreateAdminParams{
		Email:        "root@vpn.internal",
		PasswordHash: adminHash,
		Role:         pgtype.Text{String: "superadmin", Valid: true},
	})
	require.NoError(t, err)

	// 2. Login to obtain JWT
	loginBody, _ := json.Marshal(handler.LoginRequest{
		Email:    "root@vpn.internal",
		Password: "RootPassword2026!",
	})
	resp, err := client.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var tokenResp handler.TokenResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenResp)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenResp.AccessToken)
	jwtToken := tokenResp.AccessToken

	// 3. Issue API Key via JWT
	createKeyBody, _ := json.Marshal(handler.CreateAPIKeyRequest{
		Name:   "e2e-automation-key",
		Scopes: []string{"admin", "nodes:write", "users:write"},
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/api-keys", bytes.NewReader(createKeyBody))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var keyResp handler.CreateAPIKeyResponse
	err = json.NewDecoder(resp.Body).Decode(&keyResp)
	require.NoError(t, err)
	assert.NotEmpty(t, keyResp.RawKey)
	apiKey := keyResp.RawKey

	// Helper for making API Key authenticated requests
	doKeyRequest := func(method, path string, body any) (*http.Response, error) {
		var bodyReader *bytes.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(data)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		r, err := http.NewRequest(method, server.URL+path, bodyReader)
		if err != nil {
			return nil, err
		}
		r.Header.Set("X-API-Key", apiKey)
		r.Header.Set("Content-Type", "application/json")
		return client.Do(r)
	}

	// 4. Create Node via X-API-Key
	createNodeBody := handler.CreateNodeRequest{
		Name:      "stockholm-node-01",
		Region:    "eu-north",
		Country:   "SE",
		City:      "Stockholm",
		PublicIP:  "203.0.113.55",
		Capacity:  2000,
		Protocols: []string{"wireguard", "amneziawg"},
		Tags:      []string{"sweden", "fast"},
	}
	resp, err = doKeyRequest(http.MethodPost, "/api/v1/nodes", createNodeBody)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var node store.Node
	_ = json.NewDecoder(resp.Body).Decode(&node)
	assert.Equal(t, "stockholm-node-01", node.Name)
	nodeID := node.ID

	// 5. List Nodes
	resp, err = doKeyRequest(http.MethodGet, "/api/v1/nodes", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var nodesList request.PaginatedResponse[store.Node]
	_ = json.NewDecoder(resp.Body).Decode(&nodesList)
	assert.Len(t, nodesList.Items, 1)

	// 6. Create Plan
	createPlanBody := handler.CreatePlanRequest{
		Name:        "Pro 100GB",
		Price:       "14.99",
		DeviceLimit: 10,
		Protocols:   []string{"wireguard", "amneziawg"},
	}
	resp, err = doKeyRequest(http.MethodPost, "/api/v1/plans", createPlanBody)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var plan store.Plan
	_ = json.NewDecoder(resp.Body).Decode(&plan)
	assert.Equal(t, "Pro 100GB", plan.Name)
	planID := plan.ID

	// 7. Create User
	createUserBody := handler.CreateUserRequest{
		Email:        "bob@test.vpn",
		Username:     "bob_the_tester",
		PlanID:       &planID,
		TrafficLimit: 107374182400, // 100GB
	}
	resp, err = doKeyRequest(http.MethodPost, "/api/v1/users", createUserBody)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var user store.User
	_ = json.NewDecoder(resp.Body).Decode(&user)
	assert.Equal(t, "bob_the_tester", user.Username)
	userID := user.ID

	// 8. Retrieve Subscription
	resp, err = doKeyRequest(http.MethodGet, fmt.Sprintf("/api/v1/users/%s/subscription", userID), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var subResp handler.SubscriptionResponse
	_ = json.NewDecoder(resp.Body).Decode(&subResp)
	assert.NotEmpty(t, subResp.SubscriptionToken)

	// 9. Provision WireGuard Credential for User on Node
	createCredBody := handler.CreateCredentialRequest{
		UserID:   userID,
		NodeID:   nodeID,
		Protocol: "wireguard",
		IPv4:     "10.8.0.5",
	}
	resp, err = doKeyRequest(http.MethodPost, "/api/v1/credentials", createCredBody)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var credResp handler.ProvisionCredentialResponse
	_ = json.NewDecoder(resp.Body).Decode(&credResp)
	assert.NotEmpty(t, credResp.PrivateKey)
	assert.NotEmpty(t, credResp.Credential.PublicKey.String)
	credID := credResp.Credential.ID

	// 10. Rotate Credential Keys
	resp, err = doKeyRequest(http.MethodPost, fmt.Sprintf("/api/v1/credentials/%s/rotate", credID), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var rotatedCredResp handler.ProvisionCredentialResponse
	_ = json.NewDecoder(resp.Body).Decode(&rotatedCredResp)
	assert.NotEqual(t, credResp.PrivateKey, rotatedCredResp.PrivateKey)
	assert.NotEqual(t, credResp.Credential.PublicKey.String, rotatedCredResp.Credential.PublicKey.String)

	// 11. Query Traffic Analytics Overview
	resp, err = doKeyRequest(http.MethodGet, "/api/v1/analytics/overview", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var overviewResp handler.TrafficOverviewResponse
	_ = json.NewDecoder(resp.Body).Decode(&overviewResp)
	assert.Equal(t, int64(104857600), overviewResp.TotalRxBytes)
	assert.Equal(t, int64(524288000), overviewResp.TotalTxBytes)

	// 12. Reset User Traffic
	resp, err = doKeyRequest(http.MethodPost, fmt.Sprintf("/api/v1/users/%s/reset-traffic", userID), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 13. Teardown & Delete all created resources
	delCredResp, _ := doKeyRequest(http.MethodDelete, fmt.Sprintf("/api/v1/credentials/%s", credID), nil)
	assert.Equal(t, http.StatusNoContent, delCredResp.StatusCode)

	delUserResp, _ := doKeyRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%s", userID), nil)
	assert.Equal(t, http.StatusNoContent, delUserResp.StatusCode)

	delPlanResp, _ := doKeyRequest(http.MethodDelete, fmt.Sprintf("/api/v1/plans/%s", planID), nil)
	assert.Equal(t, http.StatusNoContent, delPlanResp.StatusCode)

	delNodeResp, _ := doKeyRequest(http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%s", nodeID), nil)
	assert.Equal(t, http.StatusNoContent, delNodeResp.StatusCode)

	delKeyResp, _ := doKeyRequest(http.MethodDelete, fmt.Sprintf("/api/v1/api-keys/%s", keyResp.ID), nil)
	assert.Equal(t, http.StatusNoContent, delKeyResp.StatusCode)

	_ = seedAdmin
}

func TestE2E_SecurityAndValidation(t *testing.T) {
	adminRepo := &e2eAdminRepo{admins: make(map[uuid.UUID]store.Admin)}
	apiKeyRepo := &e2eAPIKeyRepo{keys: make(map[uuid.UUID]store.ApiKey)}
	nodeRepo := &e2eNodeRepo{nodes: make(map[uuid.UUID]store.Node)}
	planRepo := &e2ePlanRepo{plans: make(map[uuid.UUID]store.Plan)}
	userRepo := &e2eUserRepo{users: make(map[uuid.UUID]store.User)}
	credRepo := &e2eCredRepo{creds: make(map[uuid.UUID]store.Credential)}
	trafficRepo := &e2eTrafficRepo{}

	jwtMgr := auth.NewJWTManager("test-jwt-super-secret-key-32-chars-long", 15*time.Minute, 24*time.Hour)
	apiKeyMgr := auth.NewAPIKeyManager(apiKeyRepo)
	pwdMgr := auth.NewPasswordManager(4)
	totpMgr := auth.NewTOTPManager("VPN-E2E")
	blacklist := auth.NewMemoryBlacklist()

	authenticator := middleware.NewAuthenticator(jwtMgr, apiKeyMgr)
	rateLimiter := middleware.NewRateLimiter(nil, 500, time.Minute)

	handlers := api.Handlers{
		Auth:       handler.NewAuthHandler(adminRepo, jwtMgr, pwdMgr, totpMgr, blacklist),
		Node:       handler.NewNodeHandler(nodeRepo, nil),
		User:       handler.NewUserHandler(userRepo, planRepo, nil),
		Plan:       handler.NewPlanHandler(planRepo, nil),
		Credential: handler.NewCredentialHandler(credRepo, userRepo, nodeRepo, nil),
		Analytics:  handler.NewAnalyticsHandler(trafficRepo),
		Admin:      handler.NewAdminHandler(adminRepo, apiKeyRepo, apiKeyMgr, pwdMgr, nil),
	}

	router := api.NewRouter(&config.Config{}, handlers, authenticator, rateLimiter, nil, nil)
	server := httptest.NewServer(router)
	defer server.Close()
	client := server.Client()

	t.Run("UnauthorizedRequests", func(t *testing.T) {
		// No auth header
		resp, err := client.Get(server.URL + "/api/v1/nodes")
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

		// Invalid bearer token
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/nodes", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-string")
		resp, err = client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

		// Invalid API key
		req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/nodes", nil)
		req.Header.Set("X-API-Key", "vpn_invalidkeyhashthatdoesntexist")
		resp, err = client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("RFC7807_ValidationErrors", func(t *testing.T) {
		// Valid admin to bypass auth
		adminHash, _ := pwdMgr.Hash("RootPassword2026!")
		admin, _ := adminRepo.Create(context.Background(), store.CreateAdminParams{
			Email:        "admin2@vpn.test",
			PasswordHash: adminHash,
			Role:         pgtype.Text{String: "admin", Valid: true},
		})
		token, _ := jwtMgr.GenerateAccessToken(admin.ID, "admin", "session-123")

		// Invalid Node Body (missing required fields)
		badNode := []byte(`{"name": ""}`)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes", bytes.NewReader(badNode))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var prob response.ProblemDetails
		err = json.NewDecoder(resp.Body).Decode(&prob)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, prob.Status)
		assert.Equal(t, "Bad Request", prob.Title)
		assert.Equal(t, "Validation failed", prob.Detail)
		assert.NotEmpty(t, prob.InvalidParams)

		// Invalid User Body (invalid email format)
		badUser := []byte(`{"email": "not-an-email", "username": "validname"}`)
		req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/users", bytes.NewReader(badUser))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err = client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}
