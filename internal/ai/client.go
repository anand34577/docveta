// Package ai connects Docveta to any OpenAI-compatible language-model server (Ollama,
// llama.cpp, vLLM, LM Studio, OpenAI, ...): document classification with suggestions,
// embeddings for meaning-based search, and "Ask your documents" with cited answers.
// Every feature degrades quietly: if the AI is off or down, documents still become ready.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Provider is one configured model server.
type Provider struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	BaseURL        string     `json:"base_url"`
	ChatModel      string     `json:"chat_model"`
	EmbeddingModel string     `json:"embedding_model"`
	IsLocal        bool       `json:"is_local"`
	IsDefault      bool       `json:"is_default"`
	Enabled        bool       `json:"enabled"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	MaxConcurrency int        `json:"max_concurrency"`
	HasAPIKey      bool       `json:"has_api_key"`
	LastError      string     `json:"last_error"`
	LastOKAt       *time.Time `json:"last_ok_at"`

	apiKey string
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// breaker stops hammering a provider that keeps failing: after five failures in a row
// it refuses calls for a minute.
type breaker struct {
	mu       sync.Mutex
	failures int
	until    time.Time
}

var (
	gate      sync.Mutex
	breakers  = map[uuid.UUID]*breaker{}
	semaphore = map[uuid.UUID]chan struct{}{}
)

func state(p Provider) (*breaker, chan struct{}) {
	gate.Lock()
	defer gate.Unlock()
	b := breakers[p.ID]
	if b == nil {
		b = &breaker{}
		breakers[p.ID] = b
	}
	n := max(p.MaxConcurrency, 1)
	s := semaphore[p.ID]
	if s == nil || cap(s) != n {
		s = make(chan struct{}, n)
		semaphore[p.ID] = s
	}
	return b, s
}

// ErrUnavailable means the provider can't be used right now (circuit open).
var ErrUnavailable = errors.New("the AI server isn't responding; try again in a minute")

// Client talks to one provider.
type Client struct {
	p    Provider
	http *http.Client
}

func NewClient(p Provider) *Client {
	t := max(p.TimeoutSeconds, 5)
	return &Client{p: p, http: &http.Client{Timeout: time.Duration(t) * time.Second}}
}

func (c *Client) url(path string) string { return strings.TrimRight(c.p.BaseURL, "/") + path }

// do sends a request through the provider's concurrency limit and circuit breaker.
func (c *Client) do(ctx context.Context, method, path string, body any, stream bool) (*http.Response, error) {
	br, sem := state(c.p)
	br.mu.Lock()
	open := time.Now().Before(br.until)
	br.mu.Unlock()
	if open {
		return nil, ErrUnavailable
	}
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-sem }

	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), rd)
	if err != nil {
		release()
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.p.apiKey)
	}
	hc := c.http
	if stream {
		hc = &http.Client{} // streaming answers outlive the normal timeout; ctx still cancels them
	}
	res, err := hc.Do(req)
	record := func(ok bool) {
		br.mu.Lock()
		defer br.mu.Unlock()
		if ok {
			br.failures = 0
			return
		}
		if br.failures++; br.failures >= 5 {
			br.until = time.Now().Add(time.Minute)
			br.failures = 0
		}
	}
	if err != nil {
		release()
		record(false)
		return nil, fmt.Errorf("can't reach the AI server at %s: %w", c.p.BaseURL, err)
	}
	record(res.StatusCode < 500)
	if stream && res.StatusCode < 300 {
		res.Body = &releasingBody{ReadCloser: res.Body, release: release}
		return res, nil
	}
	// Non-streaming callers read the whole body themselves; release when they close it.
	res.Body = &releasingBody{ReadCloser: res.Body, release: release}
	return res, nil
}

type releasingBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// httpError turns a non-2xx response into a readable error.
type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	msg := e.Body
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(e.Body), &v) == nil && v.Error.Message != "" {
		msg = v.Error.Message
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return fmt.Sprintf("the AI server answered %d: %s", e.Status, strings.TrimSpace(msg))
}

func readJSON(res *http.Response, out any) error {
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return &httpError{Status: res.StatusCode, Body: string(b)}
	}
	return json.Unmarshal(b, out)
}

// Models lists the model names the server offers (also the "test connection" call).
func (c *Client) Models(ctx context.Context) ([]string, error) {
	res, err := c.do(ctx, http.MethodGet, "/models", nil, false)
	if err != nil {
		return nil, err
	}
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct { // Ollama's native shape
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := readJSON(res, &v); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range v.Data {
		out = append(out, m.ID)
	}
	for _, m := range v.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

type chatRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float64   `json:"temperature"`
	Stream         bool      `json:"stream,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
}

func (c *Client) chatOnce(ctx context.Context, msgs []Message, format any) (string, error) {
	res, err := c.do(ctx, http.MethodPost, "/chat/completions", chatRequest{Model: c.p.ChatModel, Messages: msgs, ResponseFormat: format}, false)
	if err != nil {
		return "", err
	}
	var v struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := readJSON(res, &v); err != nil {
		return "", err
	}
	if len(v.Choices) == 0 {
		return "", errors.New("the AI server returned no answer")
	}
	return v.Choices[0].Message.Content, nil
}

// ChatJSON asks for a JSON answer. It tries a strict JSON schema first, then plain JSON
// mode, then just prompting (servers differ), and makes one repair attempt if the
// reply still isn't valid JSON.
func (c *Client) ChatJSON(ctx context.Context, msgs []Message, schema map[string]any) (json.RawMessage, error) {
	formats := []any{
		map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "result", "strict": true, "schema": schema}},
		map[string]any{"type": "json_object"},
		nil,
	}
	var lastErr error
	for _, f := range formats {
		out, err := c.chatOnce(ctx, msgs, f)
		var he *httpError
		if errors.As(err, &he) && he.Status >= 400 && he.Status < 500 && f != nil {
			lastErr = err // this server doesn't support that response format: try the next
			continue
		}
		if err != nil {
			return nil, err
		}
		if j := extractJSON(out); j != nil {
			return j, nil
		}
		// One repair attempt.
		repair := append(append([]Message{}, msgs...), Message{Role: "assistant", Content: out},
			Message{Role: "user", Content: "That wasn't valid JSON. Reply again with only the JSON object, nothing else."})
		out, err = c.chatOnce(ctx, repair, f)
		if err != nil {
			return nil, err
		}
		if j := extractJSON(out); j != nil {
			return j, nil
		}
		return nil, errors.New("the AI didn't return valid JSON")
	}
	return nil, lastErr
}

// extractJSON finds the first JSON object in a model reply (models like to add prose or
// code fences around it).
func extractJSON(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(s[i:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == nil {
			return raw
		}
	}
	return nil
}

// ChatStream streams an answer, calling onDelta with each piece of text.
func (c *Client) ChatStream(ctx context.Context, msgs []Message, onDelta func(string)) error {
	res, err := c.do(ctx, http.MethodPost, "/chat/completions", chatRequest{Model: c.p.ChatModel, Messages: msgs, Stream: true}, true)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &httpError{Status: res.StatusCode, Body: string(b)}
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return nil
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &v) == nil && len(v.Choices) > 0 && v.Choices[0].Delta.Content != "" {
			onDelta(v.Choices[0].Delta.Content)
		}
	}
	return sc.Err()
}

// Embed returns one vector per input, in order. Inputs are sent in small batches.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if c.p.EmbeddingModel == "" {
		return nil, errors.New("no embedding model is set for this AI provider")
	}
	out := make([][]float32, 0, len(inputs))
	for i := 0; i < len(inputs); i += 16 {
		batch := inputs[i:min(i+16, len(inputs))]
		res, err := c.do(ctx, http.MethodPost, "/embeddings", map[string]any{"model": c.p.EmbeddingModel, "input": batch}, false)
		if err != nil {
			return nil, err
		}
		var v struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := readJSON(res, &v); err != nil {
			return nil, err
		}
		if len(v.Data) != len(batch) {
			return nil, errors.New("the AI server returned the wrong number of embeddings")
		}
		part := make([][]float32, len(batch))
		for _, d := range v.Data {
			if d.Index < 0 || d.Index >= len(batch) {
				return nil, errors.New("bad embedding index from the AI server")
			}
			part[d.Index] = d.Embedding
		}
		out = append(out, part...)
	}
	return out, nil
}
