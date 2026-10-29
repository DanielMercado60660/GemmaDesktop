package workflow

import (
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWorkflowAccountingSurvivesResumeAndKeepsLegacyUnknown(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%v", legacy), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				calls.Add(1)
				fmt.Fprintln(w, `{"message":{"content":"observed output"},"done":true,"prompt_eval_count":5,"eval_count":2}`)
			}))
			defer srv.Close()
			client, _ := inference.NewClient(srv.URL)
			r := New(engine.New(client))
			d := Definition{ID: "accounting", Nodes: []Node{{ID: "before", Type: "agent"}, {ID: "approve", Type: "approval"}, {ID: "after", Type: "agent"}}, Edges: []Edge{{Source: "before", Target: "approve"}, {Source: "approve", Target: "after"}}}
			req := engine.Request{Model: "fixture", Prompt: "go", MaxSteps: 4}
			first, err := r.Run(t.Context(), d, req, nil, nil, nil)
			if err != nil || len(first.Pending) != 1 || !first.TokenAccounting.Complete() || first.TokenAccounting.Attempts != 1 || first.Usage.EvalCount != 2 {
				t.Fatal("initial accounting", first, err)
			}
			req.Snapshot = first.Snapshot
			if legacy {
				var cp map[string]any
				_ = json.Unmarshal(req.Snapshot, &cp)
				delete(cp, "tokenAccounting")
				req.Snapshot, _ = json.Marshal(cp)
			}
			req.Resume = map[string]any{first.Pending[0].ID: true}
			final, err := r.Run(t.Context(), d, req, nil, nil, nil)
			if err != nil || calls.Load() != 2 || final.Usage.PromptEvalCount != 10 || final.Usage.EvalCount != 4 || final.TokenAccounting == nil {
				t.Fatal("resume replayed/lost measurements", final, err, calls.Load())
			}
			if legacy {
				if final.TokenAccounting.Complete() || final.TokenAccounting.Status != "partial" || final.TokenAccounting.UnknownContributions != 1 {
					t.Fatal("legacy prior cost upgraded", final.TokenAccounting)
				}
			} else if !final.TokenAccounting.Complete() || final.TokenAccounting.Attempts != 2 {
				t.Fatal("complete workflow cost missing/doubled", final.TokenAccounting)
			}
		})
	}
}
