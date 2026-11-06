package harness

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/store"
	"time"
)

const developmentGradePolicy = "immutable-development-grade-v1"
const developmentGradeTimeout = 8 * time.Minute

// Only counts and opaque classifications leave this API. Grader names, errors,
// expected values and assertion details never enter optimizer-facing records.
type DevelopmentGrade struct {
	ID              string `json:"id"`
	ExperimentID    string `json:"experimentId"`
	TrialID         string `json:"trialId"`
	SpecHash        string `json:"specHash"`
	InputHash       string `json:"inputHash"`
	OutputHash      string `json:"outputHash"`
	GraderHash      string `json:"graderHash"`
	Policy          string `json:"policy"`
	Passed          bool   `json:"passed"`
	CheckCount      int    `json:"checkCount"`
	FailedCount     int    `json:"failedCount"`
	Infrastructure  string `json:"infrastructure,omitempty"`
	CreatedAt       string `json:"createdAt"`
	GradingFinished bool   `json:"gradingFinished"`
	ResultHash      string `json:"resultHash"`
}

func frozenSpecValid(s Spec) bool {
	hash := s.Hash
	s.Hash = ""
	return hash != "" && hash == Hash(s)
}

func revisionIdentityValid(r Revision) bool {
	if r.DelegateDigests == nil {
		r.DelegateDigests = map[string]string{}
	}
	return r.ID == r.Hash && r.Hash == Hash([]any{r.ParentID, r.WorkspaceID, r.AgentID, r.Content, r.ApprovedWorkflow, r.DelegateDigests})
}

// Task selection and revision provenance may change across development rounds;
// execution/model/grader/authority conditions may not silently change with it.
func developmentEnvironment(s Spec) any {
	return []any{s.Mode, s.WorkspaceID, s.AgentID, s.Model, s.ModelDigest, s.RuntimeVersion,
		s.Agent, s.ContextSize, s.FrozenCapability, s.ExecutorIdentity, s.ToolchainVersion,
		s.GraderVersion, s.FixtureDigest, s.AppVersion, s.SolverTools, s.ApprovedSkills,
		s.ApprovedWorkflows, s.FrozenAgents, s.ApprovedDelegateIDs, s.PythonIdentity,
		s.UpstreamAdapterHash, s.BudgetSeconds, s.RuntimeIdentity, s.FixturePackID,
		s.AuthorityHash, s.ProfileBindingHash, s.UpstreamRevision, s.CachePolicy}
}

// Called under m.mu during new admission. The prior immutable revision is
// reused losslessly; no agent configuration or product binding is activated.
func (m *Manager) developmentBaseRevision(spec Spec, baseline Content) (Revision, string, error) {
	if spec.FixturePackID == "" {
		return Revision{}, "", errors.New("development carry-forward requires a versioned fixture pack")
	}
	r, err := m.revision(spec.DevelopmentBaseRevisionID)
	if err != nil {
		return r, "", err
	}
	if !revisionIdentityValid(r) || r.WorkspaceID != spec.WorkspaceID || r.AgentID != spec.AgentID {
		return r, "", errors.New("development revision identity or target mismatch")
	}
	if err = ValidateContent(r.Content, spec); err != nil {
		return r, "", err
	}
	if Hash(r.ApprovedWorkflow) != Hash(spec.ApprovedWorkflows[r.Content.WorkflowID]) {
		return r, "", errors.New("development workflow authority changed")
	}
	for _, id := range r.Content.DelegateAgentIDs {
		if r.DelegateDigests[id] != Hash(spec.FrozenAgents[id]) {
			return r, "", errors.New("development delegate authority changed")
		}
	}
	rows, err := m.config.Store.ListHarnessMetadata("experiment")
	if err != nil {
		return r, "", err
	}
	packets, err := m.config.Store.ListHarnessMetadata("packet")
	if err != nil {
		return r, "", err
	}
	for _, row := range rows {
		metadata, err := decode[Experiment](row)
		if err != nil || (metadata.CandidateID != r.ID && metadata.Spec.BaseRevisionID != r.ID) {
			continue
		}
		var prior Experiment
		if err := m.get("experiment", metadata.ID, &prior); err != nil || !frozenSpecValid(prior.Spec) || Hash(developmentEnvironment(prior.Spec)) != Hash(developmentEnvironment(spec)) {
			continue
		}
		allDevelopment := len(prior.Spec.TaskIDs) > 0
		for _, id := range prior.Spec.TaskIDs {
			t, ok := m.taskForSpec(prior.Spec, id)
			allDevelopment = allDevelopment && ok && t.Split == "development"
		}
		if !allDevelopment {
			continue
		}
		// Retaining the unchanged initial baseline needs no invented review.
		if r.AuthoredBy.Kind == "baseline" && r.AuthoredBy.Identity == spec.AgentID && r.ParentID == "" && prior.Spec.BaseRevisionID == r.ID && Hash(r.Content) == Hash(baseline) {
			return r, prior.ID, nil
		}
		if prior.Status != "completed" || prior.CandidateID != r.ID {
			continue
		}
		for _, raw := range packets {
			metadata, err := decode[Packet](raw)
			if err != nil || metadata.ExperimentID != prior.ID || metadata.Stage != "reflection" || metadata.BaseRevisionID != r.ID {
				continue
			}
			var p Packet
			if err := m.get("packet", metadata.ID, &p); err == nil && p.Status == "submitted" && p.Response != nil && p.Response.ID == p.ID && p.Response.RequestHash == p.RequestHash && len(p.Response.Evidence) > 0 && p.Response.Verdict == "approved" && validAuthor(p.Response.AuthoredBy) && p.Response.AuthoredBy.Identity != r.AuthoredBy.Identity {
				return r, prior.ID, nil
			}
		}
	}
	return r, "", errors.New("development revision lacks compatible independently reviewed provenance")
}

// GradeDevelopmentTrial grades an existing stopped optimization output. It
// never runs the solver, changes its workspace or authorizes promotion.
func (m *Manager) GradeDevelopmentTrial(ctx context.Context, experimentID, trialID string) (DevelopmentGrade, error) {
	m.developmentGradeMu.Lock()
	defer m.developmentGradeMu.Unlock()
	var x Experiment
	if err := m.get("experiment", experimentID, &x); err != nil {
		return DevelopmentGrade{}, err
	}
	if x.Spec.FixturePackID == "" || !frozenSpecValid(x.Spec) || x.Spec.FixtureDigest != m.fixtureDigestForSpec(x.Spec) {
		return DevelopmentGrade{}, errors.New("development grader requires an unchanged versioned experiment")
	}
	t, err := m.trial(trialID)
	if err != nil {
		return DevelopmentGrade{}, err
	}
	if t.ExperimentID != x.ID || !containsID(x.TrialIDs, t.ID) || (t.Status != "completed" && t.Status != "failed" && t.Status != "interrupted") || t.OutputHash == "" {
		return DevelopmentGrade{}, errors.New("only this experiment's stopped recorded development trials may be graded")
	}
	task, ok := m.taskForSpec(x.Spec, t.TaskID)
	if !ok || task.Split != "development" || !containsID(x.Spec.TaskIDs, t.TaskID) {
		return DevelopmentGrade{}, errors.New("only admitted development tasks may be probe-graded")
	}
	r, err := m.revision(t.RevisionID)
	if err != nil || !revisionIdentityValid(r) || r.WorkspaceID != x.Spec.WorkspaceID || r.AgentID != x.Spec.AgentID || t.InputHash != Hash([]string{x.ID, task.ID, t.Label, r.Hash, x.Spec.Hash, task.PublicHash}) {
		return DevelopmentGrade{}, errors.New("development trial input or revision identity changed")
	}
	if hash, err := treeHash(t.Root); err != nil || hash != t.OutputHash {
		return DevelopmentGrade{}, errors.New("development trial output changed")
	}
	result := DevelopmentGrade{ExperimentID: x.ID, TrialID: t.ID, SpecHash: x.Spec.Hash, InputHash: t.InputHash, OutputHash: t.OutputHash, GraderHash: task.GraderHash, Policy: developmentGradePolicy}
	result.ID = Hash([]string{result.Policy, result.ExperimentID, result.TrialID, result.SpecHash, result.InputHash, result.OutputHash, result.GraderHash})
	var old DevelopmentGrade
	if err := m.get("development_grade", result.ID, &old); err == nil {
		digest := old.ResultHash
		old.ResultHash = ""
		valid := digest != "" && digest == Hash(old)
		old.ResultHash = digest
		if !valid || old.ID != result.ID || old.ExperimentID != result.ExperimentID || old.TrialID != result.TrialID || old.SpecHash != result.SpecHash || old.InputHash != result.InputHash || old.OutputHash != result.OutputHash || old.GraderHash != result.GraderHash || old.Policy != result.Policy {
			return result, errors.New("saved development grade identity changed")
		}
		return old, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return result, err
	}
	gradeCtx, cancel := context.WithTimeout(ctx, developmentGradeTimeout)
	if err := gradeCtx.Err(); err != nil {
		cancel()
		return result, err
	}
	m.mu.Lock()
	if m.closed || m.developmentGradeCancel != nil {
		m.mu.Unlock()
		cancel()
		return result, errors.New("development grading is unavailable")
	}
	// A packet may have been submitted since the first read. Re-read while
	// sharing Submit/launch's lock, and require a genuinely waiting packet if
	// the upstream worker still owns this experiment's job.
	var current Experiment
	if err := m.get("experiment", x.ID, &current); err != nil || current.Spec.Hash != x.Spec.Hash {
		m.mu.Unlock()
		cancel()
		return result, errors.New("development experiment changed before grading admission")
	}
	x = current
	for id := range m.jobs {
		if id != x.ID {
			m.mu.Unlock()
			cancel()
			return result, errors.New("another harness operation is active")
		}
	}
	if _, active := m.jobs[x.ID]; active {
		waiting := false
		for _, id := range x.PacketIDs {
			var packet Packet
			if m.get("packet", id, &packet) == nil && packet.Status == "awaiting_codex" && packet.Response == nil {
				waiting = true
			}
		}
		if x.Status != "awaiting_codex" || !waiting {
			m.mu.Unlock()
			cancel()
			return result, errors.New("wait for the active development worker before grading")
		}
	}
	if x.Status != "awaiting_codex" && x.Status != "checkpointed" && x.Status != "completed" && x.Status != "failed" && x.Status != "interrupted" && x.Status != "cancelled" {
		m.mu.Unlock()
		cancel()
		return result, errors.New("wait for a development packet or stopped experiment before grading")
	}
	m.developmentGradeCancel, m.developmentGradeExperimentID = cancel, x.ID
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.developmentGradeCancel = nil
		m.developmentGradeExperimentID = ""
		m.mu.Unlock()
		m.wg.Done()
	}()
	result.CreatedAt = m.now()
	// Reserve the attempt before invoking executable grading. A process crash
	// leaves an opaque interrupted result; repeating the API never replays it.
	result.Infrastructure = "interrupted"
	result.ResultHash = Hash(result)
	if err := m.put("development_grade", result); err != nil {
		return result, err
	}
	result.Infrastructure, result.ResultHash = "", ""
	if m.config.ValidateDevelopmentGrade != nil && m.config.ValidateDevelopmentGrade(gradeCtx) != nil {
		result.Infrastructure = "admission"
	} else if t.InfrastructureError != "" {
		result.Infrastructure = "trial"
	} else if m.config.ValidateEnvironment != nil && m.config.ValidateEnvironment(gradeCtx, x.Spec) != nil {
		result.Infrastructure = "environment"
	} else if m.config.GradeWithSpec == nil {
		result.Infrastructure = "grader_unavailable"
	} else {
		grade, gradeErr := m.config.GradeWithSpec(gradeCtx, GradeRequest{Spec: x.Spec, Task: task, Trial: t})
		if gradeErr != nil || grade.Error != "" {
			result.Infrastructure = "grading"
		} else if grade.TrialID != t.ID || grade.GraderHash != task.GraderHash || len(grade.Checks) == 0 {
			result.Infrastructure = "grader_identity"
		} else {
			result.Passed, result.CheckCount = grade.Passed, len(grade.Checks)
			for _, check := range grade.Checks {
				if !check.Passed {
					result.FailedCount++
				}
			}
		}
	}
	if errors.Is(gradeCtx.Err(), context.DeadlineExceeded) {
		result.Infrastructure = "deadline"
	} else if gradeCtx.Err() != nil {
		result.Infrastructure = "cancelled"
	}
	if result.Infrastructure != "" {
		result.Passed = false
	}
	if hash, err := treeHash(t.Root); err != nil || hash != t.OutputHash {
		return result, errors.New("development trial output changed during grading")
	}
	result.GradingFinished = true
	result.ResultHash = Hash(result)
	return result, m.put("development_grade", result)
}

func containsID(ids []string, wanted string) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}

// UpstreamRevisionBinding identifies a k=1 stage independently of its file
// content. Distinct stages may legitimately have identical trees/revisions.
type UpstreamRevisionBinding struct {
	TaskID      string `json:"taskId"`
	Label       string `json:"label"`
	RevisionID  string `json:"revisionId"`
	PacketID    string `json:"packetId,omitempty"`
	RequestHash string `json:"requestHash,omitempty"`
}

func (m *Manager) UpstreamRevisionBindings(x Experiment) ([]UpstreamRevisionBinding, error) {
	if x.Spec.Mode != "upstream" || !frozenSpecValid(x.Spec) || x.Spec.FixtureDigest != m.fixtureDigestForSpec(x.Spec) {
		return nil, errors.New("upstream frozen spec identity changed")
	}
	base, err := m.revision(x.Spec.BaseRevisionID)
	if err != nil || !revisionIdentityValid(base) || base.WorkspaceID != x.Spec.WorkspaceID || base.AgentID != x.Spec.AgentID {
		return nil, errors.New("upstream baseline revision identity changed")
	}
	bindings := make([]UpstreamRevisionBinding, 0, len(x.Spec.TaskIDs)*4)
	for _, taskID := range x.Spec.TaskIDs {
		bindings = append(bindings, UpstreamRevisionBinding{TaskID: taskID, Label: "base", RevisionID: x.Spec.BaseRevisionID})
	}
	seen := map[string]bool{}
	stages := map[string]string{"proposal_inv": "inv", "proposal_dom": "dom", "compose": "final"}
	for _, id := range x.PacketIDs {
		var p Packet
		if err := m.get("packet", id, &p); err != nil {
			return nil, err
		}
		label := stages[p.Stage]
		if label == "" || p.Response == nil {
			continue
		}
		if p.ExperimentID != x.ID || !containsID(x.Spec.TaskIDs, p.TaskID) || p.Status != "submitted" || p.RevisionID == "" || p.BaseRevisionID != x.Spec.BaseRevisionID || p.Response.ID != p.ID || p.Response.RequestHash != p.RequestHash || p.Response.Content == nil {
			return nil, errors.New("upstream stage binding has invalid packet provenance")
		}
		request := p
		request.ID, request.RequestHash, request.CreatedAt, request.RevisionID = "", "", "", ""
		request.Status, request.Response = "awaiting_codex", nil
		if Hash(request) != p.RequestHash || p.ID != Hash([]string{x.ID, p.TaskID, p.Stage, p.RequestHash})[:32] {
			return nil, errors.New("upstream packet request identity changed")
		}
		key := p.TaskID + "/" + label
		if seen[key] {
			return nil, errors.New("duplicate upstream task/stage binding")
		}
		seen[key] = true
		r, err := m.revision(p.RevisionID)
		if err != nil || !revisionIdentityValid(r) || r.ParentID != x.Spec.BaseRevisionID || r.WorkspaceID != x.Spec.WorkspaceID || r.AgentID != x.Spec.AgentID || Hash(r.Content) != Hash(*p.Response.Content) {
			return nil, errors.New("upstream stage revision identity changed")
		}
		bindings = append(bindings, UpstreamRevisionBinding{TaskID: p.TaskID, Label: label, RevisionID: r.ID, PacketID: p.ID, RequestHash: p.RequestHash})
	}
	return bindings, nil
}
