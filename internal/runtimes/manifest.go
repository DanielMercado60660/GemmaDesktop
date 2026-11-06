package runtimes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gemmadesktop.local/app/internal/inference"
)

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var versionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}$`)

func validSHA(value string, bytes int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == bytes && value == strings.ToLower(value)
}

func safeRelative(value string) bool {
	return value != "" && value != "." && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00\r\n")
}

func (s Source) validate() error {
	if !validSHA(s.SHA256, 32) || s.Size < 1 {
		return errors.New("artifact requires an exact SHA-256 and positive size")
	}
	if (s.BundledPath == "") == (s.URL == "") {
		return errors.New("artifact requires exactly one bundled path or HTTPS URL")
	}
	if s.BundledPath != "" && !safeRelative(s.BundledPath) {
		return errors.New("unsafe bundled artifact path")
	}
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return errors.New("artifact download must be an HTTPS URL without credentials")
		}
	}
	for _, host := range s.RedirectHosts {
		if host == "" || strings.ContainsAny(host, "/:@*\\ \r\n") || host != strings.ToLower(host) {
			return errors.New("invalid artifact redirect host")
		}
	}
	return nil
}

func validateFiles(files []File) (int64, error) {
	if len(files) == 0 || len(files) > 100000 {
		return 0, errors.New("artifact inventory must contain 1 to 100000 files")
	}
	seen := map[string]bool{}
	var total int64
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		if !safeRelative(file.Path) || seen[lower] || !validSHA(file.SHA256, 32) || file.Size < 0 || file.Size > 1<<40 {
			return 0, fmt.Errorf("invalid or duplicate inventory file %q", file.Path)
		}
		seen[lower] = true
		total += file.Size
		if total > 1<<44 {
			return 0, errors.New("artifact inventory exceeds size limit")
		}
	}
	for _, file := range files {
		for parent := path.Dir(file.Path); parent != "."; parent = path.Dir(parent) {
			if seen[strings.ToLower(parent)] {
				return 0, errors.New("inventory file overlaps another file's directory")
			}
		}
	}
	return total, nil
}

func (m Manifest) Validate() error {
	if m.Version != 1 || !identifier.MatchString(m.ID) || !validSHA(m.SourceSHA, 20) || m.Architecture != "arm64" || !versionPattern.MatchString(m.MinimumOS) || strings.TrimSpace(m.License) == "" {
		return errors.New("runtime manifest has invalid version, identity, platform or license")
	}
	if m.Provider != inference.ProviderOMLX && m.Provider != inference.ProviderMLXServe {
		return errors.New("unsupported managed runtime provider")
	}
	if m.PatchSHA256 != "" && !validSHA(m.PatchSHA256, 32) {
		return errors.New("invalid runtime patch digest")
	}
	if m.ReadyForInference && (m.Qualification.Transport != "qualified" || !validSHA(m.Qualification.EvidenceSHA256, 32)) {
		return errors.New("ready runtime requires transport qualification evidence")
	}
	if m.Qualification.Coding != "experimental" && m.Qualification.Coding != "qualified" {
		return errors.New("runtime must declare coding qualification")
	}
	if m.Provider == inference.ProviderMLXServe && m.SandboxProfileSHA256 != SandboxProfileDigest() {
		return errors.New("mlx-serve manifest does not qualify the required isolation profile")
	}
	if err := m.Bundle.validate(); err != nil {
		return err
	}
	if _, err := validateFiles(m.Files); err != nil {
		return err
	}
	for _, file := range m.Files {
		if file.Path == m.Executable && file.Executable {
			return nil
		}
	}
	return errors.New("runtime entrypoint is absent from the executable inventory")
}

func (m ModelManifest) Validate() error {
	if m.Version != 1 || !identifier.MatchString(m.ID) || !identifier.MatchString(m.ServeName) || m.Repository == "" || !validSHA(m.Revision, 20) || strings.TrimSpace(m.License) == "" || m.Model.ContextTokens < 1 || !m.Model.Capabilities.Completion {
		return errors.New("invalid model manifest identity, revision, license or capability")
	}
	if m.Model.Name != m.ServeName || !validSHA(strings.TrimPrefix(m.Model.ArtifactDigest, "sha256:"), 32) {
		return errors.New("model metadata must identify its served name and artifact digest")
	}
	for _, provider := range m.Providers {
		if provider != inference.ProviderOMLX && provider != inference.ProviderMLXServe {
			return errors.New("unsupported model provider")
		}
	}
	files := make([]File, len(m.Files))
	for i, file := range m.Files {
		files[i] = file.File
		if err := file.Source.validate(); err != nil {
			return err
		}
		if file.Source.Size != file.Size || file.Source.SHA256 != file.SHA256 || file.Executable {
			return errors.New("model file source/inventory mismatch or executable model file")
		}
	}
	_, err := validateFiles(files)
	if err == nil && "sha256:"+strings.TrimPrefix(m.Model.ArtifactDigest, "sha256:") != ModelArtifactDigest(m.Files) {
		return errors.New("model artifact digest does not identify its complete file inventory")
	}
	return err
}

func digestJSON(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// LoadCatalog loads trusted app-shipped catalogs, not arbitrary downloaded
// manifests. Bundle imports must match an already trusted manifest's hash.
func LoadCatalog(directory string) ([]Manifest, []ModelManifest, error) {
	var catalog struct {
		Version  int             `json:"version"`
		Runtimes []Manifest      `json:"runtimes"`
		Models   []ModelManifest `json:"models"`
	}
	b, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	if os.IsNotExist(err) {
		return []Manifest{}, []ModelManifest{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(b) > 32<<20 {
		return nil, nil, errors.New("runtime catalog exceeds size limit")
	}
	if err := json.Unmarshal(b, &catalog); err != nil {
		return nil, nil, err
	}
	if catalog.Version != 1 {
		return nil, nil, errors.New("unsupported runtime catalog version")
	}
	for _, manifest := range catalog.Runtimes {
		if err := manifest.Validate(); err != nil {
			return nil, nil, fmt.Errorf("runtime %s: %w", manifest.ID, err)
		}
	}
	for _, model := range catalog.Models {
		if err := model.Validate(); err != nil {
			return nil, nil, fmt.Errorf("model %s: %w", model.ID, err)
		}
	}
	return catalog.Runtimes, catalog.Models, nil
}
