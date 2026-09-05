package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/engine"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		slog.Error("TELEGRAM_BOT_TOKEN environment variable is required")
		os.Exit(1)
	}

	cpBaseURL := os.Getenv("CONTROL_PLANE_URL")
	if cpBaseURL == "" {
		cpBaseURL = "http://localhost:8110"
	}

	cpAPIKey := os.Getenv("CONTROL_PLANE_API_KEY")
	if cpAPIKey == "" {
		slog.Error("CONTROL_PLANE_API_KEY environment variable is required")
		os.Exit(1)
	}

	cryptoBotToken := os.Getenv("CRYPTOBOT_TOKEN")

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		slog.Error("Failed to initialize Telegram Bot API", "error", err)
		os.Exit(1)
	}

	bot.Debug = false
	slog.Info("Telegram bot authorized", "account", bot.Self.UserName)

	cpClient := client.NewCPClient(cpBaseURL, cpAPIKey)
	starsProvider := payment.NewStarsProvider(bot)
	cryptoProvider := payment.NewCryptoBotProvider(cryptoBotToken)
	paymentMgr := payment.NewManager(cpClient, starsProvider, cryptoProvider)

	botEngine := engine.NewBotEngine(bot, cpClient, paymentMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := botEngine.Start(ctx); err != nil {
			slog.Error("Bot engine stopped", "error", err)
		}
	}()

	slog.Info("Telegram commercial bot daemon running", "cp_url", cpBaseURL)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down bot daemon...")
	cancel()
	time.Sleep(1 * time.Second)
	slog.Info("Bot daemon stopped")
}
