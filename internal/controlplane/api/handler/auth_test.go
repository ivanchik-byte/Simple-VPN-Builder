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

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAdminRepo struct {
	admins map[uuid.UUID]store.Admin
	emails map[string]uuid.UUID
}

func newMockAdminRepo() *mockAdminRepo {
	return &mockAdminRepo{
		admins: make(map[uuid.UUID]store.Admin),
		emails: make(map[string]uuid.UUID),
	}
}

func (m *mockAdminRepo) Create(_ context.Context, params store.CreateAdminParams) (store.Admin, error) {
	id := uuid.New()
	admin := store.Admin{
		ID:           id,
		Email:        params.Email,
		PasswordHash: params.PasswordHash,
		Role:         params.Role,
		TotpSecret:   params.TotpSecret,
	}
	m.admins[id] = admin
	m.emails[params.Email] = id
	return admin, nil
}

func (m *mockAdminRepo) GetByID(_ context.Context, id uuid.UUID) (store.Admin, error) {
	if a, ok := m.admins[id]; ok {
		return a, nil
	}
	return store.Admin{}, errors.New("admin not found")
}

func (m *mockAdminRepo) GetByEmail(_ context.Context, email string) (store.Admin, error) {
	if id, ok := m.emails[email]; ok {
		return m.admins[id], nil
	}
	return store.Admin{}, errors.New("admin not found")
}

func (m *mockAdminRepo) List(_ context.Context) ([]store.Admin, error) {
	list := make([]store.Admin, 0, len(m.admins))
	for _, a := range m.admins {
		list = append(list, a)
	}
	return list, nil
}

func (m *mockAdminRepo) Update(_ context.Context, params store.UpdateAdminParams) (store.Admin, error) {
	a, ok := m.admins[params.ID]
	if !ok {
		return store.Admin{}, errors.New("admin not found")
	}
	a.Email = params.Email
	a.PasswordHash = params.PasswordHash
	a.Role = params.Role
	a.TotpSecret = params.TotpSecret
	m.admins[params.ID] = a
	return a, nil
}

func (m *mockAdminRepo) UpdateLastLogin(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockAdminRepo) UpdatePermissions(_ context.Context, id uuid.UUID, permissions []byte) error {
	if a, ok := m.admins[id]; ok {
		a.Permissions = permissions
		m.admins[id] = a
		return nil
	}
	return errors.New("admin not found")
}

func (m *mockAdminRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.admins, id)
	return nil
}

func (m *mockAdminRepo) SetMustChangePassword(_ context.Context, id uuid.UUID, must bool) error {
	if a, ok := m.admins[id]; ok {
		a.MustChangePassword = must
		m.admins[id] = a
	}
	return nil
}

func (m *mockAdminRepo) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	if a, ok := m.admins[id]; ok {
		a.PasswordHash = hash
		a.MustChangePassword = false
		m.admins[id] = a
	}
	return nil
}

func setupTestAuthHandler(t *testing.T) (*AuthHandler, *mockAdminRepo, *auth.JWTManager, *auth.PasswordManager, *auth.TOTPManager) {
	t.Helper()
	repo := newMockAdminRepo()
	jwtMgr := auth.NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 7*24*time.Hour)
	pwdMgr := auth.NewPasswordManager(10)
	totpMgr := auth.NewTOTPManager("TestApp")
	blacklist := auth.NewMemoryBlacklist()
	jwtMgr.WithBlacklist(blacklist)

	handler := NewAuthHandler(repo, jwtMgr, pwdMgr, totpMgr, blacklist)
	return handler, repo, jwtMgr, pwdMgr, totpMgr
}

func TestAuthHandler_Login(t *testing.T) {
	handler, repo, _, pwdMgr, _ := setupTestAuthHandler(t)
	ctx := context.Background()

	hash, err := pwdMgr.Hash("correct-password")
	require.NoError(t, err)

	_, err = repo.Create(ctx, store.CreateAdminParams{
		Email:        "admin@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
	})
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{
			Email:    "admin@vpn.test",
			Password: "correct-password",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp TokenResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotEmpty(t, resp.RefreshToken)
		assert.Equal(t, "admin@vpn.test", resp.Email)
		assert.Equal(t, "admin", resp.Role)
	})

	t.Run("InvalidPassword", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{
			Email:    "admin@vpn.test",
			Password: "wrong-password",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("NotFound", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{
			Email:    "unknown@vpn.test",
			Password: "any-password",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("EmptyFields", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{Email: "", Password: ""})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Login(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestAuthHandler_Login_WithTOTP(t *testing.T) {
	handler, repo, _, pwdMgr, _ := setupTestAuthHandler(t)
	ctx := context.Background()

	hash, _ := pwdMgr.Hash("password123")
	secret := "JBSWY3DPEHPK3PXP" // Valid base32 TOTP secret

	_, err := repo.Create(ctx, store.CreateAdminParams{
		Email:        "totp_admin@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
		TotpSecret:   pgtype.Text{String: secret, Valid: true},
	})
	require.NoError(t, err)

	// Missing code should fail
	body, _ := json.Marshal(LoginRequest{
		Email:    "totp_admin@vpn.test",
		Password: "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Login(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Valid code should pass
	validCode, _ := totp.GenerateCode(secret, time.Now().UTC())
	body, _ = json.Marshal(LoginRequest{
		Email:    "totp_admin@vpn.test",
		Password: "password123",
		TOTPCode: validCode,
	})
	req = httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	handler.Login(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_RefreshAndLogout(t *testing.T) {
	handler, repo, jwtMgr, pwdMgr, _ := setupTestAuthHandler(t)
	ctx := context.Background()

	hash, _ := pwdMgr.Hash("secret")
	admin, err := repo.Create(ctx, store.CreateAdminParams{
		Email:        "refresh@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
	})
	require.NoError(t, err)

	refreshToken, err := jwtMgr.GenerateRefreshToken(admin.ID)
	require.NoError(t, err)

	t.Run("RefreshSuccess", func(t *testing.T) {
		body, _ := json.Marshal(RefreshRequest{RefreshToken: refreshToken})
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.Refresh(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp TokenResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotEmpty(t, resp.RefreshToken)
	})

	t.Run("LogoutRevokesToken", func(t *testing.T) {
		token, err := jwtMgr.GenerateAccessToken(admin.ID, admin.Email, "admin")
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.Logout(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Validating token now must return ErrTokenRevoked
		_, err = jwtMgr.ValidateToken(token)
		assert.ErrorIs(t, err, auth.ErrTokenRevoked)
	})
}

func TestAuthHandler_TOTP_SetupAndVerify(t *testing.T) {
	handler, repo, _, pwdMgr, _ := setupTestAuthHandler(t)
	ctx := context.Background()

	hash, _ := pwdMgr.Hash("secret")
	admin, err := repo.Create(ctx, store.CreateAdminParams{
		Email:        "setup_totp@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
	})
	require.NoError(t, err)

	authCtx := &middleware.AuthContext{
		UserID: admin.ID,
		Email:  admin.Email,
		Role:   "admin",
	}

	// 1. Setup TOTP
	req := httptest.NewRequest(http.MethodPost, "/auth/totp/setup", nil)
	req = req.WithContext(context.WithValue(ctx, middleware.AuthCtxKey, authCtx))
	rec := httptest.NewRecorder()

	handler.SetupTOTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var setupResp TOTPSetupResponse
	err = json.Unmarshal(rec.Body.Bytes(), &setupResp)
	require.NoError(t, err)
	assert.NotEmpty(t, setupResp.Secret)

	// 2. Verify with valid code
	code, err := totp.GenerateCode(setupResp.Secret, time.Now().UTC())
	require.NoError(t, err)

	verifyBody, _ := json.Marshal(TOTPVerifyRequest{
		Secret: setupResp.Secret,
		Code:   code,
	})
	reqVerify := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", bytes.NewReader(verifyBody))
	reqVerify = reqVerify.WithContext(context.WithValue(ctx, middleware.AuthCtxKey, authCtx))
	recVerify := httptest.NewRecorder()

	handler.VerifyTOTP(recVerify, reqVerify)
	assert.Equal(t, http.StatusOK, recVerify.Code)

	// Confirm secret persisted in repo
	updatedAdmin, err := repo.GetByID(ctx, admin.ID)
	require.NoError(t, err)
	assert.Equal(t, setupResp.Secret, updatedAdmin.TotpSecret.String)

	// 3. Unauthenticated setup must fail
	unauthReq := httptest.NewRequest(http.MethodPost, "/auth/totp/setup", nil)
	unauthRec := httptest.NewRecorder()
	handler.SetupTOTP(unauthRec, unauthReq)
	assert.Equal(t, http.StatusUnauthorized, unauthRec.Code)

	// 4. Invalid verify payload must fail
	badVerifyReq := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", bytes.NewReader([]byte("{}")))
	badVerifyReq = badVerifyReq.WithContext(context.WithValue(ctx, middleware.AuthCtxKey, authCtx))
	badVerifyRec := httptest.NewRecorder()
	handler.VerifyTOTP(badVerifyRec, badVerifyReq)
	assert.Equal(t, http.StatusBadRequest, badVerifyRec.Code)

	// 5. Wrong code must fail
	wrongCodeBody, _ := json.Marshal(TOTPVerifyRequest{Secret: setupResp.Secret, Code: "000000"})
	wrongCodeReq := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", bytes.NewReader(wrongCodeBody))
	wrongCodeReq = wrongCodeReq.WithContext(context.WithValue(ctx, middleware.AuthCtxKey, authCtx))
	wrongCodeRec := httptest.NewRecorder()
	handler.VerifyTOTP(wrongCodeRec, wrongCodeReq)
	assert.Equal(t, http.StatusBadRequest, wrongCodeRec.Code)
}

func TestAuthHandler_ForcedPasswordChange(t *testing.T) {
	handler, repo, _, pwdMgr, _ := setupTestAuthHandler(t)
	ctx := context.Background()

	hash, err := pwdMgr.Hash("default-password-123")
	require.NoError(t, err)

	created, err := repo.Create(ctx, store.CreateAdminParams{
		Email:        "seed@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "owner", Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, repo.SetMustChangePassword(ctx, created.ID, true))

	login := func(password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(LoginRequest{Email: "seed@vpn.test", Password: password})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.Login(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusForbidden, login("default-password-123").Code)

	change := func(old, fresh string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(ChangePasswordRequest{Email: "seed@vpn.test", Password: old, NewPassword: fresh})
		req := httptest.NewRequest(http.MethodPost, "/auth/change-password", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChangePassword(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusUnauthorized, change("wrong-old", "brand-new-password-1").Code)
	assert.Equal(t, http.StatusBadRequest, change("default-password-123", "short").Code)
	assert.Equal(t, http.StatusOK, change("default-password-123", "brand-new-password-1").Code)
	assert.Equal(t, http.StatusOK, login("brand-new-password-1").Code)
	assert.Equal(t, http.StatusUnauthorized, login("default-password-123").Code)
}

func TestAuthHandler_LoginRateLimited(t *testing.T) {
	handler, repo, _, pwdMgr, _ := setupTestAuthHandler(t)
	handler.SetLoginLimiter(middleware.NewRateLimiter(nil, 3, time.Minute))
	ctx := context.Background()

	hash, err := pwdMgr.Hash("correct-password")
	require.NoError(t, err)
	_, err = repo.Create(ctx, store.CreateAdminParams{
		Email:        "limited@vpn.test",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
	})
	require.NoError(t, err)

	attempt := func(password string) int {
		body, _ := json.Marshal(LoginRequest{Email: "limited@vpn.test", Password: password})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "192.0.2.9:1234"
		rec := httptest.NewRecorder()
		handler.Login(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusUnauthorized, attempt("wrong-1"))
	assert.Equal(t, http.StatusUnauthorized, attempt("wrong-2"))
	assert.Equal(t, http.StatusUnauthorized, attempt("wrong-3"))
	assert.Equal(t, http.StatusTooManyRequests, attempt("wrong-4"), "per-account limit must trigger")
	assert.Equal(t, http.StatusTooManyRequests, attempt("correct-password"), "limit applies even to valid credentials")
}
