package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestFocusedToolsNeverExpandFrozenAuthority(t *testing.T) {
	defs := []ToolDefinition{{Name: "read_file"}, {Name: "write_file"}, {Name: "run_command"}, {Name: "browser_read"}, {Name: "mcp_call"}, {Name: "verify_task"}}
	for _, test := range []struct {
		profile string
		want    string
	}{{"inspect", "read_file"}, {"coding", "read_file,write_file,run_command,verify_task"}, {"research", "read_file,browser_read"}} {
		got := HarnessMechanisms{ToolSet: test.profile}.Focus(defs)
		names := []string{}
		for _, d := range got {
			names = append(names, d.Name)
		}
		if strings.Join(names, ",") != test.want {
			t.Fatalf("%s: %v", test.profile, names)
		}
	}
	if (HarnessMechanisms{ToolSet: "administrator"}).Validate() == nil {
		t.Fatal("unknown profile accepted")
	}
}
func TestDeferredDiscoveryIsScopedBoundedAndVisibleNextGeneration(t *testing.T) {
	defs := []ToolDefinition{{Name: "read_file"}}
	for i := 0; i < 20; i++ {
		defs = append(defs, ToolDefinition{Name: fmt.Sprintf("authorized_%02d", i), Description: "workspace metadata"})
	}
	s := newMechanismState(Request{Tools: defs, HarnessMechanisms: HarnessMechanisms{DeferredToolDiscovery: true}})
	if s.allowed("authorized_00") || s.allowed("outside") {
		t.Fatal("undiscovered tool callable")
	}
	r := s.discover(map[string]any{"query": "metadata"})
	if len(r["tools"].([]ToolDefinition)) != 5 {
		t.Fatal(r)
	}
	if !s.allowed("authorized_00") || s.allowed("authorized_19") || s.allowed("outside") {
		t.Fatal("scope expanded")
	}
	declarations := []*genai.FunctionDeclaration{}
	for _, d := range defs {
		declarations = append(declarations, &genai.FunctionDeclaration{Name: d.Name})
	}
	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: declarations}}}}
	if _, err := s.beforeModel(nil, req); err != nil {
		t.Fatal(err)
	}
	if len(req.Config.Tools[0].FunctionDeclarations) != 6 {
		t.Fatal("hidden tools leaked into generation")
	}
}
func TestResidentReadProjectionInvalidatesOnCompactionEditsAndChangedContent(t *testing.T) {
	s := newMechanismState(Request{HarnessMechanisms: HarnessMechanisms{ResidentReadReuse: true}})
	call := ToolCall{ID: "first", Name: "read_file", Arguments: map[string]any{"path": "src.py"}}
	data := func(content, receipt string) map[string]any {
		return map[string]any{"isError": false, "data": map[string]any{"content": content, "hash": content, "receiptId": receipt}}
	}
	first := s.project(call, data("before", "receipt1"), false)
	resident := func() {
		_, err := s.beforeModel(nil, &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "first", Name: "read_file", Response: first}}}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	resident()
	call.ID = "second"
	second := s.project(call, data("before", "receipt2"), false)
	if second["reusedEvidence"] == nil || second["receiptRef"].(map[string]any)["receiptId"] != "receipt2" {
		t.Fatal(second)
	}
	if first["data"].(map[string]any)["content"] != "before" {
		t.Fatal("journal result mutated")
	}
	if got := s.project(call, data("changed", "receipt3"), false); got["reusedEvidence"] != nil {
		t.Fatal("changed content reused")
	}
	s.project(ToolCall{Name: "edit_file"}, map[string]any{}, true)
	if len(s.evidence) != 0 {
		t.Fatal("failed possible edit retained cache")
	}
	call.ID = "first"
	s.project(call, data("before", "receipt4"), false)
	resident()
	_, _ = s.beforeModel(nil, &model.LLMRequest{})
	if got := s.project(call, data("before", "receipt5"), false); got["reusedEvidence"] != nil {
		t.Fatal("compacted evidence reused")
	}
}
func TestRepeatedFailureFeedbackUsesOutcomeAndIsBounded(t *testing.T) {
	s := newMechanismState(Request{HarnessMechanisms: HarnessMechanisms{RepeatedFailureFeedback: true}})
	call := ToolCall{Name: "run_command", Arguments: map[string]any{"executable": "tests", "readOnly": true}}
	failed := map[string]any{"isError": false, "data": map[string]any{"exitCode": 1, "stderr": "assertion"}}
	count := 0
	for i := 0; i < 8; i++ {
		if s.project(call, failed, false)["hostFeedback"] != nil {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("feedback count %d", count)
	}
	s = newMechanismState(Request{HarnessMechanisms: HarnessMechanisms{RepeatedFailureFeedback: true}})
	s.project(call, failed, false)
	s.project(call, map[string]any{"data": map[string]any{"exitCode": 0}}, false)
	if s.project(call, failed, false)["hostFeedback"] != nil {
		t.Fatal("success did not reset prior failure")
	}
}
func TestExplorationReminderCannotPressureEditsAndIsReadOnlyScoped(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		s := newMechanismState(Request{ReadOnly: readOnly, HarnessMechanisms: HarnessMechanisms{ExplorationReminder: true}})
		count := 0
		for i := 0; i < 24; i++ {
			r := &model.LLMRequest{}
			_, _ = s.beforeModel(nil, r)
			if r.Config != nil {
				count++
				text := r.Config.SystemInstruction.Parts[0].Text
				if !strings.Contains(text, "Do not edit") {
					t.Fatal(text)
				}
			}
		}
		want := 0
		if readOnly {
			want = 2
		}
		if count != want {
			t.Fatalf("%v count %d", readOnly, count)
		}
	}
}
func TestStreamRepetitionGuardFragmentationAndLegitimateCode(t *testing.T) {
	prose := strings.Repeat("This is a recurring explanatory sentence with ordinary words and no program syntax. ", 3)
	loop := strings.Repeat(prose+"\n\n", 8)
	g := proseRepetitionGuard{}
	var err error
	for i := 0; i < len(loop) && err == nil; i += 7 {
		err = g.Add(loop[i:min(i+7, len(loop))])
	}
	if !errors.Is(err, ErrStreamRepetition) {
		t.Fatalf("loop: %v", err)
	}
	for _, text := range []string{"```text\n" + loop + "```\n", strings.Repeat("    matrix[i] = matrix[i-1] + matrix[i-2]; // "+prose+"\n", 20), strings.Repeat("0,0,0,0,0,0\n", 100), strings.Repeat("- "+prose+"\n", 20)} {
		g = proseRepetitionGuard{}
		if err = g.Add(text); err != nil {
			t.Fatalf("legitimate repetition blocked: %v", err)
		}
	}
}
