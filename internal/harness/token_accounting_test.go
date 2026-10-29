package harness

import (
	"context"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateFailsClosedForPartialOrLegacyTokensWithoutDroppingFailures(t *testing.T) {
	for _, accounting := range []*inference.TokenAccounting{nil, inference.NewTokenAccounting(2, 1, 1), {Version: 1, Source: "provider_terminal_counts", Status: "complete", Attempts: 0}, {Version: 1, Source: "provider_terminal_counts", Status: "complete", Attempts: 1, TerminalResponses: 2, MeasuredResponses: 1}} {
		tasks, rows := gateFixture()
		rows[0].TokenAccounting = accounting
		gate := EvaluateGate(rows, tasks)
		if gate.Eligible || gate.CostComparisonValid || gate.TokenRatio != 0 || gate.TimeRatio != 0 || gate.BaselineSuccesses != 2 || gate.CandidateSuccesses != 3 || !strings.Contains(strings.Join(gate.Reasons, " "), "token accounting") {
			t.Fatal("partial cost qualified or failure denominator changed", gate)
		}
	}
	tasks, rows := gateFixture()
	g := EvaluateGate(rows, tasks)
	if !g.Eligible || !g.CostComparisonValid || g.CostAccountingStatus != "complete" || g.TokenRatio != 1 || g.TimeRatio != 1 {
		t.Fatal("complete measurement rejected", g)
	}
}
func TestActivationRechecksAccountingAfterRestartAndPreservesLegacyRecords(t *testing.T) {
	for _, mode := range []string{"legacy_both", "legacy_row", "legacy_trial", "partial_both", "malformed", "malformed_both", "complete"} {
		t.Run(mode, func(t *testing.T) {
			tasks, rows := gateFixture()
			tasks = append(tasks, Task{ID: "dev", Split: "development", PublicHash: "public", GraderHash: "grade"})
			repo := &memoryStore{}
			root := t.TempDir()
			activated := 0
			config := Config{Root: root, Store: repo, Tasks: tasks, Activate: func(context.Context, string, string, string, string) error { activated++; return nil }}
			m, err := Open(config)
			if err != nil {
				t.Fatal(err)
			}
			x, err := m.Start(Spec{Mode: "native", WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"dev"}, ModelDigest: "fixture", RuntimeVersion: "fixture", ExecutorIdentity: "sha256:fixture"}, Content{}, Author{})
			if err != nil {
				t.Fatal(err)
			}
			x.Status, x.CandidateID = "completed", x.Spec.BaseRevisionID
			if err = m.put("experiment", x); err != nil {
				t.Fatal(err)
			}
			for i := range rows {
				rows[i].TrialID = fmt.Sprintf("trial-%d", i)
				trialRoot := filepath.Join(root, "trials", rows[i].TrialID, "workspace")
				if err = os.MkdirAll(trialRoot, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(trialRoot, "public.txt"), []byte("synthetic immutable output"), 0600); err != nil {
					t.Fatal(err)
				}
				outputHash, err := treeHash(trialRoot)
				if err != nil {
					t.Fatal(err)
				}
				tr := Trial{ID: rows[i].TrialID, ExperimentID: x.ID, TaskID: rows[i].TaskID, Status: "completed", OutputHash: outputHash, Result: SolveResult{Usage: Doc{"prompt_eval_count": 90, "eval_count": 10}, TokenAccounting: inference.NewTokenAccounting(1, 1, 1)}}
				if i == 0 {
					if mode == "legacy_both" || mode == "legacy_row" {
						rows[i].TokenAccounting = nil
					}
					if mode == "legacy_both" || mode == "legacy_trial" {
						tr.Result.TokenAccounting = nil
					}
					if mode == "malformed" {
						rows[i].TokenAccounting = &inference.TokenAccounting{Version: 1, Source: "provider_terminal_counts", Status: "complete"}
					}
					if mode == "partial_both" {
						rows[i].TokenAccounting = inference.NewTokenAccounting(2, 1, 1)
						tr.Result.TokenAccounting = rows[i].TokenAccounting
					}
					if mode == "malformed_both" {
						rows[i].TokenAccounting = &inference.TokenAccounting{Version: 1, Source: "provider_terminal_counts", Status: "complete"}
						tr.Result.TokenAccounting = rows[i].TokenAccounting
					}
				}
				if err = m.put("trial", tr); err != nil {
					t.Fatal(err)
				}
			}
			v := Evaluation{ID: "evaluation", ExperimentID: x.ID, CandidateID: x.CandidateID, BaselineID: x.Spec.BaseRevisionID, Status: "completed", Results: rows, ReviewPacketID: "review", Gate: Gate{Eligible: true, EvidenceHash: Hash(rows)}}
			if err = m.put("evaluation", v); err != nil {
				t.Fatal(err)
			}
			if err = m.put("packet", Packet{ID: "review", Response: &PacketResponse{Verdict: "approved", AuthoredBy: Author{Kind: "codex", Identity: "independent-test-reviewer"}}}); err != nil {
				t.Fatal(err)
			}
			before, _ := repo.Get("harness_evaluation", v.ID)
			m.Close()
			m, err = Open(config)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			_, err = m.Promote(t.Context(), v.ID)
			if mode == "complete" {
				if err != nil || activated != 1 {
					t.Fatal("complete current evidence rejected", err)
				}
			} else if err == nil || activated != 0 {
				t.Fatal("stored legacy eligible gate bypassed measurement validation", mode, err, activated)
			}
			after, _ := repo.Get("harness_evaluation", v.ID)
			if Hash(before) != Hash(after) {
				t.Fatal("activation rewrote original evidence")
			}
		})
	}
}
func TestRestartDoesNotInferAccountingFromPositiveInterruptedTotals(t *testing.T) {
	repo := &memoryStore{}
	if err := repo.Put("harness_trial", Doc{"id": "old", "status": "running", "result": Doc{"usage": Doc{"prompt_eval_count": 100, "eval_count": 20}}}); err != nil {
		t.Fatal(err)
	}
	m, err := Open(Config{Root: t.TempDir(), Store: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	tr, err := m.trial("old")
	if err != nil || tr.Status != "interrupted" || tr.Result.TokenAccounting != nil || tr.Result.TokenAccounting.Complete() || metric(tr.Result.Usage, "eval_count") != 20 {
		t.Fatal("legacy restart invented accounting", tr, err)
	}
}

func TestIndependentReviewCanRejectUnchangedPartialAndLegacyCost(t *testing.T) {
	for _, mode := range []string{"partial", "legacy", "changed_accounting", "changed_usage", "changed_output"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			activated := false
			m, err := Open(Config{Root: root, Store: &memoryStore{}, Activate: func(context.Context, string, string, string, string) error { activated = true; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			x := Experiment{ID: "experiment", Status: "completed", CandidateID: "candidate", Spec: Spec{WorkspaceID: "workspace", AgentID: "agent", FixtureDigest: m.fixtureDigest()}}
			if err = m.put("experiment", x); err != nil {
				t.Fatal(err)
			}
			trialRoot := filepath.Join(root, "trials", "trial", "workspace")
			if err = os.MkdirAll(trialRoot, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(trialRoot, "public.txt")
			if err = os.WriteFile(file, []byte("unchanged synthetic evidence"), 0600); err != nil {
				t.Fatal(err)
			}
			outputHash, err := treeHash(trialRoot)
			if err != nil {
				t.Fatal(err)
			}
			accounting := inference.NewTokenAccounting(2, 1, 1)
			if mode == "legacy" {
				accounting = nil
			}
			trial := Trial{ID: "trial", ExperimentID: x.ID, Status: "failed", OutputHash: outputHash, Result: SolveResult{Usage: Doc{"prompt_eval_count": 90, "eval_count": 10}, TokenAccounting: accounting}}
			if err = m.put("trial", trial); err != nil {
				t.Fatal(err)
			}
			evaluation := Evaluation{ID: "evaluation", ExperimentID: x.ID, CandidateID: x.CandidateID, Status: "completed", Results: []EvalResult{{TrialID: trial.ID, Tokens: 100, TokenAccounting: accounting}}}
			evaluation.Gate = Gate{Eligible: false, EvidenceHash: Hash(evaluation.Results)}
			packet, err := m.MakePacket(x, "evaluation", "promotion_review", "candidate", "Independent review", []string{evaluation.ID}, map[string]string{"evaluation-evidence.sha256": Hash(evaluation.Results)})
			if err != nil {
				t.Fatal(err)
			}
			evaluation.ReviewPacketID = packet.ID
			if err = m.put("evaluation", evaluation); err != nil {
				t.Fatal(err)
			}
			before := Hash(evaluation)
			switch mode {
			case "changed_accounting":
				trial.Result.TokenAccounting = inference.NewTokenAccounting(2, 2, 2)
				if err = m.put("trial", trial); err != nil {
					t.Fatal(err)
				}
			case "changed_usage":
				trial.Result.Usage["eval_count"] = 11
				if err = m.put("trial", trial); err != nil {
					t.Fatal(err)
				}
			case "changed_output":
				if err = os.WriteFile(file, []byte("changed after grading"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			response := PacketResponse{ID: packet.ID, RequestHash: packet.RequestHash, Verdict: "rejected", Evidence: packet.Evidence, AuthoredBy: Author{Kind: "codex", Identity: "independent-reviewer"}, Notes: "Observed costs are incomplete or unknown; retain the baseline."}
			submitted, err := m.Submit(response)
			if strings.HasPrefix(mode, "changed_") {
				if err == nil {
					t.Fatal("changed evidence was accepted for review")
				}
				var stored Packet
				if err = m.get("packet", packet.ID, &stored); err != nil || stored.Response != nil {
					t.Fatal("failed review mutated packet", err)
				}
				return
			}
			if err != nil || submitted.Response == nil || submitted.Response.Verdict != "rejected" {
				t.Fatal("truthful rejection could not be retained", err, submitted)
			}
			if _, err = m.Submit(response); err != nil {
				t.Fatal("identical rejected review was not idempotent", err)
			}
			if _, err = m.Promote(t.Context(), evaluation.ID); err == nil || activated {
				t.Fatal("rejected/unknown cost activated", err)
			}
			var retained Evaluation
			if err = m.get("evaluation", evaluation.ID, &retained); err != nil || Hash(retained) != before {
				t.Fatal("review rewrote evaluation", err)
			}
		})
	}
}
