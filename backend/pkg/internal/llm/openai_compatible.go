package llm

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

const defaultRequestTimeout = 5 * time.Minute

// Message is the minimal chat message shape used by OpenAI-compatible APIs.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client calls an OpenAI-compatible /chat/completions endpoint.
type Client struct {
	APIKey         string
	BaseURL        string
	Model          string
	RequestTimeout time.Duration
	HTTPClient     *http.Client
}

func NewClient(apiKey, baseURL, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}

	return &Client{
		APIKey:         strings.TrimSpace(apiKey),
		BaseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:          strings.TrimSpace(model),
		RequestTimeout: timeout,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) ChatCompletionsURL() string { //appends client api url
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
}

// StreamChat sends messages to the configured model and calls onDelta for each
// streamed content delta. The request intentionally uses the smallest portable
// OpenAI-compatible payload and avoids provider-specific fields.
// first validate client then
func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) error {
	return c.streamChat(ctx, messages, nil, onDelta)
}

func (c *Client) streamChat(ctx context.Context, messages []Message, temperature *float64, onDelta func(string) error) error {
	if err := c.validate(); err != nil {
		return err
	}
	if onDelta == nil {
		return fmt.Errorf("onDelta callback is required")
	}

	ctx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()

	payload := chatCompletionRequest{
		Model:       c.Model,
		Messages:    messages,
		Stream:      true,
		Temperature: temperature,
	}

	body, err := json.Marshal(payload) //returns json
	if err != nil {
		return fmt.Errorf("marshal chat completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ChatCompletionsURL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create chat completion request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	} else {
		return fmt.Errorf("[StreamChat] no API key set ")
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: c.RequestTimeout}
	}

	resp, err := httpClient.Do(req) // <--- sends req here
	if err != nil {
		return fmt.Errorf("send chat completion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("chat completion failed: %s: %s", resp.Status, strings.TrimSpace(string(errorBody)))
	}

	return ReadContentDeltas(resp.Body, onDelta)
}

// Chat sends messages and returns the complete (non-streamed) response text.
// Used for the validator, which needs a single JSON answer rather than deltas.
// It runs at temperature 0: verdicts on identical input should be identical.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	var builder strings.Builder
	zero := 0.0
	err := c.streamChat(ctx, messages, &zero, func(delta string) error {
		builder.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return builder.String(), nil
}

func (c *Client) validate() error { // client field null checks
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be positive")
	}
	return nil
}

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature,omitempty"`
}
