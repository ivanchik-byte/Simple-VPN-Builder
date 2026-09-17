package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/engine"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cpBaseURL := os.Getenv("CONTROL_PLANE_URL")
	if cpBaseURL == "" {
		cpBaseURL = "http://localhost:8110"
	}

	cpAPIKey := os.Getenv("CONTROL_PLANE_API_KEY")
	if cpAPIKey == "" {
		slog.Error("Refusing to start: CONTROL_PLANE_API_KEY must be set (no dev default)")
		os.Exit(1)
	}

	cpClient := client.NewCPClient(cpBaseURL, cpAPIKey)
	cryptoBotToken := os.Getenv("CRYPTOBOT_TOKEN")
	envBotToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var bot *tgbotapi.BotAPI
	var lastLoggedToken string

	slog.Info("Starting Telegram bot service...", "cp_url", cpBaseURL)

	// Token resolution and authorization loop
	for {
		currentToken := envBotToken

		// Fetch billing settings from control plane to check for web-configured token
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 4*time.Second)
		settings, err := cpClient.GetBillingSettings(fetchCtx)
		if err == nil && settings != nil {
			if strings.TrimSpace(settings.SalesBotToken) != "" {
				currentToken = strings.TrimSpace(settings.SalesBotToken)
			}
			if settings.CryptobotApiToken != "" && cryptoBotToken == "" {
				cryptoBotToken = settings.CryptobotApiToken
			}
		}
		if replies, rErr := cpClient.GetBotReplies(fetchCtx); rErr == nil && replies != nil {
			if t := strings.TrimSpace(replies["bot_token"]); t != "" {
				currentToken = t
			}
		}
		fetchCancel()

		if currentToken != "" {
			b, err := tgbotapi.NewBotAPI(currentToken)
			if err == nil {
				bot = b
				bot.Debug = false
				slog.Info("Telegram bot authorized successfully", "account", bot.Self.UserName)
				break
			}
			if currentToken != lastLoggedToken {
				slog.Warn("Failed to authorize Telegram Bot API with current token", "error", err)
				lastLoggedToken = currentToken
			}
		} else {
			if lastLoggedToken != "<empty>" {
				slog.Warn("No Telegram Bot token configured yet. Waiting for configuration in Web Panel (Settings -> Billing) or TELEGRAM_BOT_TOKEN...")
				lastLoggedToken = "<empty>"
			}
		}

		select {
		case <-sigChan:
			slog.Info("Bot daemon stopped before initialization")
			return
		case <-time.After(5 * time.Second):
		}
	}

	starsProvider := payment.NewStarsProvider(bot)
	cryptoProvider := payment.NewCryptoBotProvider(cryptoBotToken)
	paymentMgr := payment.NewManager(cpClient, starsProvider, cryptoProvider)

	botEngine := engine.NewBotEngine(bot, cpClient, paymentMgr)

	go func() {
		backoff := time.Second
		const maxBackoff = 60 * time.Second
		for {
			if err := botEngine.Start(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("Bot engine stopped, restarting", "error", err, "backoff", backoff.String())
			} else if ctx.Err() != nil {
				return
			} else {
				slog.Warn("Bot engine exited, restarting", "backoff", backoff.String())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()

	slog.Info("Telegram commercial bot daemon running", "cp_url", cpBaseURL, "account", bot.Self.UserName)

	<-sigChan
	slog.Info("Shutting down bot daemon...")
	cancel()
	time.Sleep(1 * time.Second)
	slog.Info("Bot daemon stopped")
}
