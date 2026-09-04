package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrInvalidAPIKey      = errors.New("invalid API key")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type JWTManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	blacklist  TokenBlacklist
}

func NewJWTManager(secret string, accessTTL, refreshTTL time.Duration) *JWTManager {
	return &JWTManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (m *JWTManager) WithBlacklist(bl TokenBlacklist) *JWTManager {
	m.blacklist = bl
	return m
}

type Claims struct {
	AdminID   uuid.UUID `json:"admin_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	TokenType string    `json:"typ"`
	jwt.RegisteredClaims
}

func (m *JWTManager) AccessTTL() time.Duration {
	return m.accessTTL
}

func (m *JWTManager) Blacklist() TokenBlacklist {
	return m.blacklist
}

func (m *JWTManager) GenerateAccessToken(adminID uuid.UUID, email, role string) (string, error) {
	claims := Claims{
		AdminID:   adminID,
		Email:     email,
		Role:      role,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) GenerateRefreshToken(adminID uuid.UUID) (string, error) {
	claims := Claims{
		AdminID:   adminID,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   adminID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if m.blacklist != nil && claims.ID != "" {
		revoked, err := m.blacklist.IsRevoked(context.Background(), claims.ID)
		if err == nil && revoked {
			return nil, ErrTokenRevoked
		}
	}

	return claims, nil
}

func (m *JWTManager) ValidateAccessToken(tokenString string) (*Claims, error) {
	claims, err := m.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != "access" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (m *JWTManager) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := m.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != "refresh" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

type APIKeyReader interface {
	GetAPIKeyByPrefix(ctx context.Context, prefix string) (store.ApiKey, error)
}

type APIKeyManager struct {
	queries APIKeyReader
}

func NewAPIKeyManager(queries APIKeyReader) *APIKeyManager {
	return &APIKeyManager{queries: queries}
}

func (m *APIKeyManager) GenerateKey() (string, string, error) {
	prefix := "vpn_"
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	key := prefix + hex.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(key))
	return key, hex.EncodeToString(hash[:]), nil
}

func (m *APIKeyManager) ValidateKey(ctx context.Context, rawKey string) (*store.ApiKey, error) {
	if len(rawKey) < 8 || !strings.HasPrefix(rawKey, "vpn_") {
		return nil, ErrInvalidAPIKey
	}
	prefix := rawKey[:8]
	key, err := m.queries.GetAPIKeyByPrefix(ctx, prefix)
	if err != nil {
		return nil, ErrInvalidAPIKey
	}
	hash := sha256.Sum256([]byte(rawKey))
	computedHash := hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(key.KeyHash)) != 1 {
		return nil, ErrInvalidAPIKey
	}
	if key.ExpiresAt.Valid && time.Now().After(key.ExpiresAt.Time) {
		return nil, ErrInvalidAPIKey
	}
	return &key, nil
}

type PasswordManager struct {
	cost int
}

func NewPasswordManager(cost int) *PasswordManager {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}
	return &PasswordManager{cost: cost}
}

func (m *PasswordManager) Hash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), m.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(bytes), nil
}

func (m *PasswordManager) Verify(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
