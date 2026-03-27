package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

// TokenSink receives streaming tokens as they arrive.
// Implementations can forward to SSE hubs, loggers, etc.
type TokenSink interface {
	OnToken(content string)
	OnDone()
	OnError(err error)
}

// StreamChunk represents a single chunk from a streaming response.
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// Client wraps the official Ollama Go SDK with production-grade
// timeouts, retry, model pre-checking, and streaming support.
type Client struct {
	api            *api.Client
	baseURL        string
	Model          string
	KeepAlive      time.Duration
	ConnectTimeout time.Duration // fast check: is Ollama reachable? (default 5s)
	RequestTimeout time.Duration // generous: total time for LLM response (default 120s)
	StallTimeout   time.Duration // no tokens for this long = abort (default 30s)
	httpClient     *http.Client  // for streaming (raw HTTP)
}

func NewClient(baseURL, model string, connectTimeout, requestTimeout, stallTimeout, keepAlive time.Duration) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ollama url: %w", err)
	}

	if connectTimeout == 0 {
		connectTimeout = 5 * time.Second
	}
	if requestTimeout == 0 {
		requestTimeout = 120 * time.Second
	}
	if stallTimeout == 0 {
		stallTimeout = 30 * time.Second
	}

	// SDK client uses connect timeout for quick operations (heartbeat, list, version)
	sdkClient := api.NewClient(u, &http.Client{Timeout: connectTimeout})

	// Raw HTTP client for streaming (uses request timeout as overall limit)
	rawClient := &http.Client{Timeout: requestTimeout}

	return &Client{
		api:            sdkClient,
		baseURL:        baseURL,
		Model:          model,
		KeepAlive:      keepAlive,
		ConnectTimeout: connectTimeout,
		RequestTimeout: requestTimeout,
		StallTimeout:   stallTimeout,
		httpClient:     rawClient,
	}, nil
}

// IsAvailable checks if the Ollama server is reachable (fast, uses connect timeout).
func (c *Client) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, c.ConnectTimeout)
	defer cancel()
	return c.api.Heartbeat(ctx) == nil
}

// Version returns the Ollama server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.ConnectTimeout)
	defer cancel()
	return c.api.Version(ctx)
}

// ListModels returns all locally available models.
func (c *Client) ListModels(ctx context.Context) ([]api.ListModelResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.ConnectTimeout)
	defer cancel()
	resp, err := c.api.List(ctx)
	if err != nil {
		return nil, err
	}
	return resp.Models, nil
}

// EnsureModel verifies the model is available and attempts to load it if not.
func (c *Client) EnsureModel(ctx context.Context) error {
	models, err := c.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	found := false
	for _, m := range models {
		if matchesModel(m.Name, c.Model) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("model %q not pulled — run: ollama pull %s", c.Model, c.Model)
	}

	// Attempt to load (warm) the model if not already loaded
	slog.Debug("ensuring model is loaded", "model", c.Model)
	return c.LoadModel(ctx, c.Model, c.KeepAlive)
}

// Chat sends a non-streaming chat request and returns the response content.
// Kept for simple use cases (doctor, testing). Pipeline uses ChatStream.
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

// chatStreamResponse is the NDJSON structure Ollama sends for streaming.
type chatStreamResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done      bool  `json:"done"`
	DoneError string `json:"error,omitempty"`
}

// ChatStream sends a streaming chat request, returning chunks via channel.
// Supports stall detection: if no tokens arrive within StallTimeout, the
// request is cancelled and an error chunk is sent.
func (c *Client) ChatStream(ctx context.Context, system, user string, temperature float32, maxTokens int, sink TokenSink) (string, error) {
	ka := api.Duration{Duration: c.KeepAlive}
	body := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"options": map[string]any{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
		"keep_alive": ka.Duration.String(),
		"stream":     true,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	reqURL := strings.TrimRight(c.baseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s: %s", resp.Status, string(bodyBytes))
	}

	var assembled strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	stallTimer := time.NewTimer(c.StallTimeout)
	defer stallTimer.Stop()

	// Read NDJSON lines with stall detection
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
		close(lineCh)
	}()

	for {
		select {
		case <-ctx.Done():
			if sink != nil {
				sink.OnError(ctx.Err())
			}
			return assembled.String(), ctx.Err()

		case <-stallTimer.C:
			stallErr := fmt.Errorf("ollama stalled: no tokens received for %s", c.StallTimeout)
			slog.Warn("ollama stall detected", "timeout", c.StallTimeout)
			if sink != nil {
				sink.OnError(stallErr)
			}
			// Return what we have if anything
			if assembled.Len() > 0 {
				return assembled.String(), stallErr
			}
			return "", stallErr

		case err := <-errCh:
			if sink != nil {
				sink.OnError(err)
			}
			return assembled.String(), fmt.Errorf("read stream: %w", err)

		case line, ok := <-lineCh:
			if !ok {
				// Stream ended without done:true
				if sink != nil {
					sink.OnDone()
				}
				return assembled.String(), nil
			}

			if line == "" {
				continue
			}

			var chunk chatStreamResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				slog.Debug("skipping unparseable stream line", "line", line)
				continue
			}

			if chunk.DoneError != "" {
				streamErr := fmt.Errorf("ollama error: %s", chunk.DoneError)
				if sink != nil {
					sink.OnError(streamErr)
				}
				return assembled.String(), streamErr
			}

			if chunk.Message.Content != "" {
				assembled.WriteString(chunk.Message.Content)
				if sink != nil {
					sink.OnToken(chunk.Message.Content)
				}
				// Reset stall timer on every token
				stallTimer.Reset(c.StallTimeout)
			}

			if chunk.Done {
				if sink != nil {
					sink.OnDone()
				}
				return assembled.String(), nil
			}
		}
	}
}

// ChatWithRetry wraps ChatStream with a single retry on transient errors.
func (c *Client) ChatWithRetry(ctx context.Context, system, user string, temperature float32, maxTokens int, sink TokenSink) (string, error) {
	result, err := c.ChatStream(ctx, system, user, temperature, maxTokens, sink)
	if err == nil {
		return result, nil
	}

	// Only retry on transient errors (connection issues, 503)
	if !isTransient(err) {
		return result, err
	}

	slog.Info("retrying after transient error", "error", err)
	time.Sleep(500 * time.Millisecond)
	return c.ChatStream(ctx, system, user, temperature, maxTokens, sink)
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

func matchesModel(installed, required string) bool {
	return installed == required || strings.HasPrefix(installed, required)
}

func isTransient(err error) bool {
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "timeout")
}
