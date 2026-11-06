// Package runtimes owns only app-managed local inference artifacts and child
// processes. Model tools cannot install software or modify the trusted catalog.
package runtimes

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

type File struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable,omitempty"`
}

type Source struct {
	// Exactly one bundled relative path or HTTPS URL. Redirect hosts are a
	// manifest-controlled allowlist, not a request parameter.
	BundledPath   string   `json:"bundledPath,omitempty"`
	URL           string   `json:"url,omitempty"`
	RedirectHosts []string `json:"redirectHosts,omitempty"`
	SHA256        string   `json:"sha256"`
	Size          int64    `json:"size"`
}

type Manifest struct {
	Version      int                    `json:"version"`
	ID           string                 `json:"id"`
	Provider     inference.ProviderKind `json:"provider"`
	SourceSHA    string                 `json:"sourceSha"`
	PatchSHA256  string                 `json:"patchSha256,omitempty"`
	Architecture string                 `json:"architecture"`
	MinimumOS    string                 `json:"minimumOs"`
	Bundle       Source                 `json:"bundle"`
	Files        []File                 `json:"files"`
	// Presentation-only summaries. Trusted install manifests retain full Files.
	UnpackedBytes        int64  `json:"unpackedBytes,omitempty"`
	RuntimeFileCount     int    `json:"runtimeFileCount,omitempty"`
	Executable           string `json:"executable"`
	License              string `json:"license"`
	SandboxProfileSHA256 string `json:"sandboxProfileSha256,omitempty"`
	// ReadyForInference is earned by offline relocation, dependency and
	// provider conformance checks during packaging, not by extraction alone.
	ReadyForInference bool          `json:"readyForInference"`
	Qualification     Qualification `json:"qualification"`
}

type Qualification struct {
	Transport      string `json:"transport"`
	EvidenceSHA256 string `json:"evidenceSha256,omitempty"`
	Coding         string `json:"coding"`
}

type ModelFile struct {
	File
	Source Source `json:"source"`
}

type ModelManifest struct {
	Version    int                       `json:"version"`
	ID         string                    `json:"id"`
	Repository string                    `json:"repository"`
	Revision   string                    `json:"revision"`
	ServeName  string                    `json:"serveName"`
	License    string                    `json:"license"`
	Providers  []inference.ProviderKind  `json:"providers"`
	Model      inference.CompletionModel `json:"model"`
	Files      []ModelFile               `json:"files"`
}

type Config struct {
	DataDir          string
	BundlesDir       string
	Catalog          []Manifest
	ModelCatalog     []ModelManifest
	DiskReserveBytes int64
	Gate             *inference.AdmissionGate
	// Optional trusted host integrations. UI input must never populate these.
	HTTPClient *http.Client
	Changed    func()
}

type Job struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	Status         string    `json:"status"`
	CompletedBytes int64     `json:"completedBytes"`
	TotalBytes     int64     `json:"totalBytes"`
	Error          string    `json:"error,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Installed struct {
	ID             string    `json:"id"`
	ManifestDigest string    `json:"manifestDigest"`
	InstalledAt    time.Time `json:"installedAt"`
}

type Selection struct {
	Active   string `json:"active,omitempty"`
	Previous string `json:"previous,omitempty"`
}

type ProcessState struct {
	BuildID       string `json:"buildId"`
	ModelID       string `json:"modelId"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	Status        string `json:"status"`
	PID           int    `json:"pid,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Error         string `json:"error,omitempty"`
}

type Snapshot struct {
	Catalog          []Manifest                           `json:"catalog"`
	ModelCatalog     []ModelManifest                      `json:"modelCatalog"`
	Installed        []Installed                          `json:"installed"`
	Models           []Installed                          `json:"models"`
	Selections       map[inference.ProviderKind]Selection `json:"selections"`
	Jobs             []Job                                `json:"jobs"`
	Process          ProcessState                         `json:"process"`
	PlatformSupport  map[string]string                    `json:"platformSupport"`
	Qualifications   []QualificationReceipt               `json:"qualifications"`
	InferenceBlocked string                               `json:"inferenceBlocked,omitempty"`
}

type QualificationReceipt struct {
	ProtocolVersion       int                       `json:"protocolVersion"`
	BuildID               string                    `json:"buildId"`
	ModelID               string                    `json:"modelId"`
	ContextTokens         int                       `json:"contextTokens"`
	RuntimeManifestDigest string                    `json:"runtimeManifestDigest"`
	ModelManifestDigest   string                    `json:"modelManifestDigest"`
	ArtifactDigest        string                    `json:"artifactDigest"`
	TemplateDigest        string                    `json:"templateDigest,omitempty"`
	CheckedAt             time.Time                 `json:"checkedAt"`
	Status                string                    `json:"status"`
	Coding                string                    `json:"coding"`
	Checks                []string                  `json:"checks"`
	Generations           []QualificationGeneration `json:"generations,omitempty"`
}

type QualificationGeneration struct {
	Stage                   string                  `json:"stage"`
	ObservedDurationSeconds float64                 `json:"observedDurationSeconds"`
	FinishReason            string                  `json:"finishReason,omitempty"`
	TokenCountsPresent      bool                    `json:"tokenCountsPresent"`
	PromptTokens            *int                    `json:"promptTokens,omitempty"`
	CompletionTokens        *int                    `json:"completionTokens,omitempty"`
	Usage                   *inference.UsageDetails `json:"usage,omitempty"`
}

type StartRequest struct {
	BuildID       string `json:"buildId"`
	ModelID       string `json:"modelId"`
	ContextTokens int    `json:"contextTokens"`
}

// Connection contains the local secret and must not be returned in a UI
// snapshot or diagnostic export. It binds inference to a specific owned child.
type Connection struct {
	Config  inference.NativeConfig
	BuildID string
	ModelID string
}

type diskState struct {
	Version        int                                  `json:"version"`
	Installed      map[string]Installed                 `json:"installed"`
	Models         map[string]Installed                 `json:"models"`
	Selections     map[inference.ProviderKind]Selection `json:"selections"`
	Jobs           map[string]Job                       `json:"jobs"`
	Qualifications map[string]QualificationReceipt      `json:"qualifications"`
}

type Manager struct {
	mu                  sync.Mutex
	opMu                sync.Mutex
	config              Config
	root                string
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	lock                *os.File
	state               diskState
	catalog             map[string]Manifest
	presentationCatalog []Manifest
	platformSupport     map[string]string
	models              map[string]ModelManifest
	jobs                map[string]context.CancelFunc
	process             ProcessState
	child               *exec.Cmd
	childDone           chan struct{}
	childCleanupErr     error
	connection          Connection
	closed              bool
	probeCancel         context.CancelFunc
}
