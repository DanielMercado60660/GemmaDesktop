package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type cycleScriptBackend struct {
	requests []inference.CompletionRequest
	script   func(context.Context, int, func(inference.ChatChunk) error, []func(inference.ChatChunk)) error
}

func (*cycleScriptBackend) Descriptor() inference.BackendDescriptor {
	return inference.BackendDescriptor{Provider: inference.ProviderOllama}
}
func (*cycleScriptBackend) ResolveModel(context.Context, string) (inference.CompletionModel, error) {
	return inference.CompletionModel{Name: "fixture"}, nil
}
func (b *cycleScriptBackend) Generate(ctx context.Context, req inference.CompletionRequest, emit func(inference.ChatChunk) error, observe ...func(inference.ChatChunk)) error {
	b.requests = append(b.requests, req)
	return b.script(ctx, len(b.requests), emit, observe)
}
func cycleFinal(emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk), message inference.Message, reason string) error {
	chunk := inference.ChatChunk{Message: message, Done: true, DoneReason: reason, Metrics: inference.Metrics{PromptEvalCount: 10, EvalCount: 2}, TokenCountsPresent: true}
	for _, fn := range observe {
		fn(chunk)
	}
	return emit(chunk)
}
func cycleRequest() Request {
	return Request{Model: "fixture", Instructions: "Keep the same permissions.", Prompt: "Review the evidence", MaxSteps: 5, HarnessMechanisms: HarnessMechanisms{ThinkingCycleGuard: true}}
}

func TestThinkingCycleRetryPreservesCompletedToolsAndDiscardsProposals(t *testing.T) {
	b := &cycleScriptBackend{}
	b.script = func(_ context.Context, n int, emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk)) error {
		switch n {
		case 1:
			return cycleFinal(emit, observe, inference.Message{ToolCalls: []inference.ToolCall{{ID: "already-done", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{}}}}}, "stop")
		case 2:
			if err := emit(inference.ChatChunk{Message: inference.Message{Content: "DISCARDED-PARTIAL", ToolCalls: []inference.ToolCall{{ID: "discarded-proposal", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{}}}}}}); err != nil {
				return err
			}
			return emit(inference.ChatChunk{Message: inference.Message{Thinking: cycleFixture() + "observed remainder: \u00e9\n"}})
		case 3:
			return cycleFinal(emit, observe, inference.Message{ToolCalls: []inference.ToolCall{{ID: "fresh-call", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{}}}}}, "stop")
		default:
			return cycleFinal(emit, observe, inference.Message{Content: "Done."}, "stop")
		}
	}
	var events []Event
	executed := map[string]int{}
	req := cycleRequest()
	req.Tools = []ToolDefinition{{Name: "inspect"}}
	result, err := New(b).Run(t.Context(), req, func(e Event) { events = append(events, e) }, func(_ context.Context, call ToolCall) (ToolResult, error) {
		executed[call.ID]++
		return ToolResult{Content: "observed"}, nil
	})
	if err != nil || result.Incomplete || result.Content != "Done." || result.Steps != 4 || len(b.requests) != 4 || len(executed) != 2 || executed["already-done"] != 1 || executed["fresh-call"] != 1 {
		t.Fatalf("unsafe recovery: err=%v result=%+v executed=%v", err, result, executed)
	}
	if result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 4 || result.TokenAccounting.MeasuredResponses != 3 || result.Usage.EvalCount != 6 {
		t.Fatal("interrupted usage falsely complete or duplicated", result.TokenAccounting, result.Usage)
	}
	if strings.Contains(string(result.Snapshot), "DISCARDED-PARTIAL") || strings.Contains(string(result.Snapshot), "discarded-proposal") || strings.Contains(string(result.Snapshot), thinkingCycleFeedback) {
		t.Fatal("discarded content/feedback entered history")
	}
	base, retry := b.requests[1], b.requests[2]
	if len(retry.Messages) != len(base.Messages)+1 || retry.Messages[len(retry.Messages)-1].Content != thinkingCycleFeedback {
		t.Fatal("missing request-local retry suffix")
	}
	before, _ := json.Marshal(base.Messages)
	prefix, _ := json.Marshal(retry.Messages[:len(base.Messages)])
	if string(before) != string(prefix) {
		t.Fatal("completed conversation changed on retry")
	}
	retries, interruptions := 0, 0
	for _, e := range events {
		if e.Type == "generation_retry" {
			retries++
			if e.Data.(map[string]any)["reason"] != "thinking_cycle" {
				t.Fatal("wrong retry classification")
			}
		}
		if e.Type == "thinking_cycle_interrupted" {
			interruptions++
			d := e.Data.(map[string]any)
			if d["cleanInterruption"] != true || d["stagedToolCallsDiscarded"] != 1 || d["terminalReceipt"] != false {
				t.Fatal("ambiguous interruption audit", d)
			}
			if d["guard"].(thinkingCycleMatch).BytesSeen != 2048 || d["thinkingBytesObserved"] != 2071 || d["thinkingCharactersObserved"] != 2070 {
				t.Fatal("trigger prefix conflated with complete observed chunk", d)
			}
		}
	}
	if retries != 1 || interruptions != 1 {
		t.Fatal("missing audit events", retries, interruptions)
	}
}

func TestThinkingCycleSharesExistingRetryAndModelBudgets(t *testing.T) {
	for _, sequence := range [][]string{{"cycle", "cycle", "cycle"}, {"empty", "length", "cycle"}, {"cycle", "empty", "length"}} {
		t.Run(strings.Join(sequence, "_"), func(t *testing.T) {
			b := &cycleScriptBackend{script: func(_ context.Context, n int, emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk)) error {
				switch sequence[min(n-1, len(sequence)-1)] {
				case "cycle":
					return emit(inference.ChatChunk{Message: inference.Message{Thinking: cycleFixture()}})
				case "empty":
					return cycleFinal(emit, observe, inference.Message{}, "stop")
				default:
					return cycleFinal(emit, observe, inference.Message{Content: "partial"}, "length")
				}
			}}
			retries := 0
			result, err := New(b).Run(t.Context(), cycleRequest(), func(e Event) {
				if e.Type == "generation_retry" {
					retries++
				}
			}, nil)
			if !errors.Is(err, ErrIncompleteResponse) || !result.Incomplete || len(b.requests) != 3 || retries != 2 || result.Steps != 3 {
				t.Fatalf("retry budget expanded: %v %+v requests=%d retries=%d", err, result, len(b.requests), retries)
			}
		})
	}
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("one-call/shared=%v", shared), func(t *testing.T) {
			b := &cycleScriptBackend{script: func(_ context.Context, _ int, emit func(inference.ChatChunk) error, _ []func(inference.ChatChunk)) error {
				return emit(inference.ChatChunk{Message: inference.Message{Thinking: cycleFixture()}})
			}}
			req := cycleRequest()
			req.MaxSteps = 1
			ctx := t.Context()
			if shared {
				req.MaxSteps = 5
				ctx = WithBudget(ctx, &Budget{MaxModelCalls: 1, MaxToolCalls: 4, MaxOutputBytes: 1 << 20})
			}
			result, err := New(b).Run(ctx, req, nil, nil)
			if !errors.Is(err, ErrThinkingCycle) || !result.Incomplete || result.Content != "" || len(b.requests) != 1 {
				t.Fatal("exhausted call budget retried", err, result)
			}
		})
	}
}

func TestThinkingCycleDoesNotRetryCancellationCleanupOrIgnoredStop(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline", "joined cleanup", "wrapped transport", "ignored stop", "output budget", "output budget exact"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			b := &cycleScriptBackend{script: func(_ context.Context, _ int, emit func(inference.ChatChunk) error, _ []func(inference.ChatChunk)) error {
				err := emit(inference.ChatChunk{Message: inference.Message{Thinking: cycleFixture()}})
				switch kind {
				case "cancel":
					cancel()
					return err
				case "deadline":
					return errors.Join(err, context.DeadlineExceeded)
				case "joined cleanup":
					return errors.Join(err, errors.New("native cleanup failed"))
				case "wrapped transport":
					return fmt.Errorf("transport: %w", err)
				case "ignored stop":
					return nil
				default:
					return err
				}
			}}
			if kind == "output budget" {
				ctx = WithBudget(ctx, &Budget{MaxModelCalls: 5, MaxToolCalls: 4, MaxOutputBytes: 1000})
			} else if kind == "output budget exact" {
				ctx = WithBudget(ctx, &Budget{MaxModelCalls: 5, MaxToolCalls: 4, MaxOutputBytes: len(cycleFixture())})
			}
			_, err := New(b).Run(ctx, cycleRequest(), nil, nil)
			if err == nil || len(b.requests) != 1 {
				t.Fatalf("unclean stop retried: %v requests=%d", err, len(b.requests))
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if strings.HasPrefix(kind, "output budget") && !errors.Is(err, ErrBudgetExceeded) {
				t.Fatal("output budget lost", err)
			}
		})
	}
}

func TestThinkingCycleRetainsReceiptBeforeInterruptedTerminalChunk(t *testing.T) {
	b := &cycleScriptBackend{script: func(_ context.Context, n int, emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk)) error {
		message := inference.Message{Content: "Recovered."}
		if n == 1 {
			message = inference.Message{Thinking: cycleFixture()}
		}
		return cycleFinal(emit, observe, message, "stop")
	}}
	result, err := New(b).Run(t.Context(), cycleRequest(), nil, nil)
	if err != nil || result.Content != "Recovered." || len(b.requests) != 2 || !result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 2 || result.TokenAccounting.MeasuredResponses != 2 || result.Usage.EvalCount != 4 || result.Usage.PromptEvalCount != 20 {
		t.Fatal("terminal receipt lost or counted twice", result, err)
	}
}

func TestThinkingCycleOptInAndChannelExclusions(t *testing.T) {
	for _, kind := range []string{"off", "visible", "tool arguments"} {
		t.Run(kind, func(t *testing.T) {
			req := cycleRequest()
			req.MaxSteps = 2
			req.Tools = []ToolDefinition{{Name: "inspect"}}
			if kind == "off" {
				req.HarnessMechanisms.ThinkingCycleGuard = false
			}
			b := &cycleScriptBackend{script: func(_ context.Context, n int, emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk)) error {
				msg := inference.Message{Content: "Done."}
				if n == 1 {
					switch kind {
					case "off":
						msg.Thinking = cycleFixture()
					case "visible":
						msg.Content = cycleFixture()
					case "tool arguments":
						msg.Content = ""
						msg.ToolCalls = []inference.ToolCall{{ID: "legitimate", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{"text": cycleFixture()}}}}
					}
				}
				return cycleFinal(emit, observe, msg, "stop")
			}}
			interruptions := 0
			_, err := New(b).Run(t.Context(), req, func(e Event) {
				if e.Type == "thinking_cycle_interrupted" {
					interruptions++
				}
			}, func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{Content: "observed"}, nil })
			if err != nil || interruptions != 0 {
				t.Fatal("excluded channel interrupted", err, interruptions)
			}
		})
	}
	for _, summary := range []bool{true, false} {
		t.Run(fmt.Sprintf("summary=%v", summary), func(t *testing.T) {
			b := &cycleScriptBackend{script: func(_ context.Context, _ int, emit func(inference.ChatChunk) error, observe []func(inference.ChatChunk)) error {
				return cycleFinal(emit, observe, inference.Message{Thinking: cycleFixture(), Content: "summary or status"}, "stop")
			}}
			m := &ollamaModel{client: b, request: cycleRequest(), budget: &Budget{MaxModelCalls: 5, MaxOutputBytes: 1 << 20}}
			if summary {
				m.outputTokenLimit = 256
			}
			input := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("task", genai.RoleUser)}}
			for _, err := range m.generateAttempt(t.Context(), input, false, 1, !summary) {
				if err != nil {
					t.Fatal("maintenance/report interrupted", err)
				}
			}
		})
	}
}

func TestThinkingCycleNativeCleanupAndPartialToolDiscard(t *testing.T) {
	for _, provider := range []inference.ProviderKind{inference.ProviderOMLX, inference.ProviderMLXServe} {
		for _, failCleanup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failCleanup=%v", provider, failCleanup), func(t *testing.T) {
				var calls, cleanups, executions atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if calls.Add(1) == 1 {
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"partial-never-executes\",\"function\":{\"name\":\"inspect\",\"arguments\":\"{\"}}]}}]}\n\n")
						thought, _ := json.Marshal(cycleFixture())
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":%s}}]}\n\n", thought)
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						return
					}
					if cleanups.Load() != 1 {
						t.Error("retry arrived before cleanup")
					}
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Recovered.\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\ndata: [DONE]\n\n")
				}))
				defer srv.Close()
				gate := inference.NewAdmissionGate()
				config := inference.NativeConfig{BaseURL: srv.URL, Gate: gate, Models: []inference.CompletionModel{{Name: "fixture", ArtifactDigest: strings.Repeat("a", 64), ContextTokens: 32768, Capabilities: inference.CompletionCapabilities{Completion: true, Tools: true, Thinking: true}}}, Quiesce: func(context.Context) error {
					cleanups.Add(1)
					if failCleanup {
						return errors.New("fixture cleanup failed")
					}
					return nil
				}}
				var backend *inference.NativeBackend
				var err error
				if provider == inference.ProviderOMLX {
					backend, err = inference.NewOMLXBackend(config)
				} else {
					backend, err = inference.NewMLXServeBackend(config)
				}
				if err != nil {
					t.Fatal(err)
				}
				req := cycleRequest()
				req.ContextSize = 32768
				req.Tools = []ToolDefinition{{Name: "inspect"}}
				result, err := New(backend).Run(t.Context(), req, nil, func(context.Context, ToolCall) (ToolResult, error) { executions.Add(1); return ToolResult{}, nil })
				if executions.Load() != 0 || cleanups.Load() != 1 || strings.Contains(string(result.Snapshot), "partial-never-executes") {
					t.Fatal("native interrupted proposal gained authority", result, err)
				}
				if failCleanup {
					if err == nil || !strings.Contains(err.Error(), "cleanup failed") || calls.Load() != 1 || gate.BlockedReason() == "" {
						t.Fatal("native cleanup failure retried or masked", err, calls.Load())
					}
				} else {
					if err != nil || result.Content != "Recovered." || calls.Load() != 2 || result.TokenAccounting.Complete() || result.TokenAccounting.MeasuredResponses != 1 {
						t.Fatal("native recovery/accounting failed", result, err)
					}
				}
			})
		}
	}
}
