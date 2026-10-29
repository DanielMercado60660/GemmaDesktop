package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/harness"
)

// The published Python core drives these transitions through the unchanged
// stdio adapter. Only the Go solver callback is a deterministic, blocked fixture.
func TestHarnessUpstreamActualCoreReportsActiveAndWaitingStages(t *testing.T) {
	for _, terminal := range []string{"completed", "failed"} {
		t.Run(terminal, func(t *testing.T) {
			entered := make(chan harness.SolveRequest)
			release := make(chan struct{})
			var calls atomic.Int32
			_, m, x := harnessTestManager(t, "upstream", func(ctx context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
				calls.Add(1)
				select {
				case entered <- r:
				case <-ctx.Done():
					return harness.SolveResult{}, ctx.Err()
				}
				select {
				case <-release:
				case <-ctx.Done():
					return harness.SolveResult{}, ctx.Err()
				}
				if terminal == "failed" && r.Revision.Content.Instructions == "compose" {
					return harness.SolveResult{Incomplete: true, Usage: harness.Doc{"eval_count": 10}}, errors.New("controlled incomplete final")
				}
				return harness.SolveResult{Content: "controlled public output", Usage: harness.Doc{"eval_count": 10}}, nil
			})
			if x.Status != "ready" {
				t.Fatal("new experiment not ready", x.Status)
			}
			if _, err := m.Advance(x.ID); err != nil {
				t.Fatal(err)
			}
			probe := func(want string) {
				t.Helper()
				select {
				case r := <-entered:
					if r.Revision.Content.Instructions != want {
						t.Fatalf("probe order=%q want %q", r.Revision.Content.Instructions, want)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("probe did not start", want)
				}
				d, err := m.Get("experiment", x.ID)
				if err != nil || d["status"] != "running" || !m.Active() {
					t.Fatalf("active %s reported %v: %v", want, d["status"], err)
				}
				if _, err = m.Advance(x.ID); err == nil {
					t.Fatal("duplicate job admitted")
				}
				release <- struct{}{}
			}
			submit := func(stage string) {
				t.Helper()
				p := waitHarnessStatusPacket(t, m, x.ID, stage)
				r := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "status-test-proposer"}, Content: &harness.Content{Instructions: stage}, Notes: "deterministic status test"}
				if stage == "reflection" {
					r.Content = nil
					r.Verdict = "approved"
					r.AuthoredBy.Identity = "independent-status-reviewer"
					r.Evidence = p.Evidence
				}
				if _, err := m.Submit(r); err != nil {
					t.Fatal(err)
				}
			}
			probe("baseline")
			submit("proposal_inv")
			submit("proposal_dom")
			probe("proposal_inv")
			probe("proposal_dom")
			submit("compose")
			probe("compose")
			if terminal == "completed" {
				submit("reflection")
			}
			waitHarnessStatusIdle(t, m)
			d, err := m.Get("experiment", x.ID)
			if err != nil || d["status"] != terminal || calls.Load() != 4 || number(d, "rollouts", 0) != 4 || number(d, "improvementStages", 0) != 3 {
				t.Fatal("terminal outcome or method accounting changed", d, calls.Load(), err)
			}
		})
	}
}

func TestHarnessUpstreamCancellationSurvivesLateSolverAndPacketExit(t *testing.T) {
	for _, stage := range []string{"active_solver", "waiting_packet"} {
		t.Run(stage, func(t *testing.T) {
			entered := make(chan struct{})
			_, m, x := harnessTestManager(t, "upstream", func(ctx context.Context, _ harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
				if stage == "active_solver" {
					close(entered)
					<-ctx.Done()
					// Simulate a solver that returns a stale success after cancellation.
				}
				return harness.SolveResult{Content: "late deterministic output", Usage: harness.Doc{"eval_count": 10}}, nil
			})
			if _, err := m.Advance(x.ID); err != nil {
				t.Fatal(err)
			}
			if stage == "active_solver" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("solver did not start")
				}
			} else {
				waitHarnessStatusPacket(t, m, x.ID, "proposal_inv")
			}
			if err := m.Cancel(x.ID); err != nil {
				t.Fatal(err)
			}
			waitHarnessStatusIdle(t, m)
			d, err := m.Get("experiment", x.ID)
			if err != nil || d["status"] != "cancelled" {
				t.Fatal("late worker exit overwrote cancellation", d, err)
			}
			if _, err = m.Advance(x.ID); err == nil {
				t.Fatal("cancelled experiment restarted")
			}
		})
	}
}

func waitHarnessStatusPacket(t *testing.T, m *harness.Manager, id, stage string) harness.Packet {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := m.List("packet")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["stage"] != stage || row["status"] != "awaiting_codex" {
				continue
			}
			d, err := m.Get("experiment", id)
			if err != nil {
				t.Fatal(err)
			}
			// Packet creation and parent status persistence are separate writes. Wait
			// until the driver has reached its actual blocked packet stage.
			if d["status"] != "awaiting_codex" {
				continue
			}
			if !m.Active() {
				t.Fatal("upstream packet wait lost active worker")
			}
			full, err := m.Get("packet", str(row, "id"))
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(full)
			var p harness.Packet
			if err = json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("upstream packet did not wait", stage)
	return harness.Packet{}
}
func waitHarnessStatusIdle(t *testing.T, m *harness.Manager) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.Active() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.Active() {
		t.Fatal("upstream worker did not finish")
	}
}
