package runtimes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

func New(config Config) (*Manager, error) {
	if config.DataDir == "" || !filepath.IsAbs(config.DataDir) {
		return nil, errors.New("managed runtimes require an absolute private data directory")
	}
	if config.DiskReserveBytes == 0 {
		config.DiskReserveBytes = 10 << 30
	}
	if config.DiskReserveBytes < 0 {
		return nil, errors.New("negative runtime disk reserve")
	}
	if config.Gate == nil {
		config.Gate = inference.NewAdmissionGate()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{config: config, root: filepath.Join(config.DataDir, "managed-inference"), ctx: ctx, cancel: cancel, catalog: map[string]Manifest{}, models: map[string]ModelManifest{}, jobs: map[string]context.CancelFunc{}, process: ProcessState{Status: "stopped"}, state: diskState{Version: 1, Installed: map[string]Installed{}, Models: map[string]Installed{}, Selections: map[inference.ProviderKind]Selection{}, Jobs: map[string]Job{}}}
	for _, manifest := range config.Catalog {
		if err := manifest.Validate(); err != nil {
			cancel()
			return nil, fmt.Errorf("runtime %s: %w", manifest.ID, err)
		}
		if _, exists := m.catalog[manifest.ID]; exists {
			cancel()
			return nil, errors.New("duplicate runtime catalog ID")
		}
		var copy Manifest
		encoded, _ := json.Marshal(manifest)
		_ = json.Unmarshal(encoded, &copy)
		m.catalog[manifest.ID] = copy
	}
	for _, model := range config.ModelCatalog {
		if err := model.Validate(); err != nil {
			cancel()
			return nil, fmt.Errorf("model %s: %w", model.ID, err)
		}
		if _, exists := m.models[model.ID]; exists {
			cancel()
			return nil, errors.New("duplicate model catalog ID")
		}
		var copy ModelManifest
		encoded, _ := json.Marshal(model)
		_ = json.Unmarshal(encoded, &copy)
		m.models[model.ID] = copy
	}
	for _, directory := range []string{m.root, filepath.Join(m.root, "runtimes"), filepath.Join(m.root, "models"), filepath.Join(m.root, "downloads"), filepath.Join(m.root, "state"), filepath.Join(m.root, "cache")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			cancel()
			return nil, err
		}
		if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			cancel()
			return nil, errors.New("runtime data path must be a real private directory")
		}
	}
	lock, err := os.OpenFile(filepath.Join(m.root, "manager.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		cancel()
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		cancel()
		return nil, errors.New("another managed runtime manager owns this data directory")
	}
	m.lock = lock
	b, err := os.ReadFile(filepath.Join(m.root, "state.json"))
	if err == nil {
		if len(b) > 16<<20 {
			m.Close()
			return nil, errors.New("runtime state exceeds size limit")
		}
		if err = json.Unmarshal(b, &m.state); err != nil || m.state.Version != 1 || m.state.Installed == nil || m.state.Models == nil || m.state.Jobs == nil || m.state.Selections == nil {
			m.Close()
			return nil, errors.New("invalid runtime state; preserve it for recovery")
		}
	} else if !os.IsNotExist(err) {
		m.Close()
		return nil, err
	}
	for id, job := range m.state.Jobs {
		if !terminalJob(job.Status) {
			job.Status = "interrupted"
			job.Error = "Application exited before this job completed; retry explicitly"
			job.UpdatedAt = time.Now().UTC()
			m.state.Jobs[id] = job
		}
	}
	if m.state.Qualifications == nil {
		m.state.Qualifications = map[string]QualificationReceipt{}
	}
	m.config.Catalog, m.config.ModelCatalog = nil, nil
	m.presentationCatalog = []Manifest{}
	m.platformSupport = map[string]string{}
	for _, manifest := range m.catalog {
		view := manifest
		view.RuntimeFileCount = len(manifest.Files)
		for _, file := range manifest.Files {
			view.UnpackedBytes += file.Size
		}
		view.Files = []File{}
		m.presentationCatalog = append(m.presentationCatalog, view)
		if err := platformPreflight(manifest); err != nil {
			m.platformSupport[manifest.ID] = err.Error()
		} else {
			m.platformSupport[manifest.ID] = "supported"
		}
	}
	if err := m.persistLocked(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

func terminalJob(status string) bool {
	return status == "complete" || status == "failed" || status == "cancelled" || status == "interrupted"
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func writeAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp-" + randomID()
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (m *Manager) persistLocked() error {
	return writeAtomic(filepath.Join(m.root, "state.json"), m.state)
}
func (m *Manager) changed() {
	if m.config.Changed != nil {
		m.config.Changed()
	}
}

func (m *Manager) Snapshot() Snapshot {
	return m.snapshot(false)
}

// PresentationSnapshot omits the immutable installation inventory from frequent
// UI refreshes. Artifact authority remains in the manager's complete catalog.
func (m *Manager) PresentationSnapshot() Snapshot {
	return m.snapshot(true)
}

func (m *Manager) snapshot(presentation bool) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Catalog: []Manifest{}, ModelCatalog: []ModelManifest{}, Installed: []Installed{}, Models: []Installed{}, Jobs: []Job{}, Selections: map[inference.ProviderKind]Selection{}, PlatformSupport: map[string]string{}, Process: m.process}
	s.Qualifications = []QualificationReceipt{}
	s.InferenceBlocked = m.config.Gate.BlockedReason()
	for _, receipt := range m.state.Qualifications {
		s.Qualifications = append(s.Qualifications, receipt)
	}
	if presentation {
		s.Catalog = append(s.Catalog, m.presentationCatalog...)
	} else {
		for _, manifest := range m.catalog {
			s.Catalog = append(s.Catalog, manifest)
		}
	}
	for id, status := range m.platformSupport {
		s.PlatformSupport[id] = status
	}
	for _, model := range m.models {
		s.ModelCatalog = append(s.ModelCatalog, model)
	}
	for _, installed := range m.state.Installed {
		s.Installed = append(s.Installed, installed)
	}
	for _, installed := range m.state.Models {
		s.Models = append(s.Models, installed)
	}
	for _, job := range m.state.Jobs {
		s.Jobs = append(s.Jobs, job)
	}
	for provider, selection := range m.state.Selections {
		s.Selections[provider] = selection
	}
	sort.Slice(s.Catalog, func(i, j int) bool { return s.Catalog[i].ID < s.Catalog[j].ID })
	sort.Slice(s.ModelCatalog, func(i, j int) bool { return s.ModelCatalog[i].ID < s.ModelCatalog[j].ID })
	sort.Slice(s.Jobs, func(i, j int) bool { return s.Jobs[i].UpdatedAt.After(s.Jobs[j].UpdatedAt) })
	// Snapshot callers must not mutate catalog slices used by the installer.
	b, _ := json.Marshal(s)
	var detached Snapshot
	_ = json.Unmarshal(b, &detached)
	return detached
}

func (m *Manager) CancelJob(id string) error {
	m.mu.Lock()
	cancel := m.jobs[id]
	m.mu.Unlock()
	if cancel == nil {
		return errors.New("job is not active")
	}
	cancel()
	return nil
}

func (m *Manager) Activate(buildID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("runtime manager is closed")
	}
	manifest, ok := m.catalog[buildID]
	if !ok {
		return errors.New("unknown trusted runtime build")
	}
	installed, ok := m.state.Installed[buildID]
	if !ok || installed.ManifestDigest != digestJSON(manifest) {
		return errors.New("runtime build has not been installed from this manifest")
	}
	if m.child != nil {
		return errors.New("stop the owned runtime before activation")
	}
	previous := m.state.Selections[manifest.Provider]
	if previous.Active == buildID {
		return nil
	}
	m.state.Selections[manifest.Provider] = Selection{Active: buildID, Previous: previous.Active}
	if err := m.persistLocked(); err != nil {
		m.state.Selections[manifest.Provider] = previous
		return err
	}
	go m.changed()
	return nil
}

func (m *Manager) Rollback(provider inference.ProviderKind) error {
	m.mu.Lock()
	id := m.state.Selections[provider].Previous
	m.mu.Unlock()
	if id == "" {
		return errors.New("no previous runtime build is available")
	}
	return m.Activate(id)
}

func (m *Manager) Remove(buildID string) error      { return m.remove(buildID, false) }
func (m *Manager) RemoveModel(modelID string) error { return m.remove(modelID, true) }
func (m *Manager) remove(id string, model bool) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("runtime manager is closed")
	}
	if !identifier.MatchString(id) {
		return errors.New("invalid artifact ID")
	}
	for _, job := range m.state.Jobs {
		if job.Target == id && !terminalJob(job.Status) {
			return errors.New("artifact has an active job")
		}
	}
	kind := "runtimes"
	table := m.state.Installed
	if model {
		kind = "models"
		table = m.state.Models
	}
	if _, exists := table[id]; !exists {
		return errors.New("artifact is not installed")
	}
	if m.child != nil && ((!model && m.process.BuildID == id) || (model && m.process.ModelID == id)) {
		return errors.New("stop the owned runtime before removing its artifacts")
	}
	selections := make(map[inference.ProviderKind]Selection, len(m.state.Selections))
	for provider, selection := range m.state.Selections {
		selections[provider] = selection
	}
	// Move only the named owned artifact out of service; persistence failure
	// restores it. Model removal never traverses another runtime/model tree.
	source := filepath.Join(m.root, kind, id)
	trash := source + ".remove-" + randomID()
	if err := os.Rename(source, trash); err != nil {
		return err
	}
	old := table[id]
	delete(table, id)
	if !model {
		for provider, selection := range m.state.Selections {
			if selection.Active == id {
				selection.Active = ""
			}
			if selection.Previous == id {
				selection.Previous = ""
			}
			m.state.Selections[provider] = selection
		}
	}
	if err := m.persistLocked(); err != nil {
		table[id] = old
		m.state.Selections = selections
		_ = os.Rename(trash, source)
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	go m.changed()
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := m.Stop(ctx)
	m.wg.Wait()
	if m.lock != nil {
		_ = syscall.Flock(int(m.lock.Fd()), syscall.LOCK_UN)
		if closeErr := m.lock.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
