// Package harness owns immutable, Codex-guided experiments. It has no model
// provider or permission authority; all solver effects go through Config.Solve.
package harness

import (
	"context"
	"encoding/json"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"time"
)

const UpstreamRevision = "1cfe254e8d25b15f37048c3730134686abe60335"
const MethodVersion = "gemma-harness-v1"

type Doc = store.Doc
type Author struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}
type Content struct {
	Instructions     string   `json:"instructions"`
	ToolGuidance     string   `json:"toolGuidance"`
	Lessons          []string `json:"lessons"`
	SkillIDs         []string `json:"skillIds"`
	WorkflowID       string   `json:"workflowId,omitempty"`
	DelegateAgentIDs []string `json:"delegateAgentIds,omitempty"`
}
type Revision struct {
	ID               string            `json:"id"`
	Hash             string            `json:"hash"`
	ParentID         string            `json:"parentId,omitempty"`
	WorkspaceID      string            `json:"workspaceId"`
	AgentID          string            `json:"agentId"`
	Content          Content           `json:"content"`
	ApprovedWorkflow json.RawMessage   `json:"approvedWorkflow,omitempty"`
	DelegateDigests  map[string]string `json:"delegateDigests,omitempty"`
	AuthoredBy       Author            `json:"authoredBy"`
	CreatedAt        string            `json:"createdAt"`
}
type Spec struct {
	ID                  string                     `json:"id"`
	Mode                string                     `json:"mode"`
	WorkspaceID         string                     `json:"workspaceId"`
	AgentID             string                     `json:"agentId"`
	TaskIDs             []string                   `json:"taskIds"`
	BaseRevisionID      string                     `json:"baseRevisionId"`
	Model               string                     `json:"model"`
	ModelDigest         string                     `json:"modelDigest"`
	RuntimeVersion      string                     `json:"runtimeVersion"`
	Agent               Doc                        `json:"agent"`
	ContextSize         int                        `json:"contextSize"`
	FrozenCapability    json.RawMessage            `json:"frozenCapability"`
	ExecutorIdentity    string                     `json:"executorIdentity"`
	ToolchainVersion    string                     `json:"toolchainVersion"`
	GraderVersion       string                     `json:"graderVersion"`
	FixtureDigest       string                     `json:"fixtureDigest"`
	AppVersion          string                     `json:"appVersion"`
	SolverTools         []string                   `json:"solverTools"`
	ApprovedSkills      map[string]json.RawMessage `json:"approvedSkills"`
	ApprovedWorkflows   map[string]json.RawMessage `json:"approvedWorkflows,omitempty"`
	FrozenAgents        map[string]json.RawMessage `json:"frozenAgents,omitempty"`
	ApprovedDelegateIDs []string                   `json:"approvedDelegateIds,omitempty"`
	PythonIdentity      string                     `json:"pythonIdentity,omitempty"`
	UpstreamAdapterHash string                     `json:"upstreamAdapterHash,omitempty"`
	BudgetSeconds       int                        `json:"budgetSeconds"`
	CreatedAt           string                     `json:"createdAt"`
	Hash                string                     `json:"hash"`
}
type Task struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Stack          string            `json:"stack"`
	Split          string            `json:"split"`
	Prompt         string            `json:"prompt"`
	Public         map[string]string `json:"public,omitempty"`
	PublicHash     string            `json:"publicHash"`
	Command        []string          `json:"command"`
	GradeCommand   []string          `json:"gradeCommand"`
	Cases          []Case            `json:"-"`
	GraderHash     string            `json:"graderHash"`
	Probe          *Probe            `json:"-"`
	ProtectedFiles []string          `json:"protectedFiles"`
}
type Probe struct {
	Files    map[string]string `json:"files"`
	Command  []string          `json:"command"`
	Expected json.RawMessage   `json:"expected"`
	Name     string            `json:"name"`
}
type Case struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
	Critical bool            `json:"critical"`
}
type Event struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Data   any    `json:"data,omitempty"`
}
type ToolObservation struct {
	Artifacts     []Doc  `json:"artifacts,omitempty"`
	CallID        string `json:"callId"`
	Tool          string `json:"tool"`
	Arguments     Doc    `json:"arguments"`
	Content       string `json:"content"`
	Data          Doc    `json:"data,omitempty"`
	Error         string `json:"error,omitempty"`
	Sequence      int    `json:"sequence"`
	StartSequence int    `json:"startSequence"`
}
type SolveRequest struct {
	TrialID  string   `json:"trialId"`
	Root     string   `json:"root"`
	Prompt   string   `json:"prompt"`
	Revision Revision `json:"revision"`
	Spec     Spec     `json:"spec"`
	Task     Task     `json:"task"`
}
type SolveResult struct {
	Content         string                     `json:"content"`
	Incomplete      bool                       `json:"incomplete"`
	Usage           Doc                        `json:"usage"`
	TokenAccounting *inference.TokenAccounting `json:"tokenAccounting,omitempty"`
	ToolCalls       []ToolObservation          `json:"toolCalls"`
	DurationMS      int64                      `json:"durationMs"`
}
type Trial struct {
	ID           string      `json:"id"`
	ExperimentID string      `json:"experimentId"`
	TaskID       string      `json:"taskId"`
	Label        string      `json:"label"`
	RevisionID   string      `json:"revisionId"`
	InputHash    string      `json:"inputHash"`
	Status       string      `json:"status"`
	Root         string      `json:"-"`
	OutputHash   string      `json:"outputHash,omitempty"`
	CreatedAt    string      `json:"createdAt"`
	FinishedAt   string      `json:"finishedAt,omitempty"`
	Result       SolveResult `json:"result"`
	Error        string      `json:"error,omitempty"`
	Progress     Doc         `json:"progress,omitempty"`
}
type Packet struct {
	ID             string            `json:"id"`
	ExperimentID   string            `json:"experimentId"`
	TaskID         string            `json:"taskId"`
	Stage          string            `json:"stage"`
	Status         string            `json:"status"`
	RequestHash    string            `json:"requestHash"`
	CreatedAt      string            `json:"createdAt"`
	BaseRevisionID string            `json:"baseRevisionId"`
	Mission        string            `json:"mission"`
	Evidence       []string          `json:"evidence"`
	Context        map[string]string `json:"context,omitempty"`
	Library        []Lesson          `json:"library,omitempty"`
	Response       *PacketResponse   `json:"response,omitempty"`
	RevisionID     string            `json:"revisionId,omitempty"`
}
type PacketResponse struct {
	ID          string        `json:"id"`
	RequestHash string        `json:"requestHash"`
	AuthoredBy  Author        `json:"authoredBy"`
	Content     *Content      `json:"content,omitempty"`
	Notes       string        `json:"notes"`
	Verdict     string        `json:"verdict,omitempty"`
	Lessons     []LessonInput `json:"lessons,omitempty"`
	Evidence    []string      `json:"evidence,omitempty"`
	CreatedAt   string        `json:"createdAt,omitempty"`
}
type LessonInput struct {
	Scope          string   `json:"scope,omitempty"`
	Contradictions []string `json:"contradictions,omitempty"`
	Cue            string   `json:"cue"`
	Guidance       string   `json:"guidance"`
	Evidence       []string `json:"evidence"`
}
type Lesson struct {
	VersionID         string   `json:"versionId"`
	PreviousVersionID string   `json:"previousVersionId,omitempty"`
	Version           int      `json:"version"`
	Scope             string   `json:"scope"`
	Compatibility     Doc      `json:"compatibility"`
	Contradictions    []string `json:"contradictions,omitempty"`
	ID                string   `json:"id"`
	ExperimentID      string   `json:"experimentId"`
	WorkspaceID       string   `json:"workspaceId"`
	AgentID           string   `json:"agentId"`
	TaskID            string   `json:"taskId"`
	RevisionID        string   `json:"revisionId"`
	Status            string   `json:"status"`
	Cue               string   `json:"cue"`
	Guidance          string   `json:"guidance"`
	Evidence          []string `json:"evidence"`
	AuthoredBy        Author   `json:"authoredBy"`
	CreatedAt         string   `json:"createdAt"`
}
type Experiment struct {
	ID                string   `json:"id"`
	Spec              Spec     `json:"spec"`
	Status            string   `json:"status"`
	TaskIndex         int      `json:"taskIndex"`
	TrialIDs          []string `json:"trialIds"`
	PacketIDs         []string `json:"packetIds"`
	CandidateID       string   `json:"candidateId,omitempty"`
	Error             string   `json:"error,omitempty"`
	CreatedAt         string   `json:"createdAt"`
	FinishedAt        string   `json:"finishedAt,omitempty"`
	Rollouts          int      `json:"rollouts"`
	ImprovementStages int      `json:"improvementStages"`
}
type Check struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Critical bool   `json:"critical"`
	Error    string `json:"error,omitempty"`
}
type Grade struct {
	TrialID    string  `json:"trialId"`
	Passed     bool    `json:"passed"`
	Checks     []Check `json:"checks"`
	GraderHash string  `json:"graderHash"`
	Error      string  `json:"error,omitempty"`
}
type EvalResult struct {
	TaskID          string                     `json:"taskId"`
	Split           string                     `json:"split"`
	Repeat          int                        `json:"repeat"`
	Arm             string                     `json:"arm"`
	TrialID         string                     `json:"trialId"`
	Grade           Grade                      `json:"grade"`
	DurationMS      int64                      `json:"durationMs"`
	Tokens          int64                      `json:"tokens"`
	TokenAccounting *inference.TokenAccounting `json:"tokenAccounting,omitempty"`
}
type Evaluation struct {
	ID              string       `json:"id"`
	ExperimentID    string       `json:"experimentId"`
	CandidateID     string       `json:"candidateId"`
	BaselineID      string       `json:"baselineId"`
	Status          string       `json:"status"`
	CreatedAt       string       `json:"createdAt"`
	FinishedAt      string       `json:"finishedAt,omitempty"`
	Results         []EvalResult `json:"results"`
	ExpectedResults int          `json:"expectedResults,omitempty"`
	Gate            Gate         `json:"gate"`
	Error           string       `json:"error,omitempty"`
	ReviewPacketID  string       `json:"reviewPacketId,omitempty"`
}
type Gate struct {
	CostAccountingStatus string   `json:"costAccountingStatus,omitempty"`
	CostComparisonValid  bool     `json:"costComparisonValid"`
	Eligible             bool     `json:"eligible"`
	Reasons              []string `json:"reasons"`
	BaselineSuccesses    int      `json:"baselineSuccesses"`
	CandidateSuccesses   int      `json:"candidateSuccesses"`
	TimeRatio            float64  `json:"timeRatio"`
	TokenRatio           float64  `json:"tokenRatio"`
	EvidenceHash         string   `json:"evidenceHash"`
}
type Promotion struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	WorkspaceID  string `json:"workspaceId"`
	RevisionID   string `json:"revisionId"`
	PreviousID   string `json:"previousId"`
	EvaluationID string `json:"evaluationId"`
	EvidenceHash string `json:"evidenceHash"`
	CreatedAt    string `json:"createdAt"`
	Kind         string `json:"kind"`
}
type Repository interface {
	ListHarnessMetadata(string) ([]Doc, error)
	Get(string, string) (Doc, error)
	Put(string, Doc) error
	List(string) ([]Doc, error)
}
type Config struct {
	Root                string
	Store               Repository
	Tasks               []Task
	Solve               func(context.Context, SolveRequest, func(Event)) (SolveResult, error)
	Grade               func(context.Context, Task, Trial) (Grade, error)
	ValidateEnvironment func(context.Context, Spec) error
	Activate            func(context.Context, string, string, string, string) error
	Upstream            func(context.Context, *Manager, Experiment) error
	Now                 func() time.Time
}
