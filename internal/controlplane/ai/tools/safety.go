package tools

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
)

// SafetyManager handles cryptographically signed approval tokens for dangerous operations.
type SafetyManager struct {
	secretKey []byte
}

// NewSafetyManager creates a safety manager with a secret signing key.
func NewSafetyManager(secretKey []byte) *SafetyManager {
	if len(secretKey) == 0 {
		secretKey = []byte("simple-vpn-builder-ai-safety-key-2026")
	}
	return &SafetyManager{secretKey: secretKey}
}

// ComputePayloadHash returns a hex-encoded SHA-256 hash of the normalized parameter string.
func ComputePayloadHash(payload string) string {
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:16]) // 16 bytes for compact token
}

// GenerateConfirmationToken creates a signed 5-minute approval token for a specific action payload.
func (s *SafetyManager) GenerateConfirmationToken(actionName string, payloadHash string) (string, time.Time) {
	expiresAt := time.Now().Add(5 * time.Minute)
	data := fmt.Sprintf("%s:%s:%d", actionName, payloadHash, expiresAt.Unix())
	mac := hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(data))
	sig := hex.EncodeToString(mac.Sum(nil)[:16])
	token := fmt.Sprintf("%s.%d.%s", sig, expiresAt.Unix(), payloadHash)
	return token, expiresAt
}

// ValidateConfirmationToken verifies that the confirmation token matches the payload and is unexpired.
func (s *SafetyManager) ValidateConfirmationToken(token string, actionName string, payloadHash string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrInvalidToken
	}

	sig := parts[0]
	expUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrInvalidToken
	}
	hash := parts[2]

	if hash != payloadHash {
		return ErrInvalidToken
	}

	if time.Now().Unix() > expUnix {
		return ErrTokenExpired
	}

	data := fmt.Sprintf("%s:%s:%d", actionName, payloadHash, expUnix)
	mac := hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(data))
	expectedSig := hex.EncodeToString(mac.Sum(nil)[:16])

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return ErrInvalidToken
	}
	return nil
}
