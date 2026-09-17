package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTemplateTags_EscapesHTMLInFirstName(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:                userID,
		Username:          "testuser",
		TelegramFirstName: pgtype.Text{String: "<b>test</b>", Valid: true},
		TrafficUsed:       pgtype.Int8{Int64: 1024, Valid: true},
		TrafficLimit:      pgtype.Int8{Int64: 1048576, Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	out := engine.applyTemplateTags(context.Background(), 12345, "Hi {first_name}, usage {traffic_used}!")
	assert.Contains(t, out, "Hi &lt;b&gt;test&lt;/b&gt;, usage")
	assert.NotContains(t, out, "<b>test</b>")
	require.NotNil(t, trans) // harness sanity
}

func TestApplyTemplateTags_FirstNameFallbacks(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:       userID,
		Username: "plainuser",
	}

	engine, _, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	out := engine.applyTemplateTags(context.Background(), 12345, "Hi {first_name}!")
	assert.Contains(t, out, "Hi plainuser!")
}

func TestResolveBannedMessage_CustomAndFallback(t *testing.T) {
	bundle := i18n.GetBundle(i18n.EN)

	// Fallback path is covered via i18n bundle default.
	assert.Contains(t, resolveBannedMessage(nil, bundle, "spam"), "spam")
	assert.Equal(t,
		"Blocked: spam",
		resolveBannedMessage(map[string]string{"banned_message": "Blocked: %s"}, bundle, "spam"),
	)
	assert.Equal(t,
		"Blocked: spam",
		resolveBannedMessage(map[string]string{"banned_message": "Blocked: {ban_reason}"}, bundle, "spam"),
	)
}
