package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

// Only the named vendor-global state is denied. This is state isolation, not
// a general malicious-code sandbox. The signed binary and manifest are trusted.
const mlxServeSandboxTemplate = `(version 1)
(allow default)
(deny file-read* file-write* (subpath {{GLOBAL_STATE}}))
`

func SandboxProfileDigest() string {
	sum := sha256.Sum256([]byte(mlxServeSandboxTemplate))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) launch(ctx context.Context, manifest Manifest, model ModelManifest, request StartRequest, apiKey string, port int) (*exec.Cmd, error) {
	stateDir := filepath.Join(m.root, "state", manifest.ID)
	cacheDir := filepath.Join(m.root, "cache", manifest.ID)
	for _, directory := range []string{stateDir, cacheDir, filepath.Join(stateDir, "tmp")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}
	executable := filepath.Join(m.root, "runtimes", manifest.ID, filepath.FromSlash(manifest.Executable))
	modelRoot := filepath.Join(m.root, "models", model.ID)
	modelPath := filepath.Join(modelRoot, model.ServeName)
	var args []string
	if manifest.Provider == inference.ProviderOMLX {
		settings := map[string]any{"server": map[string]any{"host": "127.0.0.1", "port": port}, "sampling": map[string]int{"max_context_window": request.ContextTokens, "max_context_window_policy": request.ContextTokens}, "huggingface": map[string]bool{"hf_cache_enabled": false}}
		if err := writeAtomic(filepath.Join(stateDir, "settings.json"), settings); err != nil {
			return nil, err
		}
		modelSettings := map[string]any{"version": 2, "models": map[string]any{model.ServeName: map[string]any{"max_context_window": request.ContextTokens, "max_tool_result_tokens": nil}}}
		if err := writeAtomic(filepath.Join(stateDir, "model_settings.json"), modelSettings); err != nil {
			return nil, err
		}
		args = []string{"-I", "-B", "-m", "omlx.cli", "serve", "--base-path", stateDir, "--model-dir", modelRoot, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--headless", "--no-hf-cache", "--max-concurrent-requests", "1", "--memory-guard", "safe", "--paged-ssd-cache-dir", cacheDir, "--paged-ssd-cache-max-size", "10GB", "--hot-cache-max-size", "0", "--sse-keepalive-mode", "comment", "--log-level", "warning"}
	} else {
		args = []string{"--model", modelPath, "--serve", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--ctx-size", strconv.Itoa(request.ContextTokens), "--max-concurrent", "1", "--api-key-env", "GEMMA_RUNTIME_API_KEY", "--api-key-strict", "--no-plugins", "--no-drafter", "--no-mtp", "--prefix-cache-disk", "off", "--log-file", filepath.Join(stateDir, "server.log"), "--log-level", "warn"}
		if manifest.SandboxProfileSHA256 != SandboxProfileDigest() {
			return nil, errors.New("runtime isolation profile is not qualified by manifest")
		}
		if info, err := os.Stat("/usr/bin/sandbox-exec"); err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("required macOS runtime isolation tool is unavailable")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		profile := strings.ReplaceAll(mlxServeSandboxTemplate, "{{GLOBAL_STATE}}", strconv.Quote(filepath.Join(home, ".mlx-serve")))
		profilePath := filepath.Join(stateDir, "isolation.sb")
		if err = os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
			return nil, err
		}
		args = append([]string{"-f", profilePath, executable}, args...)
		executable = "/usr/bin/sandbox-exec"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = stateDir
	// Inherit only OS identity/session essentials, not arbitrary Python, proxy,
	// DYLD, MLX kernel/plugin or developer override variables. HOME is preserved.
	for _, key := range []string{"HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "__CF_USER_TEXT_ENCODING"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR="+filepath.Join(stateDir, "tmp"), "HF_HOME="+filepath.Join(cacheDir, "huggingface"), "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "PYTHONNOUSERSITE=1", "OMLX_API_KEY="+apiKey, "GEMMA_RUNTIME_API_KEY="+apiKey, "MLX_SERVE_ANE_CACHE_DIR="+filepath.Join(cacheDir, "ane"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

type startMode uint8

const (
	startManual startMode = iota
	startFrozen
	startProbe
)

func (m *Manager) Start(ctx context.Context, request StartRequest) (Connection, error) {
	return m.start(ctx, request, startManual)
}

// StartFrozen is a trusted host operation for an explicit immutable run binding.
// It retains every integrity/qualification gate but does not alter or require
// the user's active/previous default selections.
func (m *Manager) StartFrozen(ctx context.Context, request StartRequest) (Connection, error) {
	return m.start(ctx, request, startFrozen)
}

func (m *Manager) start(ctx context.Context, request StartRequest, mode startMode) (Connection, error) {
	probeOnly := mode == startProbe
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	release, err := m.config.Gate.Acquire(ctx)
	if err != nil {
		return Connection{}, err
	}
	defer release()
	m.opMu.Lock()
	m.mu.Lock()
	manifest, ok := m.catalog[request.BuildID]
	model, modelOK := m.models[request.ModelID]
	installed, installedOK := m.state.Installed[request.BuildID]
	modelInstalled, modelInstalledOK := m.state.Models[request.ModelID]
	if m.closed || m.child != nil {
		m.mu.Unlock()
		m.opMu.Unlock()
		return Connection{}, errors.New("runtime manager is closed or another owned runtime is active")
	}
	if !ok || !modelOK || !installedOK || !modelInstalledOK || installed.ManifestDigest != digestJSON(manifest) || modelInstalled.ManifestDigest != digestJSON(model) {
		m.mu.Unlock()
		m.opMu.Unlock()
		return Connection{}, errors.New("selected runtime and model must have verified installation receipts")
	}
	if mode == startManual && m.state.Selections[manifest.Provider].Active != manifest.ID {
		m.mu.Unlock()
		m.opMu.Unlock()
		return Connection{}, errors.New("activate this runtime build before starting it")
	}
	if !probeOnly && !m.qualifiedLocked(request, manifest, model) {
		m.mu.Unlock()
		m.opMu.Unlock()
		return Connection{}, errors.New("check this exact runtime, model and context before starting an ordinary run")
	}
	if request.ContextTokens < 1024 || request.ContextTokens > model.Model.ContextTokens {
		m.mu.Unlock()
		m.opMu.Unlock()
		return Connection{}, errors.New("requested context exceeds the verified model limit")
	}
	m.mu.Unlock()
	fail := func(err error) (Connection, error) { m.opMu.Unlock(); return Connection{}, err }
	if err := platformPreflight(manifest); err != nil {
		return fail(err)
	}
	if err := m.preflightDisk(0); err != nil {
		return fail(err)
	}
	if err := verifyInventory(ctx, filepath.Join(m.root, "runtimes", manifest.ID), manifest.Files); err != nil {
		return fail(err)
	}
	modelFiles := make([]File, len(model.Files))
	for i, file := range model.Files {
		modelFiles[i] = file.File
	}
	if err := verifyInventory(ctx, filepath.Join(m.root, "models", model.ID, model.ServeName), modelFiles); err != nil {
		return fail(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fail(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	apiKey := randomID() + randomID()
	cmd, err := m.launch(ctx, manifest, model, request, apiKey, port)
	if err != nil {
		listener.Close()
		return fail(err)
	}
	logPath := filepath.Join(m.root, "state", manifest.ID, "process.log")
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		listener.Close()
		return fail(err)
	}
	writer := &boundedLog{file: log, remaining: 4 << 20}
	cmd.Stdout = writer
	cmd.Stderr = writer
	// Release our reservation immediately before spawn. A stolen port cannot
	// pass readiness: we also require the generated bearer key to be enforced.
	listener.Close()
	if err = cmd.Start(); err != nil {
		log.Close()
		return fail(err)
	}
	done := make(chan struct{})
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	m.mu.Lock()
	completionModel := m.completionModelLocked(request, model, !probeOnly)
	m.mu.Unlock()
	connection := Connection{BuildID: manifest.ID, ModelID: model.ID, Config: inference.NativeConfig{BaseURL: endpoint, APIKey: apiKey, Descriptor: descriptor(manifest), Models: []inference.CompletionModel{completionModel}}}
	connection.Config.Gate = m.config.Gate
	connection.Config.Quiesce = func(ctx context.Context) error { return m.stopSpecific(ctx, cmd) }
	m.mu.Lock()
	m.child = cmd
	m.childDone = done
	m.childCleanupErr = nil
	m.connection = connection
	m.process = ProcessState{BuildID: manifest.ID, ModelID: model.ID, ContextTokens: request.ContextTokens, Status: "starting", PID: cmd.Process.Pid, Endpoint: endpoint}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		err := cmd.Wait()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		cleanupErr := reapOwnedProcessGroup(cleanupCtx, cmd.Process.Pid)
		cleanupCancel()
		if cleanupErr != nil {
			m.config.Gate.Block(cleanupErr)
			err = errors.Join(err, cleanupErr)
		}
		log.Close()
		m.mu.Lock()
		if m.child == cmd {
			m.childCleanupErr = cleanupErr
			m.child = nil
			m.connection = Connection{}
			m.process.PID = 0
			m.process.Endpoint = ""
			if m.process.Status == "stopping" && cleanupErr == nil {
				m.process.Status = "stopped"
			} else {
				m.process.Status = "failed"
				m.process.Error = "Runtime process exited"
				if err != nil {
					m.process.Error = err.Error()
				}
			}
		}
		close(done)
		m.mu.Unlock()
		m.changed()
	}()
	m.opMu.Unlock()
	m.changed()
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = waitReady(readyCtx, done, endpoint, apiKey, model.ServeName)
	if err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer stopCancel()
		if cleanupErr := m.stopSpecific(stopCtx, cmd); cleanupErr != nil {
			m.config.Gate.Block(cleanupErr)
			err = errors.Join(err, cleanupErr)
		}
		m.mu.Lock()
		if m.process.BuildID == manifest.ID {
			m.process.Status = "failed"
			m.process.Error = "Runtime readiness failed: " + err.Error()
		}
		m.mu.Unlock()
		m.changed()
		return Connection{}, err
	}
	m.mu.Lock()
	if m.child != cmd || m.process.Status != "starting" {
		m.mu.Unlock()
		return Connection{}, errors.New("runtime stopped during readiness")
	}
	m.process.Status = "running"
	if probeOnly {
		m.process.Status = "checking"
	}
	m.mu.Unlock()
	m.changed()
	return connection, nil
}

// Wait() reaps only the leader. A worker can ignore SIGTERM or outlive a
// crashing parent, so confirm the exact owned process group is gone as well.
func reapOwnedProcessGroup(ctx context.Context, group int) error {
	if err := syscall.Kill(-group, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Kill(-group, 0)
		if err == syscall.ESRCH {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("owned worker process group did not exit: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type boundedLog struct {
	file      *os.File
	remaining int64
}

func (w *boundedLog) Write(p []byte) (int, error) {
	n := len(p)
	if w.remaining <= 0 {
		return n, nil
	}
	part := p
	if int64(len(part)) > w.remaining {
		part = part[:w.remaining]
	}
	written, err := w.file.Write(part)
	w.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return n, nil
}

func waitReady(ctx context.Context, done <-chan struct{}, endpoint, key, model string) error {
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("runtime readiness redirects are disabled")
	}}
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("runtime exited before readiness")
		case <-ticker.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		var models struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &models) != nil {
			continue
		}
		found := false
		for _, entry := range models.Data {
			if entry.ID == model {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		wrong, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/models", nil)
		wrong.Header.Set("Authorization", "Bearer incorrect-readiness-key")
		unauthorized, err := client.Do(wrong)
		if err != nil {
			continue
		}
		unauthorized.Body.Close()
		if unauthorized.StatusCode != http.StatusUnauthorized && unauthorized.StatusCode != http.StatusForbidden {
			return errors.New("runtime did not enforce its private API key")
		}
		return nil
	}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.probeCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return m.stopSpecific(ctx, nil)
}
func (m *Manager) stopSpecific(ctx context.Context, expected *exec.Cmd) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	cmd, done := m.child, m.childDone
	if cmd == nil || expected != nil && cmd != expected {
		cleanupErr := m.childCleanupErr
		m.mu.Unlock()
		return cleanupErr
	}
	m.process.Status = "stopping"
	// Only this live exec.Cmd's process group is signalled. No persisted PID,
	// port owner, external Ollama daemon or unrelated process is ever killed.
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	m.mu.Unlock()
	m.changed()
	if err != nil && err != syscall.ESRCH {
		return err
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return m.cleanupError()
	case <-timer.C:
	case <-ctx.Done():
	}
	m.mu.Lock()
	if m.child == cmd {
		err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	m.mu.Unlock()
	if err != nil && err != syscall.ESRCH {
		return err
	}
	select {
	case <-done:
		return m.cleanupError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Connection() (Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.child == nil || m.process.Status != "running" {
		return Connection{}, errors.New("managed runtime is not running")
	}
	copy := m.connection
	copy.Config.Models = append([]inference.CompletionModel(nil), m.connection.Config.Models...)
	for i := range copy.Config.Models {
		copy.Config.Models[i] = copy.Config.Models[i].Clone()
	}
	return copy, nil
}

func (m *Manager) cleanupError() error { m.mu.Lock(); defer m.mu.Unlock(); return m.childCleanupErr }
