package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCryptoBotProvider_CreateInvoice_Success(t *testing.T) {
	expectedToken := "test-crypto-token"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/createInvoice", r.URL.Path)
		assert.Equal(t, expectedToken, r.Header.Get("Crypto-Pay-API-Token"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)
		assert.Equal(t, "USDT", reqBody["asset"])
		assert.Equal(t, "10.00", reqBody["amount"])
		assert.Equal(t, "order-uuid-123", reqBody["payload"])

		resp := cryptoBotAPIResponse{
			OK: true,
			Result: &CryptoBotInvoice{
				InvoiceID:     98765,
				Status:        "active",
				PayURL:        "https://t.me/CryptoBot?start=IV12345",
				BotInvoiceURL: "https://t.me/CryptoBot?startapp=invoice_98765",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider := NewCryptoBotProviderWithClient(expectedToken, mockServer.URL, mockServer.Client())

	ctx := context.Background()
	inv, err := provider.CreateInvoice(ctx, CryptoBotCreateInvoiceParams{
		Amount:      "10.00",
		Asset:       "USDT",
		Description: "VPN 1 month",
		Payload:     "order-uuid-123",
	})

	require.NoError(t, err)
	require.NotNil(t, inv)
	assert.Equal(t, int64(98765), inv.InvoiceID)
	assert.Equal(t, "active", inv.Status)
	assert.Equal(t, "https://t.me/CryptoBot?start=IV12345", inv.PayURL)
	assert.Equal(t, "https://t.me/CryptoBot?startapp=invoice_98765", inv.BotInvoiceURL)
}

func TestCryptoBotProvider_CreateInvoice_APIError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cryptoBotAPIResponse{
			OK: false,
			Error: &cryptoBotError{
				Code: 400,
				Name: "AMOUNT_TOO_SMALL",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider := NewCryptoBotProviderWithClient("test-token", mockServer.URL, mockServer.Client())

	ctx := context.Background()
	inv, err := provider.CreateInvoice(ctx, CryptoBotCreateInvoiceParams{
		Amount:  "0.01",
		Payload: "order-1",
	})

	require.Error(t, err)
	assert.Nil(t, inv)
	assert.Contains(t, err.Error(), "AMOUNT_TOO_SMALL")
}

func TestCryptoBotProvider_CreateInvoice_FallbackWithoutToken(t *testing.T) {
	provider := NewCryptoBotProvider("")

	ctx := context.Background()
	inv, err := provider.CreateInvoice(ctx, CryptoBotCreateInvoiceParams{
		Amount:  "5.00",
		Payload: "local-order-id",
	})

	require.NoError(t, err)
	require.NotNil(t, inv)
	assert.Equal(t, "https://t.me/CryptoBot?start=local-order-id", inv.PayURL)
}
