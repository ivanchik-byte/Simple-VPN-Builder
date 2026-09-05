package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
)

type Provider interface {
	Name() string
	CreateInvoice(ctx context.Context, orderID uuid.UUID, title, description string, amount int64, currency string) (string, error)
}

type StarsProvider struct {
	bot *tgbotapi.BotAPI
}

func NewStarsProvider(bot *tgbotapi.BotAPI) *StarsProvider {
	return &StarsProvider{bot: bot}
}

func (s *StarsProvider) Name() string {
	return "stars"
}

func (s *StarsProvider) SendStarsInvoice(chatID int64, title, description, payload string, starsAmount int) (tgbotapi.Message, error) {
	invoice := tgbotapi.NewInvoice(
		chatID,
		title,
		description,
		payload,
		"", // provider token empty for Telegram Stars
		"start_parameter",
		"XTR",
		[]tgbotapi.LabeledPrice{
			{Label: title, Amount: starsAmount},
		},
	)
	return s.bot.Send(invoice)
}

type CryptoBotProvider struct {
	apiToken   string
	baseURL    string
	httpClient *http.Client
}

func NewCryptoBotProvider(apiToken string) *CryptoBotProvider {
	return &CryptoBotProvider{
		apiToken:   apiToken,
		baseURL:    "https://pay.crypt.bot/api",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func NewCryptoBotProviderWithClient(apiToken string, baseURL string, httpClient *http.Client) *CryptoBotProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://pay.crypt.bot/api"
	}
	return &CryptoBotProvider{
		apiToken:   apiToken,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *CryptoBotProvider) Name() string {
	return "cryptobot"
}

func (c *CryptoBotProvider) SetAPIToken(token string) {
	c.apiToken = token
}

func (c *CryptoBotProvider) APIToken() string {
	return c.apiToken
}


type CryptoBotCreateInvoiceParams struct {
	Amount      string `json:"amount"`
	Asset       string `json:"asset"` // USDT, TON, BTC, etc.
	Description string `json:"description,omitempty"`
	Payload     string `json:"payload,omitempty"`
}

type cryptoBotAPIResponse struct {
	OK     bool             `json:"ok"`
	Error  *cryptoBotError  `json:"error,omitempty"`
	Result *CryptoBotInvoice `json:"result,omitempty"`
}

type cryptoBotError struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

type CryptoBotInvoice struct {
	InvoiceID     int64  `json:"invoice_id"`
	Status        string `json:"status"`
	PayURL        string `json:"pay_url"`
	BotInvoiceURL string `json:"bot_invoice_url"`
}

// CreateInvoice calls CryptoPay API to create an invoice.
// If apiToken is not set, returns a fallback pay URL without network call.
func (c *CryptoBotProvider) CreateInvoice(ctx context.Context, params CryptoBotCreateInvoiceParams) (*CryptoBotInvoice, error) {
	if c.apiToken == "" {
		fallbackURL := c.BuildPayURL(params.Payload)
		return &CryptoBotInvoice{
			Status:        "active",
			PayURL:        fallbackURL,
			BotInvoiceURL: fallbackURL,
		}, nil
	}

	if params.Asset == "" {
		params.Asset = "USDT"
	}

	reqBody := map[string]interface{}{
		"currency_type": "crypto",
		"asset":         params.Asset,
		"amount":        params.Amount,
		"description":   params.Description,
		"payload":       params.Payload,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/createInvoice", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Crypto-Pay-API-Token", c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp cryptoBotAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if !apiResp.OK || apiResp.Result == nil {
		errMsg := "unknown cryptobot error"
		if apiResp.Error != nil {
			errMsg = fmt.Sprintf("%s (code %d)", apiResp.Error.Name, apiResp.Error.Code)
		}
		return nil, fmt.Errorf("cryptobot api error: %s", errMsg)
	}

	return apiResp.Result, nil
}

func (c *CryptoBotProvider) BuildPayURL(invoiceID string) string {
	return fmt.Sprintf("https://t.me/CryptoBot?start=%s", invoiceID)
}

type Manager struct {
	cpClient *client.CPClient
	stars    *StarsProvider
	crypto   *CryptoBotProvider
}

func NewManager(cpClient *client.CPClient, stars *StarsProvider, crypto *CryptoBotProvider) *Manager {
	return &Manager{
		cpClient: cpClient,
		stars:    stars,
		crypto:   crypto,
	}
}

func (m *Manager) Stars() *StarsProvider {
	return m.stars
}

func (m *Manager) Crypto() *CryptoBotProvider {
	return m.crypto
}
