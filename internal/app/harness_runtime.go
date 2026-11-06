package app

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/harness"
)

// V2 trials own their runtime from preparation through teardown. The immutable
// policy makes the selected model cold at each attempt boundary, including
// after checkpoints. No external Ollama process or model files are removed.
func (c *Core) prepareHarnessRuntime(ctx context.Context, spec harness.Spec) error {
	if spec.CachePolicy != harness.ColdEachAttemptV2 {
		return errors.New("unsupported frozen runtime cache policy")
	}
	if c.managed != nil {
		if err := c.managed.Stop(ctx); err != nil {
			return err
		}
	}
	if c.inferenceGate != nil && c.inferenceGate.BlockedReason() != "" {
		return errors.New(c.inferenceGate.BlockedReason())
	}
	if id := str(spec.RuntimeIdentity, "runtimeId"); id == "" || id == ollamaRuntimeID {
		return c.Client().Unload(ctx, spec.Model)
	}
	return nil
}
func (c *Core) releaseHarnessRuntime(ctx context.Context, spec harness.Spec) error {
	var result error
	if c.managed != nil {
		result = c.managed.Stop(ctx)
	}
	if id := str(spec.RuntimeIdentity, "runtimeId"); id == "" || id == ollamaRuntimeID {
		result = errors.Join(result, c.Client().Unload(ctx, spec.Model))
	}
	if c.inferenceGate != nil && c.inferenceGate.BlockedReason() != "" {
		result = errors.Join(result, errors.New(c.inferenceGate.BlockedReason()))
	}
	return result
}
