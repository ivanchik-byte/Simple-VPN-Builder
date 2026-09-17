package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage represents a single message in the LLM conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool invocation request from the LLM.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the name and serialized arguments of the function.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec defines a tool available for LLM function calling.
type ToolSpec struct {
	Type     string         `json:"type"`
	Function FunctionSpec   `json:"function"`
}

// FunctionSpec contains the schema description of the function.
type FunctionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatResponse holds the response returned by the LLM.
type ChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
}

// Config defines LLM provider configuration.
type Config struct {
	BaseURL string `json:"base_url"` // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`    // e.g. gpt-4o, gpt-4o-mini, qwen2.5:14b, llama3.1
	Timeout time.Duration
}

// Client interacts with any OpenAI-compatible LLM endpoint (OpenAI, Ollama, vLLM, OpenRouter).
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NormalizeBaseURL cleans up LLM base URLs by removing redundant completion suffixes and trailing slashes.
func NormalizeBaseURL(urlStr string) string {
	u := strings.TrimSpace(urlStr)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/chat/completions")
	u = strings.TrimSuffix(u, "/completions")
	return strings.TrimRight(u, "/")
}

// NewClient creates an LLM client with endpoint configuration.
func NewClient(cfg Config) *Client {
	baseURL := NormalizeBaseURL(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		cfg: Config{
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(cfg.APIKey),
			Model:   model,
			Timeout: timeout,
		},
		httpClient: &http.Client{Timeout: timeout},
	}
}

// FormatAPIError extracts a clean readable error message from provider error responses.
func FormatAPIError(statusCode int, body []byte) error {
	var errObj struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		Detail interface{} `json:"detail"`
		Msg    string      `json:"message"`
	}

	trimmed := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &errObj) == nil {
		if errObj.Error.Message != "" {
			return fmt.Errorf("LLM API error (HTTP %d): %s", statusCode, errObj.Error.Message)
		}
		if errObj.Msg != "" {
			return fmt.Errorf("LLM API error (HTTP %d): %s", statusCode, errObj.Msg)
		}
		if errObj.Detail != nil {
			return fmt.Errorf("LLM API error (HTTP %d): %v", statusCode, errObj.Detail)
		}
	}

	if len(trimmed) > 300 {
		trimmed = trimmed[:300] + "..."
	}
	if trimmed == "" {
		trimmed = http.StatusText(statusCode)
	}
	return fmt.Errorf("LLM API error (HTTP %d): %s", statusCode, trimmed)
}

// Complete sends conversation messages and tool definitions to the LLM.
func (c *Client) Complete(ctx context.Context, messages []ChatMessage, tools []ToolSpec) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    c.cfg.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal LLM request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/chat/completions", c.cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.cfg.APIKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Fallback: If HTTP 400 occurred and tools were included, retry once without tools
		// This supports models (like Gemma on NVIDIA NIM or Ollama) that do not support tool calling.
		if resp.StatusCode == http.StatusBadRequest && len(tools) > 0 {
			lowerBody := strings.ToLower(string(bodyBytes))
			if strings.Contains(lowerBody, "tool") ||
				strings.Contains(lowerBody, "function") ||
				strings.Contains(lowerBody, "support") ||
				strings.Contains(lowerBody, "extra") ||
				strings.Contains(lowerBody, "schema") {
				return c.Complete(ctx, messages, nil)
			}
		}

		return nil, FormatAPIError(resp.StatusCode, bodyBytes)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response JSON: %w (raw: %s)", err, string(bodyBytes))
	}

	if len(parsed.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}

	first := parsed.Choices[0]
	return &ChatResponse{
		Content:      first.Message.Content,
		ToolCalls:    first.Message.ToolCalls,
		FinishReason: first.FinishReason,
	}, nil
}

// TestPing sends a lightweight ping prompt to verify connectivity, auth, and model availability.
func (c *Client) TestPing(ctx context.Context) (time.Duration, string, error) {
	start := time.Now()
	pingMessages := []ChatMessage{
		{
			Role:    "user",
			Content: "Respond with the single word 'READY'.",
		},
	}

	resp, err := c.Complete(ctx, pingMessages, nil)
	duration := time.Since(start)
	if err != nil {
		return duration, "", err
	}

	reply := strings.TrimSpace(resp.Content)
	if len(reply) > 80 {
		reply = reply[:80] + "..."
	}
	return duration, reply, nil
}

