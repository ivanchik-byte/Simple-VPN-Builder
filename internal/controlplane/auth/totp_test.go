package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOTPManager_GenerateAndValidate(t *testing.T) {
	manager := NewTOTPManager("VPN-Test")
	require.NotNil(t, manager)

	secret, qrURL, err := manager.GenerateSecret("admin@test.local")
	require.NoError(t, err)
	assert.NotEmpty(t, secret)
	assert.Contains(t, qrURL, "otpauth://totp/")
	assert.Contains(t, qrURL, "VPN-Test")

	code, err := totp.GenerateCode(secret, time.Now().UTC())
	require.NoError(t, err)

	assert.True(t, manager.ValidateCode(code, secret))
	assert.True(t, manager.ValidateCode(code[:3]+" "+code[3:], secret))
	assert.True(t, manager.ValidateCode(code[:3]+"-"+code[3:], secret))
	assert.True(t, manager.ValidateCode(code, strings.ToLower(secret)))
	assert.False(t, manager.ValidateCode("000000", secret))
	assert.False(t, manager.ValidateCode("", secret))
	assert.False(t, manager.ValidateCode(code, ""))
	assert.False(t, manager.ValidateCode("invalid", "invalid"))
}

func TestTOTPManager_DefaultIssuer(t *testing.T) {
	manager := NewTOTPManager("")
	assert.Equal(t, "Simple-VPN-Builder", manager.issuer)
}
