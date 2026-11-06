package harness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestV2RuntimeLifecycleSwitchesDistinctArmsAndNeverReplaysCachedTrials(t *testing.T) {
	m, a, b, _ := managerForV2(t)
	if a.Spec.CachePolicy != ColdEachAttemptV2 || b.Spec.CachePolicy != ColdEachAttemptV2 {
		t.Fatal("cache policy was not frozen")
	}
	order := []string{}
	active := ""
	m.config.PrepareAttempt = func(ctx context.Context, s Spec) error {
		if active != "" {
			t.Fatal("previous arm was not released")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Minute {
			t.Fatal("preparation unbounded")
		}
		active = s.Model
		order = append(order, "prepare:"+s.Model)
		return nil
	}
	solve := m.config.Solve
	m.config.Solve = func(ctx context.Context, r SolveRequest, emit func(Event)) (SolveResult, error) {
		if active != r.Spec.Model {
			t.Fatal("solver lacks its own runtime")
		}
		order = append(order, "solve:"+r.Spec.Model)
		return solve(ctx, r, emit)
	}
	m.config.ReleaseRuntime = func(ctx context.Context, s Spec) error {
		if ctx.Err() != nil {
			t.Fatal("release inherited cancellation")
		}
		order = append(order, "release:"+s.Model)
		active = ""
		return nil
	}
	grade := m.config.GradeWithSpec
	m.config.GradeWithSpec = func(ctx context.Context, r GradeRequest) (Grade, error) {
		if active != "" {
			t.Fatal("grader retained model runtime")
		}
		return grade(ctx, r)
	}
	r, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID, b.ID}, Repeats: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 2}); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if !reflect.DeepEqual(order, []string{"prepare:baseline", "solve:baseline", "release:baseline", "prepare:candidate", "solve:candidate", "release:candidate"}) {
		t.Fatal(order)
	}
	task := m.packs[r.PackID].Tasks[0]
	ctx, cancel := context.WithTimeout(t.Context(), attemptEnvelopeV2)
	defer cancel()
	before := len(order)
	again := m.executeScheduledV2(ctx, r, a, task, r.Schedule[0])
	if again.InfrastructureError != "" || len(order) != before {
		t.Fatal("cached trial restarted runtime", again, order)
	}
}

func TestV2RuntimeReleaseFailureIsDurableInfrastructureAndStopsBatch(t *testing.T) {
	m, a, _, _ := managerForV2(t)
	m.config.ReleaseRuntime = func(context.Context, Spec) error { return errors.New("owned runtime did not stop") }
	r, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID}, Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 2}); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Cursor != 1 || r.Status != "checkpointed" || len(r.Results) != 1 || !strings.Contains(r.Results[0].InfrastructureError, "runtime release") {
		t.Fatal("cleanup failure became ordinary solver evidence", r)
	}
	trial, err := m.trial(r.Results[0].TrialID)
	if err != nil || !strings.Contains(trial.InfrastructureError, "runtime release") {
		t.Fatal("cleanup failure was not durable", trial, err)
	}
}

func TestV2PreparationFailureAndCancellationStillReleaseOwnedRuntime(t *testing.T) {
	for _, phase := range []string{"prepare", "solve"} {
		t.Run(phase, func(t *testing.T) {
			m, a, _, _ := managerForV2(t)
			released := false
			solved := false
			ctx, cancel := context.WithTimeout(t.Context(), attemptEnvelopeV2)
			defer cancel()
			m.config.PrepareAttempt = func(context.Context, Spec) error {
				if phase == "prepare" {
					cancel()
					return errors.New("partial startup failed")
				}
				return nil
			}
			m.config.Solve = func(context.Context, SolveRequest, func(Event)) (SolveResult, error) {
				solved = true
				cancel()
				return SolveResult{}, context.Canceled
			}
			m.config.ReleaseRuntime = func(ctx context.Context, _ Spec) error {
				if ctx.Err() != nil {
					t.Fatal("cancelled context prevented cleanup")
				}
				released = true
				return nil
			}
			r, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID}, Repeats: 1})
			if err != nil {
				t.Fatal(err)
			}
			out := m.executeScheduledV2(ctx, r, a, m.packs[r.PackID].Tasks[0], r.Schedule[0])
			if !released || solved != (phase == "solve") {
				t.Fatal("runtime lifecycle incomplete", released, solved, out)
			}
			if phase == "prepare" && !strings.Contains(out.InfrastructureError, "runtime preparation") {
				t.Fatal("prepare error not infrastructure", out)
			}
		})
	}
}
