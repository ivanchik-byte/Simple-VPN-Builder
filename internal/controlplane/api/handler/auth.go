package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type AuthHandler struct {
	adminRepo       store.AdminRepository
	jwtManager      *auth.JWTManager
	passwordManager *auth.PasswordManager
	totpManager     *auth.TOTPManager
	blacklist       auth.TokenBlacklist
	loginLimiter    *middleware.RateLimiter
}

func NewAuthHandler(
	adminRepo store.AdminRepository,
	jwtManager *auth.JWTManager,
	passwordManager *auth.PasswordManager,
	totpManager *auth.TOTPManager,
	blacklist auth.TokenBlacklist,
) *AuthHandler {
	return &AuthHandler{
		adminRepo:       adminRepo,
		jwtManager:      jwtManager,
		passwordManager: passwordManager,
		totpManager:     totpManager,
		blacklist:       blacklist,
	}
}

// SetLoginLimiter installs the brute-force limiter for login and TOTP endpoints.
func (h *AuthHandler) SetLoginLimiter(rl *middleware.RateLimiter) {
	h.loginLimiter = rl
}

// checkLoginLimit throttles by account key and IP; true means the request may proceed.
func (h *AuthHandler) checkLoginLimit(w http.ResponseWriter, r *http.Request, key string) bool {
	if h.loginLimiter == nil {
		return true
	}
	ctx := r.Context()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if ok, _, retry, _ := h.loginLimiter.Allow(ctx, "apilogin:ip:"+ip); !ok {
		response.RespondRateLimited(w, r, retry)
		return false
	}
	if ok, _, retry, _ := h.loginLimiter.Allow(ctx, "apilogin:user:"+key); !ok {
		response.RespondRateLimited(w, r, retry)
		return false
	}
	return true
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Email        string `json:"email,omitempty"`
	Role         string `json:"role,omitempty"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		response.RespondBadRequest(w, r, "Email and password are required", nil)
		return
	}

	if !h.checkLoginLimit(w, r, strings.ToLower(req.Email)) {
		return
	}

	admin, err := h.adminRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Constant-time dummy verification to mitigate email enumeration timing attacks
		_ = h.passwordManager.Verify(req.Password, "$2a$12$e8YkYc1FXxYzAbCdEfGhIu7kJ6mN5oP4qR3sT2uV1wX0yZ9aBcDeF")
		response.RespondUnauthorized(w, r, "Invalid email or password")
		return
	}

	if err := h.passwordManager.Verify(req.Password, admin.PasswordHash); err != nil {
		response.RespondUnauthorized(w, r, "Invalid email or password")
		return
	}

	if admin.MustChangePassword {
		response.RespondForbidden(w, r, "Password change required: rotate the default credentials")
		return
	}

	// Verify TOTP if enabled for this administrator
	if admin.TotpSecret.Valid && admin.TotpSecret.String != "" {
		if req.TOTPCode == "" || !h.totpManager.ValidateCode(req.TOTPCode, admin.TotpSecret.String) {
			response.RespondUnauthorized(w, r, "Valid two-factor authentication code required")
			return
		}
	}

	accessToken, err := h.jwtManager.GenerateAccessToken(admin.ID, admin.Email, admin.Role.String)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate access token")
		return
	}

	refreshToken, err := h.jwtManager.GenerateRefreshToken(admin.ID)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate refresh token")
		return
	}

	_ = h.adminRepo.UpdateLastLogin(r.Context(), admin.ID)

	expiresIn := int64(h.jwtManager.AccessTTL().Seconds())
	if expiresIn <= 0 {
		expiresIn = 900
	}

	roleStr := "admin"
	if admin.Role.Valid && admin.Role.String != "" {
		roleStr = admin.Role.String
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		Email:        admin.Email,
		Role:         roleStr,
	})
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		response.RespondBadRequest(w, r, "Refresh token is required", nil)
		return
	}

	claims, err := h.jwtManager.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		response.RespondUnauthorized(w, r, "Invalid or expired refresh token")
		return
	}

	adminID := claims.AdminID
	if adminID == uuid.Nil {
		parsed, parseErr := uuid.Parse(claims.Subject)
		if parseErr != nil {
			response.RespondUnauthorized(w, r, "Invalid refresh token subject")
			return
		}
		adminID = parsed
	}

	admin, err := h.adminRepo.GetByID(r.Context(), adminID)
	if err != nil {
		response.RespondUnauthorized(w, r, "Admin account not found")
		return
	}

	// Revoke old refresh token to prevent replay
	if h.blacklist != nil && claims.ID != "" {
		_ = h.blacklist.Revoke(r.Context(), claims.ID, 7*24*time.Hour)
	}

	newAccessToken, err := h.jwtManager.GenerateAccessToken(admin.ID, admin.Email, admin.Role.String)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate access token")
		return
	}

	newRefreshToken, err := h.jwtManager.GenerateRefreshToken(admin.ID)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate refresh token")
		return
	}

	expiresIn := int64(h.jwtManager.AccessTTL().Seconds())
	if expiresIn <= 0 {
		expiresIn = 900
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := h.jwtManager.ValidateToken(tokenStr)
		if err == nil && claims.ID != "" && h.blacklist != nil {
			_ = h.blacklist.Revoke(r.Context(), claims.ID, 24*time.Hour)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "logged_out"})
}

type TOTPSetupResponse struct {
	Secret string `json:"secret"`
	URL    string `json:"otpauth_url"`
}

func (h *AuthHandler) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		response.RespondUnauthorized(w, r, "Authentication required")
		return
	}

	secret, qrURL, err := h.totpManager.GenerateSecret(authCtx.Email)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate TOTP secret")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(TOTPSetupResponse{
		Secret: secret,
		URL:    qrURL,
	})
}

type TOTPVerifyRequest struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

func (h *AuthHandler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		response.RespondUnauthorized(w, r, "Authentication required")
		return
	}

	var req TOTPVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Secret == "" || req.Code == "" {
		response.RespondBadRequest(w, r, "Secret and verification code are required", nil)
		return
	}

	if !h.checkLoginLimit(w, r, authCtx.UserID.String()) {
		return
	}

	if !h.totpManager.ValidateCode(req.Code, req.Secret) {
		response.RespondBadRequest(w, r, "Invalid two-factor authentication code", nil)
		return
	}

	admin, err := h.adminRepo.GetByID(r.Context(), authCtx.UserID)
	if err != nil {
		response.RespondNotFound(w, r, "Admin account not found")
		return
	}

	_, err = h.adminRepo.Update(r.Context(), store.UpdateAdminParams{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		TotpSecret:   pgtype.Text{String: req.Secret, Valid: true},
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to activate two-factor authentication")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "totp_enabled"})
}

// ChangePasswordRequest rotates credentials without a session (old password proof).
type ChangePasswordRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	NewPassword string `json:"new_password"`
	TOTPCode    string `json:"totp_code,omitempty"`
}

// ChangePassword verifies the old password and sets a new one.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" || len(req.NewPassword) < 12 {
		response.RespondBadRequest(w, r, "Valid email, current password and a new 12+ character password are required", nil)
		return
	}

	admin, err := h.adminRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		_ = h.passwordManager.Verify(req.Password, "$2a$12$e8YkYc1FXxYzAbCdEfGhIu7kJ6mN5oP4qR3sT2uV1wX0yZ9aBcDeF")
		response.RespondUnauthorized(w, r, "Invalid email or password")
		return
	}

	if err := h.passwordManager.Verify(req.Password, admin.PasswordHash); err != nil {
		response.RespondUnauthorized(w, r, "Invalid email or password")
		return
	}

	if admin.TotpSecret.Valid && admin.TotpSecret.String != "" {
		if req.TOTPCode == "" || !h.totpManager.ValidateCode(req.TOTPCode, admin.TotpSecret.String) {
			response.RespondUnauthorized(w, r, "Valid two-factor authentication code required")
			return
		}
	}

	hash, err := h.passwordManager.Hash(req.NewPassword)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to hash new password")
		return
	}

	if err := h.adminRepo.UpdatePassword(r.Context(), admin.ID, hash); err != nil {
		response.RespondInternalError(w, r, "Failed to update password")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "password_changed"})
}
