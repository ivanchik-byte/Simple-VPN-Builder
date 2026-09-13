package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// CRM & Telegram Lead extension fields for User
type TelegramLeadParams struct {
	TelegramID       int64  `json:"telegram_id"`
	TelegramUsername string `json:"telegram_username"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	LanguageCode     string `json:"language_code"`
	ReferrerCode     string `json:"referrer_code"`
}

type BotReply struct {
	KeyName    string             `json:"key_name"`
	ReplyText  string             `json:"reply_text"`
	UpdatedAt  pgtype.Timestamptz `json:"updated_at"`
}

// Helper methods on User for CRM display
func (u User) DisplayTelegram() string {
	if u.TelegramUsername.Valid && u.TelegramUsername.String != "" {
		return "@" + u.TelegramUsername.String
	}
	if u.TelegramID.Valid && u.TelegramID.Int64 > 0 {
		return fmt.Sprintf("ID: %d", u.TelegramID.Int64)
	}
	return "Web Client"
}

func (u User) CRMStatus() string {
	if u.IsBanned.Valid && u.IsBanned.Bool {
		return "banned"
	}
	if u.Status.Valid && u.Status.String == "lead" {
		return "lead"
	}
	if u.ExpiresAt.Valid && time.Now().After(u.ExpiresAt.Time) {
		return "expired"
	}
	if u.TrialUsed.Valid && u.TrialUsed.Bool && u.ExpiresAt.Valid && time.Now().Before(u.ExpiresAt.Time) && (!u.PlanID.Valid || u.PlanID.Bytes == uuid.Nil) {
		return "trial"
	}
	if u.Status.Valid && u.Status.String == "active" {
		return "active"
	}
	return "lead"
}
