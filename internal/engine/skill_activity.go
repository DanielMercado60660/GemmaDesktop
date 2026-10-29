package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
)

func isSkillTool(name string) bool {
	return name == "list_skills" || name == "load_skill" || name == "load_skill_resource"
}
func skillActivity(req Request, budget *Budget, localCalls *int, emit func(Event)) (llmagent.BeforeToolCallback, llmagent.AfterToolCallback) {
	before := func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		if !isSkillTool(t.Name()) {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		budget.mu.Lock()
		if budget.ToolCalls >= budget.MaxToolCalls || *localCalls >= req.MaxToolCalls {
			budget.mu.Unlock()
			return nil, ErrBudgetExceeded
		}
		budget.ToolCalls++
		*localCalls++
		budget.mu.Unlock()
		if emit != nil {
			emit(Event{Type: "tool_started", Title: t.Name(), Tool: t.Name(), NodeID: req.NodeID, Data: map[string]any{"id": ctx.FunctionCallID(), "name": t.Name(), "arguments": args, "authority": "instruction_resource"}})
		}
		return nil, nil
	}
	after := func(ctx agent.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
		if !isSkillTool(t.Name()) {
			return nil, nil
		}
		b, e := json.Marshal(result)
		if e != nil || len(b) > 1<<20 {
			err = fmt.Errorf("skill tool result exceeds 1 MiB")
		}
		kind := "tool_completed"
		detail := ""
		if err != nil {
			kind = "tool_failed"
			detail = err.Error()
		}
		if emit != nil {
			emit(Event{Type: kind, Title: t.Name(), Tool: t.Name(), NodeID: req.NodeID, Detail: detail, Data: map[string]any{"callId": ctx.FunctionCallID(), "authority": "instruction_resource", "bytes": len(b)}})
		}
		if e != nil || len(b) > 1<<20 {
			return nil, err
		}
		return nil, nil
	}
	return before, after
}

// AccountNestedUsage records a completed child invocation once in its parent's
// accounting ledger without falsifying any model event's prompt token count.
func AccountNestedUsage(ctx context.Context, usage inference.Metrics, accounting ...*inference.TokenAccounting) {
	var measured *inference.TokenAccounting
	if len(accounting) > 0 {
		measured = accounting[0]
	}
	recordObservedUsage(ctx, usage, measured)
}
