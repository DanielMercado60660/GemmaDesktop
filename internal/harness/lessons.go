package harness

import (
	"errors"
	"sort"
)

func (m *Manager) saveLessonVersion(l Lesson) (Lesson, error) {
	l.VersionID = ""
	l.VersionID = Hash(l)
	version := asDoc(l)
	version["lessonId"] = l.ID
	version["id"] = l.VersionID
	if e := m.config.Store.Put("harness_lesson_version", version); e != nil {
		return l, e
	}
	// Only the pointer/projection changes. Every prior complete version survives.
	return l, m.put("lesson", l)
}
func (m *Manager) LessonHistory(id string) ([]Lesson, error) {
	rows, e := m.config.Store.List("harness_lesson_version")
	if e != nil {
		return nil, e
	}
	out := []Lesson{}
	for _, d := range rows {
		if d["lessonId"] != id {
			continue
		}
		d["id"] = id
		l, e := decode[Lesson](d)
		if e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
func (m *Manager) SetLessonStatus(id, status string) (Lesson, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var l Lesson
	if e := m.get("lesson", id, &l); e != nil {
		return l, e
	}
	if status != "accepted" && status != "rejected" && status != "provisional" {
		return l, errors.New("invalid lesson status")
	}
	if status == "accepted" {
		rows, e := m.config.Store.List("harness_lesson")
		if e != nil {
			return l, e
		}
		for _, d := range rows {
			other, e := decode[Lesson](d)
			if e != nil {
				return l, e
			}
			if other.ID != l.ID && other.WorkspaceID == l.WorkspaceID && other.AgentID == l.AgentID && other.Status == "accepted" && lessonsConflict(l, other) {
				return l, errors.New("resolve the contradictory accepted lesson before accepting this version")
			}
		}
	}
	if l.Status == status {
		return l, nil
	}
	if l.VersionID == "" {
		l.Version = 1
		var e error
		l, e = m.saveLessonVersion(l)
		if e != nil {
			return l, e
		}
	}
	l.PreviousVersionID = l.VersionID
	l.Version++
	l.Status = status
	l.CreatedAt = m.now()
	return m.saveLessonVersion(l)
}
func (m *Manager) lessonLibrary(x Experiment, taskID string) ([]Lesson, error) {
	rows, e := m.config.Store.List("harness_lesson")
	if e != nil {
		return nil, e
	}
	out := []Lesson{}
	task := m.tasks[taskID]
	for _, d := range rows {
		l, e := decode[Lesson](d)
		if e != nil {
			return nil, e
		}
		if l.WorkspaceID != x.Spec.WorkspaceID || l.AgentID != x.Spec.AgentID || l.Status == "rejected" || l.TaskID == taskID {
			continue
		}
		if l.ExperimentID != x.ID && l.Status != "accepted" {
			continue
		}
		if l.Compatibility["modelDigest"] != x.Spec.ModelDigest || l.Compatibility["runtimeVersion"] != x.Spec.RuntimeVersion || l.Compatibility["methodVersion"] != MethodVersion || l.Compatibility["capabilityHash"] != Hash(x.Spec.FrozenCapability) {
			continue
		}
		if l.Scope != "workspace" && l.Compatibility["stack"] != task.Stack {
			continue
		}
		out = append(out, l)
	}
	filtered := []Lesson{}
	for _, l := range out {
		conflict := false
		for _, other := range out {
			if l.ID != other.ID && lessonsConflict(l, other) {
				conflict = true
				break
			}
		}
		if !conflict {
			filtered = append(filtered, l)
		}
	}
	out = filtered
	sort.Slice(out, func(i, j int) bool { return out[i].VersionID < out[j].VersionID })
	if len(out) > 32 {
		out = out[len(out)-32:]
	}
	return out, nil
}

func lessonsConflict(a, b Lesson) bool {
	for _, id := range a.Contradictions {
		if id == b.ID {
			return true
		}
	}
	for _, id := range b.Contradictions {
		if id == a.ID {
			return true
		}
	}
	return false
}
