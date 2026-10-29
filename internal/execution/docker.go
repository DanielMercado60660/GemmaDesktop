package execution

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

type DockerConfig struct {
	StateDir     string
	Image        string
	DockerPath   string
	PrivatePaths []string
}
type DockerStatus struct {
	Available      bool         `json:"available"`
	ImageAvailable bool         `json:"imageAvailable"`
	Version        string       `json:"version,omitempty"`
	Image          string       `json:"image"`
	ImageID        string       `json:"imageId,omitempty"`
	Error          string       `json:"error,omitempty"`
	CPUs           int          `json:"cpus"`
	MemoryBytes    int64        `json:"memoryBytes"`
	PIDLimit       int          `json:"pidLimit"`
	Network        string       `json:"network"`
	Storage        StorageUsage `json:"storage"`
}
type DockerExecutor struct {
	config DockerConfig
	host   string
	owner  string
}
type dockerLease struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Container string    `json:"container"`
	Image     string    `json:"image"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

var immutableImage = regexp.MustCompile(`^(sha256:[a-f0-9]{64}|[a-zA-Z0-9._:/-]+@sha256:[a-f0-9]{64})$`)
var dockerSlots sync.Map

func NewDockerExecutor(c DockerConfig) (*DockerExecutor, error) {
	if !immutableImage.MatchString(c.Image) {
		return nil, errors.New("Docker execution requires an immutable local image digest (repository@sha256:… or sha256:…), never a tag")
	}
	if !filepath.IsAbs(c.StateDir) {
		return nil, errors.New("Docker state directory must be absolute")
	}
	if strings.ContainsAny(c.StateDir, ",\n\r") {
		return nil, errors.New("Docker state path cannot contain CSV separators or newlines")
	}
	if c.DockerPath == "" {
		c.DockerPath = "/usr/local/bin/docker"
	}
	if !filepath.IsAbs(c.DockerPath) {
		return nil, errors.New("Docker CLI must be an absolute executable")
	}
	private := append([]string{}, workspace.DefaultPrivatePaths...)
	for _, p := range c.PrivatePaths {
		found := false
		for _, existing := range private {
			if existing == p {
				found = true
				break
			}
		}
		if !found {
			private = append(private, p)
		}
	}
	c.PrivatePaths = private
	if e := workspace.ValidatePrivatePaths(c.PrivatePaths); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(c.StateDir, 0700); e != nil {
		return nil, e
	}
	resolved, e := filepath.EvalSymlinks(c.StateDir)
	if e != nil {
		return nil, e
	}
	c.StateDir = resolved
	if e = os.Chmod(c.StateDir, 0700); e != nil {
		return nil, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	return &DockerExecutor{config: c, host: "unix://" + filepath.Join(home, ".docker", "run", "docker.sock"), owner: workspace.Hash([]byte(c.StateDir))[:24]}, nil
}
func (*DockerExecutor) Description() string {
	return "Docker Linux process tree; filtered command snapshot; automatic guarded source writeback; 4 CPUs, 8 GiB, 256 PIDs; network none; no image pull or host fallback"
}

func (d *DockerExecutor) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, d.config.DockerPath, append([]string{"--host", d.host}, args...)...)
	cmd.Env = []string{"PATH=" + SafePATH, "HOME=" + d.config.StateDir, "DOCKER_CONFIG=" + filepath.Join(d.config.StateDir, "cli-config"), "LANG=C.UTF-8"}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}
func (d *DockerExecutor) cli(ctx context.Context, limit int, args ...string) (string, error) {
	cmd := d.command(ctx, args...)
	out := &BoundedBuffer{Limit: limit}
	errout := &BoundedBuffer{Limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = errout
	e := cmd.Run()
	s, trunc := out.Snapshot()
	es, _ := errout.Snapshot()
	if trunc {
		return s, errors.New("Docker control response exceeded limit")
	}
	if e != nil {
		return s, fmt.Errorf("Docker %s: %w: %s", args[0], e, es)
	}
	return s, nil
}
func (d *DockerExecutor) Status(ctx context.Context) (DockerStatus, error) {
	s := DockerStatus{Image: d.config.Image, CPUs: 4, MemoryBytes: 8 << 30, PIDLimit: 256, Network: "none"}
	storageCtx, storageCancel := context.WithTimeout(ctx, 750*time.Millisecond)
	s.Storage, _ = measureExecutionStorage(storageCtx, []string{d.config.StateDir})
	storageCancel()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v, e := d.cli(ctx, 4096, "version", "--format", "{{.Server.Version}}")
	if e != nil {
		s.Error = e.Error()
		return s, e
	}
	s.Available = true
	s.Version = strings.TrimSpace(v)
	v, e = d.cli(ctx, 8192, "image", "inspect", d.config.Image, "--format", "{\"Id\":{{json .Id}},\"Os\":{{json .Os}},\"Architecture\":{{json .Architecture}}}")
	if e != nil {
		s.Error = "Pinned image is not installed; provision it explicitly. " + e.Error()
		return s, errors.New(s.Error)
	}
	var im struct {
		ID           string `json:"Id"`
		OS           string `json:"Os"`
		Architecture string `json:"Architecture"`
	}
	if e = json.Unmarshal([]byte(v), &im); e != nil {
		return s, e
	}
	if im.OS != "linux" || im.Architecture != "arm64" {
		return s, errors.New("executor image must be linux/arm64")
	}
	s.ImageAvailable = true
	s.ImageID = im.ID
	return s, nil
}

// RecoveryStatus inspects local ownership journals without touching Docker or
// changing recovery state. A lease held by this process is active work, not an
// abandoned command. Admission still rechecks the journals under that lease.
func (d *DockerExecutor) RecoveryStatus() (string, error) {
	active := func() bool {
		v, ok := dockerSlots.Load(d.config.StateDir)
		return ok && len(v.(chan struct{})) > 0
	}
	if active() {
		return "active", nil
	}
	err := d.pendingRecovery()
	if active() {
		return "active", nil
	}
	if err != nil {
		return "blocked", err
	}
	return "ready", nil
}

func (d *DockerExecutor) Run(ctx context.Context, req Request) (result Result, err error) {
	start := time.Now()
	result = Result{ExitCode: -1, Environment: "docker-linux", ReadOnly: req.ReadOnly, Image: d.config.Image, Publication: "not_applied"}
	defer func() {
		result.DurationMS = time.Since(start).Milliseconds()
		if ctx.Err() != nil {
			result.Cancelled = true
		}
	}()
	if req.AllowGitMetadata {
		return result, errors.New("Docker command snapshots do not expose host Git metadata; use the governed Git inspection tools")
	}
	w, e := workspace.Open(req.Workspace)
	if e != nil {
		return result, e
	}
	defer w.Close()
	req.Workspace = w.Path
	rel, e := filepath.Rel(req.Workspace, d.config.StateDir)
	if e != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return result, errors.New("execution state must be outside the selected workspace")
	}
	ctx, release, e := workspace.AcquireOperation(ctx, req.Workspace)
	if e != nil {
		return result, e
	}
	defer release()
	ch := make(chan struct{}, 1)
	v, _ := dockerSlots.LoadOrStore(d.config.StateDir, ch)
	slot := v.(chan struct{})
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	if e = d.pendingRecovery(); e != nil {
		return result, e
	}
	if req.Timeout <= 0 {
		req.Timeout = 2 * time.Minute
	}
	if req.Timeout > 10*time.Minute {
		req.Timeout = 10 * time.Minute
	}
	if req.MaxOutput <= 0 || req.MaxOutput > 1<<20 {
		req.MaxOutput = DefaultOutputLimit
	}
	if req.Executable == "" || len(req.Executable) > 4096 || strings.ContainsRune(req.Executable, 0) || strings.HasPrefix(req.Executable, "-") {
		return result, errors.New("invalid executable")
	}
	if len(req.Args) > 256 {
		return result, errors.New("too many arguments")
	}
	argumentBytes := len(req.Executable)
	for _, a := range req.Args {
		argumentBytes += len(a)
		if argumentBytes > 512<<10 {
			return result, errors.New("command arguments exceed 512 KiB")
		}
		if len(a) > 256<<10 || strings.ContainsRune(a, 0) {
			return result, errors.New("argument exceeds limit or contains NUL")
		}
	}
	cwd, e := workspace.Clean(req.WorkingDirectory, false)
	if e != nil {
		return result, e
	}
	if _, e = w.DirectoryPath(cwd); e != nil {
		return result, e
	}
	if !workspace.CommandPathAllowed(cwd, d.config.PrivatePaths) {
		return result, errors.New("working directory is private or excluded from command source")
	}
	result.WorkingDirectory = path.Join("/workspace", cwd)
	if _, e = d.Status(ctx); e != nil {
		return result, e
	}
	id := "cmd-" + strings.ToLower(rand.Text())
	result.CommandID = id
	dir := filepath.Join(d.config.StateDir, "commands", id)
	source := filepath.Join(dir, "source")
	if e = os.MkdirAll(source, 0700); e != nil {
		return result, e
	}
	leaseSaved := false
	defer func() {
		if !leaseSaved {
			_ = os.RemoveAll(dir)
		}
	}()
	if e = os.Chmod(source, 0755); e != nil {
		return result, e
	}
	base, e := workspace.ScanCommandTree(ctx, req.Workspace, source, d.config.PrivatePaths)
	if e != nil {
		return result, e
	}
	result.SnapshotDigest = base.Digest
	result.DependencyDigest = dependencyIdentity(base)
	check, e := workspace.ScanCommandTree(ctx, req.Workspace, "", d.config.PrivatePaths)
	if e != nil {
		return result, e
	}
	if !workspace.SameCommandManifest(base, check) {
		return result, fmt.Errorf("source changed during command capture: %w", workspace.ErrConflict)
	}
	name := "gemma-" + d.owner[:12] + "-" + id
	lease := dockerLease{Version: 1, ID: id, Workspace: req.Workspace, Container: name, Image: d.config.Image, Status: "creating", CreatedAt: time.Now().UTC()}
	if e = saveDockerLease(dir, lease); e != nil {
		return result, e
	}
	leaseSaved = true
	cleanupOK := false
	defer func() {
		cc, cn := context.WithTimeout(context.Background(), 20*time.Second)
		defer cn()
		if e := d.removeOwned(cc, name); e != nil {
			result.Publication = "recovery_required"
			if err == nil {
				err = e
			}
			return
		}
		cleanupOK = true
		lease.Status = "finished"
		_ = saveDockerLease(dir, lease)
		if cleanupOK {
			if cleanErr := removeCommandSource(source); cleanErr != nil {
				lease.Status = "cleanup_pending"
				_ = saveDockerLease(dir, lease)
				if err == nil {
					err = fmt.Errorf("command finished but private snapshot cleanup is pending: %w", cleanErr)
				}
			}
		}
	}()
	args := []string{"create", "--name", name, "--label", "com.gemmadesktop.owner=" + d.owner, "--label", "com.gemmadesktop.command=" + id, "--pull=never", "--platform=linux/arm64", "--init", "--network=none", "--cpus=4", "--memory=8g", "--memory-swap=8g", "--pids-limit=256", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()), "--ulimit", "nofile=1024:1024", "--ulimit", "core=0:0", "--ulimit", "fsize=67108864:67108864", "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=536870912,mode=1777", "--workdir", result.WorkingDirectory, "--mount", dockerMount(source, "/workspace", req.ReadOnly)}
	cache := filepath.Join(d.config.StateDir, "cache", workspace.Hash([]byte(req.Workspace + d.config.Image))[:32])
	if e = ensureExecutionDir(cache); e != nil {
		return result, e
	}
	args = append(args, "--mount", dockerMount(cache, "/cache", false))
	for _, p := range d.config.PrivatePaths {
		if e = os.MkdirAll(filepath.Join(source, p), 0755); e != nil {
			return result, e
		}
		privateDir := filepath.Join(cache, "environments", result.DependencyDigest, p)
		if e = ensureExecutionDir(privateDir); e != nil {
			return result, e
		}
		args = append(args, "--mount", dockerMount(privateDir, "/workspace/"+p, false))
	}
	for _, kv := range []string{"HOME=/tmp/home", "TMPDIR=/tmp", "PATH=/workspace/.venv/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "GOTOOLCHAIN=local", "GOCACHE=/cache/go-build", "GOMODCACHE=/cache/go-mod", "GOPATH=/cache/go", "npm_config_cache=/cache/npm", "PIP_CACHE_DIR=/cache/pip", "PYTHONDONTWRITEBYTECODE=1"} {
		args = append(args, "--env", kv)
	}
	args = append(args, "--entrypoint", "/bin/sh", d.config.Image, "-c", "mkdir -p /tmp/home; exec \"$@\"", "gemma-exec", req.Executable)
	args = append(args, req.Args...)
	if _, e = d.cli(ctx, 4096, args...); e != nil {
		return result, e
	}
	lease.Status = "running"
	if e = saveDockerLease(dir, lease); e != nil {
		return result, e
	}
	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	if _, e = boundedTreeUsage(ctx, []string{d.config.StateDir}); e != nil {
		return result, e
	}
	finishStorageGuard := monitorExecutionStorage(runCtx, cancel, []string{d.config.StateDir})
	cmd := d.command(runCtx, "start", "--attach", name)
	stdout := &BoundedBuffer{Limit: req.MaxOutput / 2}
	stderr := &BoundedBuffer{Limit: req.MaxOutput / 2}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	storageErr := finishStorageGuard()
	result.Stdout, result.Truncated = stdout.Snapshot()
	var st bool
	result.Stderr, st = stderr.Snapshot()
	result.Truncated = result.Truncated || st
	if runCtx.Err() != nil {
		result.Cancelled = true
		result.Publication = "discarded_cancelled"
		if storageErr != nil {
			result.Publication = "discarded_storage_limit"
			return result, storageErr
		}
		return result, runCtx.Err()
	}
	// Always stop the whole owned container before reading the writable snapshot.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer stopCancel()
	if e = d.stopOwned(stopCtx, name); e != nil {
		return result, e
	}
	stateJSON, e := d.cli(stopCtx, 4096, "inspect", "--format", "{{json .State}}", name)
	if e != nil {
		return result, e
	}
	var state struct {
		Running   bool
		ExitCode  int
		OOMKilled bool
		Error     string
	}
	if e = json.Unmarshal([]byte(stateJSON), &state); e != nil {
		return result, e
	}
	if state.Running {
		return result, errors.New("container did not stop; source publication refused")
	}
	result.ExitCode = state.ExitCode
	if state.OOMKilled {
		result.Publication = "discarded_oom"
		return result, errors.New("container exceeded its memory limit")
	}
	if runErr != nil && state.ExitCode == 0 {
		return result, fmt.Errorf("Docker attach outcome uncertain: %w", runErr)
	}
	if e = ctx.Err(); e != nil {
		result.Cancelled = true
		result.Publication = "discarded_cancelled"
		return result, e
	}
	if req.ReadOnly {
		current, e := workspace.ScanCommandTree(ctx, req.Workspace, "", d.config.PrivatePaths)
		if e != nil {
			return result, e
		}
		result.WorkspaceChanged = !workspace.SameCommandManifest(base, current)
		result.Publication = "read_only"
		return result, nil
	}
	pub, e := workspace.PublishCommand(ctx, req.Workspace, source, filepath.Join(dir, "publication"), base, d.config.PrivatePaths)
	result.Publication = pub.Status
	result.Changes = pub.Changes
	result.JournalPath = pub.JournalPath
	if errors.Is(e, workspace.ErrConflict) {
		result.WorkspaceChanged = true
	}
	return result, e
}

func dependencyIdentity(manifest workspace.CommandManifest) string {
	identities := map[string]string{}
	for name, entry := range manifest.Files {
		if entry.Directory {
			continue
		}
		switch path.Base(name) {
		case "package.json", "package-lock.json", "npm-shrinkwrap.json", "requirements.lock", "go.mod", "go.sum":
			identities[name] = entry.Hash
		}
	}
	b, _ := json.Marshal(identities)
	return workspace.Hash(b)
}

func dockerMount(source, target string, readonly bool) string {
	// Docker --mount uses CSV; StateDir/path validation rejects commas below.
	s := "type=bind,source=" + source + ",target=" + target
	if readonly {
		s += ",readonly"
	}
	return s
}
func saveDockerLease(dir string, l dockerLease) error {
	b, e := json.Marshal(l)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(dir, "lease.tmp"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(filepath.Join(dir, "lease.tmp"), filepath.Join(dir, "lease.json")); e != nil {
		return e
	}
	parent, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer parent.Close()
	return parent.Sync()
}
func (d *DockerExecutor) owned(ctx context.Context, name string) (bool, error) {
	s, e := d.cli(ctx, 4096, "inspect", "--format", "{{index .Config.Labels \"com.gemmadesktop.owner\"}}", name)
	if e != nil {
		// A list query distinguishes absence from daemon/inspect failure.
		ids, le := d.cli(ctx, 4096, "ps", "-aq", "--filter", "name=^/"+name+"$")
		if le == nil && strings.TrimSpace(ids) == "" {
			return false, nil
		}
		return false, e
	}
	if strings.TrimSpace(s) != d.owner {
		return false, errors.New("refusing to control container owned by another executor")
	}
	return true, nil
}
func (d *DockerExecutor) stopOwned(ctx context.Context, name string) error {
	ok, e := d.owned(ctx, name)
	if e != nil || !ok {
		return e
	}
	if _, e = d.cli(ctx, 4096, "stop", "--time=2", name); e != nil {
		_, e = d.cli(ctx, 4096, "kill", name)
	}
	return e
}
func (d *DockerExecutor) removeOwned(ctx context.Context, name string) error {
	ok, e := d.owned(ctx, name)
	if e != nil || !ok {
		return e
	}
	_, e = d.cli(ctx, 4096, "rm", "--force", name)
	return e
}

// Reconcile stops only recorded, owner-labelled containers and rolls back
// interrupted source journals. Call at startup before admitting new runs.
func (d *DockerExecutor) Reconcile(ctx context.Context) ([]workspace.Publication, error) {
	results := []workspace.Publication{}
	// Recover labelled orphans even if local lease metadata was not persisted.
	ids, le := d.cli(ctx, 8192, "ps", "-aq", "--filter", "label=com.gemmadesktop.owner="+d.owner)
	if le != nil {
		return results, le
	}
	for _, id := range strings.Fields(ids) {
		if !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(id) {
			return results, errors.New("invalid owned container identity")
		}
		if le = d.removeOwned(ctx, id); le != nil {
			return results, le
		}
	}
	entries, e := os.ReadDir(filepath.Join(d.config.StateDir, "commands"))
	if errors.Is(e, os.ErrNotExist) {
		return results, nil
	}
	if e != nil {
		return nil, e
	}
	if len(entries) > 10000 {
		return nil, errors.New("execution lease count exceeds recovery limit")
	}
	for _, ent := range entries {
		if !ent.IsDir() || !strings.HasPrefix(ent.Name(), "cmd-") {
			continue
		}
		dir := filepath.Join(d.config.StateDir, "commands", ent.Name())
		b, e := readExecutionMetadata(filepath.Join(dir, "lease.json"), 8192)
		if errors.Is(e, os.ErrNotExist) {
			if e = removeCommandSource(dir); e != nil {
				return results, e
			}
			continue
		}
		if e != nil {
			return results, e
		}
		if len(b) > 8192 {
			return results, errors.New("execution lease too large")
		}
		var l dockerLease
		if e = json.Unmarshal(b, &l); e != nil {
			return results, e
		}
		if l.Version != 1 || l.ID != ent.Name() || !strings.HasPrefix(l.Container, "gemma-"+d.owner[:12]+"-") {
			return results, errors.New("invalid execution lease")
		}
		if l.Status != "finished" {
			if e = d.removeOwned(ctx, l.Container); e != nil {
				return results, e
			}
		}
		jp := filepath.Join(dir, "publication", "journal.json")
		if _, e = os.Stat(jp); e == nil {
			p, re := workspace.RecoverCommandJournal(ctx, jp)
			results = append(results, p)
			if re != nil {
				return results, re
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return results, e
		}
		l.Status = "finished"
		if e = saveDockerLease(dir, l); e != nil {
			return results, e
		}
		if e = removeCommandSource(filepath.Join(dir, "source")); e != nil {
			return results, e
		}
	}
	return results, nil
}

func readExecutionMetadata(p string, limit int64) ([]byte, error) {
	inf, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !inf.Mode().IsRegular() {
		return nil, errors.New("execution metadata must be a regular file")
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("execution metadata exceeds limit")
	}
	return b, e
}
func (d *DockerExecutor) pendingRecovery() error {
	entries, e := os.ReadDir(filepath.Join(d.config.StateDir, "commands"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(entries) > 10000 {
		return errors.New("execution history needs maintenance before more commands")
	}
	for _, ent := range entries {
		if !ent.IsDir() || !strings.HasPrefix(ent.Name(), "cmd-") {
			continue
		}
		dir := filepath.Join(d.config.StateDir, "commands", ent.Name())
		b, e := readExecutionMetadata(filepath.Join(dir, "lease.json"), 8192)
		if e != nil {
			return e
		}
		var l dockerLease
		if e = json.Unmarshal(b, &l); e != nil {
			return e
		}
		if l.Status != "finished" {
			return errors.New("an earlier execution needs reconciliation before commands may run")
		}
		b, e = readExecutionMetadata(filepath.Join(dir, "publication", "journal.json"), 256<<10)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		var j struct {
			Status string `json:"status"`
		}
		if e = json.Unmarshal(b, &j); e != nil {
			return e
		}
		if j.Status != "committed" && j.Status != "rolled_back" {
			return errors.New("an interrupted source publication needs reconciliation before commands may run")
		}
	}
	return nil
}

func removeCommandSource(source string) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		// Guest code can remove owner write permission from its private dirs.
		// WalkDir does not follow symlinks; no shared original source is mounted.
		_ = filepath.WalkDir(source, func(p string, ent os.DirEntry, e error) error {
			if e == nil && ent.IsDir() {
				_ = os.Chmod(p, 0700)
			}
			return nil
		})
		err = os.RemoveAll(source)
		if err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}
