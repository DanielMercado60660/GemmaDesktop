package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestReadFilePresentationEndsAtExactSourceEOF(t *testing.T) {
	for _, source := range []string{
		"value = '\\n'\nlabel = \"日本語\"",
		"value = '\\n'\r\nlabel = \"日本語\"\r\n",
		"# BEGIN GEMMA_DATA_actual_source\n# END GEMMA_DATA_actual_source\n",
		"",
	} {
		t.Run(fmt.Sprintf("%q", source), func(t *testing.T) {
			response := map[string]any{"isError": false, "content": "A distinct read diagnostic", "data": map[string]any{"receiptId": "run:node:read_1", "content": source, "path": "source.py", "hash": "revision-123", "startLine": 1, "truncated": false}}
			req, err := toOllama(&model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "read_file", ID: "read_1", Response: response}}}}}}, Request{Model: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			var decoded inference.ChatRequest
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			content := decoded.Messages[0].Content
			heading := strings.Index(content, "\nFile content (")
			if heading < 0 {
				t.Fatal("missing source boundary heading")
			}
			body := content[heading+1:]
			body = body[strings.IndexByte(body, '\n')+1:]
			if body != source {
				t.Fatalf("file source changed or gained a trailing marker: got %q want %q", body, source)
			}
			for _, metadata := range []string{"receiptId: \"run:node:read_1\"", "hash: \"revision-123\"", "path: \"source.py\"", "truncated: false", "Untrusted evidence", "A distinct read diagnostic"} {
				if !strings.Contains(content[:heading], metadata) {
					t.Errorf("missing metadata before source: %s", metadata)
				}
			}
			if decoded.Messages[0].Role != "tool" || decoded.Messages[0].ToolCallID != "read_1" {
				t.Fatal("changed protocol role or call identity")
			}
		})
	}
}

func TestProcessPresentationPreservesLiteralOutputAndFailureStatus(t *testing.T) {
	stdout := "diff --git a/source.py b/source.py\n+value = '\\n'\n+label = \"日本語\"\n"
	stderr := "Traceback:\nAssertionError: expected \\\"literal\\\"\nFAILED (failures=1)\n"
	for _, tool := range []string{"run_command", "git_diff"} {
		t.Run(tool, func(t *testing.T) {
			response := map[string]any{"isError": true, "error": "process exited with code 1", "content": "Separate diagnostic", "data": map[string]any{"receiptId": "run:node:process_1", "stdout": stdout, "stderr": stderr, "exitCode": 1, "truncated": true, "cancelled": false, "readOnly": true}}
			req, err := toOllama(&model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: tool, ID: "process_1", Response: response}}}}}}, Request{Model: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			wire, _ := json.Marshal(req)
			var decoded inference.ChatRequest
			_ = json.Unmarshal(wire, &decoded)
			content := decoded.Messages[0].Content
			for _, exact := range []string{stdout, stderr, "receiptId: \"run:node:process_1\"", "exitCode: 1", "isError: true", "truncated: true", "readOnly: true", "process exited with code 1", "Separate diagnostic", "Untrusted evidence"} {
				if !strings.Contains(content, exact) {
					t.Fatalf("missing exact process evidence %q: %s", exact, content)
				}
			}
			quoted, _ := json.Marshal(stdout)
			if strings.Contains(content, string(quoted)) || decoded.Messages[0].Role != "tool" || decoded.Messages[0].ToolCallID != "process_1" {
				t.Fatal("presentation changed role/identity or JSON-quoted its source text")
			}
		})
	}
}

// The host may change what a call does (forced readOnly, a resolved revision);
// the note saying so must reach the model, not only the structured receipt.
func TestPresentationShowsHostAdjustmentNotes(t *testing.T) {
	for tool, data := range map[string]map[string]any{
		"run_command": {"receiptId": "r1", "stdout": "", "stderr": "", "exitCode": 0, "readOnly": true, "readOnlyNote": "readOnly was set automatically because this task protects files"},
		"write_file":  {"path": "calc.py", "hash": "new", "revisionNote": "expectedHash resolved to the revision you last read"},
	} {
		t.Run(tool, func(t *testing.T) {
			response := map[string]any{"isError": false, "data": data}
			req, err := toOllama(&model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: tool, ID: "call_1", Response: response}}}}}}, Request{Model: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			content := req.Messages[0].Content
			for key, value := range data {
				if note, ok := value.(string); ok && strings.HasSuffix(key, "Note") && !strings.Contains(content, note) {
					t.Fatalf("%s presentation drops %s:\n%s", tool, key, content)
				}
			}
		})
	}
}
