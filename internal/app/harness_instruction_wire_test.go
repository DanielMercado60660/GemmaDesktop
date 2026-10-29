package app

import (
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Exercises the production no-workflow solver, ADK InstructionProvider,
// Ollama adapter and serialized HTTP request, including a real file-tool turn.
// The server is a deterministic fixture; no model or Docker command executes.
func TestHarnessRevisionInstructionsAndToolGuidanceReachOllamaSystemWire(t *testing.T) {
	c := milestoneCore(t)
	const trialID = "instruction-wire-fixture"
	root := filepath.Join(c.DataDir, "harness", "trials", trialID, "workspace")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	const toolText = "TOOL_ONLY_CANARY_71f2\n"
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte(toolText), 0600); err != nil {
		t.Fatal(err)
	}
	const instructions = "REVISION_INSTRUCTIONS_CANARY_bca1\nRetain literal {unbound_session_key} and 日本語."
	const guidance = "REVISION_TOOL_GUIDANCE_CANARY_8d23\nUse exactly read_file with path \"source.txt\"; preserve \\literal\\path."
	const lesson = "REVISION_LESSON_CANARY_c5d0"
	const verificationGuidance = "Use readOnly:true for the prescribed verification command. Add a new regression test file; preserve the original public tests and README."
	const prompt = "USER_ONLY_CANARY_2bb3 inspect the fixture source."
	const stale = "STORED_PROFILE_INSTRUCTIONS_MUST_NOT_REPLACE_REVISION_1e9b"
	requests := make(chan inference.ChatRequest, 4)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
			return
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture request", 400)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "application/x-ndjson")
		if calls.Add(1) == 1 {
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"wire-read","function":{"name":"read_file","arguments":{"path":"source.txt"}}} ]},"done":true,"prompt_eval_count":100,"eval_count":12}`)
		} else {
			fmt.Fprintln(w, `{"message":{"content":"Fixture source observed."},"done":true,"prompt_eval_count":130,"eval_count":8}`)
		}
	}))
	defer server.Close()
	var err error
	c.client, err = inference.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	image := "sha256:" + strings.Repeat("d", 64)
	frozen := FrozenAgent{Agent: Doc{"id": "solver", "instructions": stale}, Profile: CapabilityProfile{Version: 1, Name: "fixture", ExecutionProfile: "docker", Tools: []string{"read_file"}}, ExecutionImage: image, HarnessInstructions: stale}
	frozen.Digest = contentDigest(frozen)
	raw, _ := json.Marshal(frozen)
	spec := harness.Spec{AgentID: "solver", Model: "wire-fixture", Agent: Doc{"instructions": stale, "maxSteps": 4, "thinking": "off", "temperature": 0.2, "maxOutputTokens": 8192, "toolProtocol": "native"}, ContextSize: 65536, BudgetSeconds: 60, ExecutorIdentity: image, FrozenCapability: raw, SolverTools: []string{"read_file"}}
	revision := harness.Revision{Content: harness.Content{Instructions: instructions, ToolGuidance: guidance, Lessons: []string{lesson}}}
	result, err := c.executeHarnessSolver(t.Context(), harness.SolveRequest{TrialID: trialID, Root: root, Prompt: prompt, Revision: revision, Spec: spec}, nil)
	if err != nil || result.Incomplete {
		t.Fatal(result, err)
	}
	if calls.Load() != 2 || len(result.ToolCalls) != 1 || result.ToolCalls[0].Tool != "read_file" || result.ToolCalls[0].Error != "" {
		t.Fatal("did not execute the expected file-tool continuation", calls.Load(), result)
	}
	for call := 0; call < 2; call++ {
		request := <-requests
		if len(request.Messages) < 2 || request.Messages[0].Role != "system" {
			t.Fatal("system message absent from serialized request", call)
		}
		system := request.Messages[0].Content
		for _, exact := range []string{defaultInstructions, instructions, guidance, lesson, verificationGuidance} {
			if strings.Count(system, exact) != 1 {
				t.Fatalf("request %d lost/duplicated exact instruction %q", call+1, exact)
			}
		}
		if !strings.Contains(system, instructions+"\n"+guidance+"\n"+lesson) {
			t.Fatal("revision field order or literal bytes changed", call+1)
		}
		for _, excluded := range []string{stale, prompt, toolText} {
			if strings.Contains(system, excluded) {
				t.Fatalf("request %d promoted/replaced unrelated content %q", call+1, excluded)
			}
		}
		users, tools := 0, 0
		for _, message := range request.Messages {
			if message.Role == "user" && message.Content == prompt {
				users++
			}
			if message.Role == "tool" && strings.Contains(message.Content, toolText) {
				tools++
			}
		}
		if users != 1 || tools != call {
			t.Fatal("native user/tool roles changed", call+1, users, tools)
		}
	}
	t.Log("Both serialized /api/chat system messages contained the exact revision Instructions, ToolGuidance, Lessons and appended readOnly:true verification guidance once, before and after the real read_file callback; native user/tool content remained separate.")
}
