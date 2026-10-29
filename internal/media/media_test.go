package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureClip(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("qualified media sandbox requires macOS")
	}
	if _, e := os.Stat("/opt/homebrew/bin/ffmpeg"); e != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(dir, "fixture.mp4")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/opt/homebrew/bin/ffmpeg", "-hide_banner", "-v", "error", "-nostdin", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10:duration=3", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-an", "-y", path)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture encode: %v %s", e, out)
	}
	return path
}
func waitJob(t *testing.T, m *Manager, start StartResult) Job {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		j, e := m.Job("workspace", start.JobID)
		if e != nil {
			t.Fatal(e)
		}
		if j.Status == "completed" || j.Status == "failed" || j.Status == "cancelled" {
			for m.Active() {
				time.Sleep(time.Millisecond)
			}
			return j
		}
		select {
		case <-deadline:
			t.Fatalf("job did not finish: %+v", j)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func fixtureClient(t *testing.T, embed func(http.ResponseWriter, *http.Request), digest *atomic.Bool) *inference.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprint(w, `{"version":"0.40.11"}`)
		case "/api/show":
			fmt.Fprint(w, `{"capabilities":["embedding","vision","completion"]}`)
		case "/api/tags":
			d := strings.Repeat("a", 64)
			if digest != nil && digest.Load() {
				d = strings.Repeat("b", 64)
			}
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"name": EmbeddingModel, "digest": d}, {"name": "vision:latest", "digest": d}}})
		case "/api/embed":
			if embed != nil {
				embed(w, r)
				return
			}
			var q struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&q)
			vector := make([]float32, 768)
			vector[0] = 1
			json.NewEncoder(w).Encode(map[string]any{"model": q.Model, "embeddings": [][]float32{vector}, "prompt_eval_count": 1})
		default:
			http.Error(w, "unexpected", 404)
		}
	}))
	t.Cleanup(srv.Close)
	client, e := inference.NewClient(srv.URL)
	if e != nil {
		t.Fatal(e)
	}
	return client
}
func testManager(t *testing.T, root, workspacePath string, client *inference.Client, answer func(context.Context, AnswerRequest) (string, error)) *Manager {
	t.Helper()
	m, e := Open(Config{DataDir: root, WorkspacePath: func(id string) (string, error) {
		if id == "workspace" || id == "other" {
			return workspacePath, nil
		}
		return "", errors.New("workspace missing")
	}, Client: func() *inference.Client { return client }, Answer: answer})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func TestRealMediaImportIndexAnswerAndScopedAssets(t *testing.T) {
	workspace := t.TempDir()
	fixtureClip(t, workspace)
	client := fixtureClient(t, nil, nil)
	var answerCalls int
	m := testManager(t, t.TempDir(), workspace, client, func(_ context.Context, r AnswerRequest) (string, error) {
		answerCalls++
		if r.Model != "vision:latest" || len(r.Images) != 2 || !strings.Contains(r.Prompt, "source time 2.000000") {
			t.Errorf("wrong bounded evidence request model=%s images=%d prompt=%s", r.Model, len(r.Images), r.Prompt)
		}
		return "The sampled frames show a test pattern [F001] [F002].", nil
	})
	start, e := m.Import("workspace", "fixture.mp4")
	if e != nil {
		t.Fatal(e)
	}
	if job := waitJob(t, m, start); job.Status != "completed" {
		t.Fatalf("import: %+v", job)
	}
	detail, e := m.Get("workspace", start.ClipID)
	if e != nil || detail.Clip.Status != "imported" || detail.Clip.FrameCount != 3 || detail.Clip.DurationSeconds != 3 {
		t.Fatalf("imported clip: %+v %v", detail.Clip, e)
	}
	if _, e = m.Get("other", start.ClipID); e == nil {
		t.Fatal("cross-workspace clip exposed")
	}
	original, e := m.OpenMedia("workspace", start.ClipID, "original", "")
	if e != nil {
		t.Fatal(e)
	}
	original.File.Close()
	index, e := m.Index("workspace", start.ClipID)
	if e != nil {
		t.Fatal(e)
	}
	if job := waitJob(t, m, index); job.Status != "completed" {
		t.Fatalf("index: %+v", job)
	}
	detail, e = m.Get("workspace", start.ClipID)
	if e != nil || detail.Clip.Status != "ready" {
		t.Fatal(detail, e)
	}
	ask, e := m.Ask("workspace", start.ClipID, "What is visible?", "vision:latest")
	if e != nil {
		t.Fatal(e)
	}
	if job := waitJob(t, m, ask); job.Status != "completed" {
		t.Fatalf("answer: %+v", job)
	}
	answer, e := m.GetAnswer("workspace", ask.AnswerID)
	if e != nil || answer.Status != "completed" || len(answer.Citations) != 2 || answerCalls != 1 || answer.ModelDigest == "" || answer.RuntimeVersion != "0.40.11" || answer.IndexID != detail.Clip.IndexID {
		t.Fatalf("answer receipt: %+v %v", answer, e)
	}
	asset, e := m.OpenMedia("workspace", start.ClipID, "frame", answer.Evidence[0].FrameID)
	if e != nil {
		t.Fatal(e)
	}
	name := asset.File.Name()
	asset.File.Close()
	if _, e = m.OpenMedia("other", start.ClipID, "frame", answer.Evidence[0].FrameID); e == nil {
		t.Fatal("cross-workspace frame exposed")
	}
	if e = os.Chmod(name, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(name, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.OpenMedia("workspace", start.ClipID, "frame", answer.Evidence[0].FrameID); e == nil {
		t.Fatal("mutated frame accepted")
	}
}
func TestMediaProbeRejectsUnsupportedTimelines(t *testing.T) {
	base := probeMetadata{Streams: []probeStream{{CodecType: "video", CodecName: "h264", Width: 320, Height: 180, PixelFormat: "yuv420p", Rate: "10/1", TimeBase: "1/1000", Start: "0"}}}
	base.Format.Duration = "1"
	base.Format.Start = "0"
	base.Format.Name = "mov,mp4,m4a,3gp,3g2,mj2"
	for _, test := range []struct {
		name   string
		change func(*probeMetadata)
	}{
		{"long", func(p *probeMetadata) { p.Format.Duration = "91" }}, {"rotation", func(p *probeMetadata) { p.Streams[0].Tags = map[string]string{"rotate": "90"} }}, {"HDR", func(p *probeMetadata) { p.Streams[0].Transfer = "smpte2084" }}, {"nonzero", func(p *probeMetadata) { p.Streams[0].Start = "0.1" }}, {"multiple", func(p *probeMetadata) { p.Streams = append(p.Streams, p.Streams[0]) }}, {"dimensions", func(p *probeMetadata) { p.Streams[0].Width = 3840 }}} {
		t.Run(test.name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var p probeMetadata
			json.Unmarshal(data, &p)
			test.change(&p)
			if _, _, e := validateMetadata(p); e == nil {
				t.Fatal("unsupported metadata accepted")
			}
		})
	}
	var frames probeFrames
	json.Unmarshal([]byte(`{"frames":[{"best_effort_timestamp":0},{"best_effort_timestamp":100},{"best_effort_timestamp":250}]}`), &frames)
	if _, e := validateFrames(base.Streams[0], 0.3, frames); e == nil {
		t.Fatal("VFR frame timestamps accepted")
	}
}
func TestMediaCitationsAndTemporalRetrieval(t *testing.T) {
	evidence := []Evidence{{FrameID: "actual", TimeSeconds: 2, Label: "F001"}}
	for _, text := range []string{"invented [F999]", "no citation", "view https://example.com [F001]"} {
		if _, e := validateCitations(text, evidence); e == nil {
			t.Fatalf("invalid answer accepted: %s", text)
		}
	}
	got, e := validateCitations("Observed [F001], again [F001]", evidence)
	if e != nil || len(got) != 1 || got[0].TimeSeconds != 2 {
		t.Fatal(got, e)
	}
	v := make([]float32, 768)
	v[0] = 1
	frames := []frame{{ID: "0", Seconds: 0, Vector: v}, {ID: "1", Seconds: 1, Vector: v}, {ID: "2", Seconds: 2, Vector: v}, {ID: "4", Seconds: 4, Vector: v}}
	selected, e := retrieve(frames, v)
	if e != nil || len(selected) != 3 || selected[0].frame.ID != "0" || selected[1].frame.ID != "2" || selected[2].frame.ID != "4" {
		t.Fatal(selected, e)
	}
}
func TestMediaRecoveryNeverReplaysWork(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	m := testManager(t, root, workspace, nil, nil)
	clip := Clip{ID: store.ID(), WorkspaceID: "workspace", Status: "indexing", CreatedAt: store.Now()}
	j := newJob("workspace", clip.ID, "index")
	j.Status = "running"
	if e := m.saveClip(clip); e != nil {
		t.Fatal(e)
	}
	if e := m.saveJob(j); e != nil {
		t.Fatal(e)
	}
	m.Close()
	reopened := testManager(t, root, workspace, nil, nil)
	job, e := reopened.Job("workspace", j.ID)
	if e != nil || job.Status != "interrupted" || reopened.Active() {
		t.Fatalf("restart replayed or lost interrupted job: %+v %v", job, e)
	}
}

func TestIndexCancellationNeverPublishesPartialFrames(t *testing.T) {
	workspace := t.TempDir()
	fixtureClip(t, workspace)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}, nil)
	m := testManager(t, t.TempDir(), workspace, client, nil)
	start, e := m.Import("workspace", "fixture.mp4")
	if e != nil {
		t.Fatal(e)
	}
	if j := waitJob(t, m, start); j.Status != "completed" {
		t.Fatal(j)
	}
	index, e := m.Index("workspace", start.ClipID)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("embedding fixture did not start")
	}
	if e = m.Cancel("other", index.JobID); e == nil {
		t.Fatal("cross-workspace cancellation succeeded")
	}
	if e = m.Cancel("workspace", index.JobID); e != nil {
		t.Fatal(e)
	}
	close(release)
	job := waitJob(t, m, index)
	if job.Status != "cancelled" {
		t.Fatal(job)
	}
	detail, e := m.Get("workspace", start.ClipID)
	if e != nil || detail.Clip.IndexID != "" || detail.Clip.Status == "ready" {
		t.Fatalf("partial index became searchable: %+v %v", detail.Clip, e)
	}
	var count int
	if e = m.db.QueryRow(`SELECT count(*) FROM frames`).Scan(&count); e != nil || count != 0 {
		t.Fatalf("cancelled frames left published: %d %v", count, e)
	}
}
func TestAnswerModelDriftFailsWithoutValidatedCitations(t *testing.T) {
	workspace := t.TempDir()
	fixtureClip(t, workspace)
	var drift atomic.Bool
	client := fixtureClient(t, nil, &drift)
	m := testManager(t, t.TempDir(), workspace, client, func(context.Context, AnswerRequest) (string, error) { drift.Store(true); return "Claim [F001]", nil })
	imported, e := m.Import("workspace", "fixture.mp4")
	if e != nil {
		t.Fatal(e)
	}
	if j := waitJob(t, m, imported); j.Status != "completed" {
		t.Fatal(j)
	}
	indexed, e := m.Index("workspace", imported.ClipID)
	if e != nil {
		t.Fatal(e)
	}
	if j := waitJob(t, m, indexed); j.Status != "completed" {
		t.Fatal(j)
	}
	asked, e := m.Ask("workspace", imported.ClipID, "What is visible?", "vision:latest")
	if e != nil {
		t.Fatal(e)
	}
	if j := waitJob(t, m, asked); j.Status != "failed" || !strings.Contains(j.Error, "changed during generation") {
		t.Fatal(j)
	}
	answer, e := m.GetAnswer("workspace", asked.AnswerID)
	if e != nil || answer.Status != "failed" || len(answer.Citations) != 0 {
		t.Fatal(answer, e)
	}
}

func TestVideoWithAudioTrackImportsAsVisualOnly(t *testing.T) {
	workspace := t.TempDir()
	original := fixtureClip(t, workspace)
	output := filepath.Join(workspace, "audio.mp4")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/opt/homebrew/bin/ffmpeg", "-hide_banner", "-v", "error", "-nostdin", "-i", original, "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "copy", "-c:a", "aac", "-shortest", "-y", output)
	if data, e := command.CombinedOutput(); e != nil {
		t.Fatalf("audio fixture: %v %s", e, data)
	}
	m := testManager(t, t.TempDir(), workspace, nil, nil)
	start, e := m.Import("workspace", "audio.mp4")
	if e != nil {
		t.Fatal(e)
	}
	job := waitJob(t, m, start)
	if job.Status != "completed" {
		t.Fatalf("visual import of audio-bearing clip: %+v", job)
	}
	detail, e := m.Get("workspace", start.ClipID)
	if e != nil || detail.Clip.Status != "imported" || detail.Clip.FrameCount != 3 {
		t.Fatal(detail, e)
	}
}

func TestAdmissionRollbackPreservesReadyClipAndLeavesNoOrphans(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	m := testManager(t, root, workspace, nil, func(context.Context, AnswerRequest) (string, error) {
		t.Error("worker started after admission failure")
		return "", nil
	})
	clip := Clip{ID: store.ID(), WorkspaceID: "workspace", Status: "ready", SHA256: strings.Repeat("a", 64), DurationSeconds: 3, IndexID: "previous-index", CreatedAt: store.Now()}
	if e := m.saveClip(clip); e != nil {
		t.Fatal(e)
	}
	if _, e := m.db.Exec(`INSERT INTO manifests(clip_id,body) VALUES(?,?)`, clip.ID, `{"duration":3,"samples":[]}`); e != nil {
		t.Fatal(e)
	}
	if _, e := m.db.Exec(`CREATE TRIGGER reject_jobs BEFORE INSERT ON jobs BEGIN SELECT RAISE(ABORT,'induced job insert failure'); END`); e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		name  string
		start func() (StartResult, error)
	}{{"index", func() (StartResult, error) { return m.Index("workspace", clip.ID) }}, {"answer", func() (StartResult, error) { return m.Ask("workspace", clip.ID, "What is visible?", "vision:latest") }}, {"import", func() (StartResult, error) { return m.Import("workspace", "new.mp4") }}} {
		t.Run(test.name, func(t *testing.T) {
			if _, e := test.start(); e == nil || !strings.Contains(e.Error(), "induced job insert failure") {
				t.Fatalf("admission did not surface transaction failure: %v", e)
			}
			if m.Active() {
				t.Fatal("failed admission retained active worker")
			}
			saved, e := getDocument[Clip](m.db, "clips", "workspace", clip.ID)
			if e != nil || saved.Status != "ready" || saved.IndexID != "previous-index" {
				t.Fatalf("ready clip changed across admission rollback: %+v %v", saved, e)
			}
			for table, want := range map[string]int{"clips": 1, "answers": 0, "jobs": 0} {
				var count int
				if e := m.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); e != nil || count != want {
					t.Fatalf("orphaned %s count=%d want=%d err=%v", table, count, want, e)
				}
			}
		})
	}
}
