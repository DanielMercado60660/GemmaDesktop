package runtimes

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
)

type PreviewRequest struct {
	BuildID string `json:"buildId,omitempty"`
	ModelID string `json:"modelId,omitempty"`
}

type InstallPreview struct {
	Kind              string `json:"kind"`
	Target            string `json:"target"`
	Destination       string `json:"destination"`
	TransferBytes     int64  `json:"transferBytes"`
	PackageBytes      int64  `json:"packageBytes"`
	InstalledBytes    int64  `json:"installedBytes"`
	TemporaryBytes    int64  `json:"temporaryBytes"`
	ReserveBytes      int64  `json:"reserveBytes"`
	RequiredFreeBytes int64  `json:"requiredFreeBytes"`
	AvailableBytes    int64  `json:"availableBytes"`
	Bundled           bool   `json:"bundled"`
	SourceKind        string `json:"sourceKind"`
	CanInstall        bool   `json:"canInstall"`
	Reason            string `json:"reason,omitempty"`
}

type installRequirements struct {
	packageBytes, installedBytes, temporaryBytes, transferBytes int64
	bundled                                                     bool
	sourceKind                                                  string
}

func addBytes(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errors.New("artifact disk requirement exceeds supported size")
	}
	return a + b, nil
}
func (r installRequirements) required() (int64, error) {
	return addBytes(r.installedBytes, r.temporaryBytes)
}

func runtimeRequirements(manifest Manifest) (installRequirements, error) {
	unpacked, err := validateFiles(manifest.Files)
	if err != nil {
		return installRequirements{}, err
	}
	r := installRequirements{packageBytes: manifest.Bundle.Size, installedBytes: unpacked, temporaryBytes: manifest.Bundle.Size, bundled: manifest.Bundle.BundledPath != "", sourceKind: "bundled"}
	if !r.bundled {
		r.sourceKind = "network"
		r.transferBytes = manifest.Bundle.Size
	}
	_, err = r.required()
	return r, err
}
func modelRequirements(model ModelManifest) (installRequirements, error) {
	r := installRequirements{bundled: true, sourceKind: "bundled"}
	var bundled, network bool
	for _, file := range model.Files {
		var err error
		r.installedBytes, err = addBytes(r.installedBytes, file.Size)
		if err != nil {
			return r, err
		}
		if file.Source.URL != "" {
			network = true
			r.transferBytes, err = addBytes(r.transferBytes, file.Source.Size)
			if err != nil {
				return r, err
			}
		} else {
			bundled = true
		}
	}
	r.packageBytes, r.temporaryBytes = r.installedBytes, r.installedBytes
	if network {
		r.bundled = false
		r.sourceKind = "network"
	}
	if network && bundled {
		r.sourceKind = "mixed"
	}
	_, err := r.required()
	return r, err
}

// Preview reads trusted metadata and filesystem availability only. Network bytes
// are a conservative source total: a verified cache/resume can transfer less.
// Disk requirements deliberately use the same conservative calculation as the
// installer, including bundled archives copied to its private download cache.
func (m *Manager) Preview(ctx context.Context, request PreviewRequest) (InstallPreview, error) {
	if err := ctx.Err(); err != nil {
		return InstallPreview{}, err
	}
	if (request.BuildID == "") == (request.ModelID == "") {
		return InstallPreview{}, errors.New("preview requires exactly one buildId or modelId")
	}
	m.mu.Lock()
	manifest, runtimeOK := m.catalog[request.BuildID]
	model, modelOK := m.models[request.ModelID]
	closed, busy := m.closed, len(m.jobs) > 0
	reserve := m.config.DiskReserveBytes
	m.mu.Unlock()
	if closed {
		return InstallPreview{}, errors.New("runtime manager is closed")
	}
	var requirements installRequirements
	var sources []Source
	var err error
	preview := InstallPreview{ReserveBytes: reserve, CanInstall: true}
	if request.BuildID != "" {
		if !runtimeOK {
			return preview, errors.New("unknown trusted runtime build")
		}
		requirements, err = runtimeRequirements(manifest)
		preview.Kind, preview.Target = "runtime_install", request.BuildID
		preview.Destination = filepath.Join(m.root, "runtimes", request.BuildID)
		sources = []Source{manifest.Bundle}
		if platformErr := platformPreflight(manifest); platformErr != nil {
			preview.CanInstall = false
			preview.Reason = platformErr.Error()
		}
	} else {
		if !modelOK {
			return preview, errors.New("unknown trusted model artifact")
		}
		requirements, err = modelRequirements(model)
		preview.Kind, preview.Target = "model_download", request.ModelID
		preview.Destination = filepath.Join(m.root, "models", request.ModelID, model.ServeName)
		for _, file := range model.Files {
			sources = append(sources, file.Source)
		}
	}
	if err != nil {
		return preview, err
	}
	required, err := requirements.required()
	if err != nil {
		return preview, err
	}
	preview.RequiredFreeBytes, err = addBytes(required, reserve)
	if err != nil {
		return preview, err
	}
	preview.AvailableBytes, err = availableDiskBytes(m.root)
	if err != nil {
		return preview, err
	}
	preview.TransferBytes, preview.PackageBytes = requirements.transferBytes, requirements.packageBytes
	preview.InstalledBytes, preview.TemporaryBytes = requirements.installedBytes, requirements.temporaryBytes
	preview.Bundled, preview.SourceKind = requirements.bundled, requirements.sourceKind
	if preview.RequiredFreeBytes > preview.AvailableBytes {
		preview.CanInstall = false
		preview.Reason = fmt.Sprintf("insufficient disk space: need %d bytes including reserve, have %d", preview.RequiredFreeBytes, preview.AvailableBytes)
	}
	if busy {
		preview.CanInstall = false
		preview.Reason = "another artifact job is active"
	}
	// Check bundled source accessibility without copying, hashing weights, or
	// issuing network requests. Install independently verifies every byte.
	for _, source := range sources {
		if source.BundledPath != "" {
			reader, _, err := m.sourceReader(ctx, source, 0)
			if err != nil {
				return preview, err
			}
			if err = reader.Close(); err != nil {
				return preview, err
			}
		}
	}
	return preview, nil
}
