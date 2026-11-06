package app

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/inference"
)

// Every node uses the run's backend and an admitted model artifact. External
// Ollama is user-owned, so its alias/version/template are rechecked before each
// generation instead of assuming the app owns the external installation.
type frozenCompletion struct {
	inference.CompletionBackend
	core     *Core
	bindings Doc
}

func (b *frozenCompletion) Generate(ctx context.Context, req inference.CompletionRequest, emit func(inference.ChatChunk) error, observe ...func(inference.ChatChunk)) error {
	expected := obj(b.bindings[req.Model])
	if len(expected) == 0 {
		return errors.New("generation selected a model outside the frozen run binding")
	}
	backend, current, err := b.core.completionBackend(ctx, str(expected, "runtimeId"), req.Model, number(expected, "contextTokens", 32768))
	if err != nil {
		return err
	}
	if contentDigest(obj(current)) != contentDigest(obj(expected)) {
		return errors.New("model artifact, template or runtime changed during the run")
	}
	return backend.Generate(ctx, req, emit, observe...)
}
