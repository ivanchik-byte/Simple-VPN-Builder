package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrInvalidAPIKey    = errors.New("invalid API key")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type JWTManager struct {
	secret        []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

func NewJWTManager(secret string, accessTTL, refreshTTL time.Duration) *JWTManager {
	return &JWTManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

type Claims struct {
	AdminID uuid.UUID `json:"admin_id"`
	Email   string    `json:"email"`
	Role    string    `json:"role"`
	jwt.RegisteredClaims
}

func (m *JWTManager) GenerateAccessToken(adminID uuid.UUID, email, role string) (string, error) {
	claims := Claims{
		AdminID: adminID,
		Email:   email,
		Role:    role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) GenerateRefreshToken(adminID uuid.UUID) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   adminID.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.refreshTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
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
	return claims, nil
}

type APIKeyManager struct {
	repo APIKeyRepository
}

type APIKeyRepository interface {
	GetByPrefix(ctx context.Context, prefix string) (*APIKey, error)
	Update(ctx context.Context, key *APIKey) error
}

type APIKey struct {
	ID        uuid.UUID
	KeyHash   string
	Prefix    string
	Scopes    []string
	ExpiresAt *time.Time
}

func NewAPIKeyManager(repo APIKeyRepository) *APIKeyManager {
	return &APIKeyManager{repo: repo}
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

func (m *APIKeyManager) ValidateKey(ctx context.Context, rawKey string) (*APIKey, error) {
	if !strings.HasPrefix(rawKey, "vpn_") {
		return nil, ErrInvalidAPIKey
	}
	prefix := rawKey[:8]
	key, err := m.repo.GetByPrefix(ctx, prefix)
	if err != nil {
		return nil, ErrInvalidAPIKey
	}
	hash := sha256.Sum256([]byte(rawKey))
	if hex.EncodeToString(hash[:]) != key.KeyHash {
		return nil, ErrInvalidAPIKey
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, ErrInvalidAPIKey
	}
	return key, nil
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