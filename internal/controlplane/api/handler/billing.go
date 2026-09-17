package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type BillingHandler struct {
	billingRepo      store.BillingRepository
	userRepo         store.UserRepository
	planRepo         store.PlanRepository
	adminRepo        store.AdminRepository
	audit            *middleware.AuditService
	broadcastService *service.BroadcastService
}

func NewBillingHandler(
	billingRepo store.BillingRepository,
	userRepo store.UserRepository,
	planRepo store.PlanRepository,
	audit *middleware.AuditService,
) *BillingHandler {
	return &BillingHandler{
		billingRepo: billingRepo,
		userRepo:    userRepo,
		planRepo:    planRepo,
		audit:       audit,
	}
}

func (h *BillingHandler) SetBroadcastService(svc *service.BroadcastService) {
	h.broadcastService = svc
}

// SetAdminRepo installs the admin repository used for granular permission checks.
func (h *BillingHandler) SetAdminRepo(repo store.AdminRepository) {
	h.adminRepo = repo
}

// callerCanBill reports whether the caller may change billing configuration.
// A missing identity is allowed here because route-level RequireAuth rejects
// unauthenticated requests in production; this gate only narrows permissions.
func (h *BillingHandler) callerCanBill(r *http.Request) bool {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		return true
	}
	if authCtx.Role == "owner" {
		return true
	}
	if authCtx.AuthType != "jwt" {
		return true
	}
	if h.adminRepo == nil {
		return false
	}
	admin, err := h.adminRepo.GetByID(r.Context(), authCtx.UserID)
	if err != nil {
		return false
	}
	return admin.ParsedPermissions().CanManageBilling
}

type CreateInvoiceRequest struct {
	UserID         uuid.UUID `json:"user_id" validate:"required,uuid"`
	PlanID         uuid.UUID `json:"plan_id" validate:"required,uuid"`
	Gateway        string    `json:"gateway" validate:"required,min=2,max=32"`
	DurationMonths int32     `json:"duration_months" validate:"min=1,max=24"`
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

func (h *BillingHandler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	var req CreateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	ctx := r.Context()

	settings, _ := h.billingRepo.GetBillingSettings(ctx)
	if req.Gateway == "cryptobot" && !settings.CryptobotEnabled {
		response.RespondBadRequest(w, r, "Payment gateway 'cryptobot' is currently disabled", nil)
		return
	}
	if req.Gateway == "stars" && !settings.TelegramStarsEnabled {
		response.RespondBadRequest(w, r, "Payment gateway 'stars' is currently disabled", nil)
		return
	}

	user, err := h.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	plan, err := h.planRepo.GetByID(ctx, req.PlanID)
	if err != nil {
		response.RespondNotFound(w, r, "Plan not found")
		return
	}

	duration := req.DurationMonths
	if duration <= 0 {
		duration = 1
	}

	currency := "USD"
	if req.Gateway == "stars" {
		currency = "XTR"
	}

	var totalPrice float64
	var priceFound bool

	switch duration {
	case 1:
		if plan.Price1m.Valid {
			if fVal, fErr := plan.Price1m.Float64Value(); fErr == nil && fVal.Valid && fVal.Float64 > 0 {
				totalPrice = fVal.Float64
				priceFound = true
			}
		}
	case 3:
		if plan.Price3m.Valid {
			if fVal, fErr := plan.Price3m.Float64Value(); fErr == nil && fVal.Valid && fVal.Float64 > 0 {
				totalPrice = fVal.Float64
				priceFound = true
			}
		}
	case 6:
		if plan.Price6m.Valid {
			if fVal, fErr := plan.Price6m.Float64Value(); fErr == nil && fVal.Valid && fVal.Float64 > 0 {
				totalPrice = fVal.Float64
				priceFound = true
			}
		}
	case 12:
		if plan.Price12m.Valid {
			if fVal, fErr := plan.Price12m.Float64Value(); fErr == nil && fVal.Valid && fVal.Float64 > 0 {
				totalPrice = fVal.Float64
				priceFound = true
			}
		}
	}

	if !priceFound {
		var unitPrice float64 = 5.00
		if plan.MonthlyPrice.Valid {
			if fVal, fErr := plan.MonthlyPrice.Float64Value(); fErr == nil && fVal.Valid && fVal.Float64 > 0 {
				unitPrice = fVal.Float64
			}
		}

		totalPrice = unitPrice * float64(duration)
		// Duration discounts
		if duration >= 12 {
			totalPrice *= 0.80
		} else if duration >= 6 {
			totalPrice *= 0.85
		} else if duration >= 3 {
			totalPrice *= 0.90
		}
	}

	amountStr := fmt.Sprintf("%.2f", totalPrice)

	if req.Gateway == "stars" {
		starsPerMonth := int32(250)
		if plan.PriceStars.Valid && plan.PriceStars.Int32 > 0 {
			starsPerMonth = plan.PriceStars.Int32
		} else if settings.StarsPricePerMonth > 0 {
			starsPerMonth = settings.StarsPricePerMonth
		}

		if starsPerMonth > 0 {
			starsTotal := int(starsPerMonth * duration)
			if duration >= 12 {
				starsTotal = int(float64(starsTotal) * 0.80)
			} else if duration >= 6 {
				starsTotal = int(float64(starsTotal) * 0.85)
			} else if duration >= 3 {
				starsTotal = int(float64(starsTotal) * 0.90)
			}
			amountStr = fmt.Sprintf("%d", starsTotal)
		} else {
			amountStr = "0"
		}
	}

	// Apply promo code discount if provided.
	// Redemption itself happens atomically inside CreateOrderWithPromo;
	// this lookup only prices the discount shown on the invoice.
	var promoID *uuid.UUID
	if strings.TrimSpace(req.PromoCode) != "" {
		promo, err := h.billingRepo.GetPromoCode(ctx, strings.TrimSpace(req.PromoCode))
		if err != nil {
			response.RespondBadRequest(w, r, "Invalid promo code", nil)
			return
		}
		{
			promoID = &promo.ID
			if promo.DiscountPercent.Valid && promo.DiscountPercent.Int32 > 0 {
				factor := 1.0 - (float64(promo.DiscountPercent.Int32) / 100.0)
				if factor < 0 {
					factor = 0
				}
				if req.Gateway != "stars" {
					totalPrice *= factor
					amountStr = fmt.Sprintf("%.2f", totalPrice)
				}
			}
		}
	}

	var numericAmount pgtype.Numeric
	_ = numericAmount.Scan(amountStr)

	extInvoiceID := fmt.Sprintf("inv_%s_%d", uuid.New().String()[:8], time.Now().Unix())

	metadataMap := map[string]interface{}{
		"telegram_username": user.Username,
		"plan_name":         plan.Name,
	}
	if promoID != nil {
		metadataMap["promo_id"] = promoID.String()
	}
	metaBytes, _ := json.Marshal(metadataMap)

	order, err := h.billingRepo.CreateOrderWithPromo(ctx, store.CreateOrderParams{
		UserID:            user.ID,
		PlanID:            plan.ID,
		Gateway:           req.Gateway,
		ExternalInvoiceID: pgtype.Text{String: extInvoiceID, Valid: true},
		Amount:            numericAmount,
		Currency:          currency,
		Status:            pgtype.Text{String: "pending", Valid: true},
		DurationMonths:    pgtype.Int4{Int32: duration, Valid: true},
		Metadata:          metaBytes,
	}, promoID)
	if errors.Is(err, pgx.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "promo exhausted")) {
		response.RespondConflict(w, r, "Promo code exhausted or expired")
		return
	}
	if err != nil {
		response.RespondInternalError(w, r, fmt.Sprintf("Failed to create order: %v", err))
		return
	}

	var checkoutURL string
	switch req.Gateway {
	case "manual":
		checkoutURL = ""
	case "cryptobot":
		checkoutURL = fmt.Sprintf("https://t.me/CryptoBot?start=%s", extInvoiceID)
	case "stars":
		checkoutURL = ""
	}

	if h.audit != nil {
		diffBytes, _ := json.Marshal(map[string]interface{}{
			"user_id": user.ID,
			"plan_id": plan.ID,
			"amount":  amountStr,
			"gateway": req.Gateway,
		})
		_ = h.audit.Log(r, "create_invoice", "order", &order.ID, diffBytes)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateInvoiceResponse{
		OrderID:           order.ID,
		ExternalInvoiceID: extInvoiceID,
		Amount:            amountStr,
		Currency:          currency,
		Gateway:           req.Gateway,
		DurationMonths:    duration,
		CheckoutURL:       checkoutURL,
	})
}

type PaymentWebhookRequest struct {
	ExternalInvoiceID string `json:"external_invoice_id"`
	OrderID           string `json:"order_id,omitempty"`
	Status            string `json:"status"`
}

func verifyWebhookSignature(r *http.Request, body []byte, secretToken string) bool {
	if secretToken == "" {
		return false
	}

	// 1. CryptoBot HMAC signature: header crypto-pay-api-signature
	// CryptoBot uses HMAC-SHA256 with key = SHA256(api_token)
	sigHeader := r.Header.Get("crypto-pay-api-signature")
	if sigHeader != "" {
		tokenHash := sha256.Sum256([]byte(secretToken))
		mac := hmac.New(sha256.New, tokenHash[:])
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(sigHeader), []byte(expected)) == 1 {
			return true
		}
	}

	// 2. Generic HMAC signature: X-Signature or X-Hub-Signature-256
	genericSig := r.Header.Get("X-Signature")
	if genericSig == "" {
		genericSig = strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
	}
	if genericSig != "" {
		mac := hmac.New(sha256.New, []byte(secretToken))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(genericSig), []byte(expected)) == 1 {
			return true
		}
	}

	// 3. Shared secret token header: X-Webhook-Secret
	webhookSecret := r.Header.Get("X-Webhook-Secret")
	if webhookSecret != "" && subtle.ConstantTimeCompare([]byte(webhookSecret), []byte(secretToken)) == 1 {
		return true
	}

	// 4. Query parameter token: ?token=...
	queryToken := r.URL.Query().Get("token")
	if queryToken != "" && subtle.ConstantTimeCompare([]byte(queryToken), []byte(secretToken)) == 1 {
		return true
	}

	return false
}

func (h *BillingHandler) ProcessWebhook(w http.ResponseWriter, r *http.Request) {
	gateway := chi.URLParam(r, "gateway")
	ctx := r.Context()

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		response.RespondBadRequest(w, r, "Failed to read request body", nil)
		return
	}

	// Verify gateway and authentication secret
	settings, _ := h.billingRepo.GetBillingSettings(ctx)
	if gateway == "cryptobot" && !settings.CryptobotEnabled {
		response.RespondForbidden(w, r, fmt.Sprintf("Payment gateway %q is disabled", gateway))
		return
	}
	if gateway == "stars" && !settings.TelegramStarsEnabled {
		response.RespondForbidden(w, r, fmt.Sprintf("Payment gateway %q is disabled", gateway))
		return
	}

	gw, gwErr := h.billingRepo.GetPaymentGatewayByName(ctx, gateway)
	if gwErr == nil && gw.IsEnabled.Valid && !gw.IsEnabled.Bool {
		response.RespondForbidden(w, r, fmt.Sprintf("Payment gateway %q is disabled", gateway))
		return
	}

	var secretToken string
	if gwErr == nil && gw.ConfigEncrypted != "" {
		var cfg map[string]string
		if err := json.Unmarshal([]byte(gw.ConfigEncrypted), &cfg); err == nil && cfg["token"] != "" {
			secretToken = cfg["token"]
		}
	}

	if secretToken == "" {
		if gateway == "cryptobot" && settings.CryptobotApiToken != "" {
			secretToken = settings.CryptobotApiToken
		} else if settings.WebhookSecret != "" {
			secretToken = settings.WebhookSecret
		}
	}

	if secretToken == "" {
		response.RespondForbidden(w, r, "Payment gateway is not configured")
		return
	}
	if !verifyWebhookSignature(r, bodyBytes, secretToken) {
		response.RespondUnauthorized(w, r, "Invalid webhook signature or secret token")
		return
	}

	var req PaymentWebhookRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		response.RespondBadRequest(w, r, "Invalid webhook body", nil)
		return
	}

	var order store.Order
	if req.ExternalInvoiceID != "" {
		order, err = h.billingRepo.GetOrderByExternalInvoiceID(ctx, req.ExternalInvoiceID)
	} else if req.OrderID != "" {
		if orderUUID, parseErr := uuid.Parse(req.OrderID); parseErr == nil {
			order, err = h.billingRepo.GetOrderByID(ctx, orderUUID)
		}
	}

	if err != nil {
		response.RespondNotFound(w, r, "Order not found")
		return
	}

	// Idempotency: if already paid, return 200 OK immediately without double crediting
	if order.Status.String == "paid" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "already_processed",
			"order_id": order.ID,
		})
		return
	}

	now := time.Now()
	_, err = h.billingRepo.UpdateOrderStatus(ctx, order.ID, "paid", &now)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to update order status")
		return
	}

	// Fetch plan details to calculate extra quota
	plan, err := h.planRepo.GetByID(ctx, order.PlanID)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve order plan")
		return
	}

	user, err := h.userRepo.GetByID(ctx, order.UserID)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve user for renewal")
		return
	}

	// Calculate extended expiration date
	baseTime := time.Now()
	if user.ExpiresAt.Valid && user.ExpiresAt.Time.After(baseTime) {
		baseTime = user.ExpiresAt.Time
	}
	months := int(order.DurationMonths.Int32)
	if months <= 0 {
		months = 1
	}
	newExpiresAt := baseTime.AddDate(0, months, 0)

	var extraTraffic int64
	if plan.TrafficLimit.Valid {
		extraTraffic = plan.TrafficLimit.Int64
	}

	// Extend user subscription in database
	_, _ = h.userRepo.ExtendSubscription(ctx, user.ID, newExpiresAt, extraTraffic)

	// If user was referred by another user, award referral bonus to referrer (+5 days)
	if user.ReferrerID.Valid {
		refID := user.ReferrerID.Bytes
		if refUser, err := h.userRepo.GetByID(ctx, refID); err == nil {
			refBase := time.Now()
			if refUser.ExpiresAt.Valid && refUser.ExpiresAt.Time.After(refBase) {
				refBase = refUser.ExpiresAt.Time
			}
			refBonusExpiry := refBase.AddDate(0, 0, 7) // +7 days bonus
			_, _ = h.userRepo.ExtendSubscription(ctx, refID, refBonusExpiry, 0)
		}
	}

	if h.audit != nil {
		diffBytes, _ := json.Marshal(map[string]interface{}{
			"user_id":        order.UserID,
			"gateway":        gateway,
			"new_expires_at": newExpiresAt,
		})
		_ = h.audit.Log(r, "order_paid", "order", &order.ID, diffBytes)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "paid",
		"order_id":       order.ID,
		"user_id":        user.ID,
		"new_expires_at": newExpiresAt,
	})
}

type ValidatePromoRequest struct {
	Code string `json:"code" validate:"required,min=2,max=32"`
}

func (h *BillingHandler) ValidatePromo(w http.ResponseWriter, r *http.Request) {
	var req ValidatePromoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	promo, err := h.billingRepo.GetPromoCode(r.Context(), strings.TrimSpace(req.Code))
	if err != nil || !promo.IsActive.Bool {
		response.RespondNotFound(w, r, "Invalid or expired promo code")
		return
	}

	if promo.ExpiresAt.Valid && promo.ExpiresAt.Time.Before(time.Now()) {
		response.RespondNotFound(w, r, "Promo code has expired")
		return
	}

	if promo.MaxUses.Valid && promo.MaxUses.Int32 > 0 && promo.UsedCount.Int32 >= promo.MaxUses.Int32 {
		response.RespondConflict(w, r, "Promo code redemptions limit reached")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(promo)
}

func (h *BillingHandler) ListGateways(w http.ResponseWriter, r *http.Request) {
	gateways, err := h.billingRepo.ListPaymentGateways(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to list gateways")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(gateways)
}

type UpsertGatewayRequest struct {
	Name            string `json:"name" validate:"required,min=2,max=64"`
	IsEnabled       bool   `json:"is_enabled"`
	ConfigEncrypted string `json:"config,omitempty"`
}

func (h *BillingHandler) UpsertGateway(w http.ResponseWriter, r *http.Request) {
	if !h.callerCanBill(r) {
		response.RespondForbidden(w, r, "Billing configuration requires owner role or billing permission")
		return
	}
	var req UpsertGatewayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	gw, err := h.billingRepo.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            strings.TrimSpace(req.Name),
		IsEnabled:       pgtype.Bool{Bool: req.IsEnabled, Valid: true},
		ConfigEncrypted: req.ConfigEncrypted,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to save gateway")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(gw)
}

type UpdateBillingSettingsRequest struct {
	CryptobotApiToken    string `json:"cryptobot_api_token"`
	CryptobotEnabled     bool   `json:"cryptobot_enabled"`
	TelegramStarsEnabled bool   `json:"telegram_stars_enabled"`
	StarsPricePerMonth   int32  `json:"stars_price_per_month" validate:"min=1"`
	WebhookSecret        string `json:"webhook_secret"`
}

func (h *BillingHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.billingRepo.GetBillingSettings(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve billing settings")
		return
	}

	salesBotToken := ""
	if gw, err := h.billingRepo.GetPaymentGatewayByName(r.Context(), "telegram_sales_bot"); err == nil && gw.ConfigEncrypted != "" {
		var cfg map[string]string
		if err := json.Unmarshal([]byte(gw.ConfigEncrypted), &cfg); err == nil {
			salesBotToken = cfg["token"]
		}
	}

	type ExtendedSettings struct {
		store.BillingSetting
		SalesBotToken string `json:"sales_bot_token,omitempty"`
	}

	res := ExtendedSettings{
		BillingSetting: settings,
		SalesBotToken:  salesBotToken,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *BillingHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !h.callerCanBill(r) {
		response.RespondForbidden(w, r, "Billing configuration requires owner role or billing permission")
		return
	}
	var req UpdateBillingSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if req.StarsPricePerMonth <= 0 {
		req.StarsPricePerMonth = 250
	}

	settings, err := h.billingRepo.UpsertBillingSettings(r.Context(), store.UpsertBillingSettingsParams{
		CryptobotApiToken:    strings.TrimSpace(req.CryptobotApiToken),
		CryptobotEnabled:     req.CryptobotEnabled,
		TelegramStarsEnabled: req.TelegramStarsEnabled,
		StarsPricePerMonth:   req.StarsPricePerMonth,
		WebhookSecret:        strings.TrimSpace(req.WebhookSecret),
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to update billing settings")
		return
	}

	cryptoCfg, _ := json.Marshal(map[string]string{"token": settings.CryptobotApiToken})
	_, _ = h.billingRepo.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            "cryptobot",
		IsEnabled:       pgtype.Bool{Bool: settings.CryptobotEnabled, Valid: true},
		ConfigEncrypted: string(cryptoCfg),
	})
	_, _ = h.billingRepo.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            "stars",
		IsEnabled:       pgtype.Bool{Bool: settings.TelegramStarsEnabled, Valid: true},
		ConfigEncrypted: "",
	})

	if h.audit != nil {
		_ = h.audit.Log(r, "update", "billing_settings", nil, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(settings)
}

type CreateBroadcastRequest struct {
	Title         string                    `json:"title" validate:"required"`
	TargetSegment string                    `json:"target_segment" validate:"required,oneof=all active expired"`
	MessageText   string                    `json:"message_text" validate:"required"`
	Buttons       []service.BroadcastButton `json:"buttons,omitempty"`
}

func (h *BillingHandler) CreateBroadcast(w http.ResponseWriter, r *http.Request) {
	var req CreateBroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	if h.broadcastService == nil {
		response.RespondInternalError(w, r, "Broadcast service is not initialized")
		return
	}

	campaign, err := h.broadcastService.CreateAndDispatch(r.Context(), req.Title, req.TargetSegment, req.MessageText, req.Buttons)
	if err != nil {
		response.RespondInternalError(w, r, fmt.Sprintf("Failed to initiate broadcast: %v", err))
		return
	}

	if h.audit != nil {
		diffBytes, _ := json.Marshal(map[string]interface{}{
			"title":   req.Title,
			"segment": req.TargetSegment,
		})
		_ = h.audit.Log(r, "create_broadcast", "broadcast_campaign", &campaign.ID, diffBytes)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(campaign)
}

func (h *BillingHandler) ListBroadcasts(w http.ResponseWriter, r *http.Request) {
	campaigns, err := h.billingRepo.ListBroadcastCampaigns(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to list broadcast campaigns")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"campaigns": campaigns,
	})
}

func (h *BillingHandler) GetBroadcast(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid broadcast campaign ID", nil)
		return
	}
	campaign, err := h.billingRepo.GetBroadcastCampaign(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Broadcast campaign not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(campaign)
}

// GET /api/v1/billing/bot-replies
func (h *BillingHandler) GetBotReplies(w http.ResponseWriter, r *http.Request) {
	replies, err := h.billingRepo.GetBotReplies(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve bot replies")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(replies)
}
