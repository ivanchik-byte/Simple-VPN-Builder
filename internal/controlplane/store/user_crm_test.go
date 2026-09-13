package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestUser_DisplayTelegram(t *testing.T) {
	tests := []struct {
		name     string
		user     User
		expected string
	}{
		{
			name: "with username",
			user: User{
				TelegramUsername: pgtype.Text{String: "john_doe", Valid: true},
			},
			expected: "@john_doe",
		},
		{
			name: "with numeric ID only",
			user: User{
				TelegramID: pgtype.Int8{Int64: 987654321, Valid: true},
			},
			expected: "ID: 987654321",
		},
		{
			name:     "web client without telegram",
			user:     User{},
			expected: "Web Client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.user.DisplayTelegram())
		})
	}
}

func TestUser_CRMStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		user     User
		expected string
	}{
		{
			name: "banned user",
			user: User{
				IsBanned: pgtype.Bool{Bool: true, Valid: true},
			},
			expected: "banned",
		},
		{
			name: "lead status",
			user: User{
				Status: pgtype.Text{String: "lead", Valid: true},
			},
			expected: "lead",
		},
		{
			name: "expired subscription",
			user: User{
				Status:    pgtype.Text{String: "active", Valid: true},
				ExpiresAt: pgtype.Timestamptz{Time: now.Add(-1 * time.Hour), Valid: true},
			},
			expected: "expired",
		},
		{
			name: "trial subscription",
			user: User{
				Status:    pgtype.Text{String: "active", Valid: true},
				TrialUsed: pgtype.Bool{Bool: true, Valid: true},
				ExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
			},
			expected: "trial",
		},
		{
			name: "active paying subscription",
			user: User{
				Status:    pgtype.Text{String: "active", Valid: true},
				PlanID:    pgtype.UUID{Bytes: uuid.New(), Valid: true},
				ExpiresAt: pgtype.Timestamptz{Time: now.Add(30 * 24 * time.Hour), Valid: true},
			},
			expected: "active",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.user.CRMStatus())
		})
	}
}
