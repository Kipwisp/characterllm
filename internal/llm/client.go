// Package llm provides a client for interacting with LLM servers (e.g. llama.cpp).
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

// Message represents a single turn in a conversation.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning_content,omitempty"`
}

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

// Client handles communication with the LLM server.
type Client struct {
	URL string
}

// NewClient creates a new LLM client with the provided server URL.
func NewClient(url string) *Client {
	return &Client{URL: url}
}

// Ping checks if the LLM server is reachable and returns the response latency.
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()

	// Properly extract the base URL to avoid path errors
	u, err := url.Parse(c.URL)
	if err != nil {
		return 0, fmt.Errorf("invalid LLM URL: %v", err)
	}

	// Construct the health check URL using the base host and /v1/models endpoint
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

// GenerateResponse sends a prompt to the LLM and returns the full response and reasoning.
func (c *Client) GenerateResponse(ctx context.Context, messages []Message, model string) (string, string, error) {
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
