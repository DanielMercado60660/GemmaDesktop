package harness

import (
	"context"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"sort"
)

func (m *Manager) StartEvaluation(experimentID string) (Evaluation, error) {
	m.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.mu.Unlock()
		}
	}()
	if len(m.jobs) > 0 {
		return Evaluation{}, errors.New("another harness operation is active")
	}
	var x Experiment
	if e := m.get("experiment", experimentID, &x); e != nil {
		return Evaluation{}, e
	}
	if x.Status != "completed" || x.CandidateID == "" {
		return Evaluation{}, errors.New("complete and freeze the optimization candidate first")
	}
	if x.Spec.FixtureDigest != m.fixtureDigest() {
		return Evaluation{}, errors.New("fixture policy changed since experiment admission")
	}
	// A held-out exposure is single-use. Repeated selection would turn it into tuning.
	prior, e := m.config.Store.List("harness_evaluation")
	if e != nil {
		return Evaluation{}, e
	}
	for _, d := range prior {
		if d["experimentId"] == experimentID {
			return Evaluation{}, errors.New("this candidate already has an evaluation; retain the original exposure")
		}
	}
	v := Evaluation{ID: Hash([]string{experimentID, x.CandidateID, "paired-three-v1"})[:32], ExperimentID: experimentID, CandidateID: x.CandidateID, BaselineID: x.Spec.BaseRevisionID, Status: "running", CreatedAt: m.now(), Results: []EvalResult{}, ExpectedResults: m.expectedEvaluationResults()}
	for _, t := range m.tasks {
		if t.Split == "heldout" {
			key := Hash([]string{t.ID, t.PublicHash, t.GraderHash})
			if previous, err := m.config.Store.Get("harness_exposure", key); err == nil && previous["evaluationId"] != v.ID {
				return v, errors.New("held-out pool was already reserved/exposed; register fresh fixtures for unseen qualification")
			}
		}
	}
	for _, t := range m.tasks {
		if t.Split == "heldout" {
			key := Hash([]string{t.ID, t.PublicHash, t.GraderHash})
			if e = m.config.Store.Put("harness_exposure", Doc{"id": key, "evaluationId": v.ID, "createdAt": m.now(), "status": "reserved"}); e != nil {
				return v, e
			}
		}
	}
	if e = m.put("evaluation", v); e != nil {
		return v, e
	}
	m.mu.Unlock()
	locked = false
	e = m.launch(v.ID, func(ctx context.Context) { m.evaluate(ctx, v, x) })
	if e != nil {
		v.Status = "failed"
		v.Error = e.Error()
		_ = m.put("evaluation", v)
	}
	return v, e
}
func (m *Manager) evaluate(ctx context.Context, v Evaluation, x Experiment) {
	finish := func(err error) {
		if err != nil {
			v.Status = "failed"
			v.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				v.Status = "cancelled"
				m.mu.Lock()
				if m.closed {
					v.Status = "interrupted"
				}
				m.mu.Unlock()
			}
		}
		v.FinishedAt = m.now()
		_ = m.put("evaluation", v)
	}
	if e := m.put("evaluation", v); e != nil {
		finish(e)
		return
	}
	if m.config.Grade == nil {
		finish(errors.New("independent grader is unavailable"))
		return
	}
	tasks := []Task{}
	for _, t := range m.tasks {
		if t.Split == "validation" || t.Split == "heldout" {
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for _, task := range tasks {
		for repeat := 0; repeat < 3; repeat++ {
			arms := []string{"baseline", "candidate"}
			if repeat%2 == 1 {
				arms = []string{"candidate", "baseline"}
			}
			for _, arm := range arms {
				if e := ctx.Err(); e != nil {
					finish(e)
					return
				}
				already := false
				for _, done := range v.Results {
					if done.TaskID == task.ID && done.Repeat == repeat && done.Arm == arm {
						t, e := m.trial(done.TrialID)
						if e != nil {
							finish(e)
							return
						}
						h, e := treeHash(t.Root)
						if e != nil || h != t.OutputHash {
							finish(errors.New("completed evaluation trial output changed"))
							return
						}
						already = true
						break
					}
				}
				if already {
					continue
				}
				rev := v.BaselineID
				if arm == "candidate" {
					rev = v.CandidateID
				}
				label := fmt.Sprintf("eval-%s-%s-%d", v.ID, arm, repeat)
				t, e := m.SolveTrial(ctx, x, task.ID, label, rev)
				if e != nil {
					finish(e)
					return
				}
				grade, e := m.config.Grade(ctx, task, t)
				if e != nil {
					grade.Error = e.Error()
					grade.Passed = false
				}
				grade.TrialID = t.ID
				grade.GraderHash = task.GraderHash
				if t.Status != "completed" {
					grade.Passed = false
					grade.Checks = append(grade.Checks, Check{Name: "solver completed", Passed: false, Critical: false, Error: t.Error})
				}
				tokens := metric(t.Result.Usage, "prompt_eval_count") + metric(t.Result.Usage, "eval_count")
				v.Results = append(v.Results, EvalResult{TaskID: task.ID, Split: task.Split, Repeat: repeat, Arm: arm, TrialID: t.ID, Grade: grade, DurationMS: t.Result.DurationMS, Tokens: tokens, TokenAccounting: t.Result.TokenAccounting})
				if e = m.put("evaluation", v); e != nil {
					finish(e)
					return
				}
			}
		}
	}
	v.Gate = EvaluateGate(v.Results, tasks)
	v.Status = "completed"
	evidence := []string{v.ID}
	p, e := m.MakePacket(x, "evaluation", "promotion_review", v.CandidateID, "Independent Codex reviewer: inspect the frozen candidate and paired validation/held-out results. Approve only if integrity checks are complete, no blocking defect remains, and the recorded promotion gates hold. Do not feed held-out results into further optimization or lessons.", evidence, map[string]string{"evaluation-evidence.sha256": v.Gate.EvidenceHash, "candidate-revision.txt": v.CandidateID})
	if e != nil {
		finish(e)
		return
	}
	v.ReviewPacketID = p.ID
	finish(nil)
}
func metric(d Doc, k string) int64 {
	switch v := d[k].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}
func median(v []float64) float64 {
	sort.Float64s(v)
	if len(v) == 0 {
		return 0
	}
	if len(v)%2 == 0 {
		return (v[len(v)/2-1] + v[len(v)/2]) / 2
	}
	return v[len(v)/2]
}
func ratio(candidate, base []float64) float64 {
	b := median(base)
	if b <= 0 {
		return 999
	}
	return median(candidate) / b
}
func EvaluateGate(results []EvalResult, tasks []Task) Gate {
	g := Gate{Reasons: []string{}, EvidenceHash: Hash(results)}
	expected := map[string]Task{}
	for _, t := range tasks {
		expected[t.ID] = t
	}
	seen := map[string]bool{}
	counts := map[string]map[string]int{}
	times := map[string][]float64{}
	tokens := map[string][]float64{}
	critical := true
	measurementsValid := true
	accounting := inference.NewTokenAccounting(0, 0, 0)
	for _, r := range results {
		t, ok := expected[r.TaskID]
		key := fmt.Sprintf("%s/%s/%d", r.TaskID, r.Arm, r.Repeat)
		if !ok || r.Split != t.Split || r.Repeat < 0 || r.Repeat > 2 || (r.Arm != "baseline" && r.Arm != "candidate") || seen[key] {
			g.Reasons = append(g.Reasons, "unexpected or duplicate paired trial")
			continue
		}
		seen[key] = true
		if r.Grade.Error != "" || r.Grade.GraderHash != t.GraderHash || len(r.Grade.Checks) == 0 {
			critical = false
		}
		for _, c := range r.Grade.Checks {
			if c.Critical && !c.Passed {
				critical = false
			}
		}
		if counts[r.TaskID] == nil {
			counts[r.TaskID] = map[string]int{}
		}
		if r.Grade.Passed {
			counts[r.TaskID][r.Arm]++
			if t.Split == "heldout" {
				if r.Arm == "baseline" {
					g.BaselineSuccesses++
				} else {
					g.CandidateSuccesses++
				}
			}
		}
		accounting = inference.MergeTokenAccounting(accounting, r.TokenAccounting)
		if !r.TokenAccounting.Complete() {
			measurementsValid = false
		}
		if r.DurationMS <= 0 || r.Tokens <= 0 {
			critical = false
			measurementsValid = false
		}
		times[r.Arm] = append(times[r.Arm], float64(r.DurationMS))
		tokens[r.Arm] = append(tokens[r.Arm], float64(r.Tokens))
	}
	if len(seen) != len(tasks)*6 || len(tasks) == 0 {
		g.Reasons = append(g.Reasons, "three complete paired repeats are required for every validation and held-out task")
	}
	for _, t := range tasks {
		if t.Split == "heldout" && counts[t.ID]["candidate"] < counts[t.ID]["baseline"] {
			g.Reasons = append(g.Reasons, "held-out task regressed: "+t.ID)
		}
	}
	if !critical {
		g.Reasons = append(g.Reasons, "critical integrity/measurement checks are incomplete or failed")
	}
	if g.CandidateSuccesses <= g.BaselineSuccesses {
		g.Reasons = append(g.Reasons, "aggregate held-out successes did not increase")
	}
	measurementsValid = measurementsValid && accounting.Complete()
	g.CostAccountingStatus = accounting.Status
	g.CostComparisonValid = measurementsValid && len(tasks) > 0 && len(seen) == len(tasks)*6
	if !measurementsValid {
		g.Reasons = append(g.Reasons, "token accounting is incomplete or unknown, or cost measurements are invalid")
	}
	if g.CostComparisonValid {
		g.TimeRatio = ratio(times["candidate"], times["baseline"])
		g.TokenRatio = ratio(tokens["candidate"], tokens["baseline"])
		if g.TimeRatio > 1.2 {
			g.Reasons = append(g.Reasons, "median elapsed time exceeds 1.2× baseline")
		}
		if g.TokenRatio > 1.2 {
			g.Reasons = append(g.Reasons, "median tokens exceed 1.2× baseline")
		}
	}
	g.Eligible = len(g.Reasons) == 0
	return g
}
func (m *Manager) Promote(ctx context.Context, evaluationID string) (Promotion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var v Evaluation
	if e := m.get("evaluation", evaluationID, &v); e != nil {
		return Promotion{}, e
	}
	if e := m.verifyEvaluationOutputs(v); e != nil {
		return Promotion{}, e
	}
	if v.Status != "completed" || !v.Gate.Eligible || v.Gate.EvidenceHash != Hash(v.Results) {
		return Promotion{}, errors.New("recorded independent promotion gates do not pass")
	}
	var p Packet
	if e := m.get("packet", v.ReviewPacketID, &p); e != nil {
		return Promotion{}, e
	}
	if p.Response == nil || p.Response.Verdict != "approved" {
		return Promotion{}, errors.New("fresh independent Codex approval is required")
	}
	var x Experiment
	if e := m.get("experiment", v.ExperimentID, &x); e != nil {
		return Promotion{}, e
	}
	if x.Spec.FixtureDigest != m.fixtureDigest() {
		return Promotion{}, errors.New("fixture policy changed since evaluation")
	}
	currentTasks := []Task{}
	for _, task := range m.tasks {
		if task.Split == "validation" || task.Split == "heldout" {
			currentTasks = append(currentTasks, task)
		}
	}
	if !EvaluateGate(v.Results, currentTasks).Eligible {
		return Promotion{}, errors.New("current promotion policy rejects incomplete/unknown measurement evidence or other failed gates")
	}
	if v.CandidateID != x.CandidateID {
		return Promotion{}, errors.New("candidate changed after evaluation")
	}
	if m.config.Activate == nil {
		return Promotion{}, errors.New("target activation is unavailable")
	}
	previous := x.Spec.BaseRevisionID
	records, e := m.config.Store.List("harness_promotion")
	if e != nil {
		return Promotion{}, e
	}
	for _, d := range records {
		if d["evaluationId"] == v.ID && d["kind"] == "promote" {
			return decode[Promotion](d)
		}
		if d["agentId"] == x.Spec.AgentID && d["workspaceId"] == x.Spec.WorkspaceID {
			previous, _ = d["revisionId"].(string)
		}
	}
	out := Promotion{ID: Hash([]string{v.ID, v.CandidateID, "promote"})[:32], AgentID: x.Spec.AgentID, WorkspaceID: x.Spec.WorkspaceID, RevisionID: v.CandidateID, PreviousID: previous, EvaluationID: v.ID, EvidenceHash: v.Gate.EvidenceHash, CreatedAt: m.now(), Kind: "promote"}
	// Activation is compare-and-swap. Its callback must be idempotent for retry.
	if e = m.config.Activate(ctx, out.WorkspaceID, out.AgentID, previous, out.RevisionID); e != nil {
		return out, e
	}
	return out, m.put("promotion", out)
}
func (m *Manager) Rollback(ctx context.Context, id string) (Promotion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var p Promotion
	if e := m.get("promotion", id, &p); e != nil {
		return p, e
	}
	if p.Kind != "promote" {
		return p, errors.New("only a promotion can be rolled back")
	}
	out := Promotion{ID: Hash([]string{id, "rollback"})[:32], AgentID: p.AgentID, WorkspaceID: p.WorkspaceID, RevisionID: p.PreviousID, PreviousID: p.RevisionID, EvaluationID: p.EvaluationID, EvidenceHash: p.EvidenceHash, CreatedAt: m.now(), Kind: "rollback"}
	var old Promotion
	if e := m.get("promotion", out.ID, &old); e == nil {
		return old, nil
	}
	if m.config.Activate == nil {
		return out, errors.New("activation is unavailable")
	}
	if e := m.config.Activate(ctx, p.WorkspaceID, p.AgentID, p.RevisionID, p.PreviousID); e != nil {
		return out, e
	}
	return out, m.put("promotion", out)
}

func (m *Manager) verifyEvaluationOutputs(v Evaluation) error {
	// This is an evidence-consistency check shared by review submission and
	// activation. A reviewer must be able to reject unchanged partial/legacy
	// measurements; only Promote's current gate requires complete costs.
	for _, r := range v.Results {
		t, e := m.trial(r.TrialID)
		if e != nil {
			return e
		}
		if Hash(r.TokenAccounting) != Hash(t.Result.TokenAccounting) || r.Tokens != metric(t.Result.Usage, "prompt_eval_count")+metric(t.Result.Usage, "eval_count") {
			return errors.New("trial token measurement differs from evaluation")
		}
		h, e := treeHash(t.Root)
		if e != nil || h != t.OutputHash {
			return errors.New("evaluation trial output changed after grading")
		}
	}
	return nil
}

func (m *Manager) ResumeEvaluation(id string) (Evaluation, error) {
	var v Evaluation
	if e := m.get("evaluation", id, &v); e != nil {
		return v, e
	}
	if v.Status != "interrupted" && v.Status != "cancelled" {
		return v, errors.New("only an interrupted or explicitly cancelled evaluation can continue")
	}
	var x Experiment
	if e := m.get("experiment", v.ExperimentID, &x); e != nil {
		return v, e
	}
	if x.Spec.FixtureDigest != m.fixtureDigest() {
		return v, errors.New("fixture policy changed since evaluation")
	}
	v.Status = "running"
	v.Error = ""
	v.FinishedAt = ""
	e := m.launch(v.ID, func(ctx context.Context) { m.evaluate(ctx, v, x) })
	return v, e
}

// Each frozen validation/held-out task has baseline and candidate arms, each
// repeated three times. This counts scheduled attempts, including failed ones.
func (m *Manager) expectedEvaluationResults() int {
	count := 0
	for _, task := range m.tasks {
		if task.Split == "validation" || task.Split == "heldout" {
			count += 6
		}
	}
	return count
}
