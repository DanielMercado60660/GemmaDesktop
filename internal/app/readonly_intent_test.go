package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
)

func TestExplicitWorkspaceReadOnlyRecognizesOnlyGlobalDirectives(t *testing.T) {
	for _, prompt := range []string{
		"Analyze the logs. Don't change or create any files.",
		"Please do not modify any files; explain your findings.",
		"Do not write to any files.",
		"Don’t edit files in this workspace.",
		"- Do not change files at all.",
		"Keep the workspace read-only.",
		"Leave all files unchanged.",
		"Do not change the project.",
		"Do not change any files. Now summarize the results.",
		"Write your answer in chat. Do not change any files.",
		"Our shop's web server log is at logs/access.log and covers October 14–15. Which endpoint (by path, ignoring query strings) had the most 5xx errors on October 15, and how many? Also, how many 5xx responses were there in total that day? Please don't change or create any files, I just need the numbers.",
	} {
		t.Run(prompt, func(t *testing.T) {
			constraint := explicitWorkspaceReadOnly(prompt)
			if constraint == "" || !strings.Contains(prompt, constraint) {
				t.Fatalf("exact directive lost: %q", constraint)
			}
		})
	}
	for _, prompt := range []string{
		"Fix source.py; don't modify tests/test_source.py.",
		"Do not change any files in tests/.",
		"Don't create any files; edit the existing module.",
		"Don't delete any files.",
		"Don't modify any files except source.py.",
		"Do not change files unless necessary.",
		"If possible, don't change any files.",
		"The reviewer said don't change any files.",
		"Explain the phrase \"Don't change files. Do not edit any files.\"",
		"'Don't change files. Do not edit any files.' is a quote.",
		"> Don't change any files.\nFix source.py.",
		"```text\nDon't change any files.\n```\nFix source.py.",
		"~~~text\nDo not edit files.\n~~~\nFix source.py.",
		"    Don't change any files.\nFix source.py.",
		"Add the message `Don't change any files` to README.",
		"Do not change files.csv.",
		"Do not change any files?",
		"Do not change any files. Actually, create summary.md.",
		"Keep the workspace unchanged. Except write report.md.",
		"Do not change any files. You can edit source.py.",
		"Do not change any files. Instead fix source.py.",
		"Do not change any files, I only need you to edit source.py.",
	} {
		t.Run(prompt, func(t *testing.T) {
			if constraint := explicitWorkspaceReadOnly(prompt); constraint != "" {
				t.Fatalf("narrow or quoted instruction widened to workspace constraint: %q", constraint)
			}
		})
	}
}

func completedReadOnlyAdmission(t *testing.T, h *integrationHarness, seen func() []inference.ChatRequest, method string, p Doc) (Doc, taskCheckAdmission) {
	t.Helper()
	before := len(seen())
	run := callIntegration(t, h.core, method, p)
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	cp, err := h.core.Store.Get("checkpoint", str(run, "id"))
	if err != nil {
		t.Fatal(err)
	}
	var request engine.Request
	b, _ := json.Marshal(cp["request"])
	if err = json.Unmarshal(b, &request); err != nil {
		t.Fatal(err)
	}
	if len(seen()) <= before {
		t.Fatal("no model request recorded")
	}
	return cp, taskCheckAdmission{request: request, outgoing: seen()[before]}
}

func TestWorkspaceReadOnlyAdmissionAndContinuation(t *testing.T) {
	h, seen := newTaskCheckHarness(t, false)
	constraint := "Don't change or create any files"
	cp, a := completedReadOnlyAdmission(t, h, seen, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Analyze the logs. " + constraint + "."})
	assertReadOnly := func(a taskCheckAdmission, commands bool) {
		t.Helper()
		if !a.request.ReadOnly || !strings.Contains(a.system(), constraint) || a.declares("run_command") != commands {
			t.Fatalf("constraint/command policy missing: readonly=%v commands=%v system=%q", a.request.ReadOnly, a.declares("run_command"), a.system())
		}
		for _, name := range []string{"write_file", "edit_file", "delete_file", "move_file", "make_directory", "skill_materialize", "mcp_call", "browser_open", "setup_dependencies"} {
			if a.declares(name) {
				t.Fatalf("advertised %s in a read-only run", name)
			}
		}
	}
	assertReadOnly(a, true)
	if cp["mode"] != "read-only" || str(obj(cp["workspaceReadOnly"]), "constraint") != constraint {
		t.Fatal("checkpoint did not freeze the exact constraint", cp["workspaceReadOnly"])
	}
	nextCP, next := completedReadOnlyAdmission(t, h, seen, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Please continue."})
	assertReadOnly(next, true)
	focusCP, focused := completedReadOnlyAdmission(t, h, seen, "context.focus", Doc{"runId": nextCP["id"], "prompt": "Continue"})
	assertReadOnly(focused, true)
	_, corrected := completedReadOnlyAdmission(t, h, seen, "context.focus", Doc{"runId": focusCP["id"], "prompt": "Now create report.md."})
	if corrected.request.ReadOnly || !corrected.declares("write_file") {
		t.Fatal("focused continuation ignored an explicit current-user write correction")
	}
}

func TestWorkspaceReadOnlyDoesNotRestrictIndependentTaskOrExpandConfiguredMode(t *testing.T) {
	h, seen := newTaskCheckHarness(t, false)
	_, first := completedReadOnlyAdmission(t, h, seen, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Do not change any files."})
	if !first.request.ReadOnly {
		t.Fatal("initial constraint was not enforced")
	}
	_, independent := completedReadOnlyAdmission(t, h, seen, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Now fix source.py. Don't change tests/test_source.py."})
	if independent.request.ReadOnly || !independent.declares("write_file") || strings.Contains(independent.request.Instructions, "Workspace constraint") {
		t.Fatal("prior turn-specific restriction leaked into a newly admitted repair")
	}
	callIntegration(t, h.core, "settings.save", Doc{"mode": "read-only"})
	_, configured := completedReadOnlyAdmission(t, h, seen, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Do not change any files."})
	if !configured.request.ReadOnly || configured.declares("run_command") || configured.declares("write_file") {
		t.Fatal("inference widened configured read-only permissions")
	}
}

func TestWorkspaceReadOnlyWorkflowChildCannotWidenAuthority(t *testing.T) {
	for _, childMode := range []string{"approval", "read-only"} {
		t.Run(childMode, func(t *testing.T) {
			h, seen := newTaskCheckHarness(t, false)
			child := callIntegration(t, h.core, "agent.save", Doc{"name": "Child", "mode": childMode, "maxSteps": 3})
			wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Read logs", "nodes": []Doc{{"id": "review", "type": "agent", "agentId": child["id"], "prompt": "Review the logs"}}, "edges": []Doc{}})
			_, a := completedReadOnlyAdmission(t, h, seen, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "Do not change any files."})
			if !a.request.ReadOnly || a.declares("write_file") || a.declares("mcp_call") || a.declares("run_command") != (childMode == "approval") || !strings.Contains(a.system(), "Do not change any files") {
				t.Fatal("workflow child lost or widened frozen workspace constraint", a.system())
			}
		})
	}
}

func TestWorkspaceReadOnlyCommandApprovalCannotBeWidened(t *testing.T) {
	var calls atomic.Int32
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		if calls.Add(1) == 1 {
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"cmd","function":{"name":"run_command","arguments":{"executable":"python3","args":["-c","print(6*7)"],"readOnly":false}}}]},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"The command was declined."},"done":true}`)
	})
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Calculate the total. Do not change any files."})
	approval := pendingIntegration(t, h.core, str(run, "id"))
	if obj(approval["arguments"])["readOnly"] != true {
		t.Fatal("model false reached approval unchanged", approval)
	}
	callIntegration(t, h.core, "approval.respond", Doc{"id": approval["id"], "allow": false})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
}

func TestWorkspaceReadOnlyWorkflowUsesChildExecutor(t *testing.T) {
	var calls atomic.Int32
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		if calls.Add(1) == 1 {
			for _, tool := range req.Tools {
				if tool.Function.Name == "run_command" && strings.Contains(tool.Function.Description, "Docker Linux") {
					t.Error("child command declaration borrowed parent Docker executor")
				}
			}
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"cmd","function":{"name":"run_command","arguments":{"executable":"python3","args":["-c","print(42)"],"readOnly":false}}}]},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"The command was declined."},"done":true}`)
	})
	settings := h.core.Settings()
	settings["dockerImage"] = "sha256:" + strings.Repeat("a", 64)
	if err := h.core.Store.Put("settings", settings); err != nil {
		t.Fatal(err)
	}
	parentProfile := callIntegration(t, h.core, "capability.save", Doc{"name": "Parent Docker", "tools": []string{"run_command"}, "executionProfile": "docker"})
	childProfile := callIntegration(t, h.core, "capability.save", Doc{"name": "Child restricted", "tools": []string{"run_command"}, "executionProfile": "restricted", "privatePaths": []string{"private-build"}})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Parent", "mode": "approval", "capabilityProfileId": parentProfile["id"], "maxSteps": 4})
	child := callIntegration(t, h.core, "agent.save", Doc{"name": "Child", "mode": "approval", "capabilityProfileId": childProfile["id"], "maxSteps": 4})
	wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Read logs", "nodes": []Doc{{"id": "review", "type": "agent", "agentId": child["id"], "prompt": "Calculate"}}, "edges": []Doc{}})
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "Do not change any files."})
	approval := pendingIntegration(t, h.core, str(run, "id"))
	if obj(approval["arguments"])["readOnly"] != true || !strings.Contains(str(approval, "reason"), "macOS single-process sandbox") || strings.Contains(str(approval, "reason"), "Docker Linux") {
		t.Fatal("workflow child used parent executor or writable authority", approval)
	}
	callIntegration(t, h.core, "approval.respond", Doc{"id": approval["id"], "allow": false})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
}
