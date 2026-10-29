package app

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/inference"
	"time"
)

func (c *Core) RefreshModels() {
	ctx, cancel := context.WithTimeout(c.ctx, 12*time.Second)
	defer cancel()
	client := c.Client()
	version, e := client.Version(ctx)
	runtime := Doc{"available": e == nil, "version": version, "memoryBytes": hardwareMemory(), "policy": "Local inference only"}
	models := []Doc{}
	if e != nil {
		runtime["error"] = e.Error()
	} else {
		installed, err := client.Models(ctx)
		if err != nil {
			runtime["error"] = err.Error()
			runtime["available"] = false
		}
		running, runErr := client.Running(ctx)
		if runErr != nil {
			runtime["error"] = runErr.Error()
		}
		runtime["models"] = running
		for _, m := range installed {
			d := Doc{"name": m.Name, "size": m.Size, "digest": m.Digest, "family": m.Details.Family, "parameterSize": m.Details.ParameterSize, "quantization": m.Details.QuantizationLevel, "remote": m.RemoteHost != "" || m.RemoteModel != "" || inference.IsRemoteName(m.Name), "loaded": false}
			for _, r := range running {
				if r.Name == m.Name {
					d["loaded"] = true
					d["contextLength"] = r.ContextLength
					d["loadedContextLength"] = r.ContextLength
					d["memoryBytes"] = r.SizeVRAM
				}
			}
			info, err := client.Show(ctx, m.Name)
			if err == nil {
				d["capabilities"] = info.Capabilities
				if info.RemoteHost != "" || info.RemoteModel != "" {
					d["remote"] = true
				}
				for k, v := range info.ModelInfo {
					if len(k) > 15 && k[len(k)-15:] == ".context_length" {
						d["contextLength"] = v
					}
				}
				if raw, ok := info.Raw["runner"]; ok {
					d["runner"] = string(raw)
				}
			}
			models = append(models, d)
		}
	}
	c.mu.Lock()
	c.runtime = runtime
	c.models = models
	c.mu.Unlock()
}
func (c *Core) modelAction(ctx context.Context, method string, p Doc) (any, error) {
	c.videoGuard.Lock()
	defer c.videoGuard.Unlock()
	if method != "model.cancelPull" && c.videoBusy() {
		return nil, errors.New("Wait for the active video job before changing model residency or downloads")
	}
	if method != "model.cancelPull" && c.harnessBusy() {
		return nil, errors.New("Wait for the active Harness experiment before changing model residency or downloads")
	}
	name, e := required(p, "name")
	if e != nil {
		return nil, e
	}
	client := c.Client()
	switch method {
	case "model.pull":
		c.mu.Lock()
		if _, ok := c.pulls[name]; ok {
			c.mu.Unlock()
			return nil, errors.New("This model is already downloading")
		}
		download, cancel := context.WithCancel(c.ctx)
		c.pulls[name] = cancel
		c.pullState[name] = Doc{"name": name, "status": "Starting download"}
		c.wg.Add(1)
		c.mu.Unlock()
		go func() {
			defer c.wg.Done()
			defer cancel()
			e := client.Pull(download, name, func(progress inference.PullProgress) {
				d := obj(progress)
				d["name"] = name
				c.mu.Lock()
				c.pullState[name] = d
				c.mu.Unlock()
			})
			c.mu.Lock()
			delete(c.pulls, name)
			if e != nil {
				c.pullState[name] = Doc{"name": name, "status": "failed", "error": e.Error()}
			} else {
				c.pullState[name] = Doc{"name": name, "status": "success"}
			}
			c.mu.Unlock()
			c.RefreshModels()
		}()
		return Doc{"name": name, "status": "downloading"}, nil
	case "model.cancelPull":
		c.mu.Lock()
		f := c.pulls[name]
		c.mu.Unlock()
		if f != nil {
			f()
		}
		return Doc{}, nil
	case "model.load":
		e = client.Load(ctx, name, number(c.Settings(), "contextSize", 32768))
	case "model.unload", "model.delete":
		c.mu.Lock()
		evaluating := len(c.evals) > 0
		c.mu.Unlock()
		if len(c.activeRuns()) > 0 || evaluating {
			return nil, errors.New("Wait for active runs before unloading or deleting a model")
		}
		if method == "model.unload" {
			e = client.Unload(ctx, name)
		} else {
			e = client.Delete(ctx, name)
		}
	}
	if e != nil {
		return nil, e
	}
	c.RefreshModels()
	return Doc{"name": name}, nil
}
