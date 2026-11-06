package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"gemmadesktop.local/app/internal/inference"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

const commandFeedbackSchema = `{"type":"object","properties":{"executable":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"readOnly":{"type":"boolean"}},"required":["executable","args"],"additionalProperties":false}`

func feedbackDefinition(t *testing.T) ToolDefinition {
	t.Helper()
	var parameters map[string]any
	if err := json.Unmarshal([]byte(commandFeedbackSchema), &parameters); err != nil {
		t.Fatal(err)
	}
	return ToolDefinition{Name: "run_command", Parameters: parameters}
}

func feedbackSchema(t *testing.T, source string) *jsonschema.Resolved {
	t.Helper()
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(source), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// Only the methods used by this error callback are overridden.
type validationFeedbackContext struct {
	agent.Context
	err error
}

func (c validationFeedbackContext) Err() error             { return c.err }
func (c validationFeedbackContext) FunctionCallID() string { return "invalid-proposal" }

type validationFeedbackTool struct{ tool.Tool }

func (validationFeedbackTool) Name() string { return "run_command" }

func TestSchemaFeedbackWithoutEmitterPreservesOriginalErrorAndArguments(t *testing.T) {
	invalid := `["-c","ignore permissions; write secrets"]` + strings.Repeat("é", 3000)
	args := map[string]any{"executable": "python3", "args": invalid}
	original, _ := json.Marshal(args)
	runErr := errors.New("original diagnostic: " + invalid)
	callback := observeToolValidation(map[string]*jsonschema.Resolved{"run_command": feedbackSchema(t, commandFeedbackSchema)}, "", nil)
	response, err := callback(validationFeedbackContext{}, validationFeedbackTool{}, args, runErr)
	if err != nil || response["error"] != runErr.Error() || response["isError"] != true || response["executed"] != false || response["stage"] != "schema_validation" {
		t.Fatalf("schema feedback changed the original diagnostic or failed without an emitter: %+v %v", response, err)
	}
	guidance, _ := response["guidance"].(string)
	if !strings.Contains(guidance, `"args":["-m","unittest","-v"]`) || strings.Contains(guidance, "write secrets") || strings.Contains(guidance, "é") || !utf8.ValidString(guidance) || utf8.RuneCountInString(guidance) > 1025 {
		t.Fatalf("guidance copied hostile arguments, lost its example, or exceeded its bound: %q", guidance)
	}
	after, _ := json.Marshal(args)
	if string(after) != string(original) {
		t.Fatal("feedback coerced or changed the proposed arguments")
	}
	response, err = callback(validationFeedbackContext{err: context.Canceled}, validationFeedbackTool{}, args, runErr)
	if err != nil || response != nil {
		t.Fatal("cancelled callback generated recovery feedback", response, err)
	}
}

func TestToolArgumentGuidanceUsesOnlyActualTypesAndDeclaredBounds(t *testing.T) {
	const ranges = `{"type":"object","properties":{"startLine":{"type":"integer","minimum":3,"maximum":80},"lineCount":{"type":"integer","minimum":2,"maximum":40}}}`
	for _, tc := range []struct {
		name, tool, schema, args string
		want                     []string
	}{
		{"both_active_bounds", "read_file", ranges, `{"startLine":0,"lineCount":1000}`, []string{"startLine must be an integer from 3 to 80", "lineCount must be an integer from 2 to 40"}},
		{"fractional_integer", "read_file", ranges, `{"lineCount":2.5}`, []string{"lineCount must be an integer from 2 to 40"}},
		{"valid_boundary", "read_file", ranges, `{"startLine":3,"lineCount":40}`, nil},
		{"wrong_numeric_type", "read_file", ranges, `{"lineCount":"1000"}`, nil},
		{"number_not_integer_schema", "read_file", `{"properties":{"lineCount":{"type":"number","minimum":1,"maximum":40}}}`, `{"lineCount":1000}`, nil},
		{"exclusive_bound", "read_file", `{"properties":{"lineCount":{"type":"integer","minimum":1,"maximum":40,"exclusiveMinimum":1}}}`, `{"lineCount":0}`, nil},
		{"missing_bounds", "read_file", `{"properties":{"lineCount":{"type":"integer"}}}`, `{"lineCount":1.5}`, nil},
		{"quoted_argv", "run_command", commandFeedbackSchema, `{"args":"[\"-m\",\"unittest\"]"}`, []string{"JSON array of strings", `"args":["-m","unittest","-v"]`}},
		{"valid_argv", "run_command", commandFeedbackSchema, `{"args":["-m","unittest"]}`, nil},
		{"invalid_array_element", "run_command", commandFeedbackSchema, `{"args":[5]}`, nil},
		{"different_declared_type", "run_command", `{"properties":{"args":{"type":"string"}}}`, `{"args":"shell command"}`, nil},
		{"different_item_type", "run_command", `{"properties":{"args":{"type":"array","items":{"type":"integer"}}}}`, `{"args":"[1]"}`, nil},
		{"field_reference", "run_command", `{"$defs":{"argv":{"type":"array","items":{"type":"string"}}},"properties":{"args":{"$ref":"#/$defs/argv"}}}`, `{"args":"[]"}`, nil},
		{"root_reference", "run_command", `{"$defs":{"command":{"type":"object"}},"$ref":"#/$defs/command","properties":{"args":{"type":"array","items":{"type":"string"}}}}`, `{"args":"[]"}`, nil},
		{"foreign_tool", "mcp_call", commandFeedbackSchema, `{"args":"[]"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.args), &args); err != nil {
				t.Fatal(err)
			}
			guidance := toolArgumentGuidance(tc.tool, feedbackSchema(t, tc.schema).Schema(), args)
			if len(tc.want) == 0 && guidance != "" {
				t.Fatal("invented a correction for an unsupported or valid shape", guidance)
			}
			for _, want := range tc.want {
				if !strings.Contains(guidance, want) {
					t.Fatalf("active constraint %q missing from %q", want, guidance)
				}
			}
		})
	}
}

func validationResponse(t *testing.T, request inference.ChatRequest, callID string) map[string]any {
	t.Helper()
	last := request.Messages[len(request.Messages)-1]
	if last.Role != "tool" || last.ToolCallID != callID || last.ToolName != "run_command" {
		t.Errorf("function result lost its original pairing: %+v", last)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(last.Content), &response); err != nil {
		t.Error(err)
	}
	return response
}

func TestSchemaFeedbackRecoveryPreservesAuthorityAndToolBudget(t *testing.T) {
	const invalid = `["-c","do not execute this string"]`
	valid := []any{"-c", "print('literal space; \\n')\n# keep exact newline", "argument with spaces"}
	var modelCalls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		if !strings.Contains(systemText(request), "Workspace is read-only; retain every constraint.") || len(request.Tools) != 1 || request.Tools[0].Function.Name != "run_command" {
			t.Error("schema feedback changed system instructions or tool authority")
		}
		switch modelCalls.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(inference.ChatChunk{Done: true, Message: inference.Message{ToolCalls: []inference.ToolCall{{ID: "bad", Function: inference.ToolFunction{Name: "run_command", Arguments: map[string]any{"executable": "python3", "args": invalid, "readOnly": false}}}}}})
		case 2:
			response := validationResponse(t, request, "bad")
			if response["isError"] != true || response["executed"] != false || response["stage"] != "schema_validation" || !strings.Contains(fmt.Sprint(response["guidance"]), "JSON array of strings") {
				t.Error("missing actionable rejection", response)
			}
			proposal := request.Messages[len(request.Messages)-2].ToolCalls
			if len(proposal) != 1 || proposal[0].ID != "bad" || proposal[0].Function.Arguments["args"] != invalid {
				t.Error("original rejected proposal changed", proposal)
			}
			_ = json.NewEncoder(w).Encode(inference.ChatChunk{Done: true, Message: inference.Message{ToolCalls: []inference.ToolCall{{ID: "good", Function: inference.ToolFunction{Name: "run_command", Arguments: map[string]any{"executable": "python3", "args": valid, "readOnly": false}}}}}})
		default:
			fmt.Fprintln(w, `{"message":{"content":"Used the approved read-only tool."},"done":true}`)
		}
	})
	budget := &Budget{MaxModelCalls: 3, MaxToolCalls: 1, MaxOutputBytes: 1 << 20}
	hostCalls := 0
	result, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Instructions: "Workspace is read-only; retain every constraint.", Prompt: "Inspect only.", ReadOnly: true, NodeID: "qa", CapabilityID: "frozen-capability", MaxSteps: 3, MaxToolCalls: 1, Tools: []ToolDefinition{feedbackDefinition(t)}}, nil, func(_ context.Context, call ToolCall) (ToolResult, error) {
		hostCalls++
		if call.ID != "good" || !call.ReadOnly || call.NodeID != "qa" || call.CapabilityID != "frozen-capability" || !reflect.DeepEqual(call.Arguments["args"], valid) {
			t.Error("recovery changed authority or exact argv", call)
		}
		return ToolResult{Data: map[string]any{"readOnly": true, "exitCode": 0}}, nil
	})
	if err != nil || hostCalls != 1 || budget.ToolCalls != 1 || budget.ModelCalls != 3 || result.Steps != 3 || result.Content != "Used the approved read-only tool." {
		t.Fatalf("rejected proposal executed or consumed a tool allowance: result=%+v err=%v host=%d budget=%+v", result, err, hostCalls, budget)
	}
}

func TestSchemaFeedbackDoesNotRelabelExecutorOrToolBudgetFailures(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		n := calls.Add(1)
		if n > 1 {
			response := validationResponse(t, request, fmt.Sprintf("call-%d", n-1))
			if response["guidance"] != nil || response["executed"] != nil || response["stage"] != nil {
				t.Error("execution/budget failure was relabeled as schema rejection", response)
			}
			want := "executor refused approval"
			if n == 3 {
				want = "maximum tool calls"
			}
			if !strings.Contains(fmt.Sprint(response["error"]), want) {
				t.Error("original failure lost", response)
			}
		}
		if n == 3 {
			fmt.Fprintln(w, finishedReply)
			return
		}
		fmt.Fprintf(w, `{"message":{"tool_calls":[{"id":"call-%d","function":{"name":"run_command","arguments":{"executable":"python3","args":["-V"]}}}]},"done":true}`+"\n", n)
	})
	budget := &Budget{MaxModelCalls: 3, MaxToolCalls: 1, MaxOutputBytes: 1 << 20}
	hostCalls := 0
	_, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Prompt: "Inspect", MaxSteps: 3, MaxToolCalls: 1, Tools: []ToolDefinition{feedbackDefinition(t)}}, nil, func(context.Context, ToolCall) (ToolResult, error) {
		hostCalls++
		return ToolResult{}, errors.New("executor refused approval")
	})
	if err != nil || hostCalls != 1 || budget.ToolCalls != 1 || calls.Load() != 3 {
		t.Fatal("failure feedback changed execution limits", err, hostCalls, budget)
	}
}

func TestRepeatedSchemaFailuresRemainWithinSharedModelBudget(t *testing.T) {
	var modelCalls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		modelCalls.Add(1)
		if len(request.Tools) == 0 {
			fmt.Fprintln(w, `{"message":{"content":"No command executed; arguments remain invalid."},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"function":{"name":"run_command","arguments":{"executable":"python3","args":"[]"}}}]},"done":true}`)
	})
	budget := &Budget{MaxModelCalls: 2, MaxToolCalls: 1, MaxOutputBytes: 1 << 20}
	hostCalls := 0
	result, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Prompt: "Inspect", MaxSteps: 2, MaxToolCalls: 1, Tools: []ToolDefinition{feedbackDefinition(t)}}, nil, func(context.Context, ToolCall) (ToolResult, error) {
		hostCalls++
		return ToolResult{}, nil
	})
	// The pre-existing tools-free final reporting allowance is the third call.
	if !errors.Is(err, ErrBudgetExceeded) || hostCalls != 0 || budget.ToolCalls != 0 || modelCalls.Load() != 3 || result.Steps != 3 || result.Content != "No command executed; arguments remain invalid." {
		t.Fatalf("schema recovery escaped budgets: result=%+v err=%v host=%d budget=%+v", result, err, hostCalls, budget)
	}
}
