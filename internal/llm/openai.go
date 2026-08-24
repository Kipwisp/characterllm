package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"characterllm/internal/logger"
)

// LlamaRequest is the payload sent to the LLM server for a completion request.
type LlamaRequest struct {
	Messages []Message `json:"messages"`
	Model    string    `json:"model"`
}

// LlamaResponse is the JSON response received from a LLM request.
type LlamaResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// OpenAIClient handles communication with an OpenAI-compatible LLM server (e.g., llama.cpp).
type OpenAIClient struct {
	URL                   string
	tokenizationSupported bool
	tokenizationTested    bool
}

// NewClient creates a new LLM client instance. Currently returns an OpenAIClient.
func NewClient(url string) LLMClient {
	return &OpenAIClient{URL: url}
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

	resp, err := http.DefaultClient.Do(req)
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
	return totalChars / 4
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

	reqBody := LlamaRequest{
		Messages: messages,
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

	resp, err := http.DefaultClient.Do(req)
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
	reqBody := LlamaRequest{
		Model:    model,
		Messages: messages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	var lastErr error
	maxRetries := 3
	backoff := 1 * time.Second

	for i := range maxRetries {
		req, err := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", "", fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %v", i+1, maxRetries, err)
			logger.FromContext(ctx).Debug("LLM request attempt failed", "attempt", i+1, "error", err)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server returned non-OK status: %d, body: %s (attempt %d/%d)", resp.StatusCode, string(body), i+1, maxRetries)

			// Only retry on 5xx errors (server errors) or 429 (too many requests)
			if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			// For 4xx errors (other than 429), retrying usually won't help
			return "", "", lastErr
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %v", err)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		var llamaResp LlamaResponse
		if err := json.Unmarshal(body, &llamaResp); err != nil {
			lastErr = fmt.Errorf("failed to unmarshal JSON: %v", err)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		if len(llamaResp.Choices) > 0 {
			choice := llamaResp.Choices[0].Message
			return choice.Content, choice.Reasoning, nil
		}

		lastErr = fmt.Errorf("no response from llama.cpp (attempt %d/%d)", i+1, maxRetries)
		time.Sleep(backoff)
		backoff *= 2
	}

	return "", "", fmt.Errorf("all %d retries failed: %v", maxRetries, lastErr)
}
