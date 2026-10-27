// Package workflow validates saved definitions and compiles them into ADK Go
// graph nodes. ADK owns scheduling; this package adds product correctness guards.
package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	adkw "google.golang.org/adk/v2/workflow"
)

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Node struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Label     string   `json:"label"`
	AgentID   string   `json:"agentId,omitempty"`
	Prompt    string   `json:"prompt,omitempty"`
	Condition string   `json:"condition,omitempty"`
	Position  Position `json:"position"`
	Items     []string `json:"items,omitempty"`
}
type Edge struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Label     string `json:"label,omitempty"`
	Condition string `json:"condition,omitempty"`
}
type Definition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}
type AgentResolver func(string) (engine.Request, error)
type Runtime struct{ engine *engine.Engine }

func New(e *engine.Engine) *Runtime { return &Runtime{engine: e} }

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
var ErrApprovalRejected = errors.New("workflow approval rejected")

// Validate is intentionally stricter than ADK's graph validator. In particular,
// conditional joins are rejected because v2.6.0 can silently omit their work.
func Validate(d Definition) error {
	if len(d.Nodes) == 0 || len(d.Nodes) > 64 {
		return errors.New("workflow requires between 1 and 64 nodes")
	}
	if len(d.Edges) > 256 {
		return errors.New("workflow has too many edges")
	}
	nodes := map[string]Node{}
	incoming := map[string]int{}
	outgoing := map[string][]Edge{}
	for _, n := range d.Nodes {
		if !identifier.MatchString(n.ID) {
			return fmt.Errorf("invalid node ID %q", n.ID)
		}
		if _, exists := nodes[n.ID]; exists {
			return fmt.Errorf("duplicate node %q", n.ID)
		}
		switch n.Type {
		case "agent", "approval", "condition", "parallel", "join", "output", "fanout":
		default:
			return fmt.Errorf("unsupported node type %q", n.Type)
		}
		if n.Type == "condition" {
			if _, _, err := parseCondition(n.Condition); err != nil {
				return fmt.Errorf("node %s: %w", n.ID, err)
			}
		}
		if len(n.Items) > 16 {
			return fmt.Errorf("node %s: maximum fanout is 16", n.ID)
		}
		nodes[n.ID] = n
	}
	edgeKeys := map[string]bool{}
	for _, e := range d.Edges {
		source, ok := nodes[e.Source]
		if !ok {
			return fmt.Errorf("edge references unknown source %q", e.Source)
		}
		if _, ok := nodes[e.Target]; !ok {
			return fmt.Errorf("edge references unknown target %q", e.Target)
		}
		if e.Source == e.Target {
			return errors.New("workflow cycles are not allowed")
		}
		key := e.Source + "/" + e.Target + "/" + e.Condition
		if edgeKeys[key] {
			return errors.New("duplicate workflow edge")
		}
		edgeKeys[key] = true
		if source.Type == "condition" {
			if e.Condition != "true" && e.Condition != "false" {
				return fmt.Errorf("condition node %s edges require true or false", source.ID)
			}
		} else if e.Condition != "" {
			return fmt.Errorf("only condition nodes may have conditional edges")
		}
		incoming[e.Target]++
		outgoing[e.Source] = append(outgoing[e.Source], e)
	}
	for _, n := range d.Nodes {
		if n.Type == "condition" {
			truth, falsity := 0, 0
			for _, e := range outgoing[n.ID] {
				if e.Condition == "true" {
					truth++
				} else {
					falsity++
				}
			}
			if truth != 1 || falsity != 1 {
				return fmt.Errorf("condition %s needs exactly one true and one false edge", n.ID)
			}
		}
		if incoming[n.ID] > 1 && n.Type != "join" {
			return fmt.Errorf("node %s has multiple inputs; insert an unconditional join", n.ID)
		}
		if n.Type == "join" && incoming[n.ID] < 2 {
			return fmt.Errorf("join %s needs at least two predecessors", n.ID)
		}
	}
	color := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if color[id] == 1 {
			return errors.New("workflow cycles are not allowed")
		}
		if color[id] == 2 {
			return nil
		}
		color[id] = 1
		for _, e := range outgoing[id] {
			if err := visit(e.Target); err != nil {
				return err
			}
		}
		color[id] = 2
		return nil
	}
	for id := range nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	// If a branch can be skipped, it cannot be a required join predecessor.
	for _, n := range d.Nodes {
		if n.Type != "condition" {
			continue
		}
		seen := map[string]bool{}
		queue := []string{n.ID}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			for _, e := range outgoing[id] {
				if nodes[e.Target].Type == "join" {
					return fmt.Errorf("join %s depends on condition %s; conditional joins are unsupported", e.Target, n.ID)
				}
				queue = append(queue, e.Target)
			}
		}
	}
	// ADK permits at most one terminal output. Reject concurrently reachable
	// terminals before running any node, so this cannot fail after tool effects.
	// Every non-join node has a single predecessor and conditional joins were
	// rejected above, making necessary branch guards exact for this DAG subset.
	parents := map[string]Edge{}
	for _, e := range d.Edges {
		parents[e.Target] = e
	}
	guards := map[string]map[string]string{}
	var branchGuards func(string) map[string]string
	branchGuards = func(id string) map[string]string {
		if g, ok := guards[id]; ok {
			return g
		}
		g := map[string]string{}
		if e, ok := parents[id]; ok && nodes[id].Type != "join" {
			for k, v := range branchGuards(e.Source) {
				g[k] = v
			}
			if e.Condition != "" {
				g[e.Source] = e.Condition
			}
		}
		guards[id] = g
		return g
	}
	var terminals []string
	for _, n := range d.Nodes {
		if len(outgoing[n.ID]) == 0 {
			terminals = append(terminals, n.ID)
		}
	}
	for i, a := range terminals {
		for _, b := range terminals[i+1:] {
			exclusive := false
			for condition, value := range branchGuards(a) {
				if other, ok := branchGuards(b)[condition]; ok && other != value {
					exclusive = true
				}
			}
			if !exclusive {
				return fmt.Errorf("parallel terminal nodes %s and %s must converge into a join", a, b)
			}
		}
	}
	return nil
}

func parseCondition(c string) (string, string, error) {
	parts := strings.SplitN(c, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("condition must be contains: text, not_contains: text, or equals: text")
	}
	op := strings.TrimSpace(parts[0])
	if op != "contains" && op != "not_contains" && op != "equals" {
		return "", "", errors.New("unsupported condition operator")
	}
	return op, strings.TrimSpace(parts[1]), nil
}
func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

type checkpoint struct {
	Version        int                   `json:"version"`
	DefinitionHash string                `json:"definitionHash"`
	Session        json.RawMessage       `json:"session"`
	Interrupts     map[string]string     `json:"interrupts"`
	Pending        []engine.PendingInput `json:"pending"`
	Completed      map[string]any        `json:"completed"`
	ModelCalls     int                   `json:"modelCalls"`
	ToolCalls      int                   `json:"toolCalls"`
	OutputBytes    int                   `json:"outputBytes"`
	Usage          inference.Metrics     `json:"usage"`
}

// Run pauses durably at approval nodes. Resume must provide the same definition
// and the returned Snapshot, with exactly one reply per pending interrupt.
func (r *Runtime) Run(ctx context.Context, d Definition, request engine.Request, emit func(engine.Event), execute engine.ToolExecutor, resolve AgentResolver) (result engine.Result, runErr error) {
	if err := Validate(d); err != nil {
		return result, err
	}
	if r == nil || r.engine == nil {
		return result, errors.New("workflow engine is required")
	}
	identity := d
	identity.Nodes = append([]Node(nil), d.Nodes...)
	for i := range identity.Nodes {
		identity.Nodes[i].Position = Position{}
	}
	encoded, _ := json.Marshal(identity)
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	cp := checkpoint{Version: 1, DefinitionHash: hash, Interrupts: map[string]string{}, Completed: map[string]any{}}
	if len(request.Snapshot) > 0 {
		if err := json.Unmarshal(request.Snapshot, &cp); err != nil {
			return result, fmt.Errorf("invalid workflow checkpoint: %w", err)
		}
		if cp.Version != 1 || cp.DefinitionHash != hash {
			return result, errors.New("workflow changed since checkpoint; resume is refused")
		}
		if len(request.Resume) == 0 {
			return result, errors.New("workflow checkpoint requires an explicit pending response")
		}
		pending := map[string]bool{}
		for _, p := range cp.Pending {
			pending[p.ID] = true
		}
		for id := range request.Resume {
			if !pending[id] {
				return result, fmt.Errorf("stale or unknown workflow approval %q", id)
			}
		}
	} else if len(request.Resume) > 0 {
		return result, errors.New("cannot resume without a checkpoint")
	}
	if cp.Interrupts == nil {
		cp.Interrupts = map[string]string{}
	}
	if cp.Completed == nil {
		cp.Completed = map[string]any{}
	}
	maxSteps := request.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 24
	}
	if maxSteps > 512 {
		maxSteps = 512
	}
	maxTools := request.MaxToolCalls
	if maxTools <= 0 {
		maxTools = maxSteps * 4
	}
	if maxTools > 2048 {
		maxTools = 2048
	}
	budget := &engine.Budget{MaxModelCalls: maxSteps, MaxToolCalls: maxTools, MaxOutputBytes: 8 << 20, ModelCalls: cp.ModelCalls, ToolCalls: cp.ToolCalls, OutputBytes: cp.OutputBytes}
	ctx = engine.WithBudget(ctx, budget)
	var mu sync.Mutex
	var usageMu sync.Mutex
	recordUsage := func(usage inference.Metrics) {
		usageMu.Lock()
		defer usageMu.Unlock()
		cp.Usage.TotalDuration += usage.TotalDuration
		cp.Usage.LoadDuration += usage.LoadDuration
		cp.Usage.PromptEvalCount += usage.PromptEvalCount
		cp.Usage.PromptEvalDuration += usage.PromptEvalDuration
		cp.Usage.EvalCount += usage.EvalCount
		cp.Usage.EvalDuration += usage.EvalDuration
	}
	send := func(event engine.Event) {
		if emit != nil {
			mu.Lock()
			defer mu.Unlock()
			emit(event)
		}
	}
	for _, n := range d.Nodes {
		if n.Type == "approval" && cp.Interrupts[n.ID] == "" {
			var id [12]byte
			if _, err := rand.Read(id[:]); err != nil {
				return result, err
			}
			cp.Interrupts[n.ID] = "approval_" + hex.EncodeToString(id[:])
		}
	}
	nodeMap := map[string]adkw.Node{}
	incoming := map[string]int{}
	for _, e := range d.Edges {
		incoming[e.Target]++
	}
	for _, n := range d.Nodes {
		n := n
		switch n.Type {
		case "join":
			nodeMap[n.ID] = adkw.NewJoinNode(n.ID)
		case "agent":
			base, err := nodeRequest(request, n, resolve)
			if err != nil {
				return result, err
			}
			nodeMap[n.ID] = adkw.NewFunctionNode(n.ID, func(ac agent.Context, input any) (string, error) {
				send(engine.Event{Type: "node_started", Title: n.Label, NodeID: n.ID})
				req := base
				req.Prompt = nodePrompt(n, input)
				out, err := r.engine.Run(ac, req, send, execute)
				recordUsage(out.Usage)
				if err != nil {
					send(engine.Event{Type: "node_failed", Title: n.Label, NodeID: n.ID, Detail: err.Error()})
					return "", err
				}
				send(engine.Event{Type: "node_completed", Title: n.Label, NodeID: n.ID, Detail: out.Content})
				return out.Content, nil
			}, adkw.NodeConfig{})
		case "fanout":
			base, err := nodeRequest(request, n, resolve)
			if err != nil {
				return result, err
			}
			nodeMap[n.ID] = adkw.NewDynamicNode(n.ID, func(ac agent.Context, input any, publish func(*session.Event) error) (results []string, runErr error) {
				send(engine.Event{Type: "node_started", Title: n.Label, NodeID: n.ID})
				defer func() {
					if runErr != nil {
						send(engine.Event{Type: "node_failed", Title: n.Label, NodeID: n.ID, Detail: runErr.Error()})
					}
				}()
				items := n.Items
				if len(items) == 0 {
					if err := json.Unmarshal([]byte(asText(input)), &items); err != nil {
						return nil, errors.New("fanout input must be a JSON array of strings or configured items")
					}
				}
				if len(items) == 0 || len(items) > 16 {
					return nil, errors.New("fanout requires 1 to 16 items")
				}
				outputs := make([]string, len(items))
				errs := make([]error, len(items))
				var wg sync.WaitGroup
				slots := make(chan struct{}, 4)
				for index, item := range items {
					index, item := index, item
					wg.Add(1)
					go func() {
						defer wg.Done()
						select {
						case slots <- struct{}{}:
							defer func() { <-slots }()
						case <-ac.Done():
							errs[index] = ac.Err()
							return
						}
						childID := fmt.Sprintf("%s_item_%d", n.ID, index)
						var childErr error
						child := adkw.NewFunctionNode(childID, func(childCtx agent.Context, item string) (string, error) {
							send(engine.Event{Type: "node_started", Title: n.Label, NodeID: childID})
							req := base
							req.NodeID = childID
							req.Prompt = nodePrompt(n, item)
							out, err := r.engine.Run(childCtx, req, send, execute)
							recordUsage(out.Usage)
							childErr = err
							kind := "node_completed"
							detail := out.Content
							if err != nil {
								kind = "node_failed"
								detail = err.Error()
							}
							send(engine.Event{Type: kind, Title: n.Label, NodeID: childID, Detail: detail})
							return out.Content, err
						}, adkw.NodeConfig{})
						out, err := adkw.RunNode[string](ac, child, item, adkw.WithRunID(fmt.Sprintf("item_%d", index)), adkw.WithUseSubBranch())
						if childErr != nil {
							err = errors.Join(err, childErr)
						}
						errs[index] = err
						outputs[index] = asText(out)
					}()
				}
				wg.Wait()
				for _, err := range errs {
					if err != nil {
						return nil, err
					}
				}
				send(engine.Event{Type: "node_completed", Title: n.Label, NodeID: n.ID, Data: map[string]any{"items": len(items)}})
				return outputs, nil
			}, adkw.NodeConfig{})
		default:
			reenter := true
			nodeMap[n.ID] = &actionNode{BaseNode: adkw.NewBaseNode(n.ID, n.Label, adkw.NodeConfig{RerunOnResume: &reenter}), node: n, interrupt: cp.Interrupts[n.ID], emit: send}
		}
	}
	edges := []adkw.Edge{}
	for _, n := range d.Nodes {
		if incoming[n.ID] == 0 {
			edges = append(edges, adkw.Edge{From: adkw.Start, To: nodeMap[n.ID]})
		}
	}
	for _, e := range d.Edges {
		var route adkw.Route
		if e.Condition != "" {
			route = adkw.StringRoute(e.Condition)
		}
		edges = append(edges, adkw.Edge{From: nodeMap[e.Source], To: nodeMap[e.Target], Route: route})
	}
	graph, err := adkw.New("workflow", edges, adkw.WithMaxConcurrency(4))
	if err != nil {
		return result, err
	}
	workflowAgent, err := agent.New(agent.Config{Name: "workflow", Description: d.Description, Run: func(ac agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			var sequence iter.Seq2[*session.Event, error]
			if len(request.Resume) > 0 {
				state, err := graph.ReconstructRunState(ac.Session(), ac.InvocationID())
				if err != nil {
					yield(nil, err)
					return
				}
				if state == nil {
					yield(nil, errors.New("no supported approval checkpoint to resume"))
					return
				}
				// v2.6.0 omits completed predecessors and can retain stale waiting
				// states when parallel approvals are answered in separate turns.
				// Restore only outputs recorded by this immutable DAG; never dispatch
				// cached nodes or infer successful execution from partial output.
				for id, out := range cp.Completed {
					state.Nodes[id] = &adkw.NodeState{Status: adkw.NodeCompleted, Output: out}
				}
				sequence = graph.Resume(agent.Promote(ac), state, request.Resume)
			} else {
				sequence = graph.Run(ac)
			}
			for ev, err := range sequence {
				if err != nil {
					yield(ev, err)
					return
				}
				if ev != nil && ev.Output != nil && ev.NodeInfo != nil {
					for _, n := range d.Nodes {
						if pathHasNode(ev.NodeInfo.Path, n.ID) {
							cp.Completed[n.ID] = ev.Output
							if n.Type == "join" {
								send(engine.Event{Type: "node_completed", Title: n.Label, NodeID: n.ID, Detail: asText(ev.Output)})
							}
							break
						}
					}
				}
				if !yield(ev, nil) {
					return
				}
			}
		}
	}})
	if err != nil {
		return result, err
	}
	request.Snapshot = cp.Session
	priorPending := cp.Pending
	result, runErr = r.engine.RunAgent(ctx, workflowAgent, request, send, budget)
	result.Usage = cp.Usage
	result.Pending = append(cpPendingUnanswered(priorPending, request.Resume), result.Pending...)
	cp.Session = result.Snapshot
	cp.Pending = result.Pending
	cp.ModelCalls = result.Steps
	cp.ToolCalls = budget.ToolCalls
	cp.OutputBytes = budget.OutputBytes
	checkpointBytes, err := json.Marshal(cp)
	if err != nil {
		if runErr == nil {
			runErr = err
		}
	} else {
		result.Snapshot = checkpointBytes
	}
	return result, runErr
}

func cpPendingUnanswered(pending []engine.PendingInput, answered map[string]any) []engine.PendingInput {
	out := []engine.PendingInput{}
	for _, p := range pending {
		if _, ok := answered[p.ID]; !ok {
			out = append(out, p)
		}
	}
	return out
}
func pathHasNode(p, id string) bool {
	parts := strings.Split(p, "/")
	return strings.SplitN(parts[len(parts)-1], "@", 2)[0] == id
}
func nodeRequest(base engine.Request, n Node, resolve AgentResolver) (engine.Request, error) {
	if n.AgentID != "" {
		if resolve == nil {
			return base, errors.New("agent resolver is required")
		}
		specific, err := resolve(n.AgentID)
		if err != nil {
			return base, err
		}
		if specific.Model != "" {
			base.Model = specific.Model
		}
		if specific.Instructions != "" {
			base.Instructions = specific.Instructions
		}
		if specific.MaxSteps > 0 {
			base.MaxSteps = specific.MaxSteps
		}
		if specific.MaxToolCalls > 0 {
			base.MaxToolCalls = specific.MaxToolCalls
		}
		if specific.ContextSize > 0 {
			base.ContextSize = specific.ContextSize
		}
		if specific.Timeout > 0 {
			base.Timeout = specific.Timeout
		}
		base.Temperature = specific.Temperature
		if specific.Think != nil {
			base.Think = specific.Think
		}
		base.ReadOnly = base.ReadOnly || specific.ReadOnly
		if specific.Tools != nil {
			base.Tools = specific.Tools
		}
		if specific.Skills != nil {
			base.Skills = specific.Skills
		}
	}
	base.NodeID = n.ID
	base.Snapshot = nil
	base.Resume = nil
	base.History = nil
	return base, nil
}
func nodePrompt(n Node, input any) string {
	if n.Prompt == "" {
		return asText(input)
	}
	return n.Prompt + "\n\nWorkflow input:\n" + asText(input)
}

type actionNode struct {
	adkw.BaseNode
	node      Node
	interrupt string
	emit      func(engine.Event)
}

func (n *actionNode) Run(ctx agent.Context, input any) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		n.emit(engine.Event{Type: "node_started", Title: n.node.Label, NodeID: n.node.ID})
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Output = input
		if n.node.Type == "condition" {
			op, arg, _ := parseCondition(n.node.Condition)
			text := asText(input)
			ok := false
			switch op {
			case "contains":
				ok = strings.Contains(text, arg)
			case "not_contains":
				ok = !strings.Contains(text, arg)
			case "equals":
				ok = text == arg
			}
			ev.Routes = []string{fmt.Sprint(ok)}
		}
		if n.node.Type == "approval" {
			reply, ok := ctx.ResumedInput(n.interrupt)
			if !ok {
				message := n.node.Prompt
				if message == "" {
					message = n.node.Label
				}
				if message == "" {
					message = "Approve continuing this workflow"
				}
				yield(adkw.NewRequestInputEvent(ctx, session.RequestInput{InterruptID: n.interrupt, Message: message, Payload: map[string]any{"nodeId": n.node.ID, "input": input}}), nil)
				return
			}
			allowed, ok := reply.(bool)
			if !ok || !allowed {
				n.emit(engine.Event{Type: "node_failed", Title: n.node.Label, NodeID: n.node.ID, Detail: ErrApprovalRejected.Error()})
				yield(nil, ErrApprovalRejected)
				return
			}
		}
		if n.node.Type == "output" && n.node.Prompt != "" {
			ev.Output = nodePrompt(n.node, input)
		}
		if !yield(ev, nil) {
			return
		}
		n.emit(engine.Event{Type: "node_completed", Title: n.node.Label, NodeID: n.node.ID})
	}
}

// SortedNodeIDs is useful to render a stable validated definition in diagnostics.
func SortedNodeIDs(d Definition) []string {
	ids := make([]string, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	return ids
}
