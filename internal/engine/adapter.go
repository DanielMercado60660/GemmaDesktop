package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

var ErrBudgetExceeded = errors.New("execution budget exceeded")
var ErrIncompleteResponse = errors.New("model response is incomplete")
var ErrEmptyResponse = errors.New("model stopped without an answer or tool call")
var ErrInvalidStructuredAction = errors.New("model returned an invalid structured action")

// errModelCallLimit is the per-call budget check. It, an empty turn without a
// retry slot, or an explicit deadline reserve may use the single closing status
// allowance. Output/tool-call failures do not independently earn that allowance.
var errModelCallLimit = fmt.Errorf("%w: maximum model calls reached", ErrBudgetExceeded)
var errFinishReserve = fmt.Errorf("%w: run entered its final-answer reserve", ErrBudgetExceeded)

type deadlineReportKey struct{}

// budgetReportError ends a run whose model-call budget ran out with the status
// the model wrote in its one tools-free reporting generation. The text is a
// complete message, never an incomplete one; the run stays a budget failure.
type budgetReportError struct {
	content string
	cause   error
}

func (e *budgetReportError) Error() string {
	return e.cause.Error() + "; final step was a status report"
}
func (e *budgetReportError) Unwrap() error { return e.cause }

// budgetReportKey carries the outermost run's single reporting allowance. Runs
// nested in its tools (delegates, workflow nodes) see nil and fail plainly, so
// parallel children cannot each spend an extra call; the outer run reports.
type budgetReportKey struct{}
type budgetReport struct{ used atomic.Bool }

type incompleteResponseError struct {
	content string
	usage   inference.Metrics
	cause   error
	// lengthLimit marks an ordinary generation stopped by its output bound.
	lengthLimit bool
	// thinkingCycle is set only after a callback-owned interruption returns
	// cleanly from the backend; runtime cleanup/cancellation failures bypass it.
	thinkingCycle bool
}

func (e *incompleteResponseError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "model response is incomplete: runtime reached its generation length limit"
}
func (e *incompleteResponseError) Unwrap() error        { return ErrIncompleteResponse }
func (e *incompleteResponseError) Is(target error) bool { return errors.Is(e.cause, target) }

type ollamaModel struct {
	client  inference.CompletionBackend
	request Request
	budget  *Budget
	emit    func(Event)
	usage   inference.Metrics
	calls   int
	// A dedicated model bound is necessary for context summaries: ADK's
	// summarizer deliberately removes an inherited MaxOutputTokens setting.
	outputTokenLimit int32
	// A coarse summary-only size guard. Unlike ADK's transcript shrinking,
	// exceeding this bound declines compaction without silently dropping text.
	// Characters are not tokens; the inference client also enforces its byte cap.
	transcriptCharacterLimit int
}

func (m *ollamaModel) Name() string { return m.request.Model }

const emptyGenerationFeedback = "\n\nHost protocol feedback: The preceding generation returned no visible answer and no usable tool call. Produce either a complete answer or a valid tool call whose arguments satisfy the supplied JSON schema. This feedback does not authorize actions, expand scope, or approve execution. Preserve prior observations; do not repeat completed tool actions merely because of this retry."
const lengthGenerationFeedback = "\n\nHost protocol feedback: Your previous generation reached the output length limit before producing a complete answer or tool call, so it was discarded. Keep any reasoning brief. Do not work through many rows, calculations or items in your reasoning; when a tool can do it, write a short script and run it (use python3 -c if the user asked you not to create files), or write the result to a file in steps. Respond now with exactly one tool call, or a short final answer. This feedback does not authorize actions or expand scope."
const thinkingCycleFeedback = "\n\nHost protocol feedback: The previous generation repeated the same thinking block and was interrupted. Its unfinished answer and tool proposals were discarded. Use the observations already available to produce one concise valid next action or a short answer. Preserve all user constraints and permissions; do not repeat completed tool actions or assume an unfinished proposal executed."

// Gemma 4 through Ollama can return two empty turns (its tool-call parser
// yields nothing) or two length stops in a row, so one retry is not enough.
// The second escalates to the shortest valid reply.
const maxGenerationRetries = 2
const secondRetryFeedback = " Second retry: respond with exactly one short tool call or a two-sentence answer; no long reasoning."
const budgetReportFeedback = "\n\nHost budget notice: this run has used its model-call budget, so no more tools can run. Reply now with a brief status for the user: what you changed or found, what you verified (only with tool evidence from this run), and what remains unfinished. Do not claim anything you did not observe. This notice does not authorize actions or expand scope."
const deadlineReportFeedback = "\n\nHost deadline notice: this run has entered its reserved closing window, so no more tools can run. Give a brief, honest status using only observations already available: what you found or changed, what was verified, and what remains unfinished or uncertain. The run remains incomplete because its time budget is ending. Do not invent results or claim that an unfinished task is complete. This notice grants no permissions."

func (m *ollamaModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		current := req
		var discarded *inference.Metrics
		for attempt := 0; attempt <= maxGenerationRetries; attempt++ {
			retry := false
			var limit error
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			if m.outputTokenLimit == 0 && m.request.FinishReserveSeconds > 0 {
				if deadline, present := ctx.Deadline(); present {
					remaining := time.Until(deadline)
					if remaining <= 0 {
						yield(nil, context.DeadlineExceeded)
						return
					}
					if remaining <= time.Duration(m.request.FinishReserveSeconds)*time.Second {
						limit = errFinishReserve
					}
				}
			}
			if limit == nil {
				for response, err := range m.generateAttempt(ctx, current, stream, attempt+1, false) {
					if err != nil {
						var incomplete *incompleteResponseError
						empty := errors.Is(err, ErrEmptyResponse)
						if m.outputTokenLimit == 0 && errors.Is(err, errModelCallLimit) {
							limit = err
							break
						}
						if attempt < maxGenerationRetries && m.outputTokenLimit == 0 && errors.As(err, &incomplete) && (empty || incomplete.lengthLimit || incomplete.thinkingCycle) {
							usage := incomplete.usage
							if discarded != nil {
								usage = addGenerationUsage(*discarded, usage)
							}
							discarded = &usage
							if err := ctx.Err(); err != nil {
								yield(nil, err)
								return
							}
							if !m.canGenerate() {
								if !empty {
									// Without a retry a truncated generation keeps its
									// incomplete classification and partial text.
									yield(nil, err)
									return
								}
								// The budget ran out on a turn with nothing to show, which
								// leaves the user exactly the bare budget error the report
								// exists to replace (Gemma 4's empty tool-call parse can
								// land on any step, including the last).
								limit = fmt.Errorf("%w: no model call remains for an empty-response retry", ErrBudgetExceeded)
								break
							}
							title, detail, reason, feedback := "Retrying empty generation", "No answer or usable tool call was returned.", "empty_response", emptyGenerationFeedback
							if !empty {
								title, detail, reason, feedback = "Retrying truncated generation", "The generation reached its output length limit and was discarded.", "length", lengthGenerationFeedback
							}
							if incomplete.thinkingCycle {
								title, detail, reason, feedback = "Retrying interrupted thinking", "The experimental guard interrupted an exact thinking cycle and discarded the unfinished generation.", "thinking_cycle", thinkingCycleFeedback
							}
							if attempt > 0 {
								feedback += secondRetryFeedback
							}
							detail += fmt.Sprintf(" Retry %d of %d will use the same history and existing permissions.", attempt+1, maxGenerationRetries)
							if m.emit != nil {
								m.emit(Event{Type: "generation_retry", Title: title, NodeID: m.request.NodeID, Detail: detail, Data: map[string]any{"model": m.Name(), "reason": reason, "attempt": attempt + 2, "maxRetries": maxGenerationRetries}})
							}
							// Feedback replaces, never stacks on, the previous retry's.
							current = requestWithRetryFeedback(req, feedback)
							retry = true
							break
						}
						yield(nil, err)
						return
					}
					if response != nil && !response.Partial && discarded != nil {
						// Keep the successful response's actual prompt count intact for
						// ADK compaction. Discarded-attempt cost is separate audit metadata.
						if response.CustomMetadata == nil {
							response.CustomMetadata = map[string]any{}
						}
						response.CustomMetadata["discarded_generation_usage"] = *discarded
					}
					if !yield(response, nil) {
						return
					}
				}
			}
			if limit != nil {
				if report, _ := ctx.Value(budgetReportKey{}).(*budgetReport); report != nil && (!errors.Is(limit, errFinishReserve) || m.reportHasOutputBudget()) && report.used.CompareAndSwap(false, true) {
					m.reportStatus(ctx, req, stream, limit, yield)
					return
				}
				yield(nil, limit)
				return
			}
			if !retry {
				return
			}
		}
	}
}

// reportStatus spends a run's one reporting generation after model-call
// exhaustion or entry into its optional deadline reserve. The user receives
// the model's own status instead of a bare budget error. It sends the same history with no tools and a host
// notice; tool calls are dropped, never executed. Its text ends the run as a
// budget failure. A report that fails, is empty or is cut off fails exactly as
// the budget check would have, without retries.
func (m *ollamaModel) reportStatus(ctx context.Context, req *model.LLMRequest, stream bool, limit error, yield func(*model.LLMResponse, error) bool) {
	feedback, title, reason := budgetReportFeedback, "Writing budget status report", "model_calls"
	if errors.Is(limit, errFinishReserve) {
		feedback, title, reason = deadlineReportFeedback, "Writing deadline status report", "deadline_reserve"
		ctx = context.WithValue(ctx, deadlineReportKey{}, true)
	}
	if m.emit != nil {
		m.budget.mu.Lock()
		data := map[string]any{"model": m.Name(), "maxSteps": m.request.MaxSteps, "maxModelCalls": m.budget.MaxModelCalls, "reason": reason, "finishReserveSeconds": m.request.FinishReserveSeconds}
		m.budget.mu.Unlock()
		m.emit(Event{Type: "budget_report", Title: title, NodeID: m.request.NodeID, Detail: "One final generation without tools will report observed status; the run still ends as a budget failure.", Data: data})
	}
	for response, err := range m.generateAttempt(ctx, requestWithFeedback(req, feedback), stream, 1, true) {
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				yield(nil, fmt.Errorf("%w; the status report was interrupted: %w", limit, ctxErr))
				return
			}
			// %v: a cut-off report must not become an incomplete answer.
			yield(nil, fmt.Errorf("%w; the status report failed: %v", limit, err))
			return
		}
		if response.Partial {
			if !yield(response, nil) {
				return
			}
			continue
		}
		var text strings.Builder
		for _, part := range response.Content.Parts {
			if part != nil && !part.Thought {
				text.WriteString(part.Text)
			}
		}
		yield(nil, &budgetReportError{content: text.String(), cause: limit})
		return
	}
}

// remainingCalls is how many model calls this agent may still make under both
// its own step bound and the shared run budget.
func (m *ollamaModel) remainingCalls() int {
	m.budget.mu.Lock()
	defer m.budget.mu.Unlock()
	return min(m.budget.MaxModelCalls-m.budget.ModelCalls, m.request.MaxSteps-m.calls)
}
func (m *ollamaModel) canGenerate() bool { return m.remainingCalls() > 0 }

func (m *ollamaModel) reportHasOutputBudget() bool {
	m.budget.mu.Lock()
	defer m.budget.mu.Unlock()
	return m.budget.OutputBytes < m.budget.MaxOutputBytes
}

// Retry feedback is request-local protocol context, appended after the complete
// existing conversation. Keeping the original system instruction and message
// prefix byte-for-byte retains complete tool pairs and leaves that prefix
// available for provider reuse; actual template/cache behavior is runtime-owned.
// The fresh slice also avoids mutating ADK's history or an earlier retry, and the
// synthetic message never enters the session or the original-user archive.
func requestWithRetryFeedback(req *model.LLMRequest, feedback string) *model.LLMRequest {
	copy := *req
	copy.Contents = append(make([]*genai.Content, 0, len(req.Contents)+1), req.Contents...)
	copy.Contents = append(copy.Contents, genai.NewContentFromText(feedback, genai.RoleUser))
	return &copy
}

func requestWithFeedback(req *model.LLMRequest, feedback string) *model.LLMRequest {
	copy := *req
	config := genai.GenerateContentConfig{}
	if req.Config != nil {
		config = *req.Config
	}
	instruction := genai.Content{}
	if config.SystemInstruction != nil {
		instruction = *config.SystemInstruction
		instruction.Parts = append([]*genai.Part(nil), instruction.Parts...)
	}
	instruction.Parts = append(instruction.Parts, &genai.Part{Text: feedback})
	config.SystemInstruction = &instruction
	copy.Config = &config
	return &copy
}

func addGenerationUsage(a, b inference.Metrics) inference.Metrics {
	a.TotalDuration += b.TotalDuration
	a.LoadDuration += b.LoadDuration
	a.PromptEvalCount += b.PromptEvalCount
	a.PromptEvalDuration += b.PromptEvalDuration
	a.EvalCount += b.EvalCount
	a.EvalDuration += b.EvalDuration
	return a
}

// generateAttempt makes one provider call. A report call is the run's single
// tools-free status generation after the budget check failed: it is counted
// like any call, so steps and usage stay complete, but is not refused by it.
func (m *ollamaModel) generateAttempt(ctx context.Context, req *model.LLMRequest, stream bool, attempt int, report bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		m.budget.mu.Lock()
		if !report && (m.budget.ModelCalls >= m.budget.MaxModelCalls || m.calls >= m.request.MaxSteps) {
			m.budget.mu.Unlock()
			yield(nil, fmt.Errorf("%w (agent %d, run %d)", errModelCallLimit, m.request.MaxSteps, m.budget.MaxModelCalls))
			return
		}
		m.budget.ModelCalls++
		m.calls++
		step := m.budget.ModelCalls
		m.budget.mu.Unlock()
		if m.emit != nil {
			m.emit(Event{Type: "status", Title: "Generating", NodeID: m.request.NodeID, Data: map[string]any{"step": step, "model": m.Name()}})
		}
		chat, err := toOllama(req, m.request)
		if err != nil {
			yield(nil, err)
			return
		}
		if m.outputTokenLimit > 0 {
			chat.Options["num_predict"] = m.outputTokenLimit
		}
		if report {
			// No declared tools and no action protocol: the report is prose only.
			chat.Tools = nil
			if ctx.Value(deadlineReportKey{}) == true {
				chat.Options["num_predict"] = min(chat.Options["num_predict"].(int), 1024)
			}
		}
		var protocol *StructuredActionProtocol
		if m.outputTokenLimit == 0 && m.request.ToolProtocol == ToolProtocolStructuredJSON && !report {
			chat, protocol, err = PrepareStructuredActions(chat)
			if err != nil {
				yield(nil, err)
				return
			}
		}
		if m.transcriptCharacterLimit > 0 {
			characters := 0
			for _, message := range chat.Messages {
				characters += utf8.RuneCountInString(message.Content)
			}
			if characters > m.transcriptCharacterLimit {
				yield(nil, fmt.Errorf("summary input is %d characters, above the %d-character guard; original history preserved", characters, m.transcriptCharacterLimit))
				return
			}
		}
		if m.emit != nil {
			m.emit(Event{Type: "context_prepared", Title: "Generation context prepared", NodeID: m.request.NodeID, Data: generationContextAudit(chat, step, attempt, m.outputTokenLimit > 0)})
		}
		var text, thinking strings.Builder
		var contentGuard, thinkingGuard proseRepetitionGuard
		var cycleGuard thinkingCycleGuard
		var cycleStop *thinkingCycleStop
		attemptStarted := time.Now()
		var calls []inference.ToolCall
		dropped := 0
		var metric inference.Metrics
		var usageDetails inference.UsageDetails
		var reason string
		stopped := errors.New("stream consumer stopped")
		terminal, measured := false, false
		recordReceipt := beginProviderUsage(ctx)
		// Account at the provider boundary, before downstream validation or ADK
		// cancellation can replace a response/error and hide consumed tokens.
		defer func() {
			t, n := 0, 0
			if terminal {
				t = 1
			}
			if measured {
				n = 1
			}
			recordReceipt(metric, terminal, measured)
			m.budget.mu.Lock()
			m.usage = addGenerationUsage(m.usage, metric)
			m.budget.mu.Unlock()
			if m.emit != nil {
				m.emit(Event{Type: "token_accounting", Title: "Token measurement", NodeID: m.request.NodeID, Data: map[string]any{"step": step, "attempt": attempt, "summary": m.outputTokenLimit > 0, "tokenAccounting": inference.NewTokenAccounting(1, t, n)}})
			}
		}()
		completion, err := inference.CompletionFromChat(chat)
		if err != nil {
			yield(nil, err)
			return
		}
		err = m.client.Generate(ctx, completion, func(chunk inference.ChatChunk) error {
			if cycleStop != nil {
				return cycleStop
			}
			if m.request.HarnessMechanisms.StreamRepetitionGuard && m.outputTokenLimit == 0 {
				if err := contentGuard.Add(chunk.Message.Content); err != nil {
					return err
				}
				if err := thinkingGuard.Add(chunk.Message.Thinking); err != nil {
					return err
				}
			}
			m.budget.mu.Lock()
			m.budget.OutputBytes += len(chunk.Message.Content) + len(chunk.Message.Thinking)
			tooLarge := m.budget.OutputBytes > m.budget.MaxOutputBytes
			m.budget.mu.Unlock()
			if tooLarge {
				return fmt.Errorf("%w: model output limit", ErrBudgetExceeded)
			}
			if chunk.Message.Content != "" {
				text.WriteString(chunk.Message.Content)
				if protocol == nil && stream && !yield(&model.LLMResponse{Partial: true, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: chunk.Message.Content}}}}, nil) {
					return stopped
				}
			}
			if chunk.Message.Thinking != "" {
				thinking.WriteString(chunk.Message.Thinking)
				if stream && !yield(&model.LLMResponse{Partial: true, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: chunk.Message.Thinking, Thought: true}}}}, nil) {
					return stopped
				}
			}
			for _, call := range chunk.Message.ToolCalls {
				if len(calls)+dropped >= 64 {
					return fmt.Errorf("%w: maximum tool calls in one response", ErrBudgetExceeded)
				}
				if call.Function.Name == "" {
					return errors.New("runtime returned a tool call without a name")
				}
				if call.Function.Arguments == nil {
					call.Function.Arguments = map[string]any{}
				}
				encoded, err := json.Marshal(call)
				if err != nil {
					return err
				}
				m.budget.mu.Lock()
				m.budget.OutputBytes += len(encoded)
				tooLarge := m.budget.OutputBytes > m.budget.MaxOutputBytes
				m.budget.mu.Unlock()
				if tooLarge {
					return fmt.Errorf("%w: model output limit", ErrBudgetExceeded)
				}
				if report {
					// Its output is counted, but a report never reaches a tool.
					dropped++
					continue
				}
				calls = append(calls, call)
			}
			// Count and journal the observed chunk before interruption. Calls
			// remain local proposals until a complete response is accepted below.
			if m.request.HarnessMechanisms.ThinkingCycleGuard && m.outputTokenLimit == 0 && !report {
				if match := cycleGuard.Add(chunk.Message.Thinking); match != nil {
					cycleStop = &thinkingCycleStop{match: *match}
					return cycleStop
				}
			}
			return nil
		}, func(receipt inference.ChatChunk) {
			metric, reason = receipt.Metrics, receipt.DoneReason
			usageDetails = receipt.UsageDetails
			terminal, measured = true, receipt.TokenCountsPresent
			recordReceipt(metric, terminal, measured)
		})
		if errors.Is(err, stopped) {
			return
		}
		if cycleStop != nil {
			clean := err == cycleStop && ctx.Err() == nil
			if m.emit != nil {
				// Measured after Generate returns: elapsedMs includes backend
				// cleanup, and observed thinking can extend past the trigger
				// prefix when one provider chunk contains multiple lines.
				m.emit(Event{Type: "thinking_cycle_interrupted", Title: "Repeated thinking interrupted", NodeID: m.request.NodeID, Detail: "The experimental thinking-only guard interrupted an unfinished provider attempt; no proposed tools from this attempt executed.", Data: map[string]any{
					"model": m.Name(), "step": step, "attempt": attempt, "guard": cycleStop.match,
					"elapsedMs": time.Since(attemptStarted).Milliseconds(), "backend": m.client.Descriptor(),
					"cleanInterruption": clean, "terminalReceipt": terminal, "tokenCountsPresent": measured,
					"thinkingBytesObserved": thinking.Len(), "thinkingCharactersObserved": utf8.RuneCountInString(thinking.String()),
					"stagedToolCallsDiscarded": len(calls), "visibleCharactersDiscarded": utf8.RuneCountInString(text.String()),
				}})
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				yield(nil, ctxErr)
				return
			}
			if clean {
				m.budget.mu.Lock()
				exhausted := m.budget.OutputBytes >= m.budget.MaxOutputBytes
				m.budget.mu.Unlock()
				if exhausted {
					yield(nil, fmt.Errorf("%w: no output allowance remains after thinking-cycle interruption", ErrBudgetExceeded))
					return
				}
				yield(nil, &incompleteResponseError{usage: metric, cause: cycleStop, thinkingCycle: true})
				return
			}
			// Do not retry a joined cleanup failure, wrapped transport error, or
			// a backend that ignored the callback's stop request.
			if err == nil {
				err = errors.New("backend ignored the thinking-cycle interruption")
			}
		}
		if err != nil {
			yield(nil, err)
			return
		}
		rawCharacters := utf8.RuneCountInString(text.String())
		var protocolErr error
		if protocol != nil {
			// Raw action JSON is transport data, not user-facing prose. Decode
			// only a complete response, and never dispatch a native-call fallback.
			raw := text.String()
			text.Reset()
			if reason != "length" && (strings.TrimSpace(raw) != "" || len(calls) > 0) {
				if len(calls) > 0 {
					protocolErr = errors.New("unexpected native tool calls in structured response")
				} else {
					var action StructuredAction
					action, protocolErr = protocol.Decode(raw)
					if protocolErr == nil {
						text.WriteString(action.Answer)
						if action.ToolCall != nil {
							calls = append(calls, *action.ToolCall)
						}
					}
				}
			}
		}
		if m.emit != nil {
			m.emit(Event{Type: "generation_metrics", Title: "Generation usage", NodeID: m.request.NodeID, Data: map[string]any{
				"model": m.Name(), "contextSize": m.request.ContextSize, "maxOutputTokens": chat.Options["num_predict"],
				"step": step, "attempt": attempt,
				"promptTokens": metric.PromptEvalCount, "outputTokens": metric.EvalCount, "doneReason": reason,
				"visibleCharacters": utf8.RuneCountInString(text.String()), "thinkingCharacters": utf8.RuneCountInString(thinking.String()), "toolCalls": len(calls),
				"summary":      m.outputTokenLimit > 0,
				"budgetReport": report, "droppedToolCalls": dropped,
				"toolProtocol": m.request.ToolProtocol, "rawResponseCharacters": rawCharacters,
				"backend": m.client.Descriptor(), "usageDetails": usageDetails,
			}})
		}
		// Do not publish a complete ADK turn or execute tool calls from a
		// truncated ordinary generation. Keep its streamed text as a labelled
		// incomplete result. The summary adapter instead returns MAX_TOKENS to
		// ADK's summarizer, which rejects the summary without replacing history.
		if reason == "length" && m.outputTokenLimit == 0 {
			yield(nil, &incompleteResponseError{content: text.String(), usage: metric, lengthLimit: true})
			return
		}
		if protocolErr != nil {
			yield(nil, &incompleteResponseError{usage: metric, cause: fmt.Errorf("%w: %v", ErrInvalidStructuredAction, protocolErr)})
			return
		}
		// Some runtimes can stop after hidden/filtered thinking without any
		// actionable output. That is not a completed assistant turn.
		if strings.TrimSpace(text.String()) == "" && len(calls) == 0 {
			yield(nil, &incompleteResponseError{usage: metric, cause: ErrEmptyResponse})
			return
		}
		parts := []*genai.Part{}
		if thinking.Len() > 0 {
			parts = append(parts, &genai.Part{Text: thinking.String(), Thought: true})
		}
		if text.Len() > 0 {
			parts = append(parts, &genai.Part{Text: text.String()})
		}
		seen := map[string]bool{}
		for _, call := range calls {
			if call.ID == "" {
				call.ID = newID("call")
			}
			if seen[call.ID] {
				yield(nil, fmt.Errorf("duplicate tool call ID %q", call.ID))
				return
			}
			seen[call.ID] = true
			parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Function.Name, Args: call.Function.Arguments}})
		}
		finish := genai.FinishReasonStop
		if reason == "length" {
			finish = genai.FinishReasonMaxTokens
		}
		metadata := map[string]any{"provider_metrics": metric, "backend": m.client.Descriptor(), "usage_details": usageDetails, "done_reason": reason}
		if m.client.Descriptor().Provider == inference.ProviderOllama {
			metadata["ollama_metrics"] = metric
		}
		yield(&model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: parts}, TurnComplete: true, FinishReason: finish, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(metric.PromptEvalCount), CandidatesTokenCount: int32(metric.EvalCount), TotalTokenCount: int32(metric.PromptEvalCount + metric.EvalCount)}, CustomMetadata: metadata}, nil)
	}
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func toOllama(req *model.LLMRequest, settings Request) (inference.ChatRequest, error) {
	maxTokens := settings.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxOutputTokens
	}
	if maxTokens < MinMaxOutputTokens || maxTokens > MaxMaxOutputTokens {
		return inference.ChatRequest{}, fmt.Errorf("maxOutputTokens must be %d–%d", MinMaxOutputTokens, MaxMaxOutputTokens)
	}
	if req.Config != nil && req.Config.MaxOutputTokens > 0 {
		maxTokens = min(maxTokens, int(req.Config.MaxOutputTokens))
	}
	r := inference.ChatRequest{Model: settings.Model, Stream: true, Think: settings.Think, Options: map[string]any{"num_ctx": settings.ContextSize, "temperature": settings.Temperature, "num_predict": maxTokens}}
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var text strings.Builder
		for _, p := range req.Config.SystemInstruction.Parts {
			if p != nil {
				text.WriteString(p.Text)
			}
		}
		if text.Len() > 0 {
			r.Messages = append(r.Messages, inference.Message{Role: "system", Content: text.String()})
		}
	}
	for _, c := range req.Contents {
		if c == nil {
			continue
		}
		role := c.Role
		if role == genai.RoleModel {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		message := inference.Message{Role: role}
		var toolMessages []inference.Message
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			switch {
			case p.FunctionResponse != nil:
				content, err := toolResponseText(p.FunctionResponse.Name, p.FunctionResponse.Response)
				if err != nil {
					return r, err
				}
				toolMessages = append(toolMessages, inference.Message{Role: "tool", Content: content, ToolName: p.FunctionResponse.Name, ToolCallID: p.FunctionResponse.ID})
			case p.FunctionCall != nil:
				message.ToolCalls = append(message.ToolCalls, inference.ToolCall{ID: p.FunctionCall.ID, Type: "function", Function: inference.ToolFunction{Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args}})
			case p.InlineData != nil:
				if !strings.HasPrefix(p.InlineData.MIMEType, "image/") {
					return r, fmt.Errorf("unsupported attachment MIME type %q", p.InlineData.MIMEType)
				}
				message.Images = append(message.Images, encodeImage(p.InlineData.Data))
			case p.FileData != nil:
				return r, errors.New("remote/file URI inputs are not allowed; attach approved image bytes")
			case p.Thought:
				message.Thinking += p.Text
			default:
				message.Content += p.Text
			}
		}
		if message.Content != "" || message.Thinking != "" || len(message.ToolCalls) > 0 || len(message.Images) > 0 {
			r.Messages = append(r.Messages, message)
		}
		r.Messages = append(r.Messages, toolMessages...)
	}
	if req.Config != nil {
		for _, t := range req.Config.Tools {
			if t == nil {
				continue
			}
			for _, f := range t.FunctionDeclarations {
				if f == nil {
					continue
				}
				parameters := map[string]any{"type": "object", "properties": map[string]any{}}
				if f.ParametersJsonSchema != nil {
					b, err := json.Marshal(f.ParametersJsonSchema)
					if err != nil {
						return r, err
					}
					if err = json.Unmarshal(b, &parameters); err != nil {
						return r, err
					}
				} else if f.Parameters != nil {
					b, err := json.Marshal(f.Parameters)
					if err != nil {
						return r, err
					}
					if err = json.Unmarshal(b, &parameters); err != nil {
						return r, err
					}
					normalizeSchema(parameters)
				}
				r.Tools = append(r.Tools, inference.ToolDefinition{Type: "function", Function: inference.FunctionDefinition{Name: f.Name, Description: f.Description, Parameters: parameters}})
			}
		}
		if req.Config.Temperature != nil {
			r.Options["temperature"] = *req.Config.Temperature
		}
		if req.Config.TopP != nil {
			r.Options["top_p"] = *req.Config.TopP
		}
		if req.Config.TopK != nil {
			r.Options["top_k"] = *req.Config.TopK
		}
	}
	return r, nil
}

func normalizeSchema(m map[string]any) {
	for k, v := range m {
		if k == "type" {
			if s, ok := v.(string); ok {
				m[k] = strings.ToLower(s)
			}
		}
		switch x := v.(type) {
		case map[string]any:
			normalizeSchema(x)
		case []any:
			for _, a := range x {
				if mm, ok := a.(map[string]any); ok {
					normalizeSchema(mm)
				}
			}
		}
	}
}
