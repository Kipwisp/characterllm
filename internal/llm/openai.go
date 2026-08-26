package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"characterllm/internal/logger"
)

// tokensPerImage is the rough token cost of a single attached image. Images
// are downscaled to a 512px box before sending, which lands in the low
// hundreds of tokens for common vision models.
const tokensPerImage = 500

// llamaMessage is the OpenAI JSON form of a single turn in LlamaRequest/LlamaResponse. Its
// content is either a plain JSON string or an array of content parts (a text
// part plus one image_url part per attached image).
type llamaMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Reasoning string          `json:"reasoning_content,omitempty"`
}

type imagePart struct {
	URL string `json:"url"`
}

type contentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL *imagePart `json:"image_url,omitempty"`
}

func toLlamaMessages(messages []Message) ([]llamaMessage, error) {
	out := make([]llamaMessage, 0, len(messages))
	for _, m := range messages {
		wm := llamaMessage{Role: m.Role, Reasoning: m.Reasoning}

		var payload any = m.Content
		if len(m.Images) > 0 {
			parts := make([]contentPart, 0, len(m.Images)+1)
			parts = append(parts, contentPart{Type: "text", Text: m.Content})
			for _, u := range m.Images {
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imagePart{URL: u}})
			}
			payload = parts
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		wm.Content = data
		out = append(out, wm)
	}
	return out, nil
}

// text extracts the readable text from a llamaMessage's content, accepting
// both the plain string form and the content-parts array (text parts
// concatenated).
func (w llamaMessage) text() string {
	var s string
	if err := json.Unmarshal(w.Content, &s); err == nil {
		return s
	}
	var parts []contentPart
	if err := json.Unmarshal(w.Content, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// LlamaRequest is the payload sent to the LLM server for a completion request.
type LlamaRequest struct {
	Messages []llamaMessage `json:"messages"`
	Model    string        `json:"model"`
}

// LlamaResponse is the JSON response received from a LLM request.
type LlamaResponse struct {
	Choices []llamaChoice `json:"choices"`
}

type llamaChoice struct {
	Message llamaMessage `json:"message"`
}

// OpenAIClient handles communication with an OpenAI-compatible LLM server (e.g., llama.cpp).
type OpenAIClient struct {
	URL                   string
	client                *http.Client
	tokenizationSupported bool
	tokenizationTested    bool
}

// NewClient creates a new LLM client instance. Currently returns an OpenAIClient.
// The timeout bounds every request so a stalled server surfaces as an error
// instead of blocking the caller indefinitely.
func NewClient(url string, timeout time.Duration) LLMClient {
	return &OpenAIClient{URL: url, client: &http.Client{Timeout: timeout}}
}

// Ping checks if the LLM server is reachable and returns the response latency.
func (c *OpenAIClient) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()

	u, err := url.Parse(c.URL)
	if err != nil {
		return 0, fmt.Errorf("invalid LLM URL: %v", err)
	}

	pingURL := fmt.Sprintf("%s://%s/v1/models", u.Scheme, u.Host)

	req, err := http.NewRequestWithContext(ctx, "GET", pingURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	return time.Since(start), nil
}

// EstimateTokens calculates the approximate number of tokens in a list of messages.
func (c *OpenAIClient) EstimateTokens(ctx context.Context, messages []Message) int {
	if !c.tokenizationTested && !c.tokenizationSupported {
		// Try to verify if tokenization is supported
		if c.verifyTokenizationSupport(ctx) {
			c.tokenizationSupported = true
		} else {
			c.tokenizationSupported = false
		}
		c.tokenizationTested = true
	}

	if c.tokenizationSupported {
		tokens, err := c.fetchRemoteTokens(ctx, messages)
		if err == nil {
			return tokens
		}
		logger.FromContext(ctx).Debug("remote tokenization failed, falling back to approximation", "error", err)
	}

	// Fallback: Heuristic approximation (roughly 4 characters per token)
	var totalChars int
	for _, msg := range messages {
		totalChars += len(msg.Content)
		if msg.Reasoning != "" {
			totalChars += len(msg.Reasoning)
		}
	}
	return totalChars/4 + tokensPerImage*countImages(messages)
}

func countImages(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Images)
	}
	return total
}

func (c *OpenAIClient) verifyTokenizationSupport(ctx context.Context) bool {
	messages := []Message{{Role: "user", Content: "test"}}
	tokens, err := c.fetchRemoteTokens(ctx, messages)
	return err == nil && tokens > 0
}

func (c *OpenAIClient) fetchRemoteTokens(ctx context.Context, messages []Message) (int, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return 0, err
	}
	tokenURL := fmt.Sprintf("%s://%s/v1/tokenize", u.Scheme, u.Host)

	llamaMessages, err := toLlamaMessages(messages)
	if err != nil {
		return 0, err
	}
	reqBody := LlamaRequest{
		Messages: llamaMessages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var tokenResp struct {
		Tokens int `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return 0, err
	}

	return tokenResp.Tokens, nil
}

// GenerateResponse sends a prompt to the LLM and returns the full response and reasoning.
func (c *OpenAIClient) GenerateResponse(ctx context.Context, messages []Message, model string) (string, string, error) {
	llamaMessages, err := toLlamaMessages(messages)
	if err != nil {
		return "", "", err
	}
	reqBody := LlamaRequest{
		Model:    model,
		Messages: llamaMessages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	const maxRetries = 3
	backoff := 1 * time.Second
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		content, reasoning, retryable, err := c.doGenerate(ctx, jsonData, attempt, maxRetries)
		if err == nil {
			return content, reasoning, nil
		}
		lastErr = err
		if !retryable || attempt == maxRetries {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}

	return "", "", fmt.Errorf("all %d retries failed: %v", maxRetries, lastErr)
}

// doGenerate performs a single LLM request. The boolean result indicates whether
// retrying is worthwhile (transport errors, 5xx, 429); other 4xx errors are not.
func (c *OpenAIClient) doGenerate(ctx context.Context, jsonData []byte, attempt, maxRetries int) (string, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", false, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		logger.FromContext(ctx).Debug("LLM request attempt failed", "attempt", attempt, "error", err)
		return "", "", true, fmt.Errorf("request failed (attempt %d/%d): %v", attempt, maxRetries, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return "", "", retryable, fmt.Errorf("server returned non-OK status: %d, body: %s (attempt %d/%d)", resp.StatusCode, string(body), attempt, maxRetries)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", true, fmt.Errorf("failed to read response body: %v", err)
	}

	var llamaResp LlamaResponse
	if err := json.Unmarshal(body, &llamaResp); err != nil {
		return "", "", true, fmt.Errorf("failed to unmarshal JSON: %v", err)
	}

	if len(llamaResp.Choices) > 0 {
		choice := llamaResp.Choices[0].Message
		return choice.text(), choice.Reasoning, false, nil
	}

	return "", "", true, fmt.Errorf("no response from llama.cpp (attempt %d/%d)", attempt, maxRetries)
}
