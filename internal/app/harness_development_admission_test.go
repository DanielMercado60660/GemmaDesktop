package app

import (
	"context"
	"testing"
)

func TestDevelopmentGradingAdmissionRejectsExistingClients(t *testing.T) {
	core := &Core{}
	if err := core.validateDevelopmentGradeAdmission(t.Context()); err != nil {
		t.Fatal(err)
	}
	core.runs = map[string]context.CancelFunc{"existing": func() {}}
	if err := core.validateDevelopmentGradeAdmission(t.Context()); err == nil {
		t.Fatal("existing chat ignored")
	}
	core.runs = nil
	core.evals = map[string]context.CancelFunc{"existing": func() {}}
	if err := core.validateDevelopmentGradeAdmission(t.Context()); err == nil {
		t.Fatal("existing comparison ignored")
	}
	core.evals = nil
	core.pulls = map[string]context.CancelFunc{"existing": func() {}}
	if err := core.validateDevelopmentGradeAdmission(t.Context()); err == nil {
		t.Fatal("existing model pull ignored")
	}
	core.pulls = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := core.validateDevelopmentGradeAdmission(ctx); err != context.Canceled {
		t.Fatal("cancelled admission continued", err)
	}
}
