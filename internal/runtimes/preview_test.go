package runtimes

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewBundledRuntimeMatchesInstallDiskRequirement(t *testing.T) {
	config, manifest, _ := fixtures(t)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	preview, err := m.Preview(t.Context(), PreviewRequest{BuildID: manifest.ID})
	if err != nil {
		t.Fatal(err)
	}
	unpacked, _ := validateFiles(manifest.Files)
	if preview.Kind != "runtime_install" || preview.Target != manifest.ID || preview.Destination != filepath.Join(m.root, "runtimes", manifest.ID) {
		t.Fatalf("wrong destination identity: %+v", preview)
	}
	if !preview.CanInstall || !preview.Bundled || preview.SourceKind != "bundled" || preview.TransferBytes != 0 || preview.PackageBytes != manifest.Bundle.Size || preview.InstalledBytes != unpacked || preview.TemporaryBytes != manifest.Bundle.Size || preview.RequiredFreeBytes != unpacked+manifest.Bundle.Size+config.DiskReserveBytes {
		t.Fatalf("incorrect bundled preview: %+v", preview)
	}
	if len(m.Snapshot().Jobs) != 0 || len(m.Snapshot().Installed) != 0 {
		t.Fatal("preview started or installed work")
	}
	// Both the preview and actual installer reject the same reserve requirement.
	m.config.DiskReserveBytes = 1 << 60
	blocked, err := m.Preview(t.Context(), PreviewRequest{BuildID: manifest.ID})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.CanInstall || !strings.Contains(blocked.Reason, "insufficient disk") {
		t.Fatalf("disk refusal missing: %+v", blocked)
	}
	if _, err = m.Install(manifest.ID); err == nil || !strings.Contains(err.Error(), "insufficient disk") {
		t.Fatalf("installer budget disagrees with preview: %v", err)
	}
}

func TestPreviewModelSeparatesNetworkTransferFromLocalSources(t *testing.T) {
	config, _, model := fixtures(t)
	second := model.Files[0]
	second.Path = "other.json"
	model.Files[0].Source.BundledPath = ""
	model.Files[0].Source.URL = "https://trusted.example/model-config"
	model.Files = append(model.Files, second)
	model.Model.ArtifactDigest = ModelArtifactDigest(model.Files)
	config.ModelCatalog = []ModelManifest{model}
	calls := 0
	config.HTTPClient = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("preview must not download")
	})}
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	preview, err := m.Preview(t.Context(), PreviewRequest{ModelID: model.ID})
	if err != nil {
		t.Fatal(err)
	}
	total := model.Files[0].Size + model.Files[1].Size
	if preview.Kind != "model_download" || preview.Destination != filepath.Join(m.root, "models", model.ID, model.ServeName) || preview.SourceKind != "mixed" || preview.Bundled || preview.TransferBytes != model.Files[0].Source.Size || preview.PackageBytes != total || preview.InstalledBytes != total || preview.TemporaryBytes != total || preview.RequiredFreeBytes != 2*total+config.DiskReserveBytes {
		t.Fatalf("incorrect model preview: %+v", preview)
	}
	if calls != 0 || len(m.Snapshot().Jobs) != 0 {
		t.Fatal("preview performed network or queued a job")
	}
	m.config.DiskReserveBytes = 1 << 60
	blocked, err := m.Preview(t.Context(), PreviewRequest{ModelID: model.ID})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.CanInstall {
		t.Fatal("model preview bypassed disk reserve")
	}
	if _, err = m.AcquireModel(model.ID); err == nil {
		t.Fatal("model acquisition disagreed with disk preview")
	}
}

func TestPreviewRejectsAmbiguousUnknownAndCancelledRequests(t *testing.T) {
	config, manifest, model := fixtures(t)
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, request := range []PreviewRequest{{}, {BuildID: manifest.ID, ModelID: model.ID}, {BuildID: "unknown"}, {ModelID: "unknown"}} {
		if _, err = m.Preview(t.Context(), request); err == nil {
			t.Fatalf("invalid preview accepted: %+v", request)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = m.Preview(ctx, PreviewRequest{BuildID: manifest.ID}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
