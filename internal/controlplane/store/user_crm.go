package store

import (
	"fmt"
	"strconv"
	"strings"
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

type BotReplyDefinition struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Placeholders string `json:"placeholders"`
	DefaultText  string `json:"default_text"`
	Rows         int    `json:"rows"`
}

type BotReplyCategory struct {
	Name        string               `json:"name"`
	Badge       string               `json:"badge"`
	Description string               `json:"description"`
	Replies     []BotReplyDefinition `json:"replies"`
}

func GetBotReplyCategories() []BotReplyCategory {
	return []BotReplyCategory{
		{
			Name:        "Acquisition & Onboarding",
			Badge:       "ONBOARDING",
			Description: "Greetings delivered when leads first discover or launch the bot",
			Replies: []BotReplyDefinition{
				{
					Key:          "welcome_new_user",
					Label:        "First Visit Greeting (/start)",
					Description:  "Shown to any new visitor launching the bot for the first time.",
					Placeholders: "None",
					Rows:         5,
					DefaultText:  "Welcome to Simple-VPN secure network.\n\nHigh-speed, censorship-resistant private Internet access across all your devices.\n\nFeatures:\n- Strict zero-logs & privacy architecture\n- Modern high-performance protocols: VLESS Reality and WireGuard\n- Global high-speed edge locations\n- 1-click import for iOS, Android, Windows, and macOS\n\nTap below to activate your complimentary trial or choose a subscription plan.",
				},
				{
					Key:          "welcome_referral",
					Label:        "Referral Invitation Greeting",
					Description:  "Shown when a new lead launches the bot via an invite link from a friend (?start=ref_...).",
					Placeholders: "None",
					Rows:         4,
					DefaultText:  "Welcome to Simple-VPN.\n\nYou were invited by a friend. An exclusive bonus of additional days will be credited to your account upon activating a subscription.\n\nTap below to activate your complimentary trial and test the connection.",
				},
				{
					Key:          "welcome_active_user",
					Label:        "Active Subscriber Dashboard",
					Description:  "Shown to subscribers when checking their account or sending /start.",
					Placeholders: "%s (traffic used), %s (traffic limit), %s (expiration date), %s (subscription URL)",
					Rows:         4,
					DefaultText:  "Simple-VPN Account Dashboard\n\nData Usage: %s / %s\nValid Until: %s\n\nUniversal Subscription Link:\n%s\n\nImport this URL into your VPN client to synchronize all server locations.",
				},
			},
		},
		{
			Name:        "Free Trial & Activation",
			Badge:       "TRIAL CONVERSION",
			Description: "Trial activation confirmation, pre-expiry nudges, and conversion notices",
			Replies: []BotReplyDefinition{
				{
					Key:          "trial_activated",
					Label:        "Trial Activated Confirmation",
					Description:  "Sent immediately when a user clicks 'Get Free Trial'.",
					Placeholders: "%s (bandwidth), %d (duration in hours), %s (subscription URL)",
					Rows:         4,
					DefaultText:  "Complimentary Trial Activated\n\nYour test access is now active with %s bandwidth valid for %d hours.\n\nSubscription URL:\n%s\n\nFollow the Setup Guide to install the client for your device and connect.",
				},
				{
					Key:          "trial_expiring_soon",
					Label:        "Trial Expiring Soon (3h Warning)",
					Description:  "Automated reminder sent 3 hours prior to trial expiration.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Trial Expiration Notice\n\nYour complimentary access will expire soon.\n\nTo prevent tunnel disconnection and keep your active endpoints, choose a subscription plan below.",
				},
				{
					Key:          "trial_expired",
					Label:        "Trial Expired Notice",
					Description:  "Sent immediately after trial concludes with purchase CTA.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Your Trial Access Has Concluded\n\nYour complimentary trial period has ended and tunnel credentials have been deactivated.\n\nTo restore high-speed, unrestricted connectivity, select a plan below. Your configuration link will reactivate instantly upon payment.",
				},
				{
					Key:          "trial_no_active_tier",
					Label:        "Trials Paused Notice",
					Description:  "Displayed if free trials are disabled in system settings.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Notice: Complimentary trial allocations are currently paused to guarantee peak network performance for active subscribers.\n\nPlease choose a subscription plan below to access the network.",
				},
			},
		},
		{
			Name:        "Catalog, Checkout & Invoicing",
			Badge:       "SALES & BILLING",
			Description: "Pricing catalog headers, payment prompts, invoices, and payment confirmations",
			Replies: []BotReplyDefinition{
				{
					Key:          "catalog_header",
					Label:        "Plans Catalog Header",
					Description:  "Displayed when user requests to buy or extend a plan.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Subscription Plans & Access Tiers\n\nSelect your preferred plan duration. All tiers include uncapped bandwidth, VLESS Reality and WireGuard protocols, and multi-device access.",
				},
				{
					Key:          "payment_method_prompt",
					Label:        "Payment Method Selection",
					Description:  "Prompt shown when choosing between Telegram Stars, Crypto, or Card gateways.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Payment Method\n\nChoose your preferred checkout method below. All transactions are encrypted and activate instantly upon receipt.",
				},
				{
					Key:          "invoice_created",
					Label:        "Invoice Generated Instructions",
					Description:  "Accompanying text with external or bot invoice link.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Invoice Generated\n\nPlease complete your order using the payment button below. Once confirmed, your subscription credentials will refresh automatically.",
				},
				{
					Key:          "payment_success",
					Label:        "Payment Success & Delivery",
					Description:  "Delivered immediately upon webhook confirmation.",
					Placeholders: "%s (subscription URL)",
					Rows:         4,
					DefaultText:  "Payment Confirmed\n\nThank you for your order. Your subscription has been activated and synchronized across all edge nodes.\n\nYour Universal Subscription Link:\n%s\n\nOpen your VPN client to connect.",
				},
			},
		},
		{
			Name:        "Retention & Expiration (Dunning)",
			Badge:       "RETENTION",
			Description: "Automated reminders before expiration, cut-off notices, and win-back offers",
			Replies: []BotReplyDefinition{
				{
					Key:          "expiry_warning_72h",
					Label:        "Expiration Warning (72h / 3 Days)",
					Description:  "Dispatched 3 days prior to subscription end date.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Subscription Notice: 3 Days Remaining\n\nYour access is scheduled to expire in 3 days. To maintain uninterrupted connection, extend your plan in advance. Extra time will be added directly to your remaining balance.",
				},
				{
					Key:          "expiry_warning_24h",
					Label:        "Final Notice (24 Hours)",
					Description:  "Dispatched 24 hours prior to subscription end date.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Final Notice: 24 Hours Remaining\n\nYour subscription expires tomorrow. After expiration, tunnel routing will terminate. Extend your plan below to ensure continuous connectivity.",
				},
				{
					Key:          "subscription_expired",
					Label:        "Subscription Disconnected Notice",
					Description:  "Dispatched when account passes expiration date.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Subscription Expired\n\nYour access period has elapsed and tunnel credentials have been suspended.\n\nYour configuration link is saved. You can reactivate access at any time below without reconfiguring your client apps.",
				},
				{
					Key:          "winback_offer",
					Label:        "Win-back Retention Offer (72h Post-Expiry)",
					Description:  "Follow-up discount offer sent to churned users.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Reactivation Offer\n\nWe noticed your subscription recently expired. Renew today to receive an exclusive extended access bonus on multi-month plans.",
				},
			},
		},
		{
			Name:        "Connection Guides & Setup Instructions",
			Badge:       "DEVICE SETUP",
			Description: "Client installation steps per operating system, key reset, and data quota alerts",
			Replies: []BotReplyDefinition{
				{
					Key:          "help_text",
					Label:        "General Help & Setup Guide",
					Description:  "Delivered on /help or when clicking Setup Guide.",
					Placeholders: "None",
					Rows:         4,
					DefaultText:  "Connection & Setup Instructions\n\n1. Install a compatible client for your device (Streisand, Happ, v2rayNG, sing-box, or Hiddify).\n2. Copy your Universal Subscription Link from the main menu.\n3. Import the link into your client app.\n4. Select a server and toggle Connect.",
				},
				{
					Key:          "setup_guide_ios",
					Label:        "Apple iOS Setup (iPhone / iPad)",
					Description:  "Detailed instructions for Happ, Streisand, FoXray.",
					Placeholders: "%s (button label), %s (deep link), %s (alternative link)",
					Rows:         4,
					DefaultText:  "iOS Setup (iPhone / iPad)\n\nRecommended Apps: Happ, Streisand, or FoXray (available on the App Store).\n\n1. Install Happ or Streisand from the App Store.\n2. Tap the 1-Click Import button below, or copy your subscription link.\n3. In the app, add the subscription and update servers.\n4. Allow VPN configuration when prompted by iOS and connect.",
				},
				{
					Key:          "setup_guide_android",
					Label:        "Android Setup",
					Description:  "Detailed instructions for v2rayNG, sing-box, Happ.",
					Placeholders: "%s (button label), %s (deep link), %s (alternative link)",
					Rows:         4,
					DefaultText:  "Android Setup\n\nRecommended Apps: v2rayNG, sing-box, or Happ.\n\n1. Install v2rayNG from Google Play or GitHub.\n2. In v2rayNG, open Subscription Group Settings and add your subscription link.\n3. Tap Update Subscription from the top menu.\n4. Choose a server location and tap the V icon to connect.",
				},
				{
					Key:          "setup_guide_windows",
					Label:        "Windows PC Setup",
					Description:  "Detailed instructions for Hiddify, Clash Verge Rev.",
					Placeholders: "%s (button label), %s (deep link)",
					Rows:         4,
					DefaultText:  "Windows Setup\n\nRecommended Apps: Hiddify, Clash Verge Rev, or v2rayN.\n\n1. Install Hiddify or Clash Verge Rev.\n2. Add your subscription link from clipboard.\n3. Enable TUN Mode or System Proxy in settings.\n4. Select a server location and click Connect.",
				},
				{
					Key:          "setup_guide_macos",
					Label:        "macOS Setup",
					Description:  "Detailed instructions for Streisand, Hiddify.",
					Placeholders: "%s (button label), %s (deep link)",
					Rows:         4,
					DefaultText:  "macOS Setup\n\nRecommended Apps: Streisand, Hiddify, or FoXray.\n\n1. Install Streisand or Hiddify on your Mac.\n2. Add your subscription link to profile groups.\n3. Update subscriptions to download server configurations.\n4. Enable the connection toggle to secure all network traffic.",
				},
				{
					Key:          "keys_reset_prompt",
					Label:        "Reset Keys Warning / Confirmation",
					Description:  "Safety prompt before revoking user tokens.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Confirm Credential Reset\n\nWarning: Resetting your cryptographic credentials will invalidate previous links and disconnect active devices.\n\nA new subscription URL will be issued. Your paid expiration date and traffic quota remain unchanged.",
				},
				{
					Key:          "keys_reset_success",
					Label:        "Reset Keys Completion Notice",
					Description:  "Confirmation message containing new subscription URL.",
					Placeholders: "%s (new subscription URL)",
					Rows:         3,
					DefaultText:  "Credentials Reset Successfully\n\nAll previous tokens have been revoked. Update your VPN client with your new Universal Subscription Link:\n%s",
				},
				{
					Key:          "traffic_limit_reached",
					Label:        "Bandwidth Limit Reached Alert",
					Description:  "Sent when subscriber reaches 100% of data limit.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "Bandwidth Quota Exhausted\n\nYou have consumed 100% of your allocated data limit. Tunnel routing has been paused.\n\nTo restore access, renew your subscription or upgrade your plan.",
				},
			},
		},
		{
			Name:        "Referral Program",
			Badge:       "GROWTH",
			Description: "Referral terms, invite link delivery, and commission push notices",
			Replies: []BotReplyDefinition{
				{
					Key:          "referral_overview",
					Label:        "Referral Program Dashboard",
					Description:  "Shown when user clicks Referral Program or sends /ref.",
					Placeholders: "%s (invitation link), %d (invited count), %d (bonus days earned), {refprocent} (commission %)",
					Rows:         4,
					DefaultText:  "Referral Program\n\nInvite friends and earn rewards.\n\n- Your friend receives bonus days on their first plan.\n- You receive bonus days or {refprocent}% balance bonus from their purchases.\n\nYour Invitation Link:\n%s\n\nTotal Invited: %d friends\nBonus Earned: %d days",
				},
				{
					Key:          "referral_joined_notice",
					Label:        "Friend Registered Notification",
					Description:  "Sent to inviter when a new friend joins via their link.",
					Placeholders: "None",
					Rows:         3,
					DefaultText:  "New Referral Registered\n\nA friend has registered using your invitation link. When they activate a paid plan, bonus rewards will be credited to your account.",
				},
				{
					Key:          "referral_reward_credited",
					Label:        "Bonus Days Awarded Notification",
					Description:  "Sent to inviter when their friend completes a paid plan.",
					Placeholders: "None, {refprocent}",
					Rows:         3,
					DefaultText:  "Referral Bonus Credited\n\nYour invited referral completed a subscription purchase! Bonus days and balance credits have been added to your account.",
				},
			},
		},
	}
}

func DefaultBotReplies() map[string]string {
	m := make(map[string]string)
	for _, cat := range GetBotReplyCategories() {
		for _, r := range cat.Replies {
			m[r.Key] = r.DefaultText
		}
	}
	m["referral_enabled"] = "true"
	return m
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

func (u User) DisplayName() string {
	name := strings.TrimSpace(u.TelegramFirstName.String + " " + u.TelegramLastName.String)
	if name != "" {
		return name
	}
	if u.Username != "" && !strings.HasPrefix(u.Username, "tg_") {
		return u.Username
	}
	if u.TelegramUsername.Valid && u.TelegramUsername.String != "" {
		return "@" + u.TelegramUsername.String
	}
	if u.TelegramID.Valid && u.TelegramID.Int64 > 0 {
		return fmt.Sprintf("User #%d", u.TelegramID.Int64)
	}
	return "Client #" + u.ShortID()
}

func (u User) TelegramIDString() string {
	if u.TelegramID.Valid && u.TelegramID.Int64 > 0 {
		return strconv.FormatInt(u.TelegramID.Int64, 10)
	}
	return ""
}

func (u User) ShortID() string {
	str := u.ID.String()
	if len(str) >= 8 {
		return str[:8]
	}
	return str
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

// ReferralProgramSettings holds the configuration for the viral growth engine.
type ReferralProgramSettings struct {
	Enabled       bool   `json:"enabled"`
	RewardModel   string `json:"reward_model"`   // "bonus_days"
	InviterDays   int    `json:"inviter_days"`   // default: 7
	InviteeDays   int    `json:"invitee_days"`   // default: 3
	Qualification string `json:"qualification"`  // "first_payment" or "any_activation"
	DailyCap      int    `json:"daily_cap"`      // default: 5
	RewardExpired bool   `json:"reward_expired"` // default: true
}

func DefaultReferralProgramSettings() ReferralProgramSettings {
	return ReferralProgramSettings{
		Enabled:       true,
		RewardModel:   "bonus_days",
		InviterDays:   7,
		InviteeDays:   3,
		Qualification: "first_payment",
		DailyCap:      5,
		RewardExpired: true,
	}
}

func ParseReferralSettings(replies map[string]string) ReferralProgramSettings {
	s := DefaultReferralProgramSettings()
	if v, ok := replies["referral_enabled"]; ok {
		s.Enabled = (v != "false")
	}
	if v, ok := replies["referral_reward_model"]; ok && v != "" {
		s.RewardModel = v
	}
	if v, ok := replies["referral_inviter_days"]; ok {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			s.InviterDays = d
		}
	}
	if v, ok := replies["referral_invitee_days"]; ok {
		if d, err := strconv.Atoi(v); err == nil && d >= 0 {
			s.InviteeDays = d
		}
	}
	if v, ok := replies["referral_qualification"]; ok && v != "" {
		s.Qualification = v
	}
	if v, ok := replies["referral_daily_cap"]; ok {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			s.DailyCap = d
		}
	}
	if v, ok := replies["referral_reward_expired"]; ok {
		s.RewardExpired = (v != "false")
	}
	return s
}

// LogRetentionSettings defines system audit log retention duration in days.
type LogRetentionSettings struct {
	RetentionDays int `json:"retention_days"` // default: 90 (0 = keep indefinitely)
}

func DefaultLogRetentionSettings() LogRetentionSettings {
	return LogRetentionSettings{
		RetentionDays: 90,
	}
}

func ParseLogRetentionSettings(replies map[string]string) LogRetentionSettings {
	s := DefaultLogRetentionSettings()
	if v, ok := replies["audit_log_retention_days"]; ok {
		if d, err := strconv.Atoi(v); err == nil && d >= 0 {
			s.RetentionDays = d
		}
	}
	return s
}


