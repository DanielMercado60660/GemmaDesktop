package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const embedFixtureModel = "embedding-fixture:latest"

var embedFixtureDigest = strings.Repeat("ab", 32)

func embedFixtureVector(index int) []float32 {
	v := make([]float32, EmbeddingDimensions)
	v[index] = 1
	return v
}

func embedFixtureReply(w http.ResponseWriter, count int) {
	vectors := make([][]float32, count)
	for i := range vectors {
		vectors[i] = embedFixtureVector(i)
	}
	_ = json.NewEncoder(w).Encode(EmbedResponse{Model: embedFixtureModel, Embeddings: vectors, PromptEvalCount: 19, TotalDuration: 120, LoadDuration: 30})
}

func embedFixtureClient(t *testing.T, overrides map[string]http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler := overrides[r.URL.Path]; handler != nil {
			handler(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/show":
			fmt.Fprint(w, `{"capabilities":["embedding","vision"]}`)
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []Model{{Name: embedFixtureModel, Digest: embedFixtureDigest}}})
		case "/api/embed":
			embedFixtureReply(w, 1)
		default:
			t.Errorf("unexpected endpoint (no implicit pull permitted): %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func embedFixtureRequest() EmbedRequest {
	return EmbedRequest{Model: embedFixtureModel, ExpectedDigest: "sha256:" + embedFixtureDigest, Input: []EmbedInput{{Text: QueryPrefix("a scene")}}}
}

func TestEmbedMixedInputsPreserveOrderAndIdentity(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0x00, 0xfe}
	query := "\nA café with \"quotes\"?\n"
	req := embedFixtureRequest()
	req.Input = []EmbedInput{{Text: QueryPrefix(query)}, {Image: image}}
	before, _ := json.Marshal(req)
	c := embedFixtureClient(t, map[string]http.HandlerFunc{"/api/embed": func(w http.ResponseWriter, r *http.Request) {
		var wire embedWireRequest
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		if wire.Model != req.Model || wire.Truncate || wire.Dimensions != 768 || wire.Options["num_ctx"] != 8192 || len(wire.Input) != 2 {
			t.Errorf("incorrect embedding wire request: %#v", wire)
		}
		if wire.Input[0].Text != "task: search result | query: "+query || len(wire.Input[0].Image) != 0 || wire.Input[1].Text != "" || !bytes.Equal(wire.Input[1].Image, image) {
			t.Errorf("text/media changed or reordered: %#v", wire.Input)
		}
		embedFixtureReply(w, 2)
	}})
	r, err := c.Embed(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Digest != req.ExpectedDigest || r.Model != req.Model || r.PromptEvalCount != 19 || r.TotalDuration != 120 || r.LoadDuration != 30 || !reflect.DeepEqual(r.Embeddings, [][]float32{embedFixtureVector(0), embedFixtureVector(1)}) {
		t.Fatalf("identity, metrics or vector order lost: %#v", r)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Fatal("caller request was mutated")
	}
}

func TestEmbedBoundsFailBeforeAdmissionOrHTTP(t *testing.T) {
	c, _ := NewClient("")
	var calls atomic.Int32
	probe := deadlineProbeTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected dispatch")
	})
	c.http.Transport, c.inferenceHTTP.Transport = probe, probe
	cases := map[string]EmbedRequest{
		"empty batch":    {Model: embedFixtureModel},
		"large batch":    {Model: embedFixtureModel, Input: make([]EmbedInput, MaxEmbedBatch+1)},
		"empty input":    {Model: embedFixtureModel, Input: []EmbedInput{{}}},
		"large text":     {Model: embedFixtureModel, Input: []EmbedInput{{Text: strings.Repeat("x", MaxEmbedTextBytes+1)}}},
		"large image":    {Model: embedFixtureModel, Input: []EmbedInput{{Image: make([]byte, MaxEmbedImageBytes+1)}}},
		"invalid UTF8":   {Model: embedFixtureModel, Input: []EmbedInput{{Text: string([]byte{0xff})}}},
		"invalid digest": {Model: embedFixtureModel, ExpectedDigest: "bad", Input: []EmbedInput{{Text: "query"}}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Embed(t.Context(), req); err == nil {
				t.Fatal("invalid request accepted")
			}
			if calls.Load() != 0 || len(c.admission) != 0 {
				t.Fatal("invalid request reached admission or HTTP")
			}
		})
	}
}

func TestEmbedRequiresInstalledLocalCapablePinnedModel(t *testing.T) {
	cases := []struct {
		name, show, tags, model, digest string
		image                           bool
	}{
		{name: "cloud name", model: "embedding-cloud"},
		{name: "remote show", show: `{"remote_host":"https://example.com","capabilities":["embedding"]}`},
		{name: "remote tag", tags: `{"models":[{"name":"embedding-fixture:latest","remote_host":"https://example.com"}]}`},
		{name: "unknown capabilities", show: `{}`},
		{name: "completion only", show: `{"capabilities":["completion"]}`},
		{name: "no vision", show: `{"capabilities":["embedding"]}`, image: true},
		{name: "not installed", tags: `{"models":[]}`},
		{name: "no digest", tags: `{"models":[{"name":"embedding-fixture:latest"}]}`},
		{name: "different digest", digest: strings.Repeat("cd", 32)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			overrides := map[string]http.HandlerFunc{"/api/embed": func(w http.ResponseWriter, r *http.Request) { calls.Add(1); embedFixtureReply(w, 1) }}
			if test.show != "" {
				overrides["/api/show"] = func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.show) }
			}
			if test.tags != "" {
				overrides["/api/tags"] = func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.tags) }
			}
			c := embedFixtureClient(t, overrides)
			req := embedFixtureRequest()
			if test.model != "" {
				req.Model = test.model
			}
			if test.digest != "" {
				req.ExpectedDigest = test.digest
			}
			if test.image {
				req.Input[0] = EmbedInput{Image: []byte{1}}
			}
			if _, err := c.Embed(t.Context(), req); err == nil || calls.Load() != 0 {
				t.Fatalf("unsafe model dispatched: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestEmbedRejectsTagDriftDuringInference(t *testing.T) {
	var tags atomic.Int32
	c := embedFixtureClient(t, map[string]http.HandlerFunc{"/api/tags": func(w http.ResponseWriter, r *http.Request) {
		digest := embedFixtureDigest
		if tags.Add(1) > 1 {
			digest = strings.Repeat("cd", 32)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []Model{{Name: embedFixtureModel, Digest: digest}}})
	}})
	if _, err := c.Embed(t.Context(), embedFixtureRequest()); !errors.Is(err, ErrModelIdentityChanged) {
		t.Fatalf("changed tag accepted: %v", err)
	}
}

func TestEmbedRejectsMalformedResponses(t *testing.T) {
	valid, _ := json.Marshal(EmbedResponse{Model: embedFixtureModel, Embeddings: [][]float32{embedFixtureVector(0)}})
	vectorResponse := func(v []float32) string {
		b, _ := json.Marshal(EmbedResponse{Model: embedFixtureModel, Embeddings: [][]float32{v}})
		return string(b)
	}
	badNorm := embedFixtureVector(0)
	badNorm[0] = 2
	cases := map[string]string{
		"bad JSON":          "{",
		"trailing JSON":     string(valid) + ` {}`,
		"application error": `{"error":"cannot embed"}`,
		"oversized":         string(valid) + strings.Repeat(" ", maxEmbedResponseBytes),
		"wrong model":       strings.Replace(string(valid), embedFixtureModel, "different-model", 1),
		"wrong count":       `{"model":"embedding-fixture:latest","embeddings":[]}`,
		"wrong dimensions":  vectorResponse([]float32{1}),
		"zero norm":         vectorResponse(make([]float32, EmbeddingDimensions)),
		"nonunit norm":      vectorResponse(badNorm),
		"NaN":               strings.Replace(string(valid), "[1,", "[NaN,", 1),
		"overflow":          strings.Replace(string(valid), "[1,", "[1e99,", 1),
		"null component":    strings.Replace(string(valid), "[1,0,", "[1,null,", 1),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			c := embedFixtureClient(t, map[string]http.HandlerFunc{"/api/embed": func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }})
			if _, err := c.Embed(t.Context(), embedFixtureRequest()); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}

func TestEmbedOverflowIsNotRetriedOrTruncated(t *testing.T) {
	var calls atomic.Int32
	c := embedFixtureClient(t, map[string]http.HandlerFunc{"/api/embed": func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var wire map[string]any
		_ = json.NewDecoder(r.Body).Decode(&wire)
		if value, present := wire["truncate"]; !present || value != false {
			t.Error("truncate:false was not sent")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		fmt.Fprint(w, `{"error":"expanded media exceeds context length"}`)
	}})
	_, err := c.Embed(t.Context(), embedFixtureRequest())
	if err == nil || !strings.Contains(err.Error(), "exceeds context length") || calls.Load() != 1 {
		t.Fatalf("overflow result: calls=%d err=%v", calls.Load(), err)
	}
}

func TestEmbeddingOnlyModelLoadsThroughEmbedAndCannotChat(t *testing.T) {
	var loads, unloads atomic.Int32
	c := embedFixtureClient(t, map[string]http.HandlerFunc{
		"/api/embed": func(w http.ResponseWriter, r *http.Request) {
			loads.Add(1)
			var wire embedWireRequest
			_ = json.NewDecoder(r.Body).Decode(&wire)
			if wire.Input == nil || len(wire.Input) != 0 || wire.Options["num_ctx"] != 8192 {
				t.Errorf("invalid preload: %#v", wire)
			}
			embedFixtureReply(w, 0)
		},
		"/api/generate": func(w http.ResponseWriter, r *http.Request) {
			unloads.Add(1)
			var wire map[string]any
			_ = json.NewDecoder(r.Body).Decode(&wire)
			if wire["keep_alive"] != float64(0) {
				t.Error("embedding model used generate for loading")
			}
			fmt.Fprint(w, `{"done":true}`)
		},
	})
	if err := c.Load(t.Context(), embedFixtureModel, 32768); err != nil {
		t.Fatal(err)
	}
	if err := c.Chat(t.Context(), ChatRequest{Model: embedFixtureModel}, nil); err == nil || !strings.Contains(err.Error(), "completion") {
		t.Fatalf("embedding-only chat: %v", err)
	}
	if err := c.Unload(t.Context(), embedFixtureModel); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 1 || unloads.Load() != 1 || len(c.admission) != 0 {
		t.Fatal("incorrect residency routing or leaked admission")
	}
}

func TestEmbedSharesChatAdmissionAndCancelsQueuedWork(t *testing.T) {
	started := make(chan struct{})
	var embeds atomic.Int32
	c := embedFixtureClient(t, map[string]http.HandlerFunc{
		"/api/show": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"capabilities":["completion","embedding","vision"]}`)
		},
		"/api/chat": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
		},
		"/api/embed": func(w http.ResponseWriter, r *http.Request) { embeds.Add(1); embedFixtureReply(w, 1) },
	})
	chatCtx, cancelChat := context.WithCancel(t.Context())
	defer cancelChat()
	chatDone := make(chan error, 1)
	go func() { chatDone <- c.Chat(chatCtx, ChatRequest{Model: embedFixtureModel}, nil) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("chat did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	_, err := c.Embed(ctx, embedFixtureRequest())
	if !errors.Is(err, context.DeadlineExceeded) || embeds.Load() != 0 {
		t.Fatalf("queued embedding ran: %v", err)
	}
	cancelChat()
	select {
	case <-chatDone:
	case <-time.After(2 * time.Second):
		t.Fatal("chat cancellation stuck")
	}
	if _, err := c.Embed(t.Context(), embedFixtureRequest()); err != nil || embeds.Load() != 1 {
		t.Fatalf("admission not released: %v", err)
	}
}

func TestEmbedCancellationDuringHTTPReleasesAdmission(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(fmt.Sprint(headers), func(t *testing.T) {
			started := make(chan struct{})
			var embeds atomic.Int32
			c := embedFixtureClient(t, map[string]http.HandlerFunc{"/api/embed": func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if embeds.Add(1) == 1 {
					if headers {
						fmt.Fprint(w, `{"model":`)
						w.(http.Flusher).Flush()
					}
					close(started)
					<-r.Context().Done()
					return
				}
				embedFixtureReply(w, 1)
			}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := c.Embed(ctx, embedFixtureRequest()); done <- err }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("embedding did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("embedding cancellation stuck")
			}
			if _, err := c.Embed(t.Context(), embedFixtureRequest()); err != nil {
				t.Fatalf("admission remained locked: %v", err)
			}
		})
	}
}

func TestEmbedHasFiniteInferenceDeadlineAndSeparateMetadataTransport(t *testing.T) {
	c, _ := NewClient("")
	if c.inferenceHTTP.Transport.(*http.Transport).ResponseHeaderTimeout != 0 || c.http.Transport.(*http.Transport).ResponseHeaderTimeout != 30*time.Second {
		t.Fatal("embedding would inherit metadata header deadline")
	}
	probe := deadlineProbeTransport(func(r *http.Request) (*http.Response, error) {
		deadline, exists := r.Context().Deadline()
		want := 30 * time.Second
		body := `{"capabilities":["embedding","vision"]}`
		if r.URL.Path == "/api/tags" {
			body = fmt.Sprintf(`{"models":[{"name":%q,"digest":%q}]}`, embedFixtureModel, embedFixtureDigest)
		}
		if r.URL.Path == "/api/embed" {
			want = maxEmbedDuration
			b, _ := json.Marshal(EmbedResponse{Model: embedFixtureModel, Embeddings: [][]float32{embedFixtureVector(0)}})
			body = string(b)
		}
		if !exists || time.Until(deadline) > want || time.Until(deadline) < want-time.Second {
			t.Errorf("%s deadline not bounded at %s", r.URL.Path, want)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	c.http.Transport, c.inferenceHTTP.Transport = probe, probe
	if _, err := c.Embed(context.Background(), embedFixtureRequest()); err != nil {
		t.Fatal(err)
	}
}
