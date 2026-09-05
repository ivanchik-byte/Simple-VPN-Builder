package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type BroadcastButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string, buttons []BroadcastButton) error
}

type HTTPTelegramSender struct {
	botToken   string
	baseURL    string
	httpClient *http.Client
}

func NewHTTPTelegramSender(botToken string, baseURL string) *HTTPTelegramSender {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &HTTPTelegramSender{
		botToken: botToken,
		baseURL:  baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *HTTPTelegramSender) SendMessage(ctx context.Context, chatID int64, text string, buttons []BroadcastButton) error {
	if s.botToken == "" {
		return fmt.Errorf("telegram bot token is not configured")
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", s.baseURL, s.botToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	if len(buttons) > 0 {
		var inlineKeyboard [][]map[string]string
		for _, btn := range buttons {
			inlineKeyboard = append(inlineKeyboard, []map[string]string{
				{
					"text": btn.Text,
					"url":  btn.URL,
				},
			})
		}
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": inlineKeyboard,
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api responded with status %d", resp.StatusCode)
	}

	return nil
}

type BroadcastService struct {
	billingRepo store.BillingRepository
	userRepo    store.UserRepository
	sender      TelegramSender
	rateDelay   time.Duration
	logger      *slog.Logger
}

func NewBroadcastService(billingRepo store.BillingRepository, userRepo store.UserRepository, sender TelegramSender, logger *slog.Logger) *BroadcastService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BroadcastService{
		billingRepo: billingRepo,
		userRepo:    userRepo,
		sender:      sender,
		rateDelay:   40 * time.Millisecond, // ~25 msgs/second safely under Telegram's 30/s limit
		logger:      logger,
	}
}

func (s *BroadcastService) SetRateDelay(delay time.Duration) {
	s.rateDelay = delay
}

func (s *BroadcastService) CreateAndDispatch(ctx context.Context, title, segment, text string, buttons []BroadcastButton) (store.BroadcastCampaign, error) {
	recipients, err := s.userRepo.ListTelegramIDsForBroadcast(ctx, segment)
	if err != nil {
		return store.BroadcastCampaign{}, fmt.Errorf("failed to query broadcast recipients: %w", err)
	}

	btnBytes, _ := json.Marshal(buttons)

	campaign, err := s.billingRepo.CreateBroadcastCampaign(ctx, store.CreateBroadcastCampaignParams{
		Title:           title,
		TargetSegment:   segment,
		MessageText:     text,
		InlineButtons:   btnBytes,
		TotalRecipients: pgtype.Int4{Int32: int32(len(recipients)), Valid: true},
		Status:          pgtype.Text{String: "pending", Valid: true},
	})
	if err != nil {
		return store.BroadcastCampaign{}, fmt.Errorf("failed to create broadcast campaign: %w", err)
	}

	// Launch async background dispatching
	go s.dispatch(campaign.ID, recipients, text, buttons)

	return campaign, nil
}

func (s *BroadcastService) dispatch(campaignID uuid.UUID, recipients []int64, text string, buttons []BroadcastButton) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	s.logger.Info("Starting broadcast campaign dispatch",
		"campaign_id", campaignID.String(),
		"total_recipients", len(recipients),
	)

	_, _ = s.billingRepo.UpdateBroadcastCampaignStats(ctx, store.UpdateBroadcastCampaignStatsParams{
		ID:          campaignID,
		SentCount:   pgtype.Int4{Int32: 0, Valid: true},
		FailedCount: pgtype.Int4{Int32: 0, Valid: true},
		Status:      pgtype.Text{String: "in_progress", Valid: true},
	})

	var sentCount, failedCount int32

	for _, chatID := range recipients {
		select {
		case <-ctx.Done():
			s.logger.Warn("Broadcast campaign context cancelled", "campaign_id", campaignID.String())
			break
		default:
		}

		if s.rateDelay > 0 {
			time.Sleep(s.rateDelay)
		}

		if s.sender != nil {
			err := s.sender.SendMessage(ctx, chatID, text, buttons)
			if err != nil {
				s.logger.Debug("Failed to deliver broadcast message", "chat_id", chatID, "error", err)
				failedCount++
			} else {
				sentCount++
			}
		} else {
			// No sender configured (dry-run / simulation)
			sentCount++
		}
	}

	now := time.Now()
	finalStatus := "completed"
	if len(recipients) > 0 && sentCount == 0 && failedCount > 0 {
		finalStatus = "failed"
	}

	_, err := s.billingRepo.UpdateBroadcastCampaignStats(ctx, store.UpdateBroadcastCampaignStatsParams{
		ID:          campaignID,
		SentCount:   pgtype.Int4{Int32: sentCount, Valid: true},
		FailedCount: pgtype.Int4{Int32: failedCount, Valid: true},
		Status:      pgtype.Text{String: finalStatus, Valid: true},
		CompletedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		s.logger.Error("Failed to update final broadcast campaign stats", "campaign_id", campaignID.String(), "error", err)
	}

	s.logger.Info("Completed broadcast campaign dispatch",
		"campaign_id", campaignID.String(),
		"sent", sentCount,
		"failed", failedCount,
		"status", finalStatus,
	)
}

func (s *BroadcastService) ListCampaigns(ctx context.Context) ([]store.BroadcastCampaign, error) {
	return s.billingRepo.ListBroadcastCampaigns(ctx)
}

func (s *BroadcastService) GetCampaign(ctx context.Context, id uuid.UUID) (store.BroadcastCampaign, error) {
	return s.billingRepo.GetBroadcastCampaign(ctx, id)
}
