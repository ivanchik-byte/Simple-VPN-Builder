package tools

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	apimw "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
)

// ActionSafetyTier defines whether an action is read-only or mutating.
type ActionSafetyTier string

const (
	SafetyReadOnly ActionSafetyTier = "READ_ONLY"
	SafetyMutating ActionSafetyTier = "MUTATING"
)

var (
	ErrConfirmationRequired = errors.New("confirmation token required for mutating action")
	ErrTokenExpired         = errors.New("confirmation token has expired (5-minute TTL)")
	ErrInvalidToken         = errors.New("invalid confirmation token or mismatched parameters")
	ErrTokenReplay          = errors.New("confirmation token already used")
)

// SafetyManager handles cryptographically signed approval tokens for dangerous operations.
type SafetyManager struct {
	secretKey []byte
	mu        sync.Mutex
	used      map[string]int64
}

// NewSafetyManager creates a safety manager with a secret signing key.
// Fail-closed: an empty key is rejected instead of falling back to a
// hard-coded constant, so production cannot silently run with a
// publicly known HMAC secret.
func NewSafetyManager(secretKey []byte) (*SafetyManager, error) {
	if len(secretKey) == 0 {
		return nil, errors.New("ai safety manager: empty secret key (fail-closed)")
	}
	return &SafetyManager{secretKey: secretKey, used: make(map[string]int64)}, nil
}

// ComputePayloadHash returns a hex-encoded SHA-256 hash of the normalized parameter string.
func ComputePayloadHash(payload string) string {
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:16]) // 16 bytes for compact token
}

// AdminIDFromContext extracts the authenticated admin ID for token binding.
func AdminIDFromContext(ctx context.Context) string {
	if authCtx := apimw.GetAuth(ctx); authCtx != nil && authCtx.UserID.String() != "" {
		return authCtx.UserID.String()
	}
	return ""
}

// GenerateConfirmationToken creates a signed 5-minute approval token for a specific action payload.
func (s *SafetyManager) GenerateConfirmationToken(actionName string, payloadHash string) (string, time.Time) {
	return s.GenerateConfirmationTokenFor(actionName, payloadHash, "")
}

// GenerateConfirmationTokenFor binds the token to the given admin ID.
func (s *SafetyManager) GenerateConfirmationTokenFor(actionName string, payloadHash string, adminID string) (string, time.Time) {
	expiresAt := time.Now().Add(5 * time.Minute)
	data := fmt.Sprintf("%s:%s:%s:%d", actionName, adminID, payloadHash, expiresAt.Unix())
	mac := hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(data))
	sig := hex.EncodeToString(mac.Sum(nil)[:16])
	token := fmt.Sprintf("%s.%d.%s.%s", sig, expiresAt.Unix(), payloadHash, adminID)
	return token, expiresAt
}

// ValidateConfirmationToken verifies that the confirmation token matches the payload and is unexpired.
func (s *SafetyManager) ValidateConfirmationToken(token string, actionName string, payloadHash string) error {
	return s.ValidateAndConsume(token, actionName, payloadHash, "")
}

// ValidateAndConsume verifies the token, its admin binding, and burns it (single use).
func (s *SafetyManager) ValidateAndConsume(token string, actionName string, payloadHash string, adminID string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		return ErrInvalidToken
	}

	sig := parts[0]
	expUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrInvalidToken
	}
	hash := parts[2]
	boundAdmin := parts[3]

	if hash != payloadHash {
		return ErrInvalidToken
	}

	if adminID != "" && boundAdmin != adminID {
		return ErrInvalidToken
	}

	if time.Now().Unix() > expUnix {
		return ErrTokenExpired
	}

	data := fmt.Sprintf("%s:%s:%s:%d", actionName, boundAdmin, payloadHash, expUnix)
	mac := hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(data))
	expectedSig := hex.EncodeToString(mac.Sum(nil)[:16])

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return ErrInvalidToken
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	for k, exp := range s.used {
		if exp < now {
			delete(s.used, k)
		}
	}
	if _, ok := s.used[sig]; ok {
		return ErrTokenReplay
	}
	if s.used == nil {
		s.used = make(map[string]int64)
	}
	s.used[sig] = expUnix
	return nil
}
