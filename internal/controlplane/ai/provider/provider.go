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

// NewClient creates an LLM client with endpoint configuration.
func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := cfg.Model
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
			APIKey:  cfg.APIKey,
			Model:   model,
			Timeout: timeout,
		},
		httpClient: &http.Client{Timeout: timeout},
	}
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
		return nil, fmt.Errorf("LLM endpoint request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LLM API error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
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
		return nil, fmt.Errorf("failed to parse LLM response JSON: %w", err)
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
