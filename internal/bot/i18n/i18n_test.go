package i18n_test

import (
	"testing"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/stretchr/testify/assert"
)

func TestI18n_Bundles(t *testing.T) {
	en := i18n.GetBundle(i18n.EN)
	assert.NotEmpty(t, en.WelcomeNewUser)
	assert.NotEmpty(t, en.TrialActivated)
	assert.NotEmpty(t, en.BtnGetTrial)
	assert.NotEmpty(t, en.BtnStatus)

	ru := i18n.GetBundle(i18n.RU)
	assert.NotEmpty(t, ru.WelcomeNewUser)
	assert.NotEmpty(t, ru.TrialActivated)
	assert.NotEmpty(t, ru.BtnGetTrial)
	assert.NotEmpty(t, ru.BtnStatus)

	// Fallback to EN for unknown language
	fallback := i18n.GetBundle(i18n.Language("fr"))
	assert.Equal(t, en.WelcomeNewUser, fallback.WelcomeNewUser)
}

func TestI18n_FormatBytes(t *testing.T) {
	assert.Equal(t, "500 B", i18n.FormatBytes(500))
	assert.Equal(t, "1.00 KB", i18n.FormatBytes(1024))
	assert.Equal(t, "1.00 MB", i18n.FormatBytes(1024*1024))
	assert.Equal(t, "1.00 GB", i18n.FormatBytes(1024*1024*1024))
}
