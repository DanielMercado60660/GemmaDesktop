package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Each mechanism is opt-in and serialized in the immutable run request. These
// controls can narrow an already-authorized catalogue, never grant authority.
type HarnessMechanisms struct {
	ToolSet                   string `json:"toolSet,omitempty"`
	RepeatedFailureFeedback   bool   `json:"repeatedFailureFeedback,omitempty"`
	StreamRepetitionGuard     bool   `json:"streamRepetitionGuard,omitempty"`
	ThinkingCycleGuard        bool   `json:"thinkingCycleGuard,omitempty"`
	ExplorationReminder       bool   `json:"explorationReminder,omitempty"`
	ResidentReadReuse         bool   `json:"residentReadReuse,omitempty"`
	DuplicateResultProjection bool   `json:"duplicateResultProjection,omitempty"`
	DeferredToolDiscovery     bool   `json:"deferredToolDiscovery,omitempty"`
}

func (p HarnessMechanisms) Validate() error {
	switch p.ToolSet {
	case "", "all", "inspect", "coding", "research":
		return nil
	}
	return errors.New("harness toolSet must be all, inspect, coding, or research")
}
func (p HarnessMechanisms) Focus(defs []ToolDefinition) []ToolDefinition {
	if p.ToolSet == "" || p.ToolSet == "all" {
		return defs
	}
	var out []ToolDefinition
	for _, d := range defs {
		read := isEvidenceRead(d.Name)
		coding := read || mutatingCall(ToolCall{Name: d.Name}) || d.Name == "run_command" || d.Name == "verify_task" || d.Name == "setup_dependencies" || d.Name == "checkpoint_snapshot"
		research := read || d.Name == "web_search" || d.Name == "fetch_url" || d.Name == "browser_read" || d.Name == "browser_screenshot" || d.Name == "browser_navigate"
		if p.ToolSet == "inspect" && read || p.ToolSet == "coding" && coding || p.ToolSet == "research" && research {
			out = append(out, d)
		}
	}
	return out
}
func isEvidenceRead(name string) bool {
	switch name {
	case "list_files", "read_file", "search_files", "git_status", "git_diff", "git_history", "git_branches", "git_file", "git_blame":
		return true
	}
	return false
}

type mechanismEvidence struct{ CallID, Digest, ReceiptID string }
type mechanismState struct {
	mu                         sync.Mutex
	profile                    HarnessMechanisms
	readOnly                   bool
	definitions                map[string]ToolDefinition
	exposed                    map[string]bool
	resident                   map[string]bool
	evidence                   map[string]mechanismEvidence
	failures                   map[string]int
	feedbackCount, generations int
}

func newMechanismState(req Request) *mechanismState {
	s := &mechanismState{profile: req.HarnessMechanisms, readOnly: req.ReadOnly, definitions: map[string]ToolDefinition{}, exposed: map[string]bool{}, resident: map[string]bool{}, evidence: map[string]mechanismEvidence{}, failures: map[string]int{}}
	for _, d := range req.Tools {
		s.definitions[d.Name] = d
		s.exposed[d.Name] = !req.HarnessMechanisms.DeferredToolDiscovery || len(req.Tools) <= 12 || d.Name == "read_file" || d.Name == "list_files" || d.Name == "search_files" || d.Name == "verify_task"
	}
	return s
}
func (s *mechanismState) deferred() bool {
	return s.profile.DeferredToolDiscovery && len(s.definitions) > 12
}
func (s *mechanismState) discover(args map[string]any) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	query, _ := args["query"].(string)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return map[string]any{"isError": true, "error": "Supply a specific authorized capability to discover."}
	}
	var names []string
	for name, d := range s.definitions {
		if strings.Contains(strings.ToLower(name+" "+d.Description), query) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = names[:5]
	}
	var defs []ToolDefinition
	for _, name := range names {
		s.exposed[name] = true
		defs = append(defs, s.definitions[name])
	}
	return map[string]any{"tools": defs, "scope": "Already-authorized tools in the frozen focused profile only; at most five matches."}
}
func (s *mechanismState) allowed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return name == "discover_tools" || s.exposed[name]
}
func (s *mechanismState) beforeModel(_ agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generations++
	s.resident = map[string]bool{}
	for _, c := range req.Contents {
		if c != nil {
			for _, p := range c.Parts {
				if p != nil && p.FunctionResponse != nil {
					r := p.FunctionResponse
					if r.Response["reusedEvidence"] == nil {
						s.resident[r.ID] = true
					}
				}
			}
		}
	}
	for key, e := range s.evidence {
		if !s.resident[e.CallID] {
			delete(s.evidence, key)
		}
	}
	if s.deferred() && req.Config != nil {
		config := *req.Config
		config.Tools = nil
		for _, t := range req.Config.Tools {
			if t == nil {
				continue
			}
			copy := *t
			copy.FunctionDeclarations = nil
			for _, d := range t.FunctionDeclarations {
				if d == nil {
					continue
				}
				_, owned := s.definitions[d.Name]
				if !owned || s.exposed[d.Name] {
					copy.FunctionDeclarations = append(copy.FunctionDeclarations, d)
				}
			}
			if len(copy.FunctionDeclarations) > 0 || len(t.FunctionDeclarations) == 0 {
				config.Tools = append(config.Tools, &copy)
			}
		}
		req.Config = &config
	}
	if s.profile.ExplorationReminder && s.readOnly && (s.generations == 8 || s.generations == 16) {
		appendHostInstruction(req, "Read-only exploration checkpoint: assess whether the collected evidence answers the user's question. Summarize findings and uncertainties when sufficient; investigate further only when necessary. Do not edit files or turn an investigation into an implementation task.")
	}
	return nil, nil
}
func appendHostInstruction(req *model.LLMRequest, text string) {
	config := genai.GenerateContentConfig{}
	if req.Config != nil {
		config = *req.Config
	}
	c := genai.Content{}
	if config.SystemInstruction != nil {
		c = *config.SystemInstruction
		c.Parts = append([]*genai.Part(nil), c.Parts...)
	}
	c.Parts = append(c.Parts, &genai.Part{Text: "\nHost experimental feedback (does not grant permissions): " + text})
	config.SystemInstruction = &c
	req.Config = &config
}
func evidenceDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func projectedEvidenceDigest(data map[string]any) string {
	copy := make(map[string]any, len(data))
	for k, v := range data {
		copy[k] = v
	}
	if original, ok := data["data"].(map[string]any); ok {
		payload := make(map[string]any, len(original))
		for k, v := range original {
			if k != "receiptId" {
				payload[k] = v
			}
		}
		copy["data"] = payload
	}
	return evidenceDigest(copy)
}
func unsuccessfulOutcome(data map[string]any) bool {
	payload, _ := data["data"].(map[string]any)
	if payload["verificationPassed"] == false || payload["timedOut"] == true || payload["cancelled"] == true {
		return true
	}
	switch code := payload["exitCode"].(type) {
	case int:
		return code != 0
	case float64:
		return code != 0
	case json.Number:
		return code != "0"
	}
	return false
}

// Called after full tool evidence has been durably emitted. The host still
// executes/revalidates reads; only redundant model-facing bytes are omitted.
// Compaction removes resident call IDs, immediately disabling their reuse.
func (s *mechanismState) project(call ToolCall, data map[string]any, failed bool) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	failed = failed || unsuccessfulOutcome(data)
	if mutatingCall(call) {
		s.evidence = map[string]mechanismEvidence{}
	}
	key := call.Name + ":" + evidenceDigest(call.Arguments)
	result := make(map[string]any, len(data)+2)
	for k, v := range data {
		result[k] = v
	}
	if s.profile.RepeatedFailureFeedback {
		if failed {
			failureKey := key + ":" + projectedEvidenceDigest(data)
			s.failures[failureKey]++
			if s.failures[failureKey] >= 2 && s.feedbackCount < 2 {
				s.feedbackCount++
				result["hostFeedback"] = "The identical action returned the same failure again. Use its evidence to change the approach or report the blocker; do not replay uncertain effects. Existing permissions and budgets still apply."
			}
		} else {
			for k := range s.failures {
				if strings.HasPrefix(k, key+":") {
					delete(s.failures, k)
				}
			}
		}
	}
	eligible := isEvidenceRead(call.Name) && !failed && (s.profile.DuplicateResultProjection || s.profile.ResidentReadReuse && call.Name == "read_file")
	if eligible {
		digest := projectedEvidenceDigest(data)
		payload, _ := data["data"].(map[string]any)
		receipt, _ := payload["receiptId"].(string)
		if previous, ok := s.evidence[key]; ok && previous.Digest == digest && s.resident[previous.CallID] {
			result = map[string]any{"isError": false, "reusedEvidence": map[string]any{"callId": previous.CallID, "receiptId": previous.ReceiptID, "sha256": digest}, "receiptRef": map[string]any{"callId": call.ID, "nodeId": call.NodeID, "receiptId": receipt}, "notice": "Host revalidated identical evidence. Full earlier result remains in this model context; complete current evidence is retained in the journal."}
		} else {
			s.evidence[key] = mechanismEvidence{CallID: call.ID, Digest: digest, ReceiptID: receipt}
		}
	}
	return result
}

var ErrStreamRepetition = errors.New("experimental stream repetition guard stopped a repeated prose loop")

// Conservatively recognizes long identical prose paragraphs only. Code fences,
// JSON, tables, bullets, short lines and merely similar code are excluded.
type proseRepetitionGuard struct {
	tail     string
	code     bool
	previous string
	repeats  int
}

func (g *proseRepetitionGuard) Add(delta string) error {
	g.tail += delta
	for {
		i := strings.IndexByte(g.tail, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(g.tail[:i])
		g.tail = g.tail[i+1:]
		if strings.Contains(line, "```") || strings.Contains(line, "~~~") {
			g.code = !g.code
			g.previous = ""
			g.repeats = 0
			continue
		}
		if g.code || len(line) < 120 || strings.ContainsAny(line, "{}[];=<>|`\\") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			if line != "" {
				g.previous = ""
				g.repeats = 0
			}
			continue
		}
		if line == g.previous {
			g.repeats++
		} else {
			g.previous = line
			g.repeats = 1
		}
		if g.repeats >= 8 {
			return fmt.Errorf("%w: eight identical long paragraphs", ErrStreamRepetition)
		}
	}
	if len(g.tail) > 64<<10 {
		g.tail = ""
		g.previous = ""
		g.repeats = 0
	}
	return nil
}
