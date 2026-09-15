package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type CPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewCPClient(baseURL, apiKey string) *CPClient {
	return &CPClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type TrialResponse struct {
	User              store.User `json:"user"`
	SubscriptionToken string     `json:"subscription_token"`
	SubscriptionURL   string     `json:"subscription_url"`
	TrialHours        int32      `json:"trial_hours"`
	TrafficLimitBytes int64      `json:"traffic_limit_bytes"`
}

type UserWithSubscription struct {
	User              store.User `json:"user"`
	SubscriptionToken string     `json:"subscription_token"`
	SubscriptionURL   string     `json:"subscription_url"`
}

type RotateResponse struct {
	SubscriptionToken string `json:"subscription_token"`
	SubscriptionURL   string `json:"subscription_url"`
}

type CreateInvoiceRequest struct {
	UserID         uuid.UUID `json:"user_id"`
	PlanID         uuid.UUID `json:"plan_id"`
	Gateway        string    `json:"gateway"`
	DurationMonths int32     `json:"duration_months"`
	PromoCode      string    `json:"promo_code,omitempty"`
}

type CreateInvoiceResponse struct {
	OrderID           uuid.UUID `json:"order_id"`
	ExternalInvoiceID string    `json:"external_invoice_id"`
	Amount            string    `json:"amount"`
	Currency          string    `json:"currency"`
	Gateway           string    `json:"gateway"`
	DurationMonths    int32     `json:"duration_months"`
	CheckoutURL       string    `json:"checkout_url,omitempty"`
}

func (c *CPClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBytes, resp.StatusCode, nil
}

func (c *CPClient) GetTrialPlan(ctx context.Context) (*store.Plan, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, "/api/v1/plans/trial", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var plan store.Plan
	if err := json.Unmarshal(respBytes, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (c *CPClient) ListPlans(ctx context.Context) ([]store.Plan, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, "/api/v1/plans", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var plans []store.Plan
	if err := json.Unmarshal(respBytes, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func (c *CPClient) GetUserByTelegramID(ctx context.Context, tgID int64) (*UserWithSubscription, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/by-telegram/%d", tgID), nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil // User does not exist yet
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res UserWithSubscription
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *CPClient) CreateTrial(ctx context.Context, tgID int64, username, refCode string) (*TrialResponse, error) {
	payload := map[string]any{
		"telegram_id":       tgID,
		"telegram_username": username,
		"referrer_code":     refCode,
	}

	respBytes, code, err := c.doRequest(ctx, http.MethodPost, "/api/v1/users/trial", payload)
	if err != nil {
		return nil, err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res TrialResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *CPClient) RotateKeys(ctx context.Context, userID uuid.UUID) (*RotateResponse, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%s/rotate", userID.String()), nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res RotateResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *CPClient) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*CreateInvoiceResponse, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodPost, "/api/v1/billing/invoices", req)
	if err != nil {
		return nil, err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res CreateInvoiceResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *CPClient) ListActiveNodes(ctx context.Context) ([]store.Node, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, "/api/v1/nodes", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res struct {
		Data []store.Node `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &res); err != nil {
		var direct []store.Node
		if errDirect := json.Unmarshal(respBytes, &direct); errDirect == nil {
			return direct, nil
		}
		return nil, err
	}
	return res.Data, nil
}

func (c *CPClient) ValidatePromo(ctx context.Context, code string) (*store.PromoCode, error) {
	payload := map[string]string{"code": code}
	respBytes, codeStatus, err := c.doRequest(ctx, http.MethodPost, "/api/v1/billing/promos/validate", payload)
	if err != nil {
		return nil, err
	}
	if codeStatus != http.StatusOK {
		return nil, fmt.Errorf("promo code invalid or expired: %s", string(respBytes))
	}

	var promo store.PromoCode
	if err := json.Unmarshal(respBytes, &promo); err != nil {
		return nil, err
	}
	return &promo, nil
}

func (c *CPClient) BaseURL() string {
	return c.baseURL
}

func (c *CPClient) GetSubscriptionConfig(ctx context.Context, token string, format string) ([]byte, error) {
	path := fmt.Sprintf("/sub/%s", token)
	if format != "" {
		path = fmt.Sprintf("/sub/%s?format=%s", token, format)
	}
	respBytes, codeStatus, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if codeStatus != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", codeStatus, string(respBytes))
	}
	return respBytes, nil
}


type BillingSettings struct {
	ID                   int32  `json:"id"`
	CryptobotApiToken    string `json:"cryptobot_api_token"`
	CryptobotEnabled     bool   `json:"cryptobot_enabled"`
	TelegramStarsEnabled bool   `json:"telegram_stars_enabled"`
	StarsPricePerMonth   int32  `json:"stars_price_per_month"`
	WebhookSecret        string `json:"webhook_secret"`
	SalesBotToken        string `json:"sales_bot_token,omitempty"`
}

func (c *CPClient) GetBillingSettings(ctx context.Context) (*BillingSettings, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, "/api/v1/billing/settings", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var settings BillingSettings
	if err := json.Unmarshal(respBytes, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

type ReferralStatsResponse struct {
	TelegramID           int64  `json:"telegram_id"`
	ReferralCode         string `json:"referral_code"`
	ReferralCount        int64  `json:"referral_count"`
	BonusDaysPerReferral int    `json:"bonus_days_per_referral"`
}

func (c *CPClient) GetReferralStats(ctx context.Context, tgID int64) (*ReferralStatsResponse, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/by-telegram/%d/referrals", tgID), nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d: %s", code, string(respBytes))
	}

	var res ReferralStatsResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type TelegramLeadParams struct {
	TelegramID       int64  `json:"telegram_id"`
	TelegramUsername string `json:"telegram_username"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	LanguageCode     string `json:"language_code"`
	ReferrerCode     string `json:"referrer_code"`
}

func (c *CPClient) UpsertLead(ctx context.Context, params TelegramLeadParams) error {
	_, code, err := c.doRequest(ctx, http.MethodPost, "/api/v1/users/upsert-lead", params)
	if err != nil {
		return err
	}
	if code != http.StatusOK && code != http.StatusCreated {
		return fmt.Errorf("failed to upsert lead: status %d", code)
	}
	return nil
}

func (c *CPClient) GetBotReplies(ctx context.Context) (map[string]string, error) {
	respBytes, code, err := c.doRequest(ctx, http.MethodGet, "/api/v1/billing/bot-replies", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("failed to get bot replies: status %d", code)
	}
	var replies map[string]string
	if err := json.Unmarshal(respBytes, &replies); err != nil {
		return nil, err
	}
	return replies, nil
}

type LinkEmailResponse struct {
	OK   bool       `json:"ok"`
	User store.User `json:"user"`
}

type RestoreAccountResponse struct {
	OK                bool       `json:"ok"`
	User              store.User `json:"user"`
	SubscriptionToken string     `json:"subscription_token"`
	SubscriptionURL   string     `json:"subscription_url"`
}

func (c *CPClient) LinkEmail(ctx context.Context, tgID int64, email string) (*store.User, error) {
	payload := map[string]any{
		"telegram_id": tgID,
		"email":       email,
	}
	respBytes, code, err := c.doRequest(ctx, http.MethodPost, "/api/v1/users/link-email", payload)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("failed to link email: %s", string(respBytes))
	}
	var res LinkEmailResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res.User, nil
}

func (c *CPClient) RestoreAccount(ctx context.Context, email string, tgID int64, username, firstName, lastName string) (*RestoreAccountResponse, error) {
	payload := map[string]any{
		"email":             email,
		"telegram_id":       tgID,
		"telegram_username": username,
		"first_name":        firstName,
		"last_name":         lastName,
	}
	respBytes, code, err := c.doRequest(ctx, http.MethodPost, "/api/v1/users/restore-account", payload)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("failed to restore account: %s", string(respBytes))
	}
	var res RestoreAccountResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}



