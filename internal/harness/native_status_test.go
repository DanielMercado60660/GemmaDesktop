package harness

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeStatusDistinguishesActiveProbesFromUnansweredPackets(t *testing.T) {
	entered := make(chan SolveRequest)
	release := make(chan struct{})
	var calls atomic.Int32
	m, err := Open(Config{Root: t.TempDir(), Store: &memoryStore{}, Tasks: []Task{{ID: "public-status-task", Split: "development", Public: map[string]string{"README.md": "Public status-only test fixture.", "source.txt": "unchanged"}, PublicHash: "public-fixture", GraderHash: "no-grader-invoked"}}, Solve: func(ctx context.Context, r SolveRequest, _ func(Event)) (SolveResult, error) {
		calls.Add(1)
		select {
		case entered <- r:
		case <-ctx.Done():
			return SolveResult{}, ctx.Err()
		}
		select {
		case <-release:
			return SolveResult{Content: "Deterministic test output", Usage: Doc{"eval_count": 1}}, nil
		case <-ctx.Done():
			return SolveResult{}, ctx.Err()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	x, err := m.Start(Spec{Mode: "native", WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"public-status-task"}, ModelDigest: "fixture", RuntimeVersion: "fixture", ExecutorIdentity: "sha256:fixture"}, Content{Instructions: "baseline"}, Author{})
	if err != nil {
		t.Fatal(err)
	}
	status := func(want string) {
		t.Helper()
		current, e := m.Get("experiment", x.ID)
		if e != nil || current["status"] != want {
			t.Fatalf("status=%v, want %s: %v", current["status"], want, e)
		}
	}
	advance := func() {
		t.Helper()
		if _, e := m.Advance(x.ID); e != nil {
			t.Fatal(e)
		}
	}
	probe := func(want string) {
		t.Helper()
		select {
		case r := <-entered:
			if r.Revision.Content.Instructions != want {
				t.Fatalf("probe order %q, want %q", r.Revision.Content.Instructions, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("expected probe did not start")
		}
		status("running")
		if !m.Active() {
			t.Fatal("active probe missing from worker registry")
		}
		if _, e := m.Advance(x.ID); e == nil {
			t.Fatal("duplicate execution admitted")
		}
	}
	submit := func(stage string) {
		t.Helper()
		rows, e := m.config.Store.List("harness_packet")
		if e != nil {
			t.Fatal(e)
		}
		for _, row := range rows {
			p, e := decode[Packet](row)
			if e != nil {
				t.Fatal(e)
			}
			if p.Stage != stage {
				continue
			}
			response := PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: Author{Kind: "codex", Identity: "test-proposer"}, Content: &Content{Instructions: stage}, Notes: "status-only fixture"}
			if stage == "reflection" {
				response.Content = nil
				response.Verdict = "approved"
				response.AuthoredBy.Identity = "independent-test-reviewer"
				response.Evidence = p.Evidence
			}
			if _, e = m.Submit(response); e != nil {
				t.Fatal(e)
			}
			return
		}
		t.Fatal("missing packet", stage)
	}
	advance()
	probe("baseline")
	release <- struct{}{}
	waitIdle(t, m)
	status("awaiting_codex")
	// A submitted DOM packet must not mask an unanswered INV packet.
	submit("proposal_dom")
	advance()
	waitIdle(t, m)
	status("awaiting_codex")
	if calls.Load() != 1 {
		t.Fatal("probe ran before both proposals arrived")
	}
	submit("proposal_inv")
	advance()
	probe("proposal_inv")
	release <- struct{}{}
	probe("proposal_dom")
	release <- struct{}{}
	waitIdle(t, m)
	status("awaiting_codex")
	submit("compose")
	advance()
	probe("compose")
	release <- struct{}{}
	waitIdle(t, m)
	status("awaiting_codex")
	submit("reflection")
	advance()
	waitIdle(t, m)
	status("completed")
	var final Experiment
	if err = m.get("experiment", x.ID, &final); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || final.Rollouts != 4 || final.ImprovementStages != 3 || len(final.PacketIDs) != 4 {
		t.Fatal("status change altered k=1 accounting", calls.Load(), final)
	}
}
