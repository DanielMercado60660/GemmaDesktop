package runtimes

import (
	"errors"

	"gemmadesktop.local/app/internal/inference"
)

// DescribeQualified freezes metadata without loading a model or starting a
// server. It requires exact installation and protocol qualification receipts.
// Start independently re-verifies the complete on-disk inventories before use.
// Selection is a default/rollback pointer, not authority for an explicit build.
func (m *Manager) DescribeQualified(request StartRequest) (inference.BackendDescriptor, inference.CompletionModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, ok := m.catalog[request.BuildID]
	model, modelOK := m.models[request.ModelID]
	installed, installedOK := m.state.Installed[request.BuildID]
	modelInstalled, modelInstalledOK := m.state.Models[request.ModelID]
	fail := func(message string) (inference.BackendDescriptor, inference.CompletionModel, error) {
		return inference.BackendDescriptor{}, inference.CompletionModel{}, errors.New(message)
	}
	if m.closed {
		return fail("runtime manager is closed")
	}
	if !ok || !modelOK || !installedOK || !modelInstalledOK || installed.ManifestDigest != digestJSON(manifest) || modelInstalled.ManifestDigest != digestJSON(model) {
		return fail("selected runtime and model must have verified installation receipts")
	}
	if request.ContextTokens < 1024 || request.ContextTokens > model.Model.ContextTokens {
		return fail("requested context exceeds the verified model limit")
	}
	if m.platformSupport[manifest.ID] != "supported" {
		return fail("selected runtime is unsupported on this platform")
	}
	if !m.qualifiedLocked(request, manifest, model) {
		return fail("check this exact runtime, model and context before using it")
	}
	return descriptor(manifest), m.completionModelLocked(request, model, true), nil
}

func descriptor(manifest Manifest) inference.BackendDescriptor {
	return inference.BackendDescriptor{Provider: manifest.Provider, RuntimeID: manifest.ID, BuildID: manifest.ID, SourceSHA: manifest.SourceSHA, PatchDigest: manifest.PatchSHA256}
}

func (m *Manager) completionModelLocked(request StartRequest, model ModelManifest, qualified bool) inference.CompletionModel {
	result := model.Model.Clone()
	result.ContextTokens = request.ContextTokens
	if qualified {
		receipt := m.state.Qualifications[qualificationKey(request)]
		if result.CapabilityClaims == nil {
			result.CapabilityClaims = map[string]inference.CapabilityClaim{}
		}
		for _, capability := range []string{"completion", "tools", "thinking"} {
			result.CapabilityClaims[capability] = inference.CapabilityClaim{State: "supported", Advertised: true, Source: "local_protocol_probe", EvidenceDigest: digestJSON(receipt)}
		}
	}
	return result
}
