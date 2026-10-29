// Package inference implements the local Ollama API. It never routes inference
// to a cloud model or an endpoint outside loopback.
package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrIncompleteStream = errors.New("Ollama stream ended without a completion marker")
var ErrRemoteModel = errors.New("cloud inference is disabled; select an installed local model")
var ErrTextRequestTooLarge = errors.New("Ollama text request exceeds the 512 KiB host limit; compact context or reduce attached text")

const maxResponseBytes = 16 << 20
const maxStreamLine = 8 << 20
const maxResidencyDuration = 5 * time.Minute

// MaxTextRequestBytes bounds serialized chat text, tool schemas and options,
// excluding image payloads. It is a resource limit, not a token estimate.
const MaxTextRequestBytes = 512 << 10

type Client struct {
	base          string
	http          *http.Client
	inferenceHTTP *http.Client
	admission     chan struct{}
}

// NewClient requires an explicit loopback origin. Proxy environment variables
// and redirects cannot turn a local request into remote traffic.
func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("Ollama URL must use http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Ollama URL must be an origin without credentials, path, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("Ollama URL must address localhost or a loopback IP")
	}
	if u.Port() != "" {
		u.Host = net.JoinHostPort(host, u.Port())
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Path = ""
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 4}
	// Ollama loads the runner and may prefill before sending response headers.
	// Inference/download operations use their caller's context deadline; metadata
	// retains both its header deadline and the total deadline in json.
	inferenceTransport := transport.Clone()
	inferenceTransport.ResponseHeaderTimeout = 0
	rejectRedirect := func(*http.Request, []*http.Request) error { return errors.New("Ollama redirects are disabled") }
	return &Client{base: u.String(), http: &http.Client{Transport: transport, CheckRedirect: rejectRedirect}, inferenceHTTP: &http.Client{Transport: inferenceTransport, CheckRedirect: rejectRedirect}, admission: make(chan struct{}, 1)}, nil
}

func (c *Client) BaseURL() string { return c.base }

func (c *Client) request(ctx context.Context, client *http.Client, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		return nil, fmt.Errorf("Ollama HTTP %d: %s", resp.StatusCode, e.Error)
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, method, path string, payload, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.request(ctx, c.http, method, path, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxResponseBytes {
		return errors.New("Ollama response exceeds size limit")
	}
	if len(bytes.TrimSpace(b)) == 0 && out == nil {
		return nil
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &failure) == nil && failure.Error != "" {
		return errors.New(failure.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode Ollama response: %w", err)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := c.json(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var r struct {
		Models []Model `json:"models"`
	}
	err := c.json(ctx, http.MethodGet, "/api/tags", nil, &r)
	if r.Models == nil {
		r.Models = []Model{}
	}
	return r.Models, err
}
func (c *Client) Running(ctx context.Context) ([]Model, error) {
	var r struct {
		Models []Model `json:"models"`
	}
	err := c.json(ctx, http.MethodGet, "/api/ps", nil, &r)
	if r.Models == nil {
		r.Models = []Model{}
	}
	return r.Models, err
}
func (c *Client) Show(ctx context.Context, name string) (ModelInfo, error) {
	var raw json.RawMessage
	if err := validName(name); err != nil {
		return ModelInfo{}, err
	}
	err := c.json(ctx, http.MethodPost, "/api/show", map[string]any{"model": name}, &raw)
	if err != nil {
		return ModelInfo{}, err
	}
	var r ModelInfo
	err = json.Unmarshal(raw, &r)
	_ = json.Unmarshal(raw, &r.Raw)
	return r, err
}

func validName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 512 || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("invalid model name")
	}
	return nil
}
func IsRemoteName(name string) bool {
	s := strings.ToLower(name)
	return strings.HasSuffix(s, ":cloud") || strings.Contains(s, "-cloud") || strings.HasPrefix(s, "cloud/")
}
func (c *Client) checkLocal(ctx context.Context, name string) error {
	_, err := c.localInfo(ctx, name)
	return err
}

func (c *Client) localInfo(ctx context.Context, name string) (ModelInfo, error) {
	if IsRemoteName(name) {
		return ModelInfo{}, ErrRemoteModel
	}
	info, err := c.Show(ctx, name)
	if err != nil {
		return ModelInfo{}, err
	}
	if info.RemoteHost != "" || info.RemoteModel != "" {
		return ModelInfo{}, ErrRemoteModel
	}
	return info, nil
}

// Chat permits a single inference at a time across all runs sharing this client.
// Holding this slot only around inference avoids parent/child workflow deadlocks.
// Optional receipt observers receive terminal envelope metadata before message
// decoding/validation. They never receive content or tool authority.
func (c *Client) Chat(ctx context.Context, req ChatRequest, emit func(ChatChunk) error, observeReceipt ...func(ChatChunk)) error {
	if err := validName(req.Model); err != nil {
		return err
	}
	req.Stream = true
	req.Truncate, req.Shift = false, false
	if err := validateTextRequestSize(req); err != nil {
		return err
	}
	select {
	case c.admission <- struct{}{}:
		defer func() { <-c.admission }()
	case <-ctx.Done():
		return ctx.Err()
	}
	info, err := c.localInfo(ctx, req.Model)
	if err != nil {
		return err
	}
	if len(info.Capabilities) > 0 {
		if err := ValidateCapabilities(info, "completion"); err != nil {
			return err
		}
	}
	// Host compaction owns history changes. Never permit a caller to opt into
	// silent server truncation or shifting. These flags are supported by the
	// qualified Ollama v0.32.3 API; other versions/runners need qualification.
	resp, err := c.request(ctx, c.inferenceHTTP, http.MethodPost, "/api/chat", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	done := false
	err = scanJSON(ctx, resp.Body, func(line []byte) error {
		if done {
			return errors.New("Ollama sent data after completion")
		}
		var envelope struct {
			Done       bool   `json:"done"`
			DoneReason string `json:"done_reason"`
			Metrics
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return fmt.Errorf("decode chat envelope: %w", err)
		}
		var counts struct {
			Prompt *int `json:"prompt_eval_count"`
			Output *int `json:"eval_count"`
		}
		if err := json.Unmarshal(line, &counts); err != nil {
			return fmt.Errorf("decode token counts: %w", err)
		}
		countsPresent := counts.Prompt != nil && counts.Output != nil && *counts.Prompt >= 0 && *counts.Output >= 0
		if envelope.Done {
			for _, observe := range observeReceipt {
				if observe != nil {
					observe(ChatChunk{Done: true, DoneReason: envelope.DoneReason, Metrics: envelope.Metrics, TokenCountsPresent: countsPresent})
				}
			}
		}
		var chunk ChatChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("decode chat chunk: %w", err)
		}
		chunk.TokenCountsPresent = countsPresent
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		if emit != nil {
			if err := emit(chunk); err != nil {
				return err
			}
		}
		done = chunk.Done
		return nil
	})
	if err != nil {
		return err
	}
	if !done {
		return ErrIncompleteStream
	}
	return nil
}

func validateTextRequestSize(req ChatRequest) error {
	// Detach message headers so measuring text cannot remove images from the
	// actual request or caller-owned history. All other serialized fields count.
	req.Messages = append([]Message(nil), req.Messages...)
	for i := range req.Messages {
		req.Messages[i].Images = nil
	}
	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode Ollama text request: %w", err)
	}
	if len(b) > MaxTextRequestBytes {
		return fmt.Errorf("%w (serialized text: %d bytes; images excluded; this is not a token limit)", ErrTextRequestTooLarge, len(b))
	}
	return nil
}

func scanJSON(ctx context.Context, r io.Reader, accept func([]byte) error) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64<<10), maxStreamLine)
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := accept(line); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Err(); err != nil {
		return fmt.Errorf("read Ollama stream: %w", err)
	}
	return nil
}

func (c *Client) Pull(ctx context.Context, name string, emit func(PullProgress)) error {
	if err := validName(name); err != nil {
		return err
	}
	if IsRemoteName(name) {
		return ErrRemoteModel
	}
	resp, err := c.request(ctx, c.inferenceHTTP, http.MethodPost, "/api/pull", map[string]any{"model": name, "stream": true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	done := false
	err = scanJSON(ctx, resp.Body, func(line []byte) error {
		var p PullProgress
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		if p.Error != "" {
			return errors.New(p.Error)
		}
		if emit != nil {
			emit(p)
		}
		if p.Status == "success" {
			done = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !done {
		return ErrIncompleteStream
	}
	return nil
}
func (c *Client) Load(ctx context.Context, name string, contextSize int) error {
	if contextSize < 1024 || contextSize > 131072 {
		return errors.New("model context must be between 1024 and 131072 tokens")
	}
	return c.residency(ctx, name, "5m", map[string]any{"num_ctx": contextSize})
}
func (c *Client) Unload(ctx context.Context, name string) error {
	return c.residency(ctx, name, 0, nil)
}
func (c *Client) residency(ctx context.Context, name string, keepAlive any, options map[string]any) error {
	// Native model actions can arrive with a background context. Bound loading
	// and unloading even then; an earlier caller deadline still takes priority.
	ctx, cancel := context.WithTimeout(ctx, maxResidencyDuration)
	defer cancel()
	if err := validName(name); err != nil {
		return err
	}
	select {
	case c.admission <- struct{}{}:
		defer func() { <-c.admission }()
	case <-ctx.Done():
		return ctx.Err()
	}
	info, err := c.localInfo(ctx, name)
	if err != nil {
		return err
	}
	if keepAlive != 0 && ValidateCapabilities(info, "embedding") == nil && ValidateCapabilities(info, "completion") != nil {
		if _, err := c.ModelIdentity(ctx, name, "embedding"); err != nil {
			return err
		}
		_, err := c.embedJSON(ctx, embeddingPayload(name, []embedWireInput{}), 0)
		return err
	}
	payload := map[string]any{"model": name, "keep_alive": keepAlive, "stream": false}
	if options != nil {
		payload["options"] = options
	}
	resp, err := c.request(ctx, c.inferenceHTTP, http.MethodPost, "/api/generate", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Done  bool   `json:"done"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&r); err != nil {
		return err
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	if !r.Done {
		return ErrIncompleteStream
	}
	return nil
}
func (c *Client) Delete(ctx context.Context, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	return c.json(ctx, http.MethodDelete, "/api/delete", map[string]any{"model": name}, nil)
}
