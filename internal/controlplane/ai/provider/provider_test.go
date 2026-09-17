package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"https://integrate.api.nvidia.com/v1/chat/completions", "https://integrate.api.nvidia.com/v1"},
		{"https://integrate.api.nvidia.com/v1/chat/completions/", "https://integrate.api.nvidia.com/v1"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"http://localhost:11434/v1/completions", "http://localhost:11434/v1"},
		{"  https://openrouter.ai/api/v1/chat/completions  ", "https://openrouter.ai/api/v1"},
		{"", ""},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			assert.Equal(t, c.expected, NormalizeBaseURL(c.input))
		})
	}
}

func TestClient_Complete_ToolFallback(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if att == 1 {
			// First call: reject tools
			require.Contains(t, body, "tools")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"Model google/gemma-4-31b-it does not support tools or function calling"}`))
			return
		}

		// Second call: without tools
		require.NotContains(t, body, "tools")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "All nodes active.",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL: server.URL,
		Model:   "google/gemma-4-31b-it",
	})

	resp, err := client.Complete(context.Background(), []ChatMessage{
		{Role: "user", Content: "Status check"},
	}, []ToolSpec{
		{Type: "function", Function: FunctionSpec{Name: "node_health"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "All nodes active.", resp.Content)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestClient_TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "READY",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL: server.URL,
		Model:   "test-model",
	})

	duration, reply, err := client.TestPing(context.Background())
	require.NoError(t, err)
	assert.True(t, duration >= 0)
	assert.Equal(t, "READY", reply)
}
