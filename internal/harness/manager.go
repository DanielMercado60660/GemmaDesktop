package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	config                       Config
	mu                           sync.Mutex
	jobs                         map[string]context.CancelFunc
	wg                           sync.WaitGroup
	closed                       bool
	tasks                        map[string]Task
	packs                        map[string]FixturePack
	optimizationBatches          map[string]*OptimizationBatchV2
	developmentGradeMu           sync.Mutex
	developmentGradeCancel       context.CancelFunc
	developmentGradeExperimentID string
}

func Open(c Config) (*Manager, error) {
	if c.Store == nil || c.Root == "" {
		return nil, errors.New("harness storage is required")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return nil, err
	}
	m := &Manager{config: c, jobs: map[string]context.CancelFunc{}, tasks: map[string]Task{}, packs: map[string]FixturePack{}, optimizationBatches: map[string]*OptimizationBatchV2{}}
	for _, t := range c.Tasks {
		if t.ID == "" || m.tasks[t.ID].ID != "" {
			return nil, errors.New("duplicate or missing fixture id")
		}
		m.tasks[t.ID] = t
	}
	families := map[string]string{}
	publicSplits := map[string]string{}
	for _, t := range c.Tasks {
		publicSplits[t.PublicHash] = "legacy"
	}
	for _, pack := range c.Packs {
		if _, exists := m.packs[pack.ID]; exists {
			return nil, errors.New("duplicate fixture pack")
		}
		if err := ValidateFixturePack(pack); err != nil {
			return nil, err
		}
		for _, task := range pack.Tasks {
			if split := families[task.ExposureFamily]; split != "" && split != task.Split {
				return nil, errors.New("task family crosses pack splits")
			}
			if split := publicSplits[task.PublicHash]; split != "" && split != task.Split {
				return nil, errors.New("copied public fixture cannot change evaluation split or legacy exposure")
			}
			families[task.ExposureFamily] = task.Split
			publicSplits[task.PublicHash] = task.Split
		}
		m.packs[pack.ID] = pack
	}
	// Recovery never replays a partially executed trial. Waiting packets survive.
	for _, kind := range []string{"trial", "experiment", "evaluation", "run_v2", "batch_v2", "optimization_batch_v2"} {
		rows, err := c.Store.List("harness_" + kind)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r["status"] == "running" {
				r["status"] = "interrupted"
				r["error"] = "Application stopped. Completed evidence is retained; uncertain effects are not replayed."
				if err = c.Store.Put("harness_"+kind, r); err != nil {
					return nil, err
				}
			}
		}
	}
	return m, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	for _, c := range m.jobs {
		c()
	}
	if m.developmentGradeCancel != nil {
		m.developmentGradeCancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs) > 0 || m.developmentGradeCancel != nil
}
func (m *Manager) now() string { return m.config.Now().UTC().Format(time.RFC3339Nano) }
func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func asDoc(v any) Doc { b, _ := json.Marshal(v); d := Doc{}; _ = json.Unmarshal(b, &d); return d }
func decode[T any](v Doc) (T, error) {
	var out T
	b, e := json.Marshal(v)
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	return out, e
}
func (m *Manager) put(kind string, v any) error { return m.config.Store.Put("harness_"+kind, asDoc(v)) }
func (m *Manager) get(kind, id string, v any) error {
	d, e := m.config.Store.Get("harness_"+kind, id)
	if e != nil {
		return e
	}
	b, e := json.Marshal(d)
	if e == nil {
		e = json.Unmarshal(b, v)
	}
	return e
}
func (m *Manager) List(kind string) ([]Doc, error) {
	switch kind {
	case "experiment", "revision", "packet", "trial", "lesson", "evaluation", "promotion":
	default:
		return nil, errors.New("unknown harness collection")
	}
	rows, err := m.config.Store.ListHarnessMetadata(kind)
	if err != nil || kind != "evaluation" {
		return rows, err
	}
	// New evaluations retain the admitted schedule size. Legacy rows can only
	// infer it when their experiment's fixture identity still matches exactly.
	var experiments []Doc
	for _, row := range rows {
		if _, exists := row["expectedResults"]; exists {
			continue
		}
		if experiments == nil {
			experiments, err = m.config.Store.ListHarnessMetadata("experiment")
			if err != nil {
				return nil, err
			}
		}
		for _, experiment := range experiments {
			if experiment["id"] != row["experimentId"] {
				continue
			}
			spec, ok := experiment["spec"].(map[string]any)
			if ok && spec["fixtureDigest"] == m.fixtureDigest() {
				row["expectedResults"] = m.expectedEvaluationResults()
			}
			break
		}
	}
	return rows, nil
}
func (m *Manager) Get(kind, id string) (Doc, error) {
	switch kind {
	case "experiment", "revision", "packet", "trial", "lesson", "evaluation", "promotion":
		return m.config.Store.Get("harness_"+kind, id)
	default:
		return nil, errors.New("unknown harness collection")
	}
}
func (m *Manager) Fixtures() []Task {
	out := []Task{}
	for _, t := range m.tasks {
		t.Public = nil
		t.Cases = nil
		if t.Split == "heldout" {
			t.Prompt = "Sealed until the candidate is frozen for evaluation."
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func validAuthor(a Author) bool {
	return a.Kind == "codex" && len(strings.TrimSpace(a.Identity)) >= 3 && len(a.Identity) <= 200
}
func ValidateContent(c Content, spec Spec) error {
	if len(c.Instructions)+len(c.ToolGuidance) > 24000 || len(c.Lessons) > 16 || len(c.SkillIDs) > 32 {
		return errors.New("harness exceeds bounded content limits")
	}
	for _, l := range c.Lessons {
		if len(l) > 4000 {
			return errors.New("lesson is too long")
		}
	}
	if c.WorkflowID != "" && len(spec.ApprovedWorkflows[c.WorkflowID]) == 0 {
		return errors.New("workflow is outside the frozen approved selection")
	}
	if len(c.DelegateAgentIDs) > 16 {
		return errors.New("too many delegates")
	}
	delegates := map[string]bool{}
	for _, id := range spec.ApprovedDelegateIDs {
		delegates[id] = true
	}
	used := map[string]bool{}
	for _, id := range c.DelegateAgentIDs {
		if !delegates[id] || used[id] {
			return errors.New("delegate is outside the frozen approved selection")
		}
		used[id] = true
	}
	seen := map[string]bool{}
	for _, id := range c.SkillIDs {
		if _, ok := spec.ApprovedSkills[id]; !ok || seen[id] {
			return fmt.Errorf("skill %q is not an approved frozen selection", id)
		}
		seen[id] = true
	}
	return nil
}
func (m *Manager) CreateRevision(spec Spec, parent string, c Content, a Author) (Revision, error) {
	if !validAuthor(a) && !(parent == "" && a.Kind == "baseline" && a.Identity == spec.AgentID) {
		return Revision{}, errors.New("Codex authorship identity is required")
	}
	if e := ValidateContent(c, spec); e != nil {
		return Revision{}, e
	}
	if parent != "" {
		var p Revision
		if e := m.get("revision", parent, &p); e != nil {
			return Revision{}, e
		}
		if p.WorkspaceID != spec.WorkspaceID || p.AgentID != spec.AgentID {
			return Revision{}, errors.New("revision parent belongs to another target")
		}
	}
	r := Revision{ParentID: parent, WorkspaceID: spec.WorkspaceID, AgentID: spec.AgentID, Content: c, AuthoredBy: a, CreatedAt: m.now()}
	r.ApprovedWorkflow = spec.ApprovedWorkflows[c.WorkflowID]
	r.DelegateDigests = map[string]string{}
	for _, id := range c.DelegateAgentIDs {
		r.DelegateDigests[id] = Hash(spec.FrozenAgents[id])
	}
	r.Hash = Hash([]any{parent, spec.WorkspaceID, spec.AgentID, c, r.ApprovedWorkflow, r.DelegateDigests})
	r.ID = r.Hash
	var old Revision
	if e := m.get("revision", r.ID, &old); e == nil {
		return old, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return r, e
	}
	return r, m.put("revision", r)
}
func (m *Manager) Start(spec Spec, base Content, author Author) (Experiment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Experiment{}, errors.New("harness service is closed")
	}
	if spec.Mode != "native" && spec.Mode != "upstream" {
		return Experiment{}, errors.New("mode must be native or upstream")
	}
	if len(spec.TaskIDs) < 1 || len(spec.TaskIDs) > 6 {
		return Experiment{}, errors.New("choose one to six development tasks")
	}
	if spec.FixturePackID != "" {
		if spec.CachePolicy == "" {
			spec.CachePolicy = ColdEachAttemptV2
		}
		if spec.CachePolicy != ColdEachAttemptV2 {
			return Experiment{}, errors.New("versioned campaigns require cold_each_attempt cache policy")
		}
		if err := ValidateFixturePack(m.packs[spec.FixturePackID]); err != nil {
			return Experiment{}, err
		}
	}
	seen := map[string]bool{}
	for _, id := range spec.TaskIDs {
		t, ok := m.taskForSpec(spec, id)
		if !ok || t.Split != "development" || seen[id] {
			return Experiment{}, errors.New("optimization accepts distinct development fixtures only")
		}
		seen[id] = true
	}
	if spec.ModelDigest == "" || spec.RuntimeVersion == "" || !(strings.Contains(spec.ExecutorIdentity, "@sha256:") || strings.HasPrefix(spec.ExecutorIdentity, "sha256:")) || spec.WorkspaceID == "" || spec.AgentID == "" {
		return Experiment{}, errors.New("freeze target, model digest, runtime and digest-pinned Docker executor first")
	}
	if spec.BudgetSeconds == 0 {
		spec.BudgetSeconds = 1200
	}
	if spec.BudgetSeconds < 60 || spec.BudgetSeconds > 1200 {
		return Experiment{}, errors.New("trial budget must be 60–1200 seconds")
	}
	spec.ID = store.ID()
	spec.UpstreamRevision = UpstreamRevision
	spec.CreatedAt = m.now()
	spec.DevelopmentSourceExperimentID = ""
	spec.GraderVersion = MethodVersion + "-json-contract:" + spec.AppVersion
	spec.FixtureDigest = m.fixtureDigestForSpec(spec)
	if spec.FixturePackID != "" {
		spec.GraderVersion = m.packs[spec.FixturePackID].GraderPolicy + ":" + spec.AppVersion
	}
	// Normalize embedded JSON before hashing new admissions, so persistence
	// cannot change their identity by reordering RawMessage object keys.
	var e error
	spec, e = decode[Spec](asDoc(spec))
	if e != nil {
		return Experiment{}, e
	}
	var r Revision
	if spec.DevelopmentBaseRevisionID != "" {
		r, spec.DevelopmentSourceExperimentID, e = m.developmentBaseRevision(spec, base)
	} else {
		r, e = m.CreateRevision(spec, "", base, Author{Kind: "baseline", Identity: spec.AgentID})
	}
	if e != nil {
		return Experiment{}, e
	}
	spec.BaseRevisionID = r.ID
	spec.Hash = ""
	spec.Hash = Hash(spec)
	if spec.FixturePackID != "" {
		for _, id := range spec.TaskIDs {
			t, _ := m.taskForSpec(spec, id)
			if err := m.reserveDevelopmentExposureV2(t); err != nil {
				return Experiment{}, err
			}
		}
	}
	x := Experiment{ID: spec.ID, Spec: spec, Status: "ready", CreatedAt: m.now(), TrialIDs: []string{}, PacketIDs: []string{}}
	return x, m.put("experiment", x)
}
func (m *Manager) fixtureDigest() string {
	ids := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	v := []string{}
	for _, id := range ids {
		t := m.tasks[id]
		v = append(v, id, t.PublicHash, t.GraderHash, t.Split)
	}
	return Hash(v)
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.jobs[id]; c != nil {
		c()
	}
	if m.developmentGradeExperimentID == id && m.developmentGradeCancel != nil {
		m.developmentGradeCancel()
	}
	var x Experiment
	if e := m.get("experiment", id, &x); e == nil {
		x.Status = "cancelled"
		x.FinishedAt = m.now()
		return m.put("experiment", x)
	}
	var v Evaluation
	if e := m.get("evaluation", id, &v); e != nil {
		return e
	}
	v.Status = "cancelled"
	return m.put("evaluation", v)
}
func (m *Manager) launch(id string, fn func(context.Context)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("harness service is closed")
	}
	if m.developmentGradeCancel != nil {
		return errors.New("development grading is active")
	}
	if len(m.jobs) > 0 {
		return errors.New("another harness operation is active")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.jobs[id] = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		defer func() { m.mu.Lock(); delete(m.jobs, id); m.mu.Unlock() }()
		fn(ctx)
	}()
	return nil
}
func (m *Manager) Advance(id string) (Experiment, error) {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return x, e
	}
	if x.Status == "completed" || x.Status == "cancelled" || x.Status == "failed" {
		return x, errors.New("experiment has finished")
	}
	e := m.launch(id, func(ctx context.Context) {
		if x.Spec.FixturePackID != "" {
			bounded, cancel := context.WithTimeout(ctx, 2*time.Hour)
			defer cancel()
			ctx = bounded
			if err := m.beginOptimizationBatchV2(x); err != nil {
				m.failExperiment(id, err)
				return
			}
			defer m.finishOptimizationBatchV2(id)
		}
		if x.Spec.Mode == "upstream" {
			active := x // Advance returns its admission snapshot concurrently.
			active.Status = "running"
			if err := m.putActiveExperiment(active); err != nil {
				m.failExperiment(id, err)
				return
			}
			if m.config.Upstream == nil {
				m.failExperiment(id, errors.New("upstream adapter is unavailable"))
				return
			}
			if err := m.config.Upstream(ctx, m, active); err != nil {
				if x.Spec.FixturePackID != "" && ctx.Err() != nil {
					err = ctx.Err()
				}
				m.failExperiment(id, err)
			}
		} else {
			if err := m.native(ctx, id); err != nil {
				m.failExperiment(id, err)
			}
		}
	})
	return x, e
}
func (m *Manager) failExperiment(id string, e error) {
	// Serialize terminal metadata with Cancel; a late adapter/solver error must
	// never overwrite an explicit cancellation from a stale experiment snapshot.
	m.mu.Lock()
	defer m.mu.Unlock()
	var x Experiment
	if m.get("experiment", id, &x) != nil {
		return
	}
	if x.Status != "cancelled" {
		x.Status = "failed"
		x.Error = e.Error()
		if x.Spec.FixturePackID != "" && (errors.Is(e, ErrBatchCheckpointV2) || errors.Is(e, context.DeadlineExceeded)) {
			x.Status = "checkpointed"
			x.Error = "Batch stopped at a durable checkpoint. Resume explicitly to continue untouched work."
		}
		if errors.Is(e, context.Canceled) {
			x.Status = "cancelled"
			if m.closed {
				x.Status = "interrupted"
			}
		}
		x.FinishedAt = m.now()
		_ = m.put("experiment", x)
	}
}
func (m *Manager) revision(id string) (Revision, error) {
	var r Revision
	e := m.get("revision", id, &r)
	return r, e
}
func (m *Manager) trial(id string) (Trial, error) {
	var t Trial
	e := m.get("trial", id, &t)
	t.Root = filepath.Join(m.config.Root, "trials", id, "workspace")
	return t, e
}
func (m *Manager) TrialRoot(id string) (string, error) { t, e := m.trial(id); return t.Root, e }
func safePath(p string) bool {
	return p != "" && !filepath.IsAbs(p) && filepath.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") && !strings.Contains(p, "\\")
}
func writeFiles(root string, files map[string]string) error {
	for p, b := range files {
		if !safePath(p) {
			return errors.New("unsafe fixture path")
		}
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		if e := os.WriteFile(p, []byte(b), 0600); e != nil {
			return e
		}
	}
	return nil
}
func treeHash(root string) (string, error) {
	values := map[string]string{}
	total := int64(0)
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("trial output contains a symlink")
		}
		if d.IsDir() {
			if p != root && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		i, e := d.Info()
		if e != nil {
			return e
		}
		if !i.Mode().IsRegular() {
			return errors.New("trial output contains special file")
		}
		total += i.Size()
		if total > 50<<20 || len(values) > 2000 {
			return errors.New("trial output exceeds capture bound")
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		r, _ := filepath.Rel(root, p)
		values[r] = Hash(b)
		return nil
	})
	return Hash(values), e
}
func (m *Manager) SolveTrial(ctx context.Context, x Experiment, taskID, label, revisionID string) (Trial, error) {
	if x.Spec.FixturePackID != "" {
		if err := ValidateFixturePack(m.packs[x.Spec.FixturePackID]); err != nil {
			return Trial{}, err
		}
	}
	task, ok := m.taskForSpec(x.Spec, taskID)
	if !ok {
		return Trial{}, errors.New("unknown fixture")
	}
	r, e := m.revision(revisionID)
	if e != nil {
		return Trial{}, e
	}
	if r.AgentID != x.Spec.AgentID || r.WorkspaceID != x.Spec.WorkspaceID {
		return Trial{}, errors.New("revision target mismatch")
	}
	if x.Spec.FixturePackID != "" {
		if err := m.reserveOptimizationAttemptV2(x, task, label, r); err != nil {
			return Trial{}, err
		}
		bounded, cancel := context.WithTimeout(ctx, attemptEnvelopeV2)
		defer cancel()
		ctx = bounded
	}
	return m.solveAdmittedTrial(ctx, x, task, label, r, "")
}

// The same solver lifecycle serves v1 experiments and v2 diagnostics/paired
// evaluations. New callers supply an already-admitted immutable task and arm.
func (m *Manager) solveAdmittedTrial(ctx context.Context, x Experiment, task Task, label string, r Revision, ownerKind string) (Trial, error) {
	taskID, revisionID := task.ID, r.ID
	identity := Hash([]string{x.ID, taskID, label, r.Hash, x.Spec.Hash, task.PublicHash})
	id := identity[:32]
	old, e := m.trial(id)
	if e == nil {
		if old.InputHash != identity {
			return old, errors.New("trial identity mismatch")
		}
		if old.Status == "completed" || old.Status == "failed" {
			h, he := treeHash(old.Root)
			if he != nil || h != old.OutputHash {
				return old, errors.New("saved trial output changed")
			}
			if old.InfrastructureError != "" {
				return old, errors.New(old.InfrastructureError)
			}
			return old, nil
		}
		if old.Status == "interrupted" {
			// Never replay uncertain effects. Preserve this attempt as an interrupted
			// denominator entry; evaluation may continue with only the untouched trials.
			savedHash := old.OutputHash
			old.OutputHash, e = treeHash(old.Root)
			if savedHash != "" && savedHash != old.OutputHash {
				return old, errors.New("interrupted trial output changed")
			}
			if e != nil {
				return old, e
			}
			if e = m.put("trial", old); e != nil {
				return old, e
			}
			return old, nil
		}
		return old, errors.New("trial is already running; effects are not replayed")
	}
	if !errors.Is(e, store.ErrNotFound) {
		return Trial{}, e
	}
	t := Trial{ID: id, ExperimentID: x.ID, TaskID: taskID, Label: label, RevisionID: revisionID, InputHash: identity, Status: "running", CreatedAt: m.now(), Root: filepath.Join(m.config.Root, "trials", id, "workspace")}
	if ownerKind != "" {
		t.OwnerKind, t.OwnerID = ownerKind, x.ID
	}
	if m.config.ValidateEnvironment != nil {
		if e = m.config.ValidateEnvironment(ctx, x.Spec); e != nil {
			return t, e
		}
	}
	if e = writeFiles(t.Root, task.Public); e != nil {
		return t, e
	}
	if e = m.put("trial", t); e != nil {
		return t, e
	}
	if x.Spec.CachePolicy != "" {
		if x.Spec.CachePolicy != ColdEachAttemptV2 {
			return t, errors.New("unsupported runtime cache policy")
		}
		if m.config.PrepareAttempt != nil {
			prepared := time.Now()
			prepareCtx, prepareCancel := context.WithTimeout(ctx, time.Minute)
			e = m.config.PrepareAttempt(prepareCtx, x.Spec)
			prepareCancel()
			t.PreparationMS = time.Since(prepared).Milliseconds()
			if e != nil {
				t.InfrastructureError = "runtime preparation: " + e.Error()
			}
		}
	}
	trialCtx, cancel := context.WithTimeout(ctx, time.Duration(x.Spec.BudgetSeconds)*time.Second)
	defer cancel()
	start := time.Now()
	var progressMu sync.Mutex
	toolCount := 0
	if e != nil {
		// A failed cold start is an infrastructure observation, never model evidence.
	} else if m.config.Solve == nil {
		e = errors.New("solver is unavailable")
	} else {
		t.Result, e = m.config.Solve(trialCtx, SolveRequest{TrialID: id, Root: t.Root, Prompt: task.Prompt, Revision: r, Spec: x.Spec, Task: Task{ID: task.ID, Name: task.Name, Stack: task.Stack, Command: task.Command, PublicHash: task.PublicHash, ProtectedFiles: task.ProtectedFiles, Contract: task.Contract}}, func(event Event) {
			if event.Type == "thinking" || event.Type == "adk_event" || event.Type == "text" {
				return
			}
			progressMu.Lock()
			defer progressMu.Unlock()
			if event.Type == "tool_started" {
				toolCount++
			}
			// Titles are host-authored, no event payload, arguments, text, or raw reasoning.
			title := event.Title
			if len(title) > 160 {
				title = title[:160]
			}
			t.Progress = Doc{"type": event.Type, "title": title, "toolCount": toolCount, "updatedAt": m.now()}
			_ = m.put("trial", t)
		})
	}
	t.Result.DurationMS = time.Since(start).Milliseconds()
	if x.Spec.CachePolicy == ColdEachAttemptV2 && m.config.ReleaseRuntime != nil {
		released := time.Now()
		cleanupCtx := context.WithoutCancel(ctx)
		cleanupCancel := func() {}
		if deadline, ok := ctx.Deadline(); ok {
			cleanupCtx, cleanupCancel = context.WithDeadline(cleanupCtx, deadline)
		}
		releaseCtx, releaseCancel := context.WithTimeout(cleanupCtx, 30*time.Second)
		releaseErr := m.config.ReleaseRuntime(releaseCtx, x.Spec)
		releaseCancel()
		cleanupCancel()
		t.ReleaseMS = time.Since(released).Milliseconds()
		if releaseErr != nil {
			if t.InfrastructureError != "" {
				t.InfrastructureError += "; "
			}
			t.InfrastructureError += "runtime release: " + releaseErr.Error()
			e = errors.Join(e, releaseErr)
		}
	}
	t.Status = "completed"
	if e != nil || t.Result.Incomplete {
		t.Status = "failed"
		if e != nil {
			t.Error = e.Error()
		} else {
			t.Error = "Solver response was incomplete"
		}
	}
	if errors.Is(e, context.Canceled) {
		m.mu.Lock()
		if m.closed {
			t.Status = "interrupted"
		}
		m.mu.Unlock()
	}
	t.FinishedAt = m.now()
	t.OutputHash, e = treeHash(t.Root)
	if e != nil {
		t.Status = "failed"
		t.Error = e.Error()
	}
	if e = m.put("trial", t); e != nil {
		return t, e
	}
	if t.InfrastructureError != "" {
		return t, errors.New(t.InfrastructureError)
	}
	return t, nil
}

// Cancel and active state writes share the same lock. A stale worker may retain
// local fields, but can never turn a cancelled head back into an open campaign.
func (m *Manager) putActiveExperiment(x Experiment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var current Experiment
	if e := m.get("experiment", x.ID, &current); e != nil {
		return e
	}
	if current.Status == "cancelled" || m.closed {
		return context.Canceled
	}
	if current.ImprovementStages > x.ImprovementStages {
		x.ImprovementStages = current.ImprovementStages
	}
	return m.put("experiment", x)
}
