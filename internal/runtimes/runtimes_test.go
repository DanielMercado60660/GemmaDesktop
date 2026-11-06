package runtimes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

func sum(data []byte) string { digest := sha256.Sum256(data); return hex.EncodeToString(digest[:]) }

func bundle(t *testing.T, entries []*tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	archive := tar.NewWriter(gz)
	for i, entry := range entries {
		if err := archive.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if len(bodies) > i {
			if _, err := archive.Write(bodies[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func fixtures(t *testing.T) (Config, Manifest, ModelManifest) {
	t.Helper()
	resources := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/bin/sh\nexport GEMMA_RUNTIME_TEST_HELPER=1\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestOwnedRuntimeProcessHelper -- \"$@\"\n")
	files := []File{{Path: "bin/python", SHA256: sum(script), Size: int64(len(script)), Executable: true}}
	archive := bundle(t, []*tar.Header{{Name: "bin/python", Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(script))}}, [][]byte{script})
	if err = os.WriteFile(filepath.Join(resources, "fixture.tar.gz"), archive, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1, ID: "fixture-one", Provider: inference.ProviderOMLX, SourceSHA: strings.Repeat("a", 40), Architecture: "arm64", MinimumOS: "14.0", Bundle: Source{BundledPath: "fixture.tar.gz", SHA256: sum(archive), Size: int64(len(archive))}, Files: files, Executable: "bin/python", License: "test fixture only", ReadyForInference: true, Qualification: Qualification{Transport: "qualified", EvidenceSHA256: strings.Repeat("b", 64), Coding: "experimental"}}
	configData := []byte(`{"model_type":"fixture"}`)
	if err = os.WriteFile(filepath.Join(resources, "config.json"), configData, 0600); err != nil {
		t.Fatal(err)
	}
	model := ModelManifest{Version: 1, ID: "fixture-model", Repository: "test/fixture", Revision: strings.Repeat("c", 40), ServeName: "fixture", License: "test fixture only", Providers: []inference.ProviderKind{inference.ProviderOMLX}, Model: inference.CompletionModel{Name: "fixture", ContextTokens: 4096, Capabilities: inference.CompletionCapabilities{Completion: true, Tools: true, Thinking: true}}, Files: []ModelFile{{File: File{Path: "config.json", SHA256: sum(configData), Size: int64(len(configData))}, Source: Source{BundledPath: "config.json", SHA256: sum(configData), Size: int64(len(configData))}}}}
	model.Model.ArtifactDigest = ModelArtifactDigest(model.Files)
	return Config{DataDir: t.TempDir(), BundlesDir: resources, Catalog: []Manifest{manifest}, ModelCatalog: []ModelManifest{model}, DiskReserveBytes: 1}, manifest, model
}

func waitJob(t *testing.T, m *Manager, job Job) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, state := range m.Snapshot().Jobs {
			if state.ID == job.ID && terminalJob(state.Status) {
				return state
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("artifact job did not terminate")
	return Job{}
}

func installedFixture(t *testing.T) (*Manager, Manifest, ModelManifest) {
	t.Helper()
	config, manifest, model := fixtures(t)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	job, err := m.Install(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "complete" {
		t.Fatal(result)
	}
	job, err = m.AcquireModel(model.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "complete" {
		t.Fatal(result)
	}
	return m, manifest, model
}

func TestAtomicInstallActivateRollbackAndIndependentModels(t *testing.T) {
	config, first, model := fixtures(t)
	second := first
	second.ID = "fixture-two"
	config.Catalog = append(config.Catalog, second)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, id := range []string{first.ID, second.ID} {
		job, err := m.Install(id)
		if err != nil {
			t.Fatal(err)
		}
		if result := waitJob(t, m, job); result.Status != "complete" {
			t.Fatal(result)
		}
		if err = m.Activate(id); err != nil {
			t.Fatal(err)
		}
	}
	if selected := m.Snapshot().Selections[inference.ProviderOMLX]; selected.Active != second.ID || selected.Previous != first.ID {
		t.Fatal(selected)
	}
	if err = m.Rollback(inference.ProviderOMLX); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Selections[inference.ProviderOMLX].Active != first.ID {
		t.Fatal("rollback failed")
	}
	job, err := m.AcquireModel(model.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "complete" {
		t.Fatal(result)
	}
	if err = m.Remove(first.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().Models) != 1 || len(m.Snapshot().Installed) != 1 {
		t.Fatal("runtime removal affected model or another build")
	}
	if err = m.RemoveModel(model.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().Models) != 0 || len(m.Snapshot().Installed) != 1 {
		t.Fatal("model removal affected runtime")
	}
}

func TestArchiveExtractionRejectsEscapeAndInventoryMismatch(t *testing.T) {
	content := []byte("safe")
	files := []File{{Path: "bin/runtime", SHA256: sum(content), Size: 4}}
	for _, test := range []struct {
		name    string
		entries []*tar.Header
		bodies  [][]byte
	}{
		{"traversal", []*tar.Header{{Name: "../escaped", Typeflag: tar.TypeReg, Size: 4}}, [][]byte{content}},
		{"absolute", []*tar.Header{{Name: "/tmp/escaped", Typeflag: tar.TypeReg, Size: 4}}, [][]byte{content}},
		{"symbolic link", []*tar.Header{{Name: "bin/runtime", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}}, nil},
		{"hard link", []*tar.Header{{Name: "bin/runtime", Typeflag: tar.TypeLink, Linkname: "/tmp"}}, nil},
		{"undeclared", []*tar.Header{{Name: "other", Typeflag: tar.TypeReg, Size: 4}}, [][]byte{content}},
		{"missing", nil, nil},
		{"wrong size", []*tar.Header{{Name: "bin/runtime", Typeflag: tar.TypeReg, Size: 3}}, [][]byte{content[:3]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "bundle.tar.gz")
			if err := os.WriteFile(archive, bundle(t, test.entries, test.bodies), 0600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "stage")
			os.Mkdir(destination, 0700)
			if err := extractBundle(t.Context(), archive, destination, files); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
		})
	}
	if _, err := validateFiles(append(files, File{Path: "BIN/runtime", SHA256: sum(content), Size: 4})); err == nil {
		t.Fatal("case-insensitive inventory collision accepted")
	}
}

func TestCorruptBundleNeverPublishesInstallation(t *testing.T) {
	config, manifest, _ := fixtures(t)
	data, err := os.ReadFile(filepath.Join(config.BundlesDir, "fixture.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	os.WriteFile(filepath.Join(config.BundlesDir, "fixture.tar.gz"), data, 0600)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	job, err := m.Install(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "failed" {
		t.Fatal(result)
	}
	if len(m.Snapshot().Installed) != 0 {
		t.Fatal("corrupt artifact published installation")
	}
	if err = m.Activate(manifest.ID); err == nil {
		t.Fatal("corrupt artifact activated")
	}
}

func TestInterruptedJobsRecoverWithoutAutoExecution(t *testing.T) {
	config, manifest, _ := fixtures(t)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.state.Jobs["interrupted"] = Job{ID: "interrupted", Kind: "runtime_install", Target: manifest.ID, Status: "extracting"}
	err = m.persistLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	snapshot := m.Snapshot()
	if len(snapshot.Jobs) != 1 || snapshot.Jobs[0].Status != "interrupted" || snapshot.Process.Status != "stopped" || len(snapshot.Installed) != 0 {
		t.Fatal(snapshot)
	}
	if _, err = New(config); err == nil {
		t.Fatal("duplicate data-directory owner accepted")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadCancellationAndResumeIntegrity(t *testing.T) {
	config, manifest, _ := fixtures(t)
	content := []byte("abcdef")
	source := Source{URL: "https://trusted.example/artifact", SHA256: sum(content), Size: 6}
	config.HTTPClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Range") != "bytes=3-" {
			t.Errorf("wrong resume request: %s", r.Header.Get("Range"))
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{"bytes 3-5/6"}}, Body: io.NopCloser(strings.NewReader("def"))}, nil
	})}
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	os.WriteFile(filepath.Join(m.root, "downloads", source.SHA256+".part"), []byte("abc"), 0600)
	got, err := m.download(t.Context(), source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyFile(t.Context(), got, 6, sum(content)); err != nil {
		t.Fatal(err)
	}
	manifest.Bundle = Source{URL: "https://trusted.example/slow", SHA256: strings.Repeat("e", 64), Size: 1024}
	m.catalog[manifest.ID] = manifest
	m.config.HTTPClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() { <-r.Context().Done(); writer.CloseWithError(r.Context().Err()) }()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}, nil
	})}
	job, err := m.Install(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CancelJob(job.ID); err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "cancelled" {
		t.Fatal(result)
	}
}

func TestOwnedRuntimeLifecycleAndNoChildLeak(t *testing.T) {
	m, manifest, model := installedFixture(t)
	if _, err := m.Start(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048}); err == nil {
		t.Fatal("start bypassed explicit activation")
	}
	if err := m.Activate(manifest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048}); err == nil {
		t.Fatal("ordinary start bypassed qualification")
	}
	receipt, err := m.Qualify(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048})
	if err != nil || receipt.Status != "qualified" || receipt.Coding != "experimental" {
		t.Fatalf("qualification: %+v %v", receipt, err)
	}
	if m.Snapshot().Process.Status != "stopped" {
		t.Fatal("qualification left its process running")
	}
	if receipt.ProtocolVersion != 2 {
		t.Fatal("current cancellation qualification version missing")
	}
	request := StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048}
	m.mu.Lock()
	oldReceipt := receipt
	oldReceipt.ProtocolVersion = 1
	m.state.Qualifications[qualificationKey(request)] = oldReceipt
	m.mu.Unlock()
	if _, err := m.Start(t.Context(), request); err == nil {
		t.Fatal("legacy receipt bypassed cancellation qualification")
	}
	m.mu.Lock()
	m.state.Qualifications[qualificationKey(request)] = receipt
	m.mu.Unlock()
	described, describedModel, err := m.DescribeQualified(request)
	if err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Process.Status != "stopped" {
		t.Fatal("metadata description launched a runtime")
	}
	if _, err := m.Start(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 4096}); err == nil {
		t.Fatal("qualification silently applied to a different context")
	}
	connection, err := m.Start(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if digestJSON(described) != digestJSON(connection.Config.Descriptor) || digestJSON(describedModel) != digestJSON(connection.Config.Models[0]) {
		t.Fatal("metadata freeze and live connection identities differ")
	}
	if m.Snapshot().Process.Status != "running" || connection.Config.APIKey == "" || connection.Config.Models[0].ContextTokens != 2048 {
		t.Fatal("owned runtime did not become ready")
	}
	if err = m.Remove(manifest.ID); err == nil {
		t.Fatal("active runtime was removed")
	}
	if err = m.RemoveModel(model.ID); err == nil {
		t.Fatal("active model was removed")
	}
	childData, err := os.ReadFile(filepath.Join(m.root, "state", manifest.ID, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	childPID, _ := strconv.Atoi(string(childData))
	if err = m.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Process.Status != "stopped" {
		t.Fatal(m.Snapshot().Process)
	}
	if err = syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child leaked after stop: %d %v", childPID, err)
	}
	if _, err = m.Connection(); err == nil {
		t.Fatal("stopped runtime retained a usable connection")
	}
}

func TestReadinessRejectsUnownedOpenServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := waitReady(ctx, make(chan struct{}), server.URL, "secret", "fixture"); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatal(err)
	}
}

func TestStopInterruptsQualificationBeforeAdmissionUnlock(t *testing.T) {
	m, manifest, model := installedFixture(t)
	if err := m.Activate(manifest.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := m.Qualify(t.Context(), StartRequest{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: 2048})
		result <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state := m.Snapshot().Process.Status; state == "checking" || state == "starting" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled check earned a receipt")
		}
	case <-ctx.Done():
		t.Fatal("stop blocked behind qualification")
	}
	if len(m.Snapshot().Qualifications) != 0 {
		t.Fatal("cancelled qualification was published")
	}
}

func TestOwnedRuntimeProcessHelper(t *testing.T) {
	if os.Getenv("GEMMA_RUNTIME_TEST_HELPER") != "1" {
		return
	}
	port := ""
	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	if port == "" {
		os.Exit(21)
	}
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		os.Exit(22)
	}
	if err := os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		os.Exit(23)
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+os.Getenv("OMLX_API_KEY") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			var request struct {
				Tools    []any `json:"tools"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if strings.HasPrefix(request.Messages[len(request.Messages)-1].Content, "Cancellation protocol probe:") {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"1\\n\"}}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			if len(request.Tools) > 0 && request.Messages[len(request.Messages)-1].Role != "tool" {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"brief test thought\",\"tool_calls\":[{\"index\":0,\"id\":\"probe-id\",\"function\":{\"name\":\"echo_probe\",\"arguments\":\"{\\\"value\\\":\\\"gemma-probe\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
			} else {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"gemma-probe-ok\"},\"finish_reason\":\"stop\"}]}\n\n")
			}
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`)
	})}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go server.ListenAndServe()
	<-signals
	server.Close()
	_ = child.Wait()
	os.Exit(0)
}

func TestCatalogAbsentMalformedAndDiskPreflight(t *testing.T) {
	dir := t.TempDir()
	runtimes, models, err := LoadCatalog(dir)
	if err != nil || len(runtimes) != 0 || len(models) != 0 {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(`{"version":9}`), 0600)
	if _, _, err = LoadCatalog(dir); err == nil {
		t.Fatal("invalid catalog accepted")
	}
	config, manifest, _ := fixtures(t)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.config.DiskReserveBytes = 1 << 60
	if _, err = m.Install(manifest.ID); err == nil {
		t.Fatal("disk reserve ignored")
	}
	manifest.MinimumOS = "999.0"
	if err = platformPreflight(manifest); err == nil {
		t.Fatal("unsupported OS accepted")
	}
}

func TestPresentationSnapshotOmitsRuntimeInventoryWithoutChangingAuthority(t *testing.T) {
	m, manifest, _ := installedFixture(t)
	full := m.Snapshot()
	view := m.PresentationSnapshot()
	if len(view.Catalog) != 1 || len(view.Catalog[0].Files) != 0 || view.Catalog[0].RuntimeFileCount != len(manifest.Files) {
		t.Fatalf("bad presentation summary: %+v", view.Catalog)
	}
	var size int64
	for _, f := range manifest.Files {
		size += f.Size
	}
	if view.Catalog[0].UnpackedBytes != size {
		t.Fatal("unpacked size lost")
	}
	view.Catalog[0].Bundle.SHA256 = "mutated"
	view.ModelCatalog[0].Files[0].SHA256 = "mutated"
	if digestJSON(m.Snapshot().Catalog) != digestJSON(full.Catalog) || digestJSON(m.Snapshot().ModelCatalog) != digestJSON(full.ModelCatalog) {
		t.Fatal("presentation modified authoritative manifest")
	}
}

func TestOwnedGroupCleanupKillsWorkerAfterLeaderExit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "worker.pid")
	// The leader exits immediately; its worker deliberately ignores TERM.
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; /bin/sleep 60 & echo $! > "$1"; exit 0`, "fixture", pidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	group := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(pid, 0); err != nil {
		t.Fatalf("worker fixture absent: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err = reapOwnedProcessGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("worker survived cleanup: %v", err)
	}
}

func TestFrozenCandidateQualificationPreservesActiveAndRollbackSelections(t *testing.T) {
	config, first, model := fixtures(t)
	candidate := first
	candidate.ID = "fixture-candidate"
	config.Catalog = append(config.Catalog, candidate)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, build := range []Manifest{first, candidate} {
		job, err := m.Install(build.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result := waitJob(t, m, job); result.Status != "complete" {
			t.Fatal(result)
		}
	}
	job, err := m.AcquireModel(model.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, m, job); result.Status != "complete" {
		t.Fatal(result)
	}
	if err = m.Activate(candidate.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(first.ID); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot().Selections
	request := StartRequest{BuildID: candidate.ID, ModelID: model.ID, ContextTokens: 2048}
	if _, err = m.StartFrozen(t.Context(), request); err == nil {
		t.Fatal("frozen start bypassed qualification")
	}
	if _, err = m.Qualify(t.Context(), request); err != nil {
		t.Fatalf("inactive candidate qualification: %v", err)
	}
	if _, _, err = m.DescribeQualified(request); err != nil {
		t.Fatalf("inactive candidate description: %v", err)
	}
	if _, err = m.Start(t.Context(), request); err == nil {
		t.Fatal("manual start bypassed explicit activation")
	}
	connection, err := m.StartFrozen(t.Context(), request)
	if err != nil {
		t.Fatalf("qualified frozen start: %v", err)
	}
	if connection.BuildID != candidate.ID {
		t.Fatal("frozen start substituted the selected default")
	}
	if err = m.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if digestJSON(before) != digestJSON(m.Snapshot().Selections) {
		t.Fatal("candidate qualification/start changed active or rollback selections")
	}
}
