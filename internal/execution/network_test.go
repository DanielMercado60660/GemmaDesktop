package execution

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPResponseLimitCoversMislabeledErrorBodies(t *testing.T) {
	for _, test := range []struct {
		name, mediaType string
		status          int
		stream          bool
	}{
		{"json", "application/json", 200, false},
		{"plain error", "text/plain", 500, false},
		{"missing content type", "", 400, false},
		{"mislabeled event stream error", "text/event-stream", 500, false},
		{"invalid content type", "invalid;;;;", 200, false},
		{"successful event stream keeps SDK event limit", "text/event-stream; charset=utf-8", 200, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.mediaType == "" {
					w.Header()["Content-Type"] = nil
				} else {
					w.Header().Set("Content-Type", test.mediaType)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, strings.Repeat("data: {}\n\n", 100))
			}))
			defer server.Close()
			client, _, err := EndpointClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			BoundJSONResponses(client, 32)
			response, err := client.Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if test.stream {
				if err != nil || len(body) <= 32 {
					t.Fatalf("legitimate SSE lost its event-based policy: bytes=%d error=%v", len(body), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "size limit") || len(body) > 32 {
				t.Fatalf("response escaped bound: bytes=%d error=%v", len(body), err)
			}
		})
	}
}
