package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/inference"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixturePackForTestV2() FixturePack {
	p := FixturePack{ID: "test-v2", Version: "2.0.0", GraderPolicy: "test-policy-v2"}
	for _, split := range []string{"development", "validation", "heldout"} {
		t := Task{ID: split, Name: split, Split: split, Stack: "python", Contract: "coding", ExposureFamily: "family-" + split, Prompt: "Inspect public input and repair it", Public: map[string]string{"README.md": split, "logic.py": "broken", "public_test.py": "public"}, ProtectedFiles: []string{"README.md", "public_test.py"}, Command: []string{"python3", "public_test.py"}, GradeCommand: []string{"python3", "main.py"}, ExpectedAnswer: json.RawMessage(`{"sealed":"private"}`), Cases: []Case{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
		t.PublicHash = Hash(t.Public)
		t.GraderHash = fixtureIdentity(t, p.GraderPolicy)
		p.Tasks = append(p.Tasks, t)
	}
	p.Digest = packDigest(p)
	return p
}

func managerForV2(t *testing.T) (*Manager, ArmV2, ArmV2, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	p := fixturePackForTestV2()
	m, err := Open(Config{Root: t.TempDir(), Store: &memoryStore{}, Packs: []FixturePack{p}, Solve: func(ctx context.Context, req SolveRequest, _ func(Event)) (SolveResult, error) {
		calls.Add(1)
		if len(req.Task.Cases) > 0 || len(req.Task.ExpectedAnswer) > 0 || req.Task.Probe != nil || req.Task.Public != nil || len(req.Task.GradeCommand) > 0 {
			t.Error("private grading material reached solver")
		}
		if err := os.WriteFile(filepath.Join(req.Root, "logic.py"), []byte(req.Spec.Model), 0600); err != nil {
			return SolveResult{}, err
		}
		time.Sleep(2 * time.Millisecond)
		return SolveResult{Content: "finished", Usage: Doc{"prompt_eval_count": 10, "eval_count": 2}, TokenAccounting: inference.NewTokenAccounting(1, 1, 1)}, nil
	}, GradeWithSpec: func(_ context.Context, req GradeRequest) (Grade, error) {
		if req.Task.ExpectedAnswer == nil {
			t.Error("trusted grader lost expected answer")
		}
		passed := req.Spec.Model == "candidate"
		return Grade{Passed: passed, Checks: []Check{{Name: "semantic contract", Passed: passed}, {Name: "integrity: protected files", Passed: true, Critical: true}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	s := Spec{WorkspaceID: "workspace", AgentID: "agent", Model: "baseline", ModelDigest: "baseline-digest", RuntimeVersion: "runtime", RuntimeIdentity: Doc{"backend": "fixture", "revision": "runtime", "artifactDigest": "baseline-digest"}, AppVersion: "same-host", ExecutorIdentity: "sha256:executor", FrozenCapability: json.RawMessage(`{"tools":["read_file","edit_file"]}`), BudgetSeconds: 1200, ApprovedSkills: map[string]json.RawMessage{}}
	a, err := m.RegisterArmV2(ArmV2{Name: "Current product", Spec: s, Provenance: "product:frozen", Revision: Revision{Content: Content{Instructions: "base"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Model = "candidate"
	s.ModelDigest = "candidate-digest"
	s.RuntimeIdentity = Doc{"backend": "other-local", "revision": "runtime", "artifactDigest": "candidate-digest"}
	b, err := m.RegisterArmV2(ArmV2{Name: "Candidate", Spec: s, Provenance: "development:screen", Revision: Revision{Content: Content{Instructions: "candidate"}}})
	if err != nil {
		t.Fatal(err)
	}
	return m, a, b, calls
}

func TestV2DiagnosticCheckpointUsesSharedCallbacksAndDistinctArms(t *testing.T) {
	m, a, b, calls := managerForV2(t)
	r, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID, b.ID}, Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Schedule) != 4 || r.Schedule[0].ArmID != a.ID || r.Schedule[2].ArmID != b.ID {
		t.Fatal("counterbalanced schedule missing", r.Schedule)
	}
	_, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Status != "checkpointed" || r.Cursor != 1 || calls.Load() != 1 {
		t.Fatal("batch did not stop cleanly", r.Status, r.Cursor, calls.Load())
	}
	before, _ := m.ExportRunV2(r.ID)
	again, _ := m.ExportRunV2(r.ID)
	if before["payloadHash"] != again["payloadHash"] {
		t.Fatal("export is not stable")
	}
	raw, _ := json.Marshal(before)
	if strings.Contains(string(raw), "sealed") || strings.Contains(string(raw), "expectedAnswer") {
		t.Fatal("private grading material exported")
	}
	_, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Status != "completed" || len(r.Results) != 4 || calls.Load() != 4 {
		t.Fatal("resume changed denominator", r.Status, calls.Load())
	}
	if r.Results[0].Grade.Passed || !r.Results[1].Grade.Passed {
		t.Fatal("arms did not use their own frozen configuration")
	}
	if _, err = m.QualifiedArmV2(r.ID); err == nil {
		t.Fatal("diagnostic authorized promotion")
	}
}

func TestV2QualificationReservesExposureAndRequiresIndependentReview(t *testing.T) {
	m, a, b, calls := managerForV2(t)
	r, err := m.CreateEvaluationV2(EvaluationV2Request{PackID: "test-v2", BaselineArmID: a.ID, CandidateArmID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.CreateEvaluationV2(EvaluationV2Request{PackID: "test-v2", BaselineArmID: a.ID, CandidateArmID: b.ID})
	if err != nil || again.ID != r.ID {
		t.Fatal("admission not idempotent", err)
	}
	if _, err = m.CreateEvaluationV2(EvaluationV2Request{PackID: "test-v2", BaselineArmID: b.ID, CandidateArmID: a.ID}); err == nil {
		t.Fatal("exposed heldout reused")
	}
	for r.Status != "completed" {
		if _, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 4}); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, m)
		r, _ = m.GetRunV2(r.ID)
	}
	if calls.Load() != 12 || !r.Gate.Eligible || r.Gate.CandidateSuccesses != 3 {
		t.Fatal("incomplete paired qualification", calls.Load(), r.Gate)
	}
	if _, err = m.QualifiedArmV2(r.ID); err == nil {
		t.Fatal("unreviewed qualification promoted")
	}
	bad := ReviewV2{Author: Author{Kind: "codex", Identity: "agent"}, Verdict: "approved", EvidenceHash: r.Gate.EvidenceHash, Notes: "review"}
	if _, err = m.ReviewRunV2(r.ID, bad); err == nil {
		t.Fatal("author reviewed own candidate")
	}
	bad.Author.Identity = "independent-reviewer"
	if _, err = m.ReviewRunV2(r.ID, bad); err != nil {
		t.Fatal(err)
	}
	qualified, err := m.QualifiedArmV2(r.ID)
	if err != nil || qualified.ID != b.ID {
		t.Fatal("complete profile not returned", err)
	}
	bad.Verdict = "rejected"
	if _, err = m.ReviewRunV2(r.ID, bad); err == nil {
		t.Fatal("immutable review overwritten")
	}
	trial, _ := m.trial(r.Results[0].TrialID)
	_ = os.WriteFile(filepath.Join(trial.Root, "logic.py"), []byte("changed"), 0600)
	if _, err = m.QualifiedArmV2(r.ID); err == nil {
		t.Fatal("output tampering accepted")
	}
}

func TestV2RestartNeverReplaysInterruptedSolver(t *testing.T) {
	m, a, _, calls := managerForV2(t)
	r, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID}, Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	task := m.packs[r.PackID].Tasks[0]
	s := r.Schedule[0]
	trial, err := m.solveAdmittedTrial(context.Background(), Experiment{ID: r.ID, Spec: a.Spec}, task, fmtAttemptLabelV2(s), a.Revision, r.Kind)
	if err != nil {
		t.Fatal(err)
	}
	trial.Status = "interrupted"
	if err = m.put("trial", trial); err != nil {
		t.Fatal(err)
	}
	r.Status = "interrupted"
	if err = m.put("run_v2", r); err != nil {
		t.Fatal(err)
	}
	_, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if calls.Load() != 2 || r.Results[0].TrialStatus != "interrupted" || r.Results[0].Grade.Passed {
		t.Fatal("interrupted attempt replayed or erased", calls.Load(), r.Results)
	}
}

func TestV2RejectsUnsafeBoundsAndIdentityDrift(t *testing.T) {
	m, a, _, _ := managerForV2(t)
	for _, l := range []BatchLimits{{WallSeconds: 7201}, {WallSeconds: 1800}, {MaxAttempts: 5}} {
		if _, err := normalizedLimitsV2(l); err == nil {
			t.Fatal("unsafe batch limits accepted", l)
		}
	}
	if _, err := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"heldout"}, ArmIDs: []string{a.ID}, Repeats: 1}); err == nil {
		t.Fatal("heldout leaked into diagnostics")
	}
	r, _ := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID}, Repeats: 1})
	a.Spec.Model = "mutated"
	_ = m.put("arm_v2", a)
	if _, err := m.AdvanceRunV2(r.ID, BatchLimits{}); err == nil {
		t.Fatal("arm identity drift accepted")
	}
}

func TestV2InfrastructureFailurePreservesAttemptAndCheckpoints(t *testing.T) {
	m, a, _, calls := managerForV2(t)
	m.config.ValidateEnvironment = func(context.Context, Spec) error { return errors.New("runtime unavailable") }
	r, _ := m.CreateDiagnosticV2(DiagnosticRequest{PackID: "test-v2", TaskIDs: []string{"development"}, ArmIDs: []string{a.ID}, Repeats: 2})
	_, err := m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Cursor != 1 || r.Status != "checkpointed" || r.Results[0].InfrastructureError == "" || calls.Load() != 0 {
		t.Fatal("infrastructure failure erased or continued", r)
	}
}

func TestVersionedPacksPreserveLegacyAndSealAssertions(t *testing.T) {
	legacy, err := LoadFixtures()
	if err != nil || len(legacy) != 18 {
		t.Fatal("legacy fixtures changed", err)
	}
	packs, err := LoadFixturePacks()
	if err != nil || len(packs) != 3 {
		t.Fatal("pack loading failed", err)
	}
	// These opaque source hashes pin every original catalog/task/reference byte.
	// New coverage is a sibling, never a rewrite of the original split material.
	expectedOriginalSources := map[string]string{
		"packs/coding-diagnostics-v2/duration-units/reference.json":          "3c4c814648c34a6a0663951c4735022461a92485c301c5ef6dd2938ba7564da9",
		"packs/coding-diagnostics-v2/duration-units/task.json":               "35ffcb4fe63e26221fb5f0ca10f7e65eac924833f92482af25137648b91bf391",
		"packs/coding-diagnostics-v2/event-join/reference.json":              "9fec648b9584a0af1e805e0ff5de11dc38e8461f25567d88001ea5bb2389939b",
		"packs/coding-diagnostics-v2/event-join/task.json":                   "6782f449d99ff62f76ca7731dbcc012f6a5d2620f39d2b6eb3bfaf93431b0f19",
		"packs/coding-diagnostics-v2/inspect-routing/reference.json":         "ca3d163bab055381827226140568f3bef7eaac187cebd76878e0b63e9e442356",
		"packs/coding-diagnostics-v2/inspect-routing/task.json":              "c88d153f8c0240ee157a75f0d5600221686a3154f7bdd312bc8e888b0c4611f1",
		"packs/coding-diagnostics-v2/json-pointer/reference.json":            "622cd6e9e88a23ef672f16076ec33856335abc3719f47041b49d50e4e4dbfde6",
		"packs/coding-diagnostics-v2/json-pointer/task.json":                 "83a958ad5389c35c6c8284070c8ddc757f922f5a9ad6a04440bec4fe3e10c8f2",
		"packs/coding-diagnostics-v2/literal-spans/reference.json":           "230a33ecb69d2620518dedb272f386813b5fe6701083144871123b207c076e67",
		"packs/coding-diagnostics-v2/literal-spans/task.json":                "73a00ced23226222c6a655a94af2e7942a8c1f54384142c9c506fa531a5b8f69",
		"packs/coding-diagnostics-v2/manifest.json":                          "1796b896589ad8965b3a41d8b866e076b0214622eeeb837005c7395680daad3d",
		"packs/coding-diagnostics-v2/path-boundary/reference.json":           "5e67dd200787fa58e1e9e362d847815ec215b65e4a3079ef7ff836793c0c2958",
		"packs/coding-diagnostics-v2/path-boundary/task.json":                "4a2036c6952587f21ec830f14259c521d73985146961510bde74530d11be9d69",
		"packs/coding-qualification-v2/h-array-depth/reference.json":         "96fcfe0e744ab0c05e3fcddc566840cbb392884ddbf2375e46c8ed794ad19e04",
		"packs/coding-qualification-v2/h-array-depth/task.json":              "6335eb75c679cb68e8148d48996692679cf821ba68bee9a39832108cb48b1243",
		"packs/coding-qualification-v2/h-balanced-markers/reference.json":    "1ebbcd4abd6f1ac59383db3e6157b3f93b1fc4aebfe5f29ad0beba06ee84020b",
		"packs/coding-qualification-v2/h-balanced-markers/task.json":         "98f48d13e3e9acd3e497d9b71c4bfd18f1d78d83a3b918fbf55167afbe9c6066",
		"packs/coding-qualification-v2/h-deduplicate-records/reference.json": "68c922001f47d82b6c8daa1e8d84ca3db654160aebc391b47c1cbe90a46ed452",
		"packs/coding-qualification-v2/h-deduplicate-records/task.json":      "898cfe79a296751257e682110dab60c0d2cd9dfbdd8d2436649e9458cd2964e9",
		"packs/coding-qualification-v2/h-signed-rounded/reference.json":      "bf3726bfff406f221e1429b28a0540e942bc50e3d934b5852c743f6e945dc83e",
		"packs/coding-qualification-v2/h-signed-rounded/task.json":           "2e3b47d699f7500d0ff95862db852108e7bc6ed9254a02567cd98d2f5f96bfd1",
		"packs/coding-qualification-v2/h-suffix-trim/reference.json":         "146b8de351dd9bc112c014b5a7056d9a7021b0c080be2692577c8f142e22f3e2",
		"packs/coding-qualification-v2/h-suffix-trim/task.json":              "1a3c11af12bc21e1a7f92e948832685d25009a8c413b4316da258f1814e6c83d",
		"packs/coding-qualification-v2/h-window-minimum/reference.json":      "92324638141da296a9b7fe40fa19a2cfcdc85ad492eea70226db7e839361bfe3",
		"packs/coding-qualification-v2/h-window-minimum/task.json":           "1551053041284b5829adde1264a39545d2b6a1db2d1a36af9f5aedb749135227",
		"packs/coding-qualification-v2/manifest.json":                        "0376b7b34f71436b31b406e496b79970d07980d4bc1ae3f7edd264a7ac4a91f8",
		"packs/coding-qualification-v2/v-byte-chunks/reference.json":         "6bbc3dea5b3b906be816b4308e1b61f41baca5e4a964f98fce887e91e6c8cbf4",
		"packs/coding-qualification-v2/v-byte-chunks/task.json":              "75bed57f4ae4fbde02c5be8cac269faecaa2559df84bdb9e33587f7c84e23aeb",
		"packs/coding-qualification-v2/v-circular-distance/reference.json":   "808c196eea7d3a6ede311a78f59dab0515a4ee3f957316c2e598065e0e1d650a",
		"packs/coding-qualification-v2/v-circular-distance/task.json":        "fb90ecd1ef2b33b5e51821940b56898e65de364f8fd8e456f341efb1efec1144",
		"packs/coding-qualification-v2/v-line-endings/reference.json":        "2b1ca71480bf4c1ac46477420aea9c2c24db3da89015eb12f4770f7605670d82",
		"packs/coding-qualification-v2/v-line-endings/task.json":             "4ac593a10eaa83751bad670b5171b182e5d572a2a0d19c1036348c85ca9c3484",
		"packs/coding-qualification-v2/v-percent-decoding/reference.json":    "c957fdd1fb65534c0adc9bb58fdd618c3fe0457368134b7d7e7c15d803a05a96",
		"packs/coding-qualification-v2/v-percent-decoding/task.json":         "a506f81d152074351b3cfd6513ee681865b8b16a8079795211773615a288761f",
		"packs/coding-qualification-v2/v-run-length/reference.json":          "75aefc68c9a747f2c028159c9c4e68ef4dba13983fa181ef8dfee8b7e5c1abaf",
		"packs/coding-qualification-v2/v-run-length/task.json":               "29d03a144127caf33738fbeac197cd85be750fa83a76c27371e2787ab4b31327",
		"packs/coding-qualification-v2/v-stable-frequency/reference.json":    "6e586eb38edb2b3190d7fae0013dae5b87e8fb7d79735673a27aef153905475c",
		"packs/coding-qualification-v2/v-stable-frequency/task.json":         "c883a19daa7d1e2fdb555811006380f9eb73303567325f82dbabb9745e5b10c9",
	}
	for path, expected := range expectedOriginalSources {
		raw, err := packFiles.ReadFile(path)
		if err != nil {
			t.Fatal("original fixture source missing", path)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != expected {
			t.Fatal("original fixture source identity changed", path)
		}
	}
	counts, originalCounts := map[string]int{}, map[string]int{}
	originalIDs := map[string]bool{"coding-diagnostics-v2": true, "coding-qualification-v2": true}
	seenOriginal := map[string]bool{}
	for _, p := range packs {
		original := originalIDs[p.ID]
		if original {
			seenOriginal[p.ID] = true
			if p.Version != "2.1.0" || p.GraderPolicy != "task-contract-v3" {
				t.Fatal("original catalog metadata changed", p.ID)
			}
		} else if p.ID != "coding-diagnostics-v2-public-coverage" || p.Version != "2.2.0" || p.GraderPolicy != "task-contract-v3" {
			t.Fatal("unexpected fixture catalog", p.ID)
		}
		for _, task := range p.Tasks {
			counts[task.Split]++
			if original {
				originalCounts[task.Split]++
			}
			b, _ := json.Marshal(task)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(b, &fields)
			if fields["expectedAnswer"] != nil || fields["cases"] != nil || fields["probe"] != nil {
				t.Fatal("private fixture fields exposed")
			}
			if task.Contract == "coding" && (len(task.RegressionTestGlobs) == 0 || len(task.DiscoveryCommand) == 0) {
				t.Fatal("missing test discovery contract")
			}
		}
	}
	if len(seenOriginal) != 2 || originalCounts["development"] != 6 || originalCounts["validation"] != 6 || originalCounts["heldout"] != 6 {
		t.Fatal("original catalog or split inventory changed", originalCounts)
	}
	if counts["development"] != 7 || counts["validation"] != 6 || counts["heldout"] != 6 {
		t.Fatal("incorrect split counts", counts)
	}
}

func TestReliabilityV2RelativeCorrectnessAndIntegrityPolicy(t *testing.T) {
	m, a, b, _ := managerForV2(t)
	r, err := m.CreateEvaluationV2(EvaluationV2Request{PackID: "test-v2", BaselineArmID: a.ID, CandidateArmID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	for r.Status != "completed" {
		if _, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 4}); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, m)
		r, _ = m.GetRunV2(r.ID)
	}
	for _, tc := range []struct {
		name     string
		change   func([]ResultV2)
		eligible bool
	}{
		{"slow correct candidate", func(rows []ResultV2) {
			for i := range rows {
				if rows[i].Arm == "candidate" {
					rows[i].DurationMS = 1200000
				}
			}
		}, true},
		{"candidate protected file changed", func(rows []ResultV2) { rows[1].Grade.Checks[1].Passed = false }, false},
		{"candidate authority violation", func(rows []ResultV2) {
			rows[1].Grade.Checks = append(rows[1].Grade.Checks, Check{Name: "authority: path boundary", Passed: false})
		}, false},
		{"baseline integrity failure retained", func(rows []ResultV2) { rows[0].Grade.Checks[1].Passed = false }, true},
		{"validation task regressed", func(rows []ResultV2) {
			for i := range rows {
				if rows[i].Split == "validation" {
					pass := rows[i].Arm == "baseline"
					rows[i].Grade.Passed = pass
					rows[i].Grade.Checks[0].Passed = pass
				}
			}
		}, false},
		{"missing terminal accounting", func(rows []ResultV2) { rows[0].AccountingComplete = false }, false},
		{"zero invented usage", func(rows []ResultV2) { rows[1].Tokens = 0 }, false},
		{"infrastructure omitted", func(rows []ResultV2) { rows[0].InfrastructureError = "lost transport" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows []ResultV2
			b, _ := json.Marshal(r.Results)
			_ = json.Unmarshal(b, &rows)
			tc.change(rows)
			g := EvaluateReliabilityV2(r.Schedule, rows, m.packs[r.PackID].Tasks)
			if g.Eligible != tc.eligible {
				t.Fatal(g)
			}
		})
	}
	if g := EvaluateReliabilityV2(r.Schedule, r.Results[:len(r.Results)-1], m.packs[r.PackID].Tasks); g.Eligible {
		t.Fatal("partial schedule promoted")
	}
}

func TestV2QualificationDefaultsToPairAndCancellationPersists(t *testing.T) {
	m, a, b, calls := managerForV2(t)
	r, err := m.CreateEvaluationV2(EvaluationV2Request{PackID: "test-v2", BaselineArmID: a.ID, CandidateArmID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.AdvanceRunV2(r.ID, BatchLimits{MaxAttempts: 1}); err == nil {
		t.Fatal("unstartable paired batch accepted")
	}
	if _, err = m.AdvanceRunV2(r.ID, BatchLimits{}); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Cursor != 2 || calls.Load() != 2 {
		t.Fatal("default qualification batch was not exactly one pair")
	}
	if err = m.CancelRunV2(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.AdvanceRunV2(r.ID, BatchLimits{}); err == nil {
		t.Fatal("cancelled campaign advanced implicitly")
	}
	r, _ = m.GetRunV2(r.ID)
	if len(r.Results) != 2 || r.Status != "cancelled" {
		t.Fatal("cancel erased evidence")
	}
	if _, err = m.ResumeRunV2(r.ID, BatchLimits{}); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	r, _ = m.GetRunV2(r.ID)
	if r.Cursor != 4 || calls.Load() != 4 {
		t.Fatal("explicit resume replayed completed work")
	}
}
