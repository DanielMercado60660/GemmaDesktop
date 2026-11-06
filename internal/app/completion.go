package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/runtimes"
	"gemmadesktop.local/app/internal/store"
)

const ollamaRuntimeID = "ollama-default"

type completionKey struct{}

func nativeRuntimeIdentity(descriptor inference.BackendDescriptor, model inference.CompletionModel, contextTokens int) Doc {
	return Doc{"runtimeId": descriptor.RuntimeID, "backend": string(descriptor.Provider), "revision": descriptor.BuildID, "artifactDigest": model.ArtifactDigest, "model": model.Name, "contextTokens": contextTokens, "descriptor": descriptor, "modelIdentity": model}
}

// Freezing a profile is metadata-only. It never loads a managed model or changes
// its selected runtime version before an attempt is admitted.
func (c *Core) completionIdentity(ctx context.Context, runtimeID, model string, contextTokens int) (Doc, error) {
	if runtimeID == "" || runtimeID == ollamaRuntimeID {
		_, identity, err := c.completionBackend(ctx, ollamaRuntimeID, model, contextTokens)
		return identity, err
	}
	if c.managed == nil {
		return nil, errors.New("managed runtimes are unavailable")
	}
	for _, candidate := range c.managed.PresentationSnapshot().ModelCatalog {
		if candidate.ServeName != model {
			continue
		}
		descriptor, metadata, err := c.managed.DescribeQualified(runtimes.StartRequest{BuildID: runtimeID, ModelID: candidate.ID, ContextTokens: contextTokens})
		if err != nil {
			return nil, err
		}
		return nativeRuntimeIdentity(descriptor, metadata, contextTokens), nil
	}
	return nil, errors.New("selected model is absent from the verified managed catalog")
}

func (c *Core) runtimeID(agent Doc) string {
	id := str(agent, "runtimeId")
	if id == "" {
		id = str(c.Settings(), "defaultCompletionRuntimeId")
	}
	if id == "" {
		id = ollamaRuntimeID
	}
	return id
}
func (c *Core) completionFromContext(ctx context.Context) inference.CompletionBackend {
	if backend, ok := ctx.Value(completionKey{}).(inference.CompletionBackend); ok {
		return backend
	}
	return c.Client()
}

func (c *Core) openManagedRuntimes() error {
	settings, err := c.Store.Get("settings", "default")
	if err != nil {
		return err
	}
	changed := false
	if str(settings, "defaultCompletionRuntimeId") == "" {
		settings["defaultCompletionRuntimeId"] = ollamaRuntimeID
		changed = true
	}
	if str(settings, "mediaRuntimeId") == "" {
		settings["mediaRuntimeId"] = ollamaRuntimeID
		changed = true
	}
	if changed {
		if err = c.Store.Put("settings", settings); err != nil {
			return err
		}
	}
	if _, err = c.Store.Get("runtime", ollamaRuntimeID); errors.Is(err, store.ErrNotFound) {
		if err = c.Store.Put("runtime", Doc{"id": ollamaRuntimeID, "provider": "ollama", "management": "external", "endpointSetting": "ollamaUrl", "media": true}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	dir := os.Getenv("GEMMA_RUNTIME_BUNDLES")
	if dir != "" && !filepath.IsAbs(dir) {
		return errors.New("GEMMA_RUNTIME_BUNDLES must be absolute")
	}
	if dir == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		dir = filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "Resources", "runtimes"))
	}
	catalog, models, err := runtimes.LoadCatalog(dir)
	if err != nil {
		return err
	}
	c.managed, err = runtimes.New(runtimes.Config{DataDir: c.DataDir, BundlesDir: dir, Catalog: catalog, ModelCatalog: models, Gate: c.inferenceGate})
	return err
}

func (c *Core) validateCompletionChoice(runtimeID, model string) error {
	if runtimeID == "" || runtimeID == ollamaRuntimeID {
		return c.checkCompletionSelection(model)
	}
	if c.managed == nil {
		return errors.New("managed runtimes are unavailable")
	}
	s := c.managed.PresentationSnapshot()
	found := false
	for _, build := range s.Catalog {
		if build.ID == runtimeID {
			found = true
		}
	}
	if !found {
		return errors.New("unknown managed runtime; install an app-catalog runtime first")
	}
	if model == "" {
		return nil
	}
	for _, candidate := range s.ModelCatalog {
		if candidate.ServeName == model {
			for _, installed := range s.Models {
				if installed.ID == candidate.ID {
					return nil
				}
			}
		}
	}
	return errors.New("selected model is not verified in the managed model store")
}

// completionBackend is the strict identity boundary used by admitted
// comparisons and new chats. Historical checkpoints without artifact identity
// retain their explicitly legacy compatibility path.
func (c *Core) completionBackend(ctx context.Context, runtimeID, model string, contextTokens int) (inference.CompletionBackend, Doc, error) {
	if runtimeID == "" {
		runtimeID = ollamaRuntimeID
	}
	if runtimeID == ollamaRuntimeID {
		client := c.Client()
		m, err := client.ResolveModel(ctx, model)
		if err != nil {
			return nil, nil, err
		}
		version, err := client.Version(ctx)
		if err != nil {
			return nil, nil, err
		}
		info, err := client.Show(ctx, model)
		if err != nil {
			return nil, nil, err
		}
		identity := Doc{"runtimeId": runtimeID, "backend": "ollama", "revision": version, "artifactDigest": m.ArtifactDigest, "model": m.Name, "contextTokens": contextTokens, "endpointDigest": contentDigest(client.BaseURL()), "modelIdentity": m}
		identity["templateDigest"] = contentDigest(info.Raw["template"])
		identity["parametersDigest"] = contentDigest(info.Parameters)
		identity["quantization"] = info.Details.QuantizationLevel
		identity["drafter"] = Doc{"status": "unreported by runtime metadata"}
		if raw := info.Raw["drafter"]; len(raw) > 0 {
			identity["drafter"] = raw
		}
		return client, identity, nil
	}
	if c.managed == nil {
		return nil, nil, errors.New("managed runtimes are unavailable")
	}
	s := c.managed.Snapshot()
	modelID := ""
	for _, m := range s.ModelCatalog {
		if m.ServeName == model {
			modelID = m.ID
			break
		}
	}
	if modelID == "" {
		return nil, nil, errors.New("model is absent from the trusted artifact catalog")
	}
	connection, err := c.managed.Connection()
	if err == nil {
		if connection.BuildID != runtimeID || connection.ModelID != modelID || len(connection.Config.Models) != 1 || connection.Config.Models[0].ContextTokens != contextTokens {
			return nil, nil, errors.New("stop the current managed runtime before selecting a different build, model or context")
		}
	} else {
		connection, err = c.managed.StartFrozen(ctx, runtimes.StartRequest{BuildID: runtimeID, ModelID: modelID, ContextTokens: contextTokens})
		if err != nil {
			return nil, nil, err
		}
	}
	config := connection.Config
	config.Gate = c.inferenceGate
	var backend inference.CompletionBackend
	switch config.Descriptor.Provider {
	case inference.ProviderOMLX:
		backend, err = inference.NewOMLXBackend(config)
	case inference.ProviderMLXServe:
		backend, err = inference.NewMLXServeBackend(config)
	default:
		return nil, nil, errors.New("unsupported managed completion provider")
	}
	if err != nil {
		return nil, nil, err
	}
	m, err := backend.ResolveModel(ctx, model)
	if err != nil {
		return nil, nil, err
	}
	id := nativeRuntimeIdentity(config.Descriptor, m, contextTokens)
	return backend, id, nil
}

func (c *Core) completionForSnapshot(ctx context.Context, snapshot Doc) (context.Context, error) {
	binding := obj(snapshot["runtimeBinding"])
	if len(binding) == 0 {
		return ctx, nil
	} // legacy run: existing Ollama path only
	id := str(binding, "runtimeId")
	if id == ollamaRuntimeID && str(binding, "artifactDigest") == "" && len(obj(snapshot["qualifiedProfile"])) == 0 {
		if str(binding, "endpointDigest") != contentDigest(c.Client().BaseURL()) {
			return ctx, errors.New("Ollama endpoint changed since run admission")
		}
		return context.WithValue(ctx, completionKey{}, c.Client()), nil
	}
	backend, identity, err := c.completionBackend(ctx, id, str(binding, "model"), number(binding, "contextTokens", 32768))
	if err != nil {
		return ctx, err
	}
	if contentDigest(obj(identity)) != contentDigest(obj(binding)) {
		return ctx, errors.New("completion runtime identity changed after admission")
	}
	bindings := obj(snapshot["modelBindings"])
	if len(bindings) == 0 {
		bindings = Doc{str(binding, "model"): binding}
	}
	bound := &frozenCompletion{CompletionBackend: backend, core: c, bindings: bindings}
	return context.WithValue(ctx, completionKey{}, bound), nil
}

func (c *Core) managedRuntimeCall(ctx context.Context, method string, p Doc) (any, error) {
	if c.managed == nil {
		return nil, errors.New("managed runtime service is unavailable")
	}
	if method == "runtime.managed.status" {
		return c.managed.PresentationSnapshot(), nil
	}
	if method == "runtime.managed.preview" {
		return c.managed.Preview(ctx, runtimes.PreviewRequest{BuildID: str(p, "buildId"), ModelID: str(p, "modelId")})
	}
	if method == "runtime.managed.jobs.cancel" {
		return Doc{}, c.managed.CancelJob(str(p, "jobId"))
	}
	if method == "runtime.managed.stop" {
		status := c.managed.Snapshot().Process.Status
		if status == "checking" || status == "starting" {
			// Qualification holds the admission guard while probing. Stop must
			// cancel that probe before waiting for either admission or launch.
			return Doc{}, c.managed.Stop(ctx)
		}
	}
	c.runtimeGuard.Lock()
	defer c.runtimeGuard.Unlock()
	c.videoGuard.Lock()
	defer c.videoGuard.Unlock()
	c.mu.Lock()
	busy := len(c.runs) > 0 || len(c.evals) > 0 || len(c.pulls) > 0 || c.harnessBusy()
	c.mu.Unlock()
	if busy || c.videoBusy() {
		return nil, errors.New("wait for active runs, media, comparisons and downloads before changing runtimes")
	}
	switch method {
	case "runtime.managed.install":
		return c.managed.Install(str(p, "buildId"))
	case "runtime.managed.activate":
		return Doc{}, c.managed.Activate(str(p, "buildId"))
	case "runtime.managed.rollback":
		return Doc{}, c.managed.Rollback(inference.ProviderKind(str(p, "provider")))
	case "runtime.managed.model.acquire":
		return c.managed.AcquireModel(str(p, "modelId"))
	case "runtime.managed.start":
		_, err := c.managed.Start(ctx, runtimes.StartRequest{BuildID: str(p, "buildId"), ModelID: str(p, "modelId"), ContextTokens: number(p, "contextTokens", number(c.Settings(), "contextSize", 32768))})
		return c.managed.PresentationSnapshot(), err
	case "runtime.managed.qualify":
		return c.managed.Qualify(ctx, runtimes.StartRequest{BuildID: str(p, "buildId"), ModelID: str(p, "modelId"), ContextTokens: number(p, "contextTokens", number(c.Settings(), "contextSize", 32768))})
	case "runtime.managed.stop":
		return Doc{}, c.managed.Stop(ctx)
	case "runtime.managed.remove":
		return Doc{}, c.managed.Remove(str(p, "buildId"))
	case "runtime.managed.model.remove":
		return Doc{}, c.managed.RemoveModel(str(p, "modelId"))
	}
	return nil, errors.New("unknown managed runtime operation: " + strings.TrimPrefix(method, "runtime.managed."))
}
