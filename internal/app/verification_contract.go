package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/execution"
)

func decodeVerificationContract(value any) (*execution.VerificationContract, error) {
	if value == nil {
		return nil, nil
	}
	b, err := json.Marshal(value)
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("invalid verification contract")
	}
	var contract execution.VerificationContract
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&contract); err != nil {
		return nil, err
	}
	if err = contract.Validate(); err != nil {
		return nil, err
	}
	return &contract, nil
}

// admitTaskCheck freezes a contract's effect on the request before it is
// checkpointed, so resume and recovery read the same values. Ordinary runs get
// host completion feedback; a qualified profile keeps the value it was
// evaluated with. A workflow keeps its configured value too: the workflow
// runtime clears it for every node, so no node gets the host check, and the
// outer agent has no host check, so forced feedback could only re-drive the
// whole graph with reminders.
// A toolSet that hides verify_task (inspect, research) gets neither the host
// check nor reminders, so feedback is not forced and the text names no tool
// the model lacks.
func admitTaskCheck(req *engine.Request, contract *execution.VerificationContract, qualified, workflow bool) {
	req.VerificationRequired = true
	callable := len(req.HarnessMechanisms.Focus([]engine.ToolDefinition{{Name: "verify_task"}})) > 0
	if !qualified && !workflow && callable {
		req.VerificationFeedback = true
	}
	req.Instructions += taskCheckInstructions(contract, callable, callable && req.VerificationFeedback && !req.ReadOnly && !workflow)
}

// taskCheckInstructions describes the user-declared check to the model. It is
// display text only: verify_task always executes the frozen argv, so bounding
// the rendering never changes what runs. The model is told to call verify_task
// only when its catalogue keeps it, and the completion promise is made only
// when the host will actually re-check and report.
func taskCheckInstructions(v *execution.VerificationContract, callable, feedback bool) string {
	words := make([]string, len(v.Command))
	for i, a := range v.Command {
		words[i] = shellWord(a)
	}
	dir := "the workspace root"
	if v.WorkingDirectory != "" && v.WorkingDirectory != "." {
		dir = receiptText(v.WorkingDirectory, 512)
	}
	text := "\n\nTask check: the user declared a check for this task: " + receiptText(strings.Join(words, " "), 2048) + " (working directory: " + dir + ")."
	if callable {
		text += " After your last change, call verify_task (it takes no arguments) to run that check read-only."
	} else {
		text += " Your tools in this run cannot execute it, so do not claim it passed."
	}
	if callable && feedback {
		// Matches the repair feedback, which is granted with room for an edit
		// and an answer only: after a reported failure, verify_task is optional.
		text += " When you finish after changing files, the host runs it again and tells you if it fails; then fix the cause and finish without calling verify_task again, because the host re-runs the check."
	}
	if v.RequireRegressionTest {
		text += " The user also requires a regression test that fails on the original defect and passes after your fix."
	}
	if len(v.ProtectedPaths) > 0 {
		text += " Do not modify these protected paths: " + receiptText(strings.Join(v.ProtectedPaths, ", "), 2048) + "."
	}
	return text
}

func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
