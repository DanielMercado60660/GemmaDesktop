package harness

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/store"
	"time"
)

var ErrBatchCheckpointV2 = errors.New("harness batch checkpoint reached")

type OptimizationBatchV2 struct {
	ID           string   `json:"id"`
	ExperimentID string   `json:"experimentId"`
	SpecHash     string   `json:"specHash"`
	AttemptIDs   []string `json:"attemptIds"`
	MaxAttempts  int      `json:"maxAttempts"`
	WallSeconds  int      `json:"wallSeconds"`
	Status       string   `json:"status"`
	StartedAt    string   `json:"startedAt"`
	Deadline     string   `json:"deadline"`
	FinishedAt   string   `json:"finishedAt,omitempty"`
}

// Cross-task composition is external Codex work, never a model self-selection.
// It combines only observed development probes and cannot authorize promotion.
func (m *Manager) MakeCompositionAllV2(id string) (Packet, error) {
	var x Experiment
	if err := m.get("experiment", id, &x); err != nil {
		return Packet{}, err
	}
	if x.Spec.FixturePackID == "" || len(x.Spec.TaskIDs) < 2 {
		return Packet{}, errors.New("cross-task composition requires a versioned multi-task experiment")
	}
	evidence := []string{}
	finals := map[string]Trial{}
	for _, trialID := range x.TrialIDs {
		t, err := m.trial(trialID)
		if err != nil {
			return Packet{}, err
		}
		if t.Label == "final" {
			if _, exists := finals[t.TaskID]; exists {
				return Packet{}, errors.New("duplicate final development probe")
			}
			finals[t.TaskID] = t
		}
	}
	contexts := map[string]string{}
	for _, taskID := range x.Spec.TaskIDs {
		trial, ok := finals[taskID]
		if !ok {
			return Packet{}, errors.New("every development task needs a final probe before composition")
		}
		evidence = append(evidence, trial.ID)
		revision, err := m.revision(trial.RevisionID)
		if err != nil {
			return Packet{}, err
		}
		b, _ := json.Marshal(revision.Content)
		contexts[taskID+"/candidate.json"] = string(b)
	}
	p, err := m.MakePacket(x, "all-development", "compose_all", x.Spec.BaseRevisionID, "Compose one coherent declarative candidate from every development task's observed final probe. Retain baseline behavior where evidence is weak; resolve conflicting guidance explicitly. This composition has not itself been probed and must enter diagnostics and fresh two-arm qualification before promotion. Never optimize against validation or held-out tasks.", evidence, contexts)
	if err != nil {
		return p, err
	}
	return p, m.recordPacket(&x, p)
}
func (m *Manager) UpstreamCompositionV2(ctx context.Context, id string) (Packet, error) {
	p, err := m.MakeCompositionAllV2(id)
	if err != nil {
		return p, err
	}
	return m.WaitPacket(ctx, p)
}

func (m *Manager) beginOptimizationBatchV2(x Experiment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := &OptimizationBatchV2{ID: store.ID(), ExperimentID: x.ID, SpecHash: x.Spec.Hash, AttemptIDs: []string{}, MaxAttempts: 3, WallSeconds: 7200, Status: "running", StartedAt: m.now(), Deadline: time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)}
	if err := m.put("optimization_batch_v2", b); err != nil {
		return err
	}
	m.optimizationBatches[x.ID] = b
	return nil
}
func (m *Manager) reserveOptimizationAttemptV2(x Experiment, task Task, label string, r Revision) error {
	id := Hash([]string{x.ID, task.ID, label, r.Hash, x.Spec.Hash, task.PublicHash})[:32]
	if _, err := m.trial(id); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.optimizationBatches[x.ID]
	if b == nil {
		return errors.New("versioned optimization requires an active bounded batch")
	}
	for _, attempt := range b.AttemptIDs {
		if attempt == id {
			return errors.New("reserved attempt has uncertain effects; it cannot be replayed")
		}
	}
	deadline, err := time.Parse(time.RFC3339Nano, b.Deadline)
	if err != nil {
		return err
	}
	if b.SpecHash != x.Spec.Hash {
		return errors.New("optimizer identity changed within batch")
	}
	if len(b.AttemptIDs) >= b.MaxAttempts || time.Until(deadline) < attemptEnvelopeV2+batchFlushReserveV2 {
		var current Experiment
		if err = m.get("experiment", x.ID, &current); err != nil {
			return err
		}
		if current.Status != "cancelled" {
			current.Status = "checkpointed"
			if err = m.put("experiment", current); err != nil {
				return err
			}
		}
		return ErrBatchCheckpointV2
	}
	b.AttemptIDs = append(b.AttemptIDs, id)
	return m.put("optimization_batch_v2", b)
}
func (m *Manager) finishOptimizationBatchV2(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.optimizationBatches[id]
	if b == nil {
		return
	}
	var x Experiment
	if m.get("experiment", id, &x) == nil {
		b.Status = x.Status
	}
	if b.Status == "running" {
		b.Status = "checkpointed"
		x.Status = b.Status
		_ = m.put("experiment", x)
	}
	b.FinishedAt = m.now()
	_ = m.put("optimization_batch_v2", b)
	delete(m.optimizationBatches, id)
}

func (m *Manager) OptimizationBatchesV2(id string) ([]Doc, error) {
	all, err := m.config.Store.List("harness_optimization_batch_v2")
	if err != nil {
		return nil, err
	}
	out := []Doc{}
	for _, b := range all {
		if b["experimentId"] == id {
			out = append(out, b)
		}
	}
	return out, nil
}
