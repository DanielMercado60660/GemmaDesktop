package workflow

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
)

func TestFanoutChildEventsCarryExplicitParentAndIndex(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%v", failed), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				if failed {
					http.Error(w, "fixture generation failure", http.StatusServiceUnavailable)
					return
				}
				fmt.Fprintln(w, `{"message":{"content":"complete"},"done":true}`)
			}))
			defer srv.Close()
			client, err := inference.NewClient(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			const parent = "batch_item_9"
			items := []string{"first", "second", "third"}
			if failed {
				items = items[:1]
			}
			definition := Definition{Nodes: []Node{{ID: parent, Type: "fanout", Items: items}}}
			starts, finishes := map[int]string{}, map[int]string{}
			_, err = New(engine.New(client)).Run(t.Context(), definition, engine.Request{Model: "fixture", Prompt: "Process", MaxSteps: 4}, func(event engine.Event) {
				if event.NodeID == parent || (event.Type != "node_started" && event.Type != "node_completed" && event.Type != "node_failed") {
					return
				}
				data, ok := event.Data.(map[string]any)
				if !ok || data["parentNodeId"] != parent {
					t.Errorf("child event lacks explicit parent: %+v", event)
					return
				}
				index, ok := data["childIndex"].(int)
				if !ok || index < 0 || index >= len(items) {
					t.Errorf("child event has invalid index: %+v", event)
					return
				}
				if event.Type == "node_started" {
					starts[index] = event.NodeID
				} else {
					finishes[index] = event.NodeID
					if (event.Type == "node_failed") != failed {
						t.Errorf("wrong terminal event: %+v", event)
					}
				}
			}, nil, nil)
			if (err != nil) != failed || len(starts) != len(items) || len(finishes) != len(items) {
				t.Fatalf("missing child lifecycle: started=%v finished=%v err=%v", starts, finishes, err)
			}
			for index, nodeID := range starts {
				if finishes[index] != nodeID {
					t.Fatalf("child identity changed: index=%d start=%s finish=%s", index, nodeID, finishes[index])
				}
			}
		})
	}
}
