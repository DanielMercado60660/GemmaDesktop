package harness

import (
	"context"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
	"strings"
	"time"
)

func (m *Manager) MakePacket(x Experiment, taskID, stage, base, mission string, evidence []string, content map[string]string) (Packet, error) {
	// The first packet freezes its library/context. Later lesson-head changes
	// must not replace a waiting or submitted packet during restart/advance.
	rows, e := m.config.Store.List("harness_packet")
	if e != nil {
		return Packet{}, e
	}
	for _, d := range rows {
		old, e := decode[Packet](d)
		if e == nil && old.ExperimentID == x.ID && old.TaskID == taskID && old.Stage == stage && old.BaseRevisionID == base {
			return old, nil
		}
	}
	p := Packet{ExperimentID: x.ID, TaskID: taskID, Stage: stage, Status: "awaiting_codex", BaseRevisionID: base, Mission: mission, Evidence: evidence, Context: content}
	p.Library, e = m.lessonLibrary(x, taskID)
	if e != nil {
		return p, e
	}
	p.RequestHash = Hash(p)
	p.ID = Hash([]string{x.ID, taskID, stage, p.RequestHash})[:32]
	p.CreatedAt = m.now()
	var old Packet
	if e := m.get("packet", p.ID, &old); e == nil {
		if old.RequestHash != p.RequestHash {
			return p, errors.New("packet identity mismatch")
		}
		return old, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return p, e
	}
	return p, m.put("packet", p)
}
func (m *Manager) Submit(response PacketResponse) (Packet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var p Packet
	if e := m.get("packet", response.ID, &p); e != nil {
		return p, e
	}
	if !validAuthor(response.AuthoredBy) {
		return p, errors.New("packet must identify its Codex author")
	}
	if response.RequestHash != p.RequestHash {
		return p, errors.New("stale or mismatched packet hash")
	}
	if len(response.Notes) > 32000 || len(response.Lessons) > 16 {
		return p, errors.New("packet response exceeds bounds")
	}
	var x Experiment
	if e := m.get("experiment", p.ExperimentID, &x); e != nil {
		return p, e
	}
	if x.Status == "cancelled" || x.Status == "failed" {
		return p, errors.New("experiment is no longer accepting packets")
	}
	if p.Response != nil {
		copy := response
		copy.CreatedAt = p.Response.CreatedAt
		if Hash(copy) == Hash(*p.Response) {
			if e := m.recountImprovementStages(x.ID); e != nil {
				return p, e
			}
			return p, m.retainPacketLessons(x, p)
		}
		return p, errors.New("packet response is immutable")
	}
	allowed := map[string]bool{}
	for _, id := range p.Evidence {
		allowed[id] = true
	}
	for _, id := range response.Evidence {
		if !allowed[id] {
			return p, errors.New("review cites evidence outside its packet")
		}
	}
	switch p.Stage {
	case "proposal_inv", "proposal_dom", "compose":
		if response.Content == nil {
			return p, errors.New("proposal/composition requires declarative content")
		}
		r, e := m.CreateRevision(x.Spec, p.BaseRevisionID, *response.Content, response.AuthoredBy)
		if e != nil {
			return p, e
		}
		p.RevisionID = r.ID
	case "reflection", "promotion_review":
		if p.Stage == "promotion_review" {
			if len(p.Evidence) != 1 {
				return p, errors.New("invalid evaluation review evidence")
			}
			var evaluation Evaluation
			if e := m.get("evaluation", p.Evidence[0], &evaluation); e != nil {
				return p, e
			}
			if p.Context["evaluation-evidence.sha256"] != Hash(evaluation.Results) {
				return p, errors.New("evaluation changed after review packet creation")
			}
			if e := m.verifyEvaluationOutputs(evaluation); e != nil {
				return p, e
			}
		}
		if response.Verdict != "approved" && response.Verdict != "rejected" {
			return p, errors.New("independent review must approve or reject")
		}
		if len(response.Evidence) == 0 {
			return p, errors.New("independent review requires evidence references")
		}
		rows, e := m.config.Store.List("harness_packet")
		if e != nil {
			return p, e
		}
		for _, d := range rows {
			other, e := decode[Packet](d)
			if e == nil && other.ExperimentID == p.ExperimentID && other.Response != nil && (other.Stage == "proposal_inv" || other.Stage == "proposal_dom" || other.Stage == "compose") && other.Response.AuthoredBy.Identity == response.AuthoredBy.Identity {
				return p, errors.New("reviewer must be distinct from proposal/composition authors")
			}
		}
		for _, l := range response.Lessons {
			if l.Scope != "" && l.Scope != "stack" && l.Scope != "workspace" {
				return p, errors.New("lesson scope must be stack or workspace")
			}
			if len(l.Contradictions) > 16 {
				return p, errors.New("too many lesson contradictions")
			}
			for _, id := range l.Contradictions {
				var other Lesson
				if e := m.get("lesson", id, &other); e != nil || other.WorkspaceID != x.Spec.WorkspaceID || other.AgentID != x.Spec.AgentID {
					return p, errors.New("contradiction references an unrelated lesson")
				}
			}

			if strings.TrimSpace(l.Cue) == "" || strings.TrimSpace(l.Guidance) == "" || len(l.Cue) > 1000 || len(l.Guidance) > 4000 || len(l.Evidence) == 0 {
				return p, errors.New("lesson needs bounded cue, guidance and evidence")
			}
			for _, id := range l.Evidence {
				if !allowed[id] {
					return p, errors.New("lesson cites evidence outside its packet")
				}
			}
		}
	default:
		return p, errors.New("unsupported packet stage")
	}
	response.CreatedAt = m.now()
	p.Response = &response
	p.Status = "submitted"
	if e := m.put("packet", p); e != nil {
		return p, e
	}
	if e := m.recountImprovementStages(x.ID); e != nil {
		return p, e
	}
	return p, m.retainPacketLessons(x, p)
}
func appendID(ids []string, id string) []string {
	for _, v := range ids {
		if v == id {
			return ids
		}
	}
	return append(ids, id)
}
func (m *Manager) recordTrial(x *Experiment, t Trial) error {
	x.TrialIDs = appendID(x.TrialIDs, t.ID)
	x.Rollouts = len(x.TrialIDs)
	return m.putActiveExperiment(*x)
}
func (m *Manager) recordPacket(x *Experiment, p Packet) error {
	x.PacketIDs = appendID(x.PacketIDs, p.ID)
	// Revisited packets are durable provenance, not a new wait. Keep running
	// while their submitted responses are being probed.
	if p.Response == nil {
		x.Status = "awaiting_codex"
	}
	return m.putActiveExperiment(*x)
}
func (m *Manager) native(ctx context.Context, id string) error {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return e
	}
	if x.Spec.FixtureDigest != m.fixtureDigest() {
		return errors.New("fixture collection changed since admission")
	}
	x.Status = "running"
	if e := m.putActiveExperiment(x); e != nil {
		return e
	}
	for x.TaskIndex < len(x.Spec.TaskIDs) {
		if e := ctx.Err(); e != nil {
			return e
		}
		task := m.tasks[x.Spec.TaskIDs[x.TaskIndex]]
		base, e := m.SolveTrial(ctx, x, task.ID, "base", x.Spec.BaseRevisionID)
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e = m.recordTrial(&x, base); e != nil {
			return e
		}
		packets := []Packet{}
		for _, stage := range []string{"proposal_inv", "proposal_dom"} {
			scope := "transferable execution guidance"
			if stage == "proposal_dom" {
				scope = "task-domain execution guidance"
			}
			p, e := m.MakePacket(x, task.ID, stage, x.Spec.BaseRevisionID, "Codex: propose a bounded change to "+scope+" using the public task and observed baseline. Do not change security, solver settings, test oracle, or claim hidden evaluation results. Return declarative content and evidence-based notes.", []string{base.ID}, task.Public)
			if e != nil {
				return e
			}
			if e = m.recordPacket(&x, p); e != nil {
				return e
			}
			packets = append(packets, p)
		}
		if packets[0].Response == nil || packets[1].Response == nil {
			return nil
		}
		evidence := []string{base.ID}
		for i, p := range packets {
			t, e := m.SolveTrial(ctx, x, task.ID, []string{"inv", "dom"}[i], p.RevisionID)
			if e != nil {
				return e
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if e = m.recordTrial(&x, t); e != nil {
				return e
			}
			evidence = append(evidence, t.ID)
		}
		comp, e := m.MakePacket(x, task.ID, "compose", x.Spec.BaseRevisionID, "Codex: compose the supported improvements from both candidate revisions and their actual probes. Retain baseline behavior when evidence does not support a change. Return declarative content and contrastive notes.", evidence, task.Public)
		if e != nil {
			return e
		}
		if e = m.recordPacket(&x, comp); e != nil {
			return e
		}
		if comp.Response == nil {
			return nil
		}
		final, e := m.SolveTrial(ctx, x, task.ID, "final", comp.RevisionID)
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e = m.recordTrial(&x, final); e != nil {
			return e
		}
		evidence = append(evidence, final.ID)
		x.CandidateID = comp.RevisionID
		x.ImprovementStages = 3 * (x.TaskIndex + 1)
		review, e := m.MakePacket(x, task.ID, "reflection", comp.RevisionID, "Independent Codex reviewer: inspect the actual four rollouts and harness changes. Approve or reject the evidence-supported reflection, identify limits, and retain only transferable lessons. This is not hidden-test grading or production promotion.", evidence, task.Public)
		if e != nil {
			return e
		}
		if e = m.recordPacket(&x, review); e != nil {
			return e
		}
		if review.Response == nil {
			return nil
		}
		if review.Response.Verdict != "approved" {
			return errors.New("independent reflection rejected the candidate")
		}
		x.TaskIndex++
		x.Status = "running"
		if e = m.putActiveExperiment(x); e != nil {
			return e
		}
	}
	x.Status = "completed"
	x.FinishedAt = m.now()
	return m.putActiveExperiment(x)
}
func (m *Manager) WaitPacket(ctx context.Context, p Packet) (Packet, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var current Packet
		if e := m.get("packet", p.ID, &current); e != nil {
			return p, e
		}
		if current.Response != nil {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return p, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Upstream methods expose only this admitted experiment, never the application facade.
func (m *Manager) UpstreamSolve(ctx context.Context, id, task, label, revision string) (Trial, error) {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return Trial{}, e
	}
	found := false
	for _, t := range x.Spec.TaskIDs {
		found = found || t == task
	}
	if x.Spec.Mode != "upstream" || !found {
		return Trial{}, errors.New("upstream task outside admitted experiment")
	}
	if label != "base" && label != "inv" && label != "dom" && label != "final" {
		return Trial{}, errors.New("invalid upstream rollout label")
	}
	if err := ctx.Err(); err != nil {
		return Trial{}, err
	}
	x.Status = "running"
	if err := m.putActiveExperiment(x); err != nil {
		return Trial{}, err
	}
	t, e := m.SolveTrial(ctx, x, task, label, revision)
	if e == nil {
		if err := ctx.Err(); err != nil {
			return t, err
		}
		e = m.recordTrial(&x, t)
	}
	return t, e
}
func (m *Manager) UpstreamPacket(ctx context.Context, id, task, stage, base, mission string, evidence []string, files map[string]string) (Packet, error) {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return Packet{}, e
	}
	if x.Spec.Mode != "upstream" {
		return Packet{}, errors.New("not an upstream experiment")
	}
	for _, tid := range evidence {
		t, e := m.trial(tid)
		if e != nil || t.ExperimentID != id || t.TaskID != task {
			return Packet{}, errors.New("upstream evidence ownership mismatch")
		}
	}
	if len(mission) > 100000 || len(files) > 1000 {
		return Packet{}, errors.New("upstream packet exceeds bounds")
	}
	total := 0
	for p, b := range files {
		if !safePath(p) {
			return Packet{}, errors.New("unsafe packet path")
		}
		total += len(b)
	}
	if total > 4<<20 {
		return Packet{}, errors.New("upstream packet exceeds byte bound")
	}
	p, e := m.MakePacket(x, task, stage, base, mission, evidence, files)
	if e != nil {
		return p, e
	}
	if e = m.recordPacket(&x, p); e != nil {
		return p, e
	}
	p, e = m.WaitPacket(ctx, p)
	if e != nil {
		return p, e
	}
	if e = ctx.Err(); e != nil {
		return p, e
	}
	// Submission may have updated stage counts while this RPC was waiting.
	// Refresh metadata, then use the same cancellation-protected writer.
	if e = m.get("experiment", id, &x); e != nil {
		return p, e
	}
	x.Status = "running"
	return p, m.putActiveExperiment(x)
}
func (m *Manager) FinishUpstream(id, candidate string, rollouts, stages int) error {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return e
	}
	if rollouts != 4*len(x.Spec.TaskIDs) || stages != 3*len(x.Spec.TaskIDs) || len(x.TrialIDs) != rollouts {
		return fmt.Errorf("upstream accounting mismatch: %d rollouts/%d stages", rollouts, stages)
	}
	for _, id := range x.TrialIDs {
		t, e := m.trial(id)
		if e != nil {
			return e
		}
		h, e := treeHash(t.Root)
		if e != nil || h != t.OutputHash {
			return errors.New("cached upstream output changed")
		}
	}
	if _, e := m.revision(candidate); e != nil {
		return e
	}
	x.CandidateID = candidate
	x.Rollouts = rollouts
	x.ImprovementStages = stages
	x.TaskIndex = len(x.Spec.TaskIDs)
	x.Status = "completed"
	x.FinishedAt = m.now()
	return m.putActiveExperiment(x)
}

func (m *Manager) retainPacketLessons(x Experiment, p Packet) error {
	if p.Response == nil {
		return nil
	}
	if p.Stage == "reflection" && p.Response.Verdict == "approved" {
		for _, in := range p.Response.Lessons {
			l := Lesson{ExperimentID: x.ID, WorkspaceID: x.Spec.WorkspaceID, AgentID: x.Spec.AgentID, TaskID: p.TaskID, RevisionID: p.BaseRevisionID, Status: "provisional", Cue: in.Cue, Guidance: in.Guidance, Evidence: in.Evidence, AuthoredBy: p.Response.AuthoredBy, CreatedAt: p.Response.CreatedAt}
			l.ID = Hash([]any{p.ID, in})[:32]
			if _, err := m.Get("lesson", l.ID); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			l.Version = 1
			l.Scope = in.Scope
			if l.Scope == "" {
				l.Scope = "stack"
			}
			l.Contradictions = in.Contradictions
			l.Compatibility = Doc{"modelDigest": x.Spec.ModelDigest, "runtimeVersion": x.Spec.RuntimeVersion, "methodVersion": MethodVersion, "stack": m.tasks[p.TaskID].Stack, "capabilityHash": Hash(x.Spec.FrozenCapability)}
			if _, e := m.saveLessonVersion(l); e != nil {
				return e
			}
		}
	}
	return nil
}

// Accepted proposal/composition packets complete improvement stages even when a
// subsequent solver rollout fails. Reflection is a separate review, not stage4.
// Recounting is idempotent after a response was persisted but a later write failed.
func (m *Manager) recountImprovementStages(experimentID string) error {
	rows, e := m.config.Store.List("harness_packet")
	if e != nil {
		return e
	}
	count := 0
	for _, d := range rows {
		if d["experimentId"] != experimentID || d["status"] != "submitted" || d["revisionId"] == nil || d["revisionId"] == "" {
			continue
		}
		switch d["stage"] {
		case "proposal_inv", "proposal_dom", "compose":
			count++
		}
	}
	var x Experiment
	if e = m.get("experiment", experimentID, &x); e != nil {
		return e
	}
	if count == x.ImprovementStages {
		return nil
	}
	x.ImprovementStages = count
	return m.put("experiment", x)
}
