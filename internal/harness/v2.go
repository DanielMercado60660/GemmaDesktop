package harness

import (
	"context"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
	"sort"
	"strings"
	"time"
)

const attemptEnvelopeV2 = 30 * time.Minute
const batchFlushReserveV2 = 30 * time.Second

func (m *Manager) RegisterArmV2(a ArmV2) (ArmV2, error) {
	// Persistence represents RawMessage and nested provider structs as JSON
	// objects. Normalize before hashing so their key order cannot change identity
	// when the arm crosses the Store boundary. Legacy v1 hashes are untouched.
	s, normalizeErr := decode[Spec](asDoc(a.Spec))
	if normalizeErr != nil {
		return a, normalizeErr
	}
	if s.WorkspaceID == "" || s.AgentID == "" || s.Model == "" || s.ModelDigest == "" || s.RuntimeVersion == "" || s.AppVersion == "" || len(s.FrozenCapability) == 0 || !(strings.Contains(s.ExecutorIdentity, "@sha256:") || strings.HasPrefix(s.ExecutorIdentity, "sha256:")) {
		return a, errors.New("freeze target, runtime, capability, application and executor before registering an arm")
	}
	for _, key := range []string{"backend", "revision", "artifactDigest"} {
		if value, ok := s.RuntimeIdentity[key].(string); !ok || strings.TrimSpace(value) == "" {
			return a, fmt.Errorf("missing runtime identity %s", key)
		}
	}
	if a.Name == "" || len(a.Name) > 200 || a.Provenance == "" || len(a.Provenance) > 1000 {
		return a, errors.New("arm requires a bounded name and provenance")
	}
	if s.BudgetSeconds == 0 {
		s.BudgetSeconds = 1200
	}
	if s.CachePolicy == "" {
		s.CachePolicy = ColdEachAttemptV2
	}
	if s.CachePolicy != ColdEachAttemptV2 {
		return a, errors.New("v2 arms require cold_each_attempt cache policy")
	}
	if s.BudgetSeconds < 60 || s.BudgetSeconds > 1200 {
		return a, errors.New("trial budget must be 60–1200 seconds")
	}
	var r Revision
	var err error
	if a.Revision.ID != "" {
		r, err = m.revision(a.Revision.ID)
		if err == nil && (r.WorkspaceID != s.WorkspaceID || r.AgentID != s.AgentID) {
			err = errors.New("revision target mismatch")
		}
		if err == nil {
			err = ValidateContent(r.Content, s)
		}
	} else {
		author := a.Revision.AuthoredBy
		if author.Kind == "" && author.Identity == "" {
			author = Author{Kind: "baseline", Identity: s.AgentID}
		}
		r, err = m.CreateRevision(s, "", a.Revision.Content, author)
	}
	if err != nil {
		return a, err
	}
	s.Hash = ""
	s.Hash = Hash(s)
	a.Spec = s
	a.Revision = r
	a.CreatedAt = ""
	a.ID = ""
	a.Hash = ""
	a.Hash = Hash([]any{a.Name, s, r.Hash, a.Provenance})
	a.ID = a.Hash
	var old ArmV2
	if err = m.get("arm_v2", a.ID, &old); err == nil {
		return old, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return a, err
	}
	a.CreatedAt = m.now()
	return a, m.put("arm_v2", a)
}

func (m *Manager) GetArmV2(id string) (ArmV2, error) {
	var a ArmV2
	err := m.get("arm_v2", id, &a)
	if err == nil {
		err = validateArmIdentityV2(a)
	}
	return a, err
}
func (m *Manager) ListArmsV2() ([]Doc, error) {
	return m.config.Store.List("harness_arm_v2")
}
func (m *Manager) GetRunV2(id string) (RunV2, error) {
	var r RunV2
	err := m.get("run_v2", id, &r)
	return r, err
}
func (m *Manager) GetBatchV2(id string) (BatchV2, error) {
	var b BatchV2
	err := m.get("batch_v2", id, &b)
	return b, err
}
func (m *Manager) ListRunsV2() ([]Doc, error) {
	rows, err := m.config.Store.List("harness_run_v2")
	if err != nil {
		return nil, err
	}
	for _, d := range rows {
		schedule, _ := d["schedule"].([]any)
		results, _ := d["results"].([]any)
		d["expectedResults"] = len(schedule)
		d["recordedResults"] = len(results)
		delete(d, "schedule")
		delete(d, "results")
	}
	return rows, nil
}

func validateArmIdentityV2(a ArmV2) error {
	s := a.Spec
	if s.CachePolicy != ColdEachAttemptV2 {
		return errors.New("arm lacks the required explicit runtime cache policy")
	}
	h := s.Hash
	s.Hash = ""
	if h != Hash(s) || a.ID != a.Hash || a.Hash != Hash([]any{a.Name, a.Spec, a.Revision.Hash, a.Provenance}) {
		return errors.New("frozen arm identity changed")
	}
	r := a.Revision
	// Empty delegate selections are omitted by JSON persistence. Canonical v1
	// revision creation hashes the empty map, so restore that representation.
	if r.DelegateDigests == nil {
		r.DelegateDigests = map[string]string{}
	}
	if r.ID != r.Hash || r.Hash != Hash([]any{r.ParentID, r.WorkspaceID, r.AgentID, r.Content, r.ApprovedWorkflow, r.DelegateDigests}) {
		return errors.New("frozen arm revision changed")
	}
	return nil
}

func (m *Manager) admitRunV2(packID string, armIDs []string) (FixturePack, error) {
	p, ok := m.packs[packID]
	if !ok {
		return p, errors.New("unknown fixture pack")
	}
	if err := ValidateFixturePack(p); err != nil {
		return p, err
	}
	if len(armIDs) < 1 || len(armIDs) > 3 {
		return p, errors.New("choose one to three distinct arms")
	}
	seen := map[string]bool{}
	var shared Spec
	for i, id := range armIDs {
		if seen[id] {
			return p, errors.New("duplicate arm")
		}
		seen[id] = true
		a, err := m.GetArmV2(id)
		if err != nil {
			return p, err
		}
		if err = validateArmIdentityV2(a); err != nil {
			return p, err
		}
		if i == 0 {
			shared = a.Spec
		} else if shared.WorkspaceID != a.Spec.WorkspaceID || shared.AgentID != a.Spec.AgentID || shared.AppVersion != a.Spec.AppVersion || shared.ExecutorIdentity != a.Spec.ExecutorIdentity || shared.BudgetSeconds != a.Spec.BudgetSeconds || shared.AuthorityHash != a.Spec.AuthorityHash || shared.ProfileBindingHash != a.Spec.ProfileBindingHash || shared.CachePolicy != a.Spec.CachePolicy {
			return p, errors.New("arms must share target, corrected host, executor and trial wall limit")
		}
	}
	return p, nil
}

func (m *Manager) CreateDiagnosticV2(req DiagnosticRequest) (RunV2, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.admitRunV2(req.PackID, req.ArmIDs)
	if err != nil {
		return RunV2{}, err
	}
	if req.Repeats < 1 || req.Repeats > 3 || len(req.TaskIDs) < 1 || len(req.TaskIDs) > 12 {
		return RunV2{}, errors.New("choose one to twelve development tasks and one to three repeats")
	}
	byID := map[string]Task{}
	for _, t := range p.Tasks {
		byID[t.ID] = t
	}
	r := RunV2{ID: store.ID(), Kind: "diagnostic", PackID: p.ID, PackDigest: p.Digest, ArmIDs: append([]string{}, req.ArmIDs...), Status: "ready", Results: []ResultV2{}, BatchIDs: []string{}, CreatedAt: m.now()}
	seen := map[string]bool{}
	for ti, id := range req.TaskIDs {
		t, ok := byID[id]
		if !ok || t.Split != "development" || seen[id] {
			return RunV2{}, errors.New("diagnostics accept distinct development tasks only")
		}
		seen[id] = true
		for repeat := 0; repeat < req.Repeats; repeat++ {
			for n := range req.ArmIDs {
				ai := (n + ti + repeat) % len(req.ArmIDs)
				r.Schedule = append(r.Schedule, ScheduledAttemptV2{Index: len(r.Schedule), TaskID: id, Split: t.Split, ArmID: req.ArmIDs[ai], Arm: fmt.Sprintf("arm-%d", ai), Repeat: repeat})
			}
		}
	}
	r.ScheduleHash = Hash(r.Schedule)
	for _, id := range req.TaskIDs {
		if err = m.reserveDevelopmentExposureV2(byID[id]); err != nil {
			return RunV2{}, err
		}
	}
	return r, m.put("run_v2", r)
}

func (m *Manager) reserveDevelopmentExposureV2(task Task) error {
	for _, key := range []string{Hash([]string{"family", task.ExposureFamily}), Hash([]string{"public", task.PublicHash})} {
		old, err := m.config.Store.Get("harness_exposure_v2", key)
		if err == nil {
			if old["exposure"] != "development" {
				return errors.New("qualification material cannot be reused as fresh development")
			}
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err = m.config.Store.Put("harness_exposure_v2", Doc{"id": key, "exposure": "development", "createdAt": m.now()}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) CreateEvaluationV2(req EvaluationV2Request) (RunV2, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.admitRunV2(req.PackID, []string{req.BaselineArmID, req.CandidateArmID})
	if err != nil {
		return RunV2{}, err
	}
	tasks := append([]Task{}, p.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	r := RunV2{Kind: "qualification", PackID: p.ID, PackDigest: p.Digest, ArmIDs: []string{req.BaselineArmID, req.CandidateArmID}, Status: "ready", Results: []ResultV2{}, BatchIDs: []string{}, CreatedAt: m.now()}
	counts := map[string]int{}
	for _, t := range tasks {
		if t.Split == "development" {
			continue
		}
		counts[t.Split]++
		for repeat := 0; repeat < 3; repeat++ {
			for n := 0; n < 2; n++ {
				ai := (n + repeat) % 2
				arm := "baseline"
				if ai == 1 {
					arm = "candidate"
				}
				r.Schedule = append(r.Schedule, ScheduledAttemptV2{Index: len(r.Schedule), TaskID: t.ID, Split: t.Split, ArmID: r.ArmIDs[ai], Arm: arm, Repeat: repeat})
			}
		}
	}
	if counts["validation"] < 1 || counts["heldout"] < 1 {
		return RunV2{}, errors.New("qualification requires validation and held-out tasks")
	}
	r.ScheduleHash = Hash(r.Schedule)
	r.ID = Hash([]any{ReliabilityPolicyV2, p.ID, p.Digest, r.ArmIDs, r.ScheduleHash})[:32]
	if old, e := m.GetRunV2(r.ID); e == nil {
		return old, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return r, e
	}
	// Family and public-byte reservations are deliberately independent of version
	// numbers/grader changes. Renaming an exposed task cannot make it unseen.
	keys := []string{}
	for _, t := range tasks {
		if t.Split != "heldout" {
			continue
		}
		keys = append(keys, Hash([]string{"family", t.ExposureFamily}), Hash([]string{"public", t.PublicHash}))
	}
	for _, key := range keys {
		old, e := m.config.Store.Get("harness_exposure_v2", key)
		if e == nil && old["runId"] != r.ID {
			return r, errors.New("held-out task family or public inputs already exposed")
		}
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return r, e
		}
	}
	for _, key := range keys {
		if err = m.config.Store.Put("harness_exposure_v2", Doc{"id": key, "runId": r.ID, "createdAt": m.now()}); err != nil {
			return r, err
		}
	}
	return r, m.put("run_v2", r)
}

func (m *Manager) validateRunV2(r RunV2) error {
	p, err := m.admitRunV2(r.PackID, r.ArmIDs)
	if err != nil {
		return err
	}
	if p.Digest != r.PackDigest || r.ScheduleHash != Hash(r.Schedule) || r.Cursor != len(r.Results) || r.Cursor > len(r.Schedule) {
		return errors.New("frozen run schedule or fixture identity changed")
	}
	for i, result := range r.Results {
		if result.Index != i || result.Index >= len(r.Schedule) || Hash(result.ScheduledAttemptV2) != Hash(r.Schedule[result.Index]) {
			return errors.New("result does not match its frozen schedule")
		}
		if result.TrialID == "" {
			if result.InfrastructureError != "" {
				continue
			}
			return errors.New("missing trial evidence")
		}
		t, err := m.trial(result.TrialID)
		if err != nil {
			return err
		}
		h, err := treeHash(t.Root)
		if err != nil || h != t.OutputHash || h != result.OutputHash {
			return errors.New("recorded output changed after grading")
		}
		if result.DurationMS != t.Result.DurationMS || result.Tokens != metric(t.Result.Usage, "prompt_eval_count")+metric(t.Result.Usage, "eval_count") || result.AccountingComplete != (t.Result.TokenAccounting != nil && t.Result.TokenAccounting.Complete()) {
			return errors.New("recorded accounting changed")
		}
	}
	return nil
}

func normalizedLimitsV2(l BatchLimits) (BatchLimits, error) {
	if l.WallSeconds == 0 {
		l.WallSeconds = 7200
	}
	if l.MaxAttempts == 0 {
		l.MaxAttempts = 3
	}
	if l.WallSeconds < 1830 || l.WallSeconds > 7200 || l.MaxAttempts < 1 || l.MaxAttempts > 4 {
		return l, errors.New("batch must allow a complete 30-minute attempt and flush reserve, at most two hours/four attempts")
	}
	return l, nil
}

// AdvanceRunV2 starts exactly one batch. Repeating while running is an
// idempotent observation; reaching a checkpoint never launches another batch.
func (m *Manager) AdvanceRunV2(id string, limits BatchLimits) (RunV2, error) {
	l, err := normalizedLimitsV2(limits)
	if err != nil {
		return RunV2{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.GetRunV2(id)
	if err != nil {
		return r, err
	}
	if r.Kind == "qualification" && limits.MaxAttempts == 0 {
		l.MaxAttempts = 2
	}
	if r.Status == "running" && m.jobs[id] != nil {
		return r, nil
	}
	if r.Status != "ready" && r.Status != "checkpointed" && r.Status != "interrupted" {
		return r, errors.New("run cannot advance from its current state")
	}
	if m.closed || len(m.jobs) > 0 {
		return r, errors.New("another harness operation is active or the service is closed")
	}
	if err = m.validateRunV2(r); err != nil {
		return r, err
	}
	if r.Kind == "qualification" && (l.MaxAttempts < 2 || l.WallSeconds < 3630) {
		return r, errors.New("qualification batch must reserve two attempts and 3630 seconds")
	}
	if m.config.GradeWithSpec == nil {
		return r, errors.New("shared spec-aware grader is unavailable")
	}
	b := BatchV2{ID: store.ID(), RunID: id, ScheduleHash: r.ScheduleHash, StartCursor: r.Cursor, NextCursor: r.Cursor, Limits: l, Status: "running", StartedAt: m.now()}
	if err = m.put("batch_v2", b); err != nil {
		return r, err
	}
	r.BatchIDs = append(r.BatchIDs, b.ID)
	r.Status = "running"
	r.Error = ""
	if err = m.put("run_v2", r); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(l.WallSeconds)*time.Second)
	m.jobs[id] = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		defer func() { m.mu.Lock(); delete(m.jobs, id); m.mu.Unlock() }()
		m.runBatchV2(ctx, r, b)
	}()
	return r, nil
}

func (m *Manager) CancelRunV2(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.GetRunV2(id)
	if err != nil {
		return err
	}
	if r.Status == "completed" {
		return errors.New("completed run is immutable")
	}
	if c := m.jobs[id]; c != nil {
		c()
	}
	r.Status = "cancelled"
	return m.put("run_v2", r)
}

// Resume is explicit and preserves every recorded/uncertain attempt. Cancel
// never authorizes an automatic relaunch or a fresh qualification denominator.
func (m *Manager) ResumeRunV2(id string, limits BatchLimits) (RunV2, error) {
	m.mu.Lock()
	r, err := m.GetRunV2(id)
	if err == nil && r.Status == "cancelled" {
		if m.jobs[id] != nil {
			err = errors.New("wait for cancelled work to reach its checkpoint")
		} else if err = m.validateRunV2(r); err == nil {
			r.Status = "checkpointed"
			err = m.put("run_v2", r)
		}
	}
	m.mu.Unlock()
	if err != nil {
		return r, err
	}
	return m.AdvanceRunV2(id, limits)
}

func (m *Manager) persistRunV2(r RunV2) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	saved, err := m.GetRunV2(r.ID)
	if err != nil {
		return err
	}
	if saved.Status == "cancelled" {
		r.Status = "cancelled"
	}
	return m.put("run_v2", r)
}

func (m *Manager) runBatchV2(ctx context.Context, r RunV2, b BatchV2) {
	deadline, _ := ctx.Deadline()
	attempts := 0
	b.StopReason = "attempt limit"
	defer func() {
		if r.Status == "running" {
			r.Status = "checkpointed"
		}
		if r.Cursor == len(r.Schedule) {
			r.Status = "completed"
			r.FinishedAt = m.now()
			if r.Kind == "qualification" {
				r.Gate = EvaluateReliabilityV2(r.Schedule, r.Results, m.packs[r.PackID].Tasks)
			}
		}
		m.mu.Lock()
		if m.closed && r.Status != "completed" {
			r.Status = "interrupted"
		}
		m.mu.Unlock()
		if saved, err := m.GetRunV2(r.ID); err == nil && saved.Status == "cancelled" {
			r.Status = "cancelled"
		}
		b.NextCursor = r.Cursor
		b.Status = r.Status
		b.FinishedAt = m.now()
		if err := m.put("batch_v2", b); err != nil {
			r.Status = "failed"
			r.Error = err.Error()
		}
		_ = m.persistRunV2(r)
	}()
	for r.Cursor < len(r.Schedule) && attempts < b.Limits.MaxAttempts {
		if err := ctx.Err(); err != nil {
			b.StopReason = err.Error()
			return
		}
		needed := attemptEnvelopeV2
		// Keep paired arm observations together whenever starting a fresh pair.
		if r.Kind == "qualification" && r.Cursor%2 == 0 {
			needed *= 2
			if b.Limits.MaxAttempts-attempts < 2 {
				b.StopReason = "paired attempt reserve"
				return
			}
		}
		if time.Until(deadline) < needed+batchFlushReserveV2 {
			b.StopReason = "wall-time reserve"
			return
		}
		scheduled := r.Schedule[r.Cursor]
		a, err := m.GetArmV2(scheduled.ArmID)
		if err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			return
		}
		var task Task
		for _, t := range m.packs[r.PackID].Tasks {
			if t.ID == scheduled.TaskID {
				task = t
				break
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptEnvelopeV2)
		result := m.executeScheduledV2(attemptCtx, r, a, task, scheduled)
		cancel()
		r.Results = append(r.Results, result)
		r.Cursor++
		attempts++
		b.NextCursor = r.Cursor
		if err = m.persistRunV2(r); err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			return
		}
		if result.InfrastructureError != "" {
			r.Status = "checkpointed"
			r.Error = result.InfrastructureError
			b.StopReason = "infrastructure requires review"
			return
		}
	}
}

func (m *Manager) executeScheduledV2(ctx context.Context, r RunV2, a ArmV2, task Task, s ScheduledAttemptV2) ResultV2 {
	out := ResultV2{ScheduledAttemptV2: s}
	x := Experiment{ID: r.ID, Spec: a.Spec}
	label := fmtAttemptLabelV2(s)
	// Preflight is separately bounded; the common solver still validates before
	// dispatch, under the outer attempt deadline.
	identity := Hash([]string{x.ID, task.ID, label, a.Revision.Hash, x.Spec.Hash, task.PublicHash})[:32]
	_, lookupErr := m.trial(identity)
	if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		out.InfrastructureError = "saved attempt unavailable: " + lookupErr.Error()
		return out
	}
	if errors.Is(lookupErr, store.ErrNotFound) && m.config.ValidateEnvironment != nil {
		preflight, cancel := context.WithTimeout(ctx, time.Minute)
		err := m.config.ValidateEnvironment(preflight, a.Spec)
		cancel()
		if err != nil {
			out.InfrastructureError = "preflight: " + err.Error()
			return out
		}
	}
	t, err := m.solveAdmittedTrial(ctx, x, task, label, a.Revision, r.Kind)
	if err != nil {
		out.InfrastructureError = "solver infrastructure: " + err.Error()
		if saved, saveErr := m.trial(t.ID); saveErr == nil {
			t = saved
		} else {
			return out
		}
	}
	out.TrialID = t.ID
	out.TrialStatus = t.Status
	out.DurationMS = t.Result.DurationMS
	out.OutputHash = t.OutputHash
	out.Tokens = metric(t.Result.Usage, "prompt_eval_count") + metric(t.Result.Usage, "eval_count")
	out.AccountingComplete = t.Result.TokenAccounting != nil && t.Result.TokenAccounting.Complete()
	if t.InfrastructureError != "" {
		out.InfrastructureError = t.InfrastructureError
		out.Grade = Grade{TrialID: t.ID, GraderHash: task.GraderHash, Error: t.InfrastructureError}
		return out
	}
	if err != nil {
		return out
	}
	gradeCtx, cancel := context.WithTimeout(ctx, 7*time.Minute)
	out.Grade, err = m.config.GradeWithSpec(gradeCtx, GradeRequest{Spec: a.Spec, Task: task, Trial: t})
	cancel()
	out.Grade.TrialID = t.ID
	out.Grade.GraderHash = task.GraderHash
	if err != nil {
		out.InfrastructureError = "grader infrastructure: " + err.Error()
		out.Grade.Error = err.Error()
		out.Grade.Passed = false
	}
	if t.Status != "completed" {
		out.Grade.Passed = false
		out.Grade.Checks = append(out.Grade.Checks, Check{Name: "solver completed", Passed: false, Error: t.Error})
	}
	return out
}

func fmtAttemptLabelV2(s ScheduledAttemptV2) string {
	return fmt.Sprintf("v2-%06d-%s", s.Index, s.ArmID)
}
