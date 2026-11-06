package harness

import (
	"errors"
	"strings"
)

// EvaluateReliabilityV2 compares complete configurations. Cost is reported,
// never a 1.2x veto. Absolute success-rate floors are intentionally absent.
// Legacy EvaluateGate retains its original meaning.
func EvaluateReliabilityV2(schedule []ScheduledAttemptV2, results []ResultV2, tasks []Task) GateV2 {
	g := GateV2{Policy: ReliabilityPolicyV2, Reasons: []string{}, EvidenceHash: Hash([]any{ReliabilityPolicyV2, schedule, results})}
	byID := map[string]Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	counts := map[string]map[string]int{}
	seen := map[string]bool{}
	if len(schedule) == 0 || len(results) != len(schedule) {
		g.Reasons = append(g.Reasons, "complete frozen schedule is required")
	}
	for i, s := range schedule {
		key := Hash([]any{s.TaskID, s.Arm, s.Repeat})
		t, ok := byID[s.TaskID]
		if !ok || s.Index != i || s.Split != t.Split || (s.Split != "validation" && s.Split != "heldout") || (s.Arm != "baseline" && s.Arm != "candidate") || s.Repeat < 0 || s.Repeat > 2 || seen[key] {
			g.Reasons = append(g.Reasons, "invalid or duplicate scheduled attempt")
			continue
		}
		seen[key] = true
		if i >= len(results) {
			continue
		}
		r := results[i]
		if Hash(r.ScheduledAttemptV2) != Hash(s) {
			g.Reasons = append(g.Reasons, "result differs from frozen schedule")
			continue
		}
		if r.InfrastructureError != "" || r.Grade.Error != "" || r.TrialID == "" || r.OutputHash == "" || r.Grade.GraderHash != t.GraderHash || len(r.Grade.Checks) == 0 {
			g.Reasons = append(g.Reasons, "infrastructure or evidence integrity is incomplete")
		}
		if !r.AccountingComplete || r.DurationMS <= 0 || r.Tokens <= 0 {
			g.Reasons = append(g.Reasons, "terminal usage accounting or elapsed measurement is incomplete")
		}
		passed := r.Grade.Passed && r.TrialStatus == "completed"
		for _, c := range r.Grade.Checks {
			if !c.Passed {
				passed = false
			}
			name := strings.ToLower(c.Name)
			if !c.Passed && strings.HasPrefix(name, "infrastructure:") {
				g.Reasons = append(g.Reasons, "infrastructure check failed")
			}
			if s.Arm == "candidate" && !c.Passed && (strings.HasPrefix(name, "integrity:") || strings.HasPrefix(name, "authority:")) {
				g.Reasons = append(g.Reasons, "candidate integrity or authority violation")
			}
		}
		if counts[s.TaskID] == nil {
			counts[s.TaskID] = map[string]int{}
		}
		if passed {
			counts[s.TaskID][s.Arm]++
			if s.Split == "heldout" {
				if s.Arm == "baseline" {
					g.BaselineSuccesses++
				} else {
					g.CandidateSuccesses++
				}
			}
		}
	}
	heldout := 0
	for _, t := range tasks {
		if t.Split != "validation" && t.Split != "heldout" {
			continue
		}
		for _, arm := range []string{"baseline", "candidate"} {
			for repeat := 0; repeat < 3; repeat++ {
				if !seen[Hash([]any{t.ID, arm, repeat})] {
					g.Reasons = append(g.Reasons, "three paired repeats required for every qualification task")
				}
			}
		}
		if t.Split == "heldout" {
			heldout++
		}
		if counts[t.ID]["candidate"] < counts[t.ID]["baseline"] {
			g.Reasons = append(g.Reasons, "qualification task regressed: "+t.ID)
		}
	}
	if heldout == 0 || g.CandidateSuccesses <= g.BaselineSuccesses {
		g.Reasons = append(g.Reasons, "held-out aggregate success did not strictly increase")
	}
	unique := []string{}
	dedup := map[string]bool{}
	for _, reason := range g.Reasons {
		if !dedup[reason] {
			unique = append(unique, reason)
			dedup[reason] = true
		}
	}
	g.Reasons = unique
	g.Eligible = len(g.Reasons) == 0
	return g
}

func (m *Manager) ReviewRunV2(id string, review ReviewV2) (RunV2, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.GetRunV2(id)
	if err != nil {
		return r, err
	}
	if r.Kind != "qualification" || r.Status != "completed" {
		return r, errors.New("only a completed qualification can be reviewed")
	}
	if err = m.validateRunV2(r); err != nil {
		return r, err
	}
	g := EvaluateReliabilityV2(r.Schedule, r.Results, m.packs[r.PackID].Tasks)
	if review.EvidenceHash != g.EvidenceHash || !validAuthor(review.Author) || (review.Verdict != "approved" && review.Verdict != "rejected") || len(review.Notes) > 32000 || strings.TrimSpace(review.Notes) == "" {
		return r, errors.New("review requires valid independent authorship, verdict, notes and current evidence hash")
	}
	for _, armID := range r.ArmIDs {
		a, err := m.GetArmV2(armID)
		if err != nil {
			return r, err
		}
		if a.Revision.AuthoredBy.Identity == review.Author.Identity {
			return r, errors.New("reviewer must differ from the revision author")
		}
		// If provenance is a known development experiment, exclude every proposal
		// author rather than only the author of the final composition.
		packets, err := m.config.Store.List("harness_packet")
		if err != nil {
			return r, err
		}
		for _, d := range packets {
			p, e := decode[Packet](d)
			if e != nil {
				return r, e
			}
			if p.ExperimentID == a.Provenance && p.Response != nil && p.Response.Content != nil && p.Response.AuthoredBy.Identity == review.Author.Identity {
				return r, errors.New("reviewer authored a development proposal")
			}
		}
	}
	if r.Review != nil {
		review.CreatedAt = r.Review.CreatedAt
		if Hash(review) != Hash(*r.Review) {
			return r, errors.New("qualification review is immutable")
		}
		return r, nil
	}
	review.CreatedAt = m.now()
	r.Gate = g
	r.Review = &review
	return r, m.put("run_v2", r)
}

// QualifiedArmV2 validates evidence and independent review before returning the
// complete frozen profile to the application's scoped activation/rollback path.
func (m *Manager) QualifiedArmV2(id string) (ArmV2, error) {
	r, err := m.GetRunV2(id)
	if err != nil {
		return ArmV2{}, err
	}
	if r.Kind != "qualification" || r.Status != "completed" || len(r.ArmIDs) != 2 {
		return ArmV2{}, errors.New("qualification is not complete")
	}
	if err = m.validateRunV2(r); err != nil {
		return ArmV2{}, err
	}
	g := EvaluateReliabilityV2(r.Schedule, r.Results, m.packs[r.PackID].Tasks)
	if !g.Eligible || r.Review == nil || r.Review.Verdict != "approved" || r.Review.EvidenceHash != g.EvidenceHash {
		return ArmV2{}, errors.New("fresh independent approval and reliability-v2 gates are required")
	}
	return m.GetArmV2(r.ArmIDs[1])
}
