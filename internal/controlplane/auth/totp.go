package auth

import (
	"errors"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

var (
	ErrInvalidTOTPSecret = errors.New("invalid totp secret")
	ErrInvalidTOTPCode   = errors.New("invalid totp passcode")
)

type TOTPManager struct {
	issuer string
}

func NewTOTPManager(issuer string) *TOTPManager {
	if issuer == "" {
		issuer = "Simple-VPN-Builder"
	}
	return &TOTPManager{issuer: issuer}
}

// GenerateSecret creates a new TOTP key returning secret and otpauth URI.
func (m *TOTPManager) GenerateSecret(accountName string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      m.issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateCode checks a passcode against a secret with time-step drift tolerance.
func (m *TOTPManager) ValidateCode(passcode string, secret string) bool {
	if passcode == "" || secret == "" {
		return false
	}
	valid, err := totp.ValidateCustom(
		passcode,
		secret,
		time.Now().UTC(),
		totp.ValidateOpts{
			Period:    30,
			Skew:      1,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		},
	)
	if err != nil {
		return false
	}
	return valid
}
