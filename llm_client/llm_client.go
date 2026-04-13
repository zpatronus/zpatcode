package llm_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/zpatronus/zpatcode/config"
)

type Request struct {
	Messages []Message
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Result struct {
	LLMProviderName  string
	ModelDisplayName string
	Response         string
	Err              error
}

type Client struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Chat(ctx context.Context, req Request) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		ch <- c.doChat(ctx, req)
	}()
	return ch
}

func (c *Client) doChat(ctx context.Context, req Request) Result {
	retries := c.cfg.LLMMaxRetries
	if retries <= 0 {
		retries = 0
	}
	timeoutSec := c.cfg.LLMTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	var lastErr error

	for i := 0; i <= retries; i++ {
		select {
		case <-ctx.Done():
			return Result{Err: ctx.Err()}
		default:
		}

		providerName, providerBaseURL, modelAPIName, modelDisplayName, token, err := c.pickRandom()

		if err != nil {
			lastErr = err
			continue
		}

		result := c.tryRequest(ctx, providerName, providerBaseURL, modelAPIName, modelDisplayName, token, req.Messages, time.Duration(timeoutSec)*time.Second)

		if result.Err == nil {
			return result
		}
		lastErr = result.Err
	}

	return Result{Err: fmt.Errorf("all %d attempts failed. Last error: %w", retries+1, lastErr)}

}

func (c *Client) pickRandom() (providerName, providerBaseURL, modelAPIName, modelDisplayName, token string, err error) {
	allProviders := c.cfg.LLMProviders
	for i := 0; i < 1000; i++ {
		for providerName, provider := range allProviders {
			allModels := provider.Models
			allTokens := provider.Tokens
			if len(allModels) == 0 || len(allTokens) == 0 {
				break
			}
			for modelAPIName, modelDisplayName := range allModels {
				token := allTokens[rand.Intn(len(allTokens))]
				providerBaseURL := provider.BaseURL
				return providerName, providerBaseURL, modelAPIName, modelDisplayName, token, nil
			}
		}
	}
	err = fmt.Errorf("Cannot find a valid model")
	return
}

func (c *Client) tryRequest(ctx context.Context, providerName, providerBaseURL, modelAPIName, modelDisplayName, token string, messages []Message, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload := map[string]any{
		"model":       modelAPIName,
		"messages":    messages,
		"temperature": 0.6,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{Err: fmt.Errorf("marshal payload: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", providerBaseURL, bytes.NewBuffer(body))

	if err != nil {
		return Result{Err: fmt.Errorf("create request: %w", err)}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{Err: fmt.Errorf("request failed: %w", err)}
	}

	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{Err: fmt.Errorf("read response body: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		return Result{Err: fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))}
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return Result{Err: fmt.Errorf("decode response: %w, body: %s", err, string(bodyBytes))}
	}

	if len(result.Choices) == 0 {
		return Result{Err: fmt.Errorf("empty response from provider, body: %s", string(bodyBytes))}
	}

	content := result.Choices[0].Message.Content
	if strings.HasPrefix(content, "<think>") {
		if end := strings.Index(content, "</think>"); end != -1 {
			content = strings.TrimSpace(content[end+len("</think>"):])
		}
	}

	return Result{
		LLMProviderName:  providerName,
		ModelDisplayName: modelDisplayName,
		Response:         content,
	}
}
