package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workflow"
)

// A run shares one Manager across its workflow nodes, fan-out children and
// delegates; the file a reader node was served must not let a writer node that
// never read it write with a host-filled revision.
func TestWorkflowConversationsDoNotShareRevisionMemory(t *testing.T) {
	for _, c := range []struct {
		name          string
		nodes         []Doc
		edges         []Doc
		reader, write string
	}{
		{"sequential agent nodes", []Doc{{"id": "reader", "type": "agent", "prompt": "READER"}, {"id": "writer", "type": "agent", "prompt": "WRITER"}}, []Doc{{"source": "reader", "target": "writer"}}, "reader", "writer"},
		{"fanout children", []Doc{{"id": "fan", "type": "fanout", "items": []string{"READER", "WRITER"}}}, []Doc{}, "fan_item_0", "fan_item_1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			read := make(chan struct{})
			var readOnce sync.Once
			h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
				writer, wrote, calls := false, false, 0
				for _, m := range req.Messages {
					writer = writer || m.Role == "user" && strings.Contains(m.Content, "WRITER")
					for _, call := range m.ToolCalls {
						wrote = wrote || call.Function.Name == "write_file"
						calls++
					}
				}
				switch {
				case !writer && calls == 0:
					fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"r","function":{"name":"read_file","arguments":{"path":"shared.txt"}}}]},"done":true}`)
					return
				case !writer:
					readOnce.Do(func() { close(read) }) // the reader has been served the file
				case !wrote:
					// Fan-out children run concurrently and inference is serialized, so
					// the writer yields with a harmless read until the reader was served.
					select {
					case <-read:
						fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"w","function":{"name":"write_file","arguments":{"path":"shared.txt","content":"clobbered"}}}]},"done":true}`)
					default:
						fmt.Fprintf(w, `{"message":{"tool_calls":[{"id":"y%d","function":{"name":"list_files","arguments":{}}}]},"done":true}`+"\n", calls)
					}
					return
				}
				fmt.Fprintln(w, `{"message":{"content":"done"},"done":true}`)
			})
			callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Fixture", "maxSteps": 12, "mode": "approval"})
			shared := filepath.Join(h.workspace, "shared.txt")
			if err := os.WriteFile(shared, []byte("original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Reader then writer", "nodes": c.nodes, "edges": c.edges})
			id := str(callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "seed"}), "id")
			var approvals []Doc
			waitIntegration(t, func() bool {
				var err error
				if approvals, err = h.core.Store.List("approval"); err != nil {
					t.Fatal(err)
				}
				run, err := h.core.Store.Get("run", id)
				if err != nil {
					t.Fatal(err)
				}
				return len(approvals) > 0 || !active(str(run, "status")) && !h.core.workspaceBusy(str(run, "workspaceId"))
			})
			if len(approvals) != 0 {
				t.Fatalf("writer reached approval with a revision only the reader saw: %v", approvals[0]["arguments"])
			}
			finishedIntegration(t, h.core, id, "completed")
			if got, _ := os.ReadFile(shared); string(got) != "original\n" {
				t.Fatalf("shared file changed: %q", got)
			}
			calls, err := h.core.Store.List("toolcall")
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]Doc{}
			for _, call := range calls {
				seen[str(call, "nodeId")+"/"+str(call, "tool")] = call
			}
			if r := seen[c.reader+"/read_file"]; r == nil || r["status"] != "completed" {
				t.Fatalf("reader did not read: %v", calls)
			}
			if w := seen[c.write+"/write_file"]; w == nil || w["status"] != "failed" || !strings.Contains(str(w, "error"), "has not been read") {
				t.Fatalf("writer was not asked to read first: %v", w)
			}
		})
	}
}

// The harness solver also shares one Manager across an approved workflow's
// nodes, and auto-approves granted writes, so an unscoped executor would let the
// writer node clobber a file only the reader node was served.
func TestHarnessWorkflowNodesDoNotShareRevisionMemory(t *testing.T) {
	c := milestoneCore(t)
	root := filepath.Join(c.DataDir, "harness", "trials", "scope-fixture", "workspace")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(shared, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
			return
		}
		var req inference.ChatRequest
		if r.URL.Path != "/api/chat" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		writer, calls := false, 0
		for _, m := range req.Messages {
			writer = writer || m.Role == "user" && strings.Contains(m.Content, "WRITER")
			calls += len(m.ToolCalls)
		}
		switch {
		case calls > 0:
			fmt.Fprintln(w, `{"message":{"content":"done"},"done":true}`)
		case writer:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"w","function":{"name":"write_file","arguments":{"path":"shared.txt","content":"clobbered"}}}]},"done":true}`)
		default:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"r","function":{"name":"read_file","arguments":{"path":"shared.txt"}}}]},"done":true}`)
		}
	}))
	defer server.Close()
	var err error
	if c.client, err = inference.NewClient(server.URL); err != nil {
		t.Fatal(err)
	}
	image := "sha256:09c68348396bd61f72b3570279e009741490473e40f8cc70517531696767578c"
	granted := []string{"read_file", "write_file"}
	f := FrozenAgent{Agent: Doc{"id": "solver"}, Profile: CapabilityProfile{Version: 1, Name: "trial", ExecutionProfile: "docker", Tools: granted}, ExecutionImage: image}
	f.Digest = contentDigest(f)
	frozen, _ := json.Marshal(f)
	def, _ := json.Marshal(workflow.Definition{ID: "wf", Name: "Reader then writer", Nodes: []workflow.Node{{ID: "reader", Type: "agent", Prompt: "READER"}, {ID: "writer", Type: "agent", Prompt: "WRITER"}}, Edges: []workflow.Edge{{Source: "reader", Target: "writer"}}})
	spec := harness.Spec{AgentID: "solver", Model: "fixture", Agent: Doc{"maxSteps": 8, "thinking": "off", "temperature": 0.2, "maxOutputTokens": 8192, "toolProtocol": "native"}, ContextSize: 65536, BudgetSeconds: 60, ExecutorIdentity: image, FrozenCapability: frozen, SolverTools: granted, ApprovedWorkflows: map[string]json.RawMessage{"wf": def}}
	result, err := c.executeHarnessSolver(t.Context(), harness.SolveRequest{TrialID: "scope-fixture", Root: root, Prompt: "seed", Revision: harness.Revision{Content: harness.Content{WorkflowID: "wf"}}, Spec: spec}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(shared); string(got) != "original\n" {
		t.Fatalf("writer node clobbered a file only the reader node read: %q", got)
	}
	seen := map[string]harness.ToolObservation{}
	for _, call := range result.ToolCalls {
		seen[call.Tool] = call
	}
	if r, ok := seen["read_file"]; !ok || r.Error != "" {
		t.Fatalf("reader did not read: %+v", result.ToolCalls)
	}
	if w, ok := seen["write_file"]; !ok || !strings.Contains(w.Error, "has not been read") {
		t.Fatalf("writer was not asked to read first: %+v", result.ToolCalls)
	}
}

// Every conversation a run can start reaches the Manager under its own scope.
func TestConversationScopedSeparatesEveryRunConversation(t *testing.T) {
	nodes := []string{"", "reader", "fan_item_0", "fan_item_1", "delegate_x"}
	for _, reader := range nodes {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0600); err != nil {
			t.Fatal(err)
		}
		m, err := tools.New(root, tools.Options{ArtifactDir: t.TempDir(), Approve: func(context.Context, tools.Approval) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		exec := func(node, name string, args Doc) error {
			_, err := m.Execute(conversationScoped(t.Context(), engine.ToolCall{NodeID: node, Name: name}), name, args)
			return err
		}
		if err = exec(reader, "read_file", Doc{"path": "a.txt"}); err != nil {
			t.Fatal(err)
		}
		for _, other := range nodes {
			if other == reader {
				continue
			}
			if err = exec(other, "write_file", Doc{"path": "a.txt", "content": "b\n"}); err == nil || !strings.Contains(err.Error(), "has not been read") {
				t.Fatalf("%q wrote with %q's revision: %v", other, reader, err)
			}
		}
		if err = exec(reader, "write_file", Doc{"path": "a.txt", "content": "b\n"}); err != nil {
			t.Fatalf("%q could not write what it read: %v", reader, err)
		}
		m.Close()
	}
}

// NodeID is the conversation identity, so no node may take a fan-out child's.
func TestWorkflowRejectsNodesNamedLikeFanoutItems(t *testing.T) {
	for id, collides := range map[string]bool{"fan_item_0": true, "fan_item_15": true, "fan_item_": false, "fan_item_x": false, "fan_items": false, "fan_item_0_item_1": false} {
		err := workflow.Validate(workflow.Definition{Nodes: []workflow.Node{{ID: "fan", Type: "fanout", Items: []string{"a"}}, {ID: id, Type: "agent"}}, Edges: []workflow.Edge{{Source: "fan", Target: id}}})
		if (err != nil) != collides {
			t.Fatalf("%s: collides=%v err=%v", id, collides, err)
		}
	}
	if err := workflow.Validate(workflow.Definition{Nodes: []workflow.Node{{ID: "batch_item_9", Type: "fanout", Items: []string{"a"}}}}); err != nil {
		t.Fatal(err)
	}
}
