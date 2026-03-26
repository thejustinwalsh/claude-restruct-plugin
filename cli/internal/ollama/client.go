package ollama

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ollama/ollama/api"
)

// Client wraps the official Ollama Go SDK.
type Client struct {
	api       *api.Client
	Model     string
	KeepAlive time.Duration
}

func NewClient(baseURL, model string, timeout, keepAlive time.Duration) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ollama url: %w", err)
	}
	c := api.NewClient(u, &http.Client{Timeout: timeout})
	return &Client{api: c, Model: model, KeepAlive: keepAlive}, nil
}

// Chat sends a non-streaming chat request and returns the response content.
func (c *Client) Chat(ctx context.Context, system, user string, temperature float32, maxTokens int) (string, error) {
	ka := api.Duration{Duration: c.KeepAlive}
	req := &api.ChatRequest{
		Model: c.Model,
		Messages: []api.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Options: map[string]any{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
		KeepAlive: &ka,
	}

	var result string
	err := c.api.Chat(ctx, req, func(resp api.ChatResponse) error {
		result += resp.Message.Content
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("ollama chat: %w", err)
	}
	return result, nil
}

// IsAvailable checks if the Ollama server is reachable.
func (c *Client) IsAvailable(ctx context.Context) bool {
	err := c.api.Heartbeat(ctx)
	return err == nil
}

// Version returns the Ollama server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	return c.api.Version(ctx)
}

// ListModels returns all locally available models.
func (c *Client) ListModels(ctx context.Context) ([]api.ListModelResponse, error) {
	resp, err := c.api.List(ctx)
	if err != nil {
		return nil, err
	}
	return resp.Models, nil
}

// PullModel pulls a model by name, calling progress on each update.
func (c *Client) PullModel(ctx context.Context, model string, progress func(api.ProgressResponse)) error {
	req := &api.PullRequest{Model: model}
	return c.api.Pull(ctx, req, func(resp api.ProgressResponse) error {
		progress(resp)
		return nil
	})
}

// LoadModel sends a minimal request with keep_alive to preload the model into memory.
func (c *Client) LoadModel(ctx context.Context, model string, keepAlive time.Duration) error {
	ka := api.Duration{Duration: keepAlive}
	req := &api.ChatRequest{
		Model:     model,
		Messages:  []api.Message{{Role: "user", Content: "hi"}},
		KeepAlive: &ka,
	}
	return c.api.Chat(ctx, req, func(resp api.ChatResponse) error { return nil })
}
