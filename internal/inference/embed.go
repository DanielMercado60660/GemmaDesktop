package inference

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	EmbeddingDimensions   = 768
	MaxEmbedBatch         = 8
	MaxEmbedImageBytes    = 1 << 20
	MaxEmbedTextBytes     = 16 << 10
	maxEmbedResponseBytes = 1 << 20
	maxEmbedDuration      = 5 * time.Minute
)

var ErrModelIdentityChanged = errors.New("embedding model digest changed; rebuild the index with the selected model")

// QueryPrefix is Google's search-query instruction. Pure image inputs must not
// receive a text prefix. Corpus text uses title: <title or none> | text: <body>.
func QueryPrefix(query string) string { return "task: search result | query: " + query }

// ValidateCapabilities is strict: absent metadata is not proof of support.
// Legacy Chat alone retains its existing behavior for absent capabilities.
func ValidateCapabilities(info ModelInfo, required ...string) error {
	if len(required) > 0 && len(info.Capabilities) == 0 {
		return errors.New("model capabilities could not be verified; refresh local model metadata")
	}
	for _, capability := range required {
		if !slices.Contains(info.Capabilities, capability) {
			return fmt.Errorf("model does not support %s", capability)
		}
	}
	return nil
}

func normalizedDigest(digest string) (string, error) {
	digest = strings.TrimPrefix(digest, "sha256:")
	if len(digest) != 64 {
		return "", errors.New("local model has no valid SHA-256 digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("local model has no valid SHA-256 digest")
	}
	return "sha256:" + strings.ToLower(digest), nil
}

func canonicalModelName(name string) string {
	if strings.LastIndex(name, ":") <= strings.LastIndex(name, "/") {
		return name + ":latest"
	}
	return name
}

// ModelIdentity does metadata reads only; it never loads or downloads a model.
func (c *Client) ModelIdentity(ctx context.Context, name string, requiredCapabilities ...string) (ModelIdentity, error) {
	if err := validName(name); err != nil {
		return ModelIdentity{}, err
	}
	info, err := c.localInfo(ctx, name)
	if err != nil {
		return ModelIdentity{}, err
	}
	if err := ValidateCapabilities(info, requiredCapabilities...); err != nil {
		return ModelIdentity{}, err
	}
	models, err := c.Models(ctx)
	if err != nil {
		return ModelIdentity{}, err
	}
	for _, model := range models {
		if canonicalModelName(model.Name) != canonicalModelName(name) && (model.Model == "" || canonicalModelName(model.Model) != canonicalModelName(name)) {
			continue
		}
		if model.RemoteHost != "" || model.RemoteModel != "" || IsRemoteName(model.Name) || IsRemoteName(model.Model) {
			return ModelIdentity{}, ErrRemoteModel
		}
		digest, err := normalizedDigest(model.Digest)
		if err != nil {
			return ModelIdentity{}, err
		}
		return ModelIdentity{Name: model.Name, Digest: digest, Capabilities: slices.Clone(info.Capabilities)}, nil
	}
	return ModelIdentity{}, fmt.Errorf("model %q is not installed locally; download it explicitly before indexing", name)
}

type embedWireInput struct {
	Text  string `json:"text,omitempty"`
	Image []byte `json:"image,omitempty"`
}

type embedWireRequest struct {
	Model      string           `json:"model"`
	Input      []embedWireInput `json:"input"`
	Truncate   bool             `json:"truncate"`
	Dimensions int              `json:"dimensions"`
	KeepAlive  string           `json:"keep_alive"`
	Options    map[string]int   `json:"options"`
}

func embeddingPayload(name string, inputs []embedWireInput) embedWireRequest {
	return embedWireRequest{Model: name, Input: inputs, Truncate: false, Dimensions: EmbeddingDimensions, KeepAlive: "5m", Options: map[string]int{"num_ctx": 8192}}
}

func prepareEmbed(req EmbedRequest) (embedWireRequest, []string, error) {
	if err := validName(req.Model); err != nil {
		return embedWireRequest{}, nil, err
	}
	if len(req.Input) < 1 || len(req.Input) > MaxEmbedBatch {
		return embedWireRequest{}, nil, fmt.Errorf("embedding batch must contain 1 to %d inputs", MaxEmbedBatch)
	}
	inputs := make([]embedWireInput, len(req.Input))
	required := []string{"embedding"}
	for i, input := range req.Input {
		if len(input.Text) > MaxEmbedTextBytes || !utf8.ValidString(input.Text) {
			return embedWireRequest{}, nil, fmt.Errorf("embedding input %d text must be valid UTF-8 and at most %d bytes", i, MaxEmbedTextBytes)
		}
		if len(input.Image) > MaxEmbedImageBytes {
			return embedWireRequest{}, nil, fmt.Errorf("embedding input %d image exceeds %d bytes", i, MaxEmbedImageBytes)
		}
		if input.Text == "" && len(input.Image) == 0 {
			return embedWireRequest{}, nil, fmt.Errorf("embedding input %d is empty", i)
		}
		if len(input.Image) > 0 && !slices.Contains(required, "vision") {
			required = append(required, "vision")
		}
		inputs[i] = embedWireInput{Text: input.Text, Image: bytes.Clone(input.Image)}
	}
	return embeddingPayload(req.Model, inputs), required, nil
}

// Embed serializes requests with Chat and model residency operations on this
// Client. In Ollama 0.40.11, cancellation abandons queued work but cannot interrupt
// a synchronous MLX forward already executing. A returned cancellation therefore
// does not establish GPU-idle. No endpoint here triggers a model download.
func (c *Client) Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	payload, required, err := prepareEmbed(req)
	if err != nil {
		return EmbedResponse{}, err
	}
	var expected string
	if req.ExpectedDigest != "" {
		expected, err = normalizedDigest(req.ExpectedDigest)
		if err != nil {
			return EmbedResponse{}, fmt.Errorf("expected model digest: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, maxEmbedDuration)
	defer cancel()
	select {
	case c.admission <- struct{}{}:
		defer func() { <-c.admission }()
	case <-ctx.Done():
		return EmbedResponse{}, ctx.Err()
	}
	identity, err := c.ModelIdentity(ctx, req.Model, required...)
	if err != nil {
		return EmbedResponse{}, err
	}
	if expected != "" && expected != identity.Digest {
		return EmbedResponse{}, ErrModelIdentityChanged
	}
	response, err := c.embedJSON(ctx, payload, len(payload.Input))
	if err != nil {
		return EmbedResponse{}, err
	}
	// Ollama's API addresses mutable tags, without an expected-digest precondition.
	// Detect changes across inference; this cannot prove absence of an external
	// change-and-restore race against the shared daemon.
	after, err := c.ModelIdentity(ctx, req.Model, required...)
	if err != nil {
		return EmbedResponse{}, err
	}
	if after.Digest != identity.Digest {
		return EmbedResponse{}, ErrModelIdentityChanged
	}
	response.Digest = identity.Digest
	return response, nil
}

func (c *Client) embedJSON(ctx context.Context, payload embedWireRequest, count int) (EmbedResponse, error) {
	resp, err := c.request(ctx, c.inferenceHTTP, http.MethodPost, "/api/embed", payload)
	if err != nil {
		return EmbedResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEmbedResponseBytes+1))
	if err != nil {
		return EmbedResponse{}, err
	}
	if len(body) > maxEmbedResponseBytes {
		return EmbedResponse{}, errors.New("embedding response exceeds 1 MiB limit")
	}
	var envelope struct {
		EmbedResponse
		// Pointers distinguish numeric zero from JSON null. A null component is
		// malformed, even if the remaining components happen to have unit norm.
		Vectors [][]*float32 `json:"embeddings"`
		Error   string       `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return EmbedResponse{}, fmt.Errorf("decode embedding response: %w", err)
	}
	if envelope.Error != "" {
		return EmbedResponse{}, errors.New(envelope.Error)
	}
	r := envelope.EmbedResponse
	if canonicalModelName(r.Model) != canonicalModelName(payload.Model) {
		return EmbedResponse{}, errors.New("embedding response model does not match request")
	}
	if len(envelope.Vectors) != count {
		return EmbedResponse{}, fmt.Errorf("embedding response count %d does not match input count %d", len(envelope.Vectors), count)
	}
	r.Embeddings = make([][]float32, count)
	for i, vector := range envelope.Vectors {
		if len(vector) != EmbeddingDimensions {
			return EmbedResponse{}, fmt.Errorf("embedding %d has %d dimensions; expected %d", i, len(vector), EmbeddingDimensions)
		}
		var squaredNorm float64
		r.Embeddings[i] = make([]float32, EmbeddingDimensions)
		for j, v := range vector {
			if v == nil {
				return EmbedResponse{}, fmt.Errorf("embedding %d contains a null component", i)
			}
			f := float64(*v)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return EmbedResponse{}, fmt.Errorf("embedding %d contains non-finite values", i)
			}
			squaredNorm += f * f
			r.Embeddings[i][j] = *v
		}
		if math.Abs(math.Sqrt(squaredNorm)-1) > 1e-3 {
			return EmbedResponse{}, fmt.Errorf("embedding %d is not a nonzero unit vector", i)
		}
	}
	return r, nil
}
