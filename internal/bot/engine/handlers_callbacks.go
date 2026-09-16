package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"strconv"
	"strings"
)

func (e *BotEngine) handleCallbackQuery(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

	// Acknowledge callback query
	callbackResp := tgbotapi.NewCallback(cb.ID, "")
	_, _ = e.bot.Request(callbackResp)

	// Ban guard: if user is banned by admin, block actions
	if userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID); err == nil && userRes != nil {
		if userRes.User.IsBanned.Valid && userRes.User.IsBanned.Bool {
			t := i18n.GetBundle(e.lang)
			reason := "Suspended by admin"
			if userRes.User.BanReason.Valid && userRes.User.BanReason.String != "" {
				reason = userRes.User.BanReason.String
			}
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, reason), nil)
			return
		}
	}

	if data == "action:trial" || data == "trial" || strings.HasPrefix(data, "action:claim_trial") {
		parts := strings.Split(data, ":")
		refCode := ""
		if len(parts) > 2 {
			refCode = parts[2]
		}
		e.handleClaimTrial(ctx, chatID, cb.From.UserName, refCode)
		return
	}

	if data == "action:start" || data == "start" || data == "action:back_main" || data == "back_main" {
		e.handleStart(ctx, cb.Message, "")
		return
	}

	if data == "action:status" || data == "status" {
		e.handleStatus(ctx, chatID)
		return
	}

	if data == "action:buy" || data == "action:plans" || data == "plans" || data == "buy" {
		e.handleBuy(ctx, chatID)
		return
	}

	if data == "action:reset_keys" {
		e.handleResetPrompt(ctx, chatID)
		return
	}

	if data == "action:confirm_reset_keys" {
		e.handleConfirmResetKeys(ctx, chatID)
		return
	}

	if data == "action:cancel_reset" {
		e.handleStatus(ctx, chatID)
		return
	}

	if data == "action:devices" || data == "devices" {
		e.handleDeviceWizard(ctx, chatID)
		return
	}

	if strings.HasPrefix(data, "action:device:") {
		platform := strings.TrimPrefix(data, "action:device:")
		e.handlePlatformGuide(ctx, chatID, platform)
		return
	}

	if strings.HasPrefix(data, "device:") {
		platform := strings.TrimPrefix(data, "device:")
		e.handlePlatformGuide(ctx, chatID, platform)
		return
	}

	if data == "action:nodes" || data == "action:servers" || data == "servers" {
		e.handleServerList(ctx, chatID)
		return
	}

	if data == "action:promo" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
		return
	}

	if data == "action:email" || data == "action:link_email" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_email"
		e.sendMessage(chatID, t.EmailPrompt, nil)
		return
	}

	if data == "action:restore" || data == "action:restore_account" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_restore"
		e.sendMessage(chatID, t.RestorePrompt, nil)
		return
	}

	if data == "action:referral" || data == "referral" {
		e.handleReferral(ctx, chatID)
		return
	}

	if data == "action:help" || data == "help" {
		e.handleHelp(ctx, chatID)
		return
	}

	if data == "action:support" || data == "support" {
		e.handleSupport(ctx, chatID)
		return
	}

	if data == "action:back_main" || data == "back_main" {
		e.handleStart(ctx, cb.Message, "")
		return
	}

	if strings.HasPrefix(data, "plan:") {
		// Plan selected -> show duration
		planID := strings.TrimPrefix(data, "plan:")
		e.handleSelectDuration(ctx, chatID, planID)
		return
	}

	if strings.HasPrefix(data, "dur:") {
		// Duration selected (dur:planID:months)
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			planID := parts[1]
			months, _ := strconv.Atoi(parts[2])
			e.handleSelectPayment(ctx, chatID, planID, int32(months))
		}
		return
	}

	if strings.HasPrefix(data, "pay:") {
		// Gateway selected (pay:planID:months:gateway)
		parts := strings.Split(data, ":")
		if len(parts) == 4 {
			planID := parts[1]
			months, _ := strconv.Atoi(parts[2])
			gateway := parts[3]
			e.handleCheckout(ctx, chatID, planID, int32(months), gateway)
		}
		return
	}

	if strings.HasPrefix(data, "action:qr:") {
		token := strings.TrimPrefix(data, "action:qr:")
		e.sendQRCode(chatID, token)
		return
	}
}
