package runtimes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

func qualificationKey(r StartRequest) string {
	return fmt.Sprintf("%s/%s/%d", r.BuildID, r.ModelID, r.ContextTokens)
}

func (m *Manager) qualifiedLocked(request StartRequest, manifest Manifest, model ModelManifest) bool {
	receipt, exists := m.state.Qualifications[qualificationKey(request)]
	return exists && receipt.ProtocolVersion == 2 && receipt.Status == "qualified" && receipt.RuntimeManifestDigest == digestJSON(manifest) && receipt.ModelManifestDigest == digestJSON(model) && receipt.ArtifactDigest == model.Model.ArtifactDigest && receipt.TemplateDigest == model.Model.TemplateDigest
}

// Qualify is a bounded host-owned probe. It has no tool executor: the one tool
// result is synthetic, fixed test data. Ordinary runs remain unavailable until
// this exact runtime/model/context has earned and persisted a matching receipt.
// The owned process is stopped on success, failure and cancellation.
func (m *Manager) Qualify(ctx context.Context, request StartRequest) (QualificationReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	m.mu.Lock()
	if m.probeCancel != nil || m.child != nil || m.closed {
		m.mu.Unlock()
		return QualificationReceipt{}, errors.New("another runtime operation is active")
	}
	key := qualificationKey(request)
	previous, had := m.state.Qualifications[key]
	delete(m.state.Qualifications, key)
	if err := m.persistLocked(); err != nil {
		if had {
			m.state.Qualifications[key] = previous
		}
		m.mu.Unlock()
		return QualificationReceipt{}, err
	}
	m.probeCancel = cancel
	m.wg.Add(1)
	m.process = ProcessState{BuildID: request.BuildID, ModelID: request.ModelID, ContextTokens: request.ContextTokens, Status: "checking"}
	m.mu.Unlock()
	m.changed()
	defer func() {
		defer m.wg.Done()
		m.mu.Lock()
		m.probeCancel = nil
		if m.child == nil && m.process.Status == "checking" {
			m.process.Status = "stopped"
		}
		m.mu.Unlock()
		m.changed()
	}()
	connection, err := m.start(ctx, request, startProbe)
	if err != nil {
		return QualificationReceipt{}, err
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer stopCancel()
		_ = m.stopSpecific(stopCtx, nil)
	}()
	var backend *inference.NativeBackend
	if connection.Config.Descriptor.Provider == inference.ProviderOMLX {
		backend, err = inference.NewOMLXBackend(connection.Config)
	} else {
		backend, err = inference.NewMLXServeBackend(connection.Config)
	}
	if err != nil {
		return QualificationReceipt{}, err
	}
	model := connection.Config.Models[0]
	if !model.Capabilities.Tools || !model.Capabilities.Thinking {
		return QualificationReceipt{}, errors.New("coding runtime probe requires advertised tools and thinking support")
	}
	zero := float64(0)
	req := inference.CompletionRequest{Model: model.Name, Controls: inference.GenerationControls{ContextTokens: request.ContextTokens, MaxOutputTokens: min(1024, request.ContextTokens/2), Temperature: &zero, Thinking: inference.ThinkingOn}, Messages: []inference.Message{{Role: "system", Content: "This is a bounded protocol test. Think briefly. First call echo_probe exactly once with value gemma-probe. After its result, reply with exactly gemma-probe-ok. Do not call any other tool."}, {Role: "user", Content: "Run the protocol test now."}}, Tools: []inference.ToolDefinition{{Type: "function", Function: inference.FunctionDefinition{Name: "echo_probe", Description: "Return a fixed test string; no external action is performed.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "enum": []string{"gemma-probe"}}}, "required": []string{"value"}, "additionalProperties": false}}}}}
	var generations []QualificationGeneration
	generate := func(stage string) (inference.Message, error) {
		var message inference.Message
		var receipt inference.ChatChunk
		started := time.Now()
		err := backend.Generate(ctx, req, func(chunk inference.ChatChunk) error {
			message.Content += chunk.Message.Content
			message.Thinking += chunk.Message.Thinking
			message.ToolCalls = append(message.ToolCalls, chunk.Message.ToolCalls...)
			return nil
		}, func(observed inference.ChatChunk) { receipt = observed })
		observation := QualificationGeneration{Stage: stage, ObservedDurationSeconds: time.Since(started).Seconds(), FinishReason: receipt.DoneReason, TokenCountsPresent: receipt.TokenCountsPresent, Usage: &receipt.UsageDetails}
		if receipt.TokenCountsPresent {
			prompt, completion := receipt.PromptEvalCount, receipt.EvalCount
			observation.PromptTokens = &prompt
			observation.CompletionTokens = &completion
		}
		generations = append(generations, observation)
		if err != nil {
			return message, err
		}
		if !receipt.Done || !receipt.TokenCountsPresent || receipt.DoneReason == "length" {
			return message, errors.New("probe did not produce a complete measured response")
		}
		return message, nil
	}
	first, err := generate("thinking-on tool call")
	if err != nil {
		return QualificationReceipt{}, fmt.Errorf("tool-call probe: %w", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "echo_probe" || len(first.ToolCalls[0].Function.Arguments) != 1 || first.ToolCalls[0].Function.Arguments["value"] != "gemma-probe" {
		return QualificationReceipt{}, errors.New("probe model did not return the requested bounded tool call")
	}
	first.Role = "assistant"
	req.Messages = append(req.Messages, first, inference.Message{Role: "tool", ToolCallID: first.ToolCalls[0].ID, ToolName: "echo_probe", Content: `{"value":"gemma-probe"}`})
	second, err := generate("tool-result continuation")
	if err != nil {
		return QualificationReceipt{}, fmt.Errorf("tool-result probe: %w", err)
	}
	if len(second.ToolCalls) != 0 || !strings.Contains(second.Content, "gemma-probe-ok") {
		return QualificationReceipt{}, errors.New("probe model failed the tool-result continuation")
	}
	if first.Thinking == "" && second.Thinking == "" {
		return QualificationReceipt{}, errors.New("thinking-on probe returned no separate reasoning channel")
	}
	req.Tools = nil
	req.Messages = []inference.Message{{Role: "user", Content: "Reply with exactly gemma-probe-ok."}}
	req.Controls.Thinking = inference.ThinkingOff
	third, err := generate("thinking-off")
	if err != nil {
		return QualificationReceipt{}, fmt.Errorf("thinking-off probe: %w", err)
	}
	if len(third.ToolCalls) != 0 || !strings.Contains(third.Content, "gemma-probe-ok") || strings.TrimSpace(third.Thinking) != "" {
		return QualificationReceipt{}, errors.New("thinking-off probe did not honor its output controls")
	}
	// The last probe interrupts an actively streaming response. Native adapters
	// must stop/reap this exact owned process before releasing shared admission.
	req.Messages = []inference.Message{{Role: "user", Content: "Cancellation protocol probe: count from 1 to 10000, one number per line. Do not stop early."}}
	cancelCtx, cancelStream := context.WithCancel(ctx)
	started := false
	cancellationStarted := time.Now()
	err = backend.Generate(cancelCtx, req, func(chunk inference.ChatChunk) error {
		if chunk.Message.Content != "" || chunk.Message.Thinking != "" {
			started = true
			cancelStream()
			return context.Canceled
		}
		return nil
	})
	cancelStream()
	generations = append(generations, QualificationGeneration{Stage: "active-stream cancellation and cleanup", ObservedDurationSeconds: time.Since(cancellationStarted).Seconds()})
	if !started || !errors.Is(err, context.Canceled) {
		return QualificationReceipt{}, fmt.Errorf("active stream cancellation was not observed: %w", err)
	}
	m.mu.Lock()
	reaped := m.child == nil && m.process.Status == "stopped"
	m.mu.Unlock()
	if !reaped {
		return QualificationReceipt{}, errors.New("cancelled native stream did not reap its owned process")
	}
	// Stop and reap before publishing a receipt: cancellation/teardown is part
	// of qualification, and no probe-owned process becomes a chat backend.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopCancel()
	if err = m.stopSpecific(stopCtx, nil); err != nil {
		return QualificationReceipt{}, fmt.Errorf("probe process cleanup: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return QualificationReceipt{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || ctx.Err() != nil {
		return QualificationReceipt{}, context.Canceled
	}
	manifest := m.catalog[request.BuildID]
	modelManifest := m.models[request.ModelID]
	receipt := QualificationReceipt{ProtocolVersion: 2, BuildID: request.BuildID, ModelID: request.ModelID, ContextTokens: request.ContextTokens, RuntimeManifestDigest: digestJSON(manifest), ModelManifestDigest: digestJSON(modelManifest), ArtifactDigest: modelManifest.Model.ArtifactDigest, TemplateDigest: modelManifest.Model.TemplateDigest, CheckedAt: time.Now().UTC(), Status: "qualified", Coding: "experimental", Checks: []string{"owned authenticated loopback process", "verified runtime and complete model inventory", "bounded thinking-on tool call", "stable tool ID and synthetic result continuation", "thinking-off control", "complete provider token receipts", "active stream cancellation reaps owned process before admission release", "owned process teardown"}}
	receipt.Generations = generations
	m.state.Qualifications[key] = receipt
	if err = m.persistLocked(); err != nil {
		delete(m.state.Qualifications, key)
		return QualificationReceipt{}, err
	}
	go m.changed()
	return receipt, nil
}
