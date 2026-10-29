package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	config Config
	root   string
	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	active *Job
	stop   context.CancelFunc
}

func Open(c Config) (*Manager, error) {
	if c.DataDir == "" || c.WorkspacePath == nil {
		return nil, errors.New("media data directory and workspace resolver are required")
	}
	if c.EmbeddingModel == "" {
		c.EmbeddingModel = EmbeddingModel
	}
	root := filepath.Join(c.DataDir, "media")
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	db, e := openDatabase(root)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{config: c, root: root, db: db, ctx: ctx, cancel: cancel}
	if e = m.recoverJobs(); e != nil {
		cancel()
		db.Close()
		return nil, e
	}
	return m, nil
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
	m.wg.Wait()
	return m.db.Close()
}
func (m *Manager) Active() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.active != nil }
func (m *Manager) scope(id string) error {
	if id == "" {
		return errors.New("workspaceId is required")
	}
	_, e := m.config.WorkspacePath(id)
	return e
}
func (m *Manager) List(workspaceID string) ([]Clip, error) {
	if e := m.scope(workspaceID); e != nil {
		return nil, e
	}
	return listDocuments[Clip](m.db, "clips", workspaceID, "")
}
func (m *Manager) Get(workspaceID, clipID string) (ClipDetail, error) {
	if e := m.scope(workspaceID); e != nil {
		return ClipDetail{}, e
	}
	c, e := getDocument[Clip](m.db, "clips", workspaceID, clipID)
	if e != nil {
		return ClipDetail{}, e
	}
	jobs, e := listDocuments[Job](m.db, "jobs", workspaceID, clipID)
	if e != nil {
		return ClipDetail{}, e
	}
	answers, e := listDocuments[Answer](m.db, "answers", workspaceID, clipID)
	if len(answers) > 10 {
		answers = answers[:10]
	}
	return ClipDetail{Clip: c, Jobs: jobs, RecentAnswers: answers}, e
}
func (m *Manager) Job(workspaceID, id string) (Job, error) {
	if e := m.scope(workspaceID); e != nil {
		return Job{}, e
	}
	return getDocument[Job](m.db, "jobs", workspaceID, id)
}
func (m *Manager) GetAnswer(workspaceID, id string) (Answer, error) {
	if e := m.scope(workspaceID); e != nil {
		return Answer{}, e
	}
	return getDocument[Answer](m.db, "answers", workspaceID, id)
}
func (m *Manager) Cancel(workspaceID, id string) error {
	if e := m.scope(workspaceID); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.active.ID != id || m.active.WorkspaceID != workspaceID {
		return errors.New("video job is no longer active in this workspace")
	}
	m.stop()
	return nil
}
func (m *Manager) launch(j Job, prepare func(*sql.Tx) error, work func(context.Context, *Job, *inference.Client) error) (StartResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return StartResult{}, errors.New("video service is closed")
	}
	if m.active != nil {
		return StartResult{}, errors.New("wait for the active video job or cancel it first")
	}
	// Admission is one durable unit: a failed queued-job insert must not
	// strand a previously ready clip or leave an answer with no worker.
	tx, err := m.db.BeginTx(m.ctx, nil)
	if err != nil {
		return StartResult{}, err
	}
	defer tx.Rollback()
	if err = prepare(tx); err != nil {
		return StartResult{}, err
	}
	if err = putDocument(tx, "jobs", j.ID, j.WorkspaceID, j.ClipID, j); err != nil {
		return StartResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return StartResult{}, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	m.active = &j
	m.stop = cancel
	var client *inference.Client
	if m.config.Client != nil {
		client = m.config.Client()
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		j.Status = "running"
		e := m.saveJob(j)
		if e == nil {
			e = work(ctx, &j, client)
		}
		j.FinishedAt = store.Now()
		j.Status = "completed"
		if e != nil {
			j.Error = e.Error()
			j.Status = "failed"
			if ctx.Err() != nil {
				j.Status = "cancelled"
			}
			if j.Kind != "answer" {
				if clip, loadErr := getDocument[Clip](m.db, "clips", j.WorkspaceID, j.ClipID); loadErr == nil {
					clip.Status = "failed"
					if clip.IndexID != "" {
						clip.Status = "ready"
					}
					clip.Error = j.Error
					_ = m.saveClip(clip)
				}
			}
			if j.AnswerID != "" {
				if a, loadErr := getDocument[Answer](m.db, "answers", j.WorkspaceID, j.AnswerID); loadErr == nil {
					a.Status = j.Status
					a.Error = j.Error
					_ = m.saveAnswer(a)
				}
			}
		}
		_ = m.saveJob(j)
		m.emit(j, "video_job_finished", j.Error)
		m.mu.Lock()
		m.active = nil
		m.stop = nil
		m.mu.Unlock()
	}()
	return StartResult{ClipID: j.ClipID, JobID: j.ID, AnswerID: j.AnswerID}, nil
}
func (m *Manager) emit(j Job, kind, detail string) {
	if m.config.Emit != nil {
		m.config.Emit(Event{JobID: j.ID, ClipID: j.ClipID, WorkspaceID: j.WorkspaceID, Type: kind, Detail: detail})
	}
}
func (m *Manager) progress(j *Job, stage string, completed, total int) error {
	j.Stage = stage
	j.Completed = completed
	j.Total = total
	if e := m.saveJob(*j); e != nil {
		return e
	}
	m.emit(*j, "video_progress", fmt.Sprintf("%s %d/%d", stage, completed, total))
	return nil
}
func newJob(workspaceID, clipID, kind string) Job {
	return Job{ID: store.ID(), WorkspaceID: workspaceID, ClipID: clipID, Kind: kind, Status: "queued", Stage: kind, CreatedAt: store.Now()}
}
func (m *Manager) Import(workspaceID, path string) (StartResult, error) {
	root, e := m.config.WorkspacePath(workspaceID)
	if e != nil {
		return StartResult{}, e
	}
	clean, e := workspace.Clean(path, false)
	if e != nil {
		return StartResult{}, e
	}
	if strings.ToLower(filepath.Ext(clean)) != ".mp4" {
		return StartResult{}, errors.New("select a workspace MP4 file")
	}
	clip := Clip{ID: store.ID(), WorkspaceID: workspaceID, Name: filepath.Base(clean), Status: "importing", CreatedAt: store.Now()}
	job := newJob(workspaceID, clip.ID, "import")
	return m.launch(job, func(tx *sql.Tx) error { return putDocument(tx, "clips", clip.ID, clip.WorkspaceID, "", clip) }, func(ctx context.Context, j *Job, _ *inference.Client) error {
		if e := m.progress(j, "copying", 0, 1); e != nil {
			return e
		}
		dir := filepath.Join(m.root, "assets", clip.ID)
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		temp := filepath.Join(dir, "source.partial")
		f, e := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer os.Remove(temp)
		w, e := workspace.Open(root)
		if e != nil {
			f.Close()
			return e
		}
		source, e := w.CopyMedia(ctx, clean, f, MaxSourceBytes)
		w.Close()
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		input := filepath.Join(dir, "source.mp4")
		if e = os.Rename(temp, input); e != nil {
			return e
		}
		if e = os.Chmod(input, 0400); e != nil {
			return e
		}
		clip.SHA256 = source.SHA256
		clip.Bytes = source.Bytes
		if e = m.saveClip(clip); e != nil {
			return e
		}
		if e = m.progress(j, "probing", 0, 1); e != nil {
			return e
		}
		scratch, e := os.MkdirTemp(m.root, "probe-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(scratch)
		metadata, e := probe(ctx, input, scratch)
		if e != nil {
			return e
		}
		body, e := json.Marshal(metadata)
		if e != nil {
			return e
		}
		if _, e = m.db.Exec(`INSERT INTO manifests(clip_id,body) VALUES(?,?)`, clip.ID, string(body)); e != nil {
			return e
		}
		clip.DurationSeconds = metadata.Duration
		clip.FrameCount = len(metadata.Samples)
		clip.Status = "imported"
		if e = m.saveClip(clip); e != nil {
			return e
		}
		return m.progress(j, "imported", 1, 1)
	})
}
