package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockUserRepo struct {
	users map[uuid.UUID]store.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[uuid.UUID]store.User)}
}

func (m *mockUserRepo) Create(_ context.Context, params store.CreateUserParams) (store.User, error) {
	id := uuid.New()
	subToken := uuid.New()
	user := store.User{
		ID:                id,
		Email:             params.Email,
		Username:          params.Username,
		Status:            params.Status,
		PlanID:            params.PlanID,
		TrafficLimit:      params.TrafficLimit,
		TrafficUsed:       params.TrafficUsed,
		SubscriptionToken: subToken,
	}
	m.users[id] = user
	return user, nil
}

func (m *mockUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) GetByUsername(_ context.Context, _ string) (store.User, error) {
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) GetByEmail(_ context.Context, _ string) (store.User, error) {
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) GetBySubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) RotateSubscriptionToken(_ context.Context, id uuid.UUID) (store.User, error) {
	u, ok := m.users[id]
	if !ok {
		return store.User{}, errors.New("user not found")
	}
	u.SubscriptionToken = uuid.New()
	m.users[id] = u
	return u, nil
}

func (m *mockUserRepo) List(_ context.Context, filter store.UserFilter) ([]store.User, int64, error) {
	var list []store.User
	for _, u := range m.users {
		if filter.Status != "" && u.Status.String != filter.Status {
			continue
		}
		list = append(list, u)
	}
	return list, int64(len(list)), nil
}

func (m *mockUserRepo) Update(_ context.Context, params store.UpdateUserParams) (store.User, error) {
	u, ok := m.users[params.ID]
	if !ok {
		return store.User{}, errors.New("user not found")
	}
	u.Email = params.Email
	u.Username = params.Username
	u.Status = params.Status
	u.TrafficLimit = params.TrafficLimit
	m.users[params.ID] = u
	return u, nil
}

func (m *mockUserRepo) UpdateTraffic(_ context.Context, _ uuid.UUID, _ int64) error {
	return nil
}

func (m *mockUserRepo) ResetTraffic(_ context.Context, id uuid.UUID) error {
	u, ok := m.users[id]
	if !ok {
		return errors.New("user not found")
	}
	u.TrafficUsed.Int64 = 0
	m.users[id] = u
	return nil
}

func (m *mockUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.users, id)
	return nil
}

func (m *mockUserRepo) GetByTelegramID(_ context.Context, tgID int64) (store.User, error) {
	for _, u := range m.users {
		if u.TelegramID.Valid && u.TelegramID.Int64 == tgID {
			return u, nil
		}
	}
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) GetByReferralCode(_ context.Context, code string) (store.User, error) {
	for _, u := range m.users {
		if u.ReferralCode.Valid && u.ReferralCode.String == code {
			return u, nil
		}
	}
	return store.User{}, errors.New("user not found")
}

func (m *mockUserRepo) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.User, error) {
	result := make([]store.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := m.users[id]; ok {
			result = append(result, u)
		}
	}
	return result, nil
}

func (m *mockUserRepo) ExtendSubscription(_ context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (store.User, error) {
	u, ok := m.users[id]
	if !ok {
		return store.User{}, errors.New("user not found")
	}
	u.ExpiresAt = pgtype.Timestamptz{Time: expiresAt, Valid: true}
	u.TrafficLimit.Int64 += extraTrafficBytes
	m.users[id] = u
	return u, nil
}

func (m *mockUserRepo) SetBanStatus(_ context.Context, id uuid.UUID, isBanned bool, reason string) error {
	u, ok := m.users[id]
	if !ok {
		return errors.New("user not found")
	}
	u.IsBanned = pgtype.Bool{Bool: isBanned, Valid: true}
	u.BanReason = pgtype.Text{String: reason, Valid: true}
	m.users[id] = u
	return nil
}

func (m *mockUserRepo) UpdateTelegramMetadata(_ context.Context, id uuid.UUID, tgID int64, tgUsername string, trialUsed bool, referrerID *uuid.UUID, refCode string) (store.User, error) {
	u, ok := m.users[id]
	if !ok {
		return store.User{}, errors.New("user not found")
	}
	u.TelegramID = pgtype.Int8{Int64: tgID, Valid: tgID > 0}
	u.TelegramUsername = pgtype.Text{String: tgUsername, Valid: tgUsername != ""}
	u.TrialUsed = pgtype.Bool{Bool: trialUsed, Valid: true}
	if referrerID != nil {
		u.ReferrerID = pgtype.UUID{Bytes: *referrerID, Valid: true}
	}
	u.ReferralCode = pgtype.Text{String: refCode, Valid: refCode != ""}
	m.users[id] = u
	return u, nil
}

func (m *mockUserRepo) CountReferrals(_ context.Context, referrerID uuid.UUID) (int64, error) {
	var count int64
	for _, u := range m.users {
		if u.ReferrerID.Valid && u.ReferrerID.Bytes == referrerID {
			count++
		}
	}
	return count, nil
}

func (m *mockUserRepo) ListTelegramIDsForBroadcast(_ context.Context, _ string) ([]int64, error) {
	var ids []int64
	for _, u := range m.users {
		if u.TelegramID.Valid {
			ids = append(ids, u.TelegramID.Int64)
		}
	}
	return ids, nil
}


func TestUserHandler_CRUD(t *testing.T) {
	userRepo := newMockUserRepo()
	handler := NewUserHandler(userRepo, nil, nil)

	r := chi.NewRouter()
	r.Get("/users", handler.List)
	r.Post("/users", handler.Create)
	r.Get("/users/{id}", handler.Get)
	r.Patch("/users/{id}", handler.Update)
	r.Delete("/users/{id}", handler.Delete)
	r.Post("/users/{id}/reset-traffic", handler.ResetTraffic)
	r.Get("/users/{id}/subscription", handler.GetSubscription)
	r.Post("/users/{id}/subscription/rotate", handler.RotateSubscription)

	// 1. Create User
	createBody, _ := json.Marshal(CreateUserRequest{
		Email:        "alice@vpn.test",
		Username:     "alice",
		TrafficLimit: 10737418240, // 10 GB
	})
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var createdUser store.User
	err := json.Unmarshal(rec.Body.Bytes(), &createdUser)
	require.NoError(t, err)
	assert.Equal(t, "alice@vpn.test", createdUser.Email.String)
	assert.Equal(t, "alice", createdUser.Username)

	// 2. List Users
	req = httptest.NewRequest(http.MethodGet, "/users?page=1&per_page=20", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var listResp request.PaginatedResponse[store.User]
	err = json.Unmarshal(rec.Body.Bytes(), &listResp)
	require.NoError(t, err)
	assert.Len(t, listResp.Items, 1)

	// 3. Get User
	req = httptest.NewRequest(http.MethodGet, "/users/"+createdUser.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Update User
	updateBody, _ := json.Marshal(UpdateUserRequest{Status: "suspended"})
	req = httptest.NewRequest(http.MethodPatch, "/users/"+createdUser.ID.String(), bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var updatedUser store.User
	_ = json.Unmarshal(rec.Body.Bytes(), &updatedUser)
	assert.Equal(t, "suspended", updatedUser.Status.String)

	// 5. Reset Traffic
	req = httptest.NewRequest(http.MethodPost, "/users/"+createdUser.ID.String()+"/reset-traffic", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 6. Get Subscription
	req = httptest.NewRequest(http.MethodGet, "/users/"+createdUser.ID.String()+"/subscription", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var subResp SubscriptionResponse
	err = json.Unmarshal(rec.Body.Bytes(), &subResp)
	require.NoError(t, err)
	assert.NotEmpty(t, subResp.SubscriptionToken)
	assert.Contains(t, subResp.SubscriptionURL, "/sub/")

	// 7. Rotate Subscription
	req = httptest.NewRequest(http.MethodPost, "/users/"+createdUser.ID.String()+"/subscription/rotate", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var rotatedSub SubscriptionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rotatedSub)
	assert.NotEqual(t, subResp.SubscriptionToken, rotatedSub.SubscriptionToken)

	// 8. Delete User
	req = httptest.NewRequest(http.MethodDelete, "/users/"+createdUser.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
