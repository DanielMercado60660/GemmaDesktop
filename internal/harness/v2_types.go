package harness

// V2 records are additive. Their hashes, schedules and gate policy never change
// the interpretation of the original Harness campaign.
const ReliabilityPolicyV2 = "reliability-v2"
const ColdEachAttemptV2 = "cold_each_attempt"

type GradeRequest struct {
	Spec  Spec
	Task  Task
	Trial Trial
}

type FixturePack struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
	GraderPolicy string `json:"graderPolicy"`
	Tasks        []Task `json:"-"`
}

type ArmV2 struct {
	ID       string   `json:"id"`
	Hash     string   `json:"hash"`
	Name     string   `json:"name"`
	Spec     Spec     `json:"spec"`
	Revision Revision `json:"revision"`
	// Provenance identifies the completed development experiment, or a frozen
	// current-product baseline. It does not grant permissions.
	Provenance string `json:"provenance"`
	CreatedAt  string `json:"createdAt"`
}

type DiagnosticRequest struct {
	PackID  string   `json:"packId"`
	TaskIDs []string `json:"taskIds"`
	ArmIDs  []string `json:"armIds"`
	Repeats int      `json:"repeats"`
}

type EvaluationV2Request struct {
	PackID         string `json:"packId"`
	BaselineArmID  string `json:"baselineArmId"`
	CandidateArmID string `json:"candidateArmId"`
}

type ScheduledAttemptV2 struct {
	Index  int    `json:"index"`
	TaskID string `json:"taskId"`
	Split  string `json:"split"`
	ArmID  string `json:"armId"`
	Arm    string `json:"arm"`
	Repeat int    `json:"repeat"`
}

type ResultV2 struct {
	ScheduledAttemptV2
	TrialID string `json:"trialId"`
	Grade   Grade  `json:"grade"`
	// Infrastructure failures invalidate comparison evidence in either arm.
	// A model's failed checks remain ordinary observed attempt outcomes.
	InfrastructureError string `json:"infrastructureError,omitempty"`
	TrialStatus         string `json:"trialStatus"`
	DurationMS          int64  `json:"durationMs"`
	Tokens              int64  `json:"tokens"`
	AccountingComplete  bool   `json:"accountingComplete"`
	OutputHash          string `json:"outputHash"`
}

type ReviewV2 struct {
	Author       Author `json:"author"`
	Verdict      string `json:"verdict"`
	EvidenceHash string `json:"evidenceHash"`
	Notes        string `json:"notes"`
	CreatedAt    string `json:"createdAt"`
}

type GateV2 struct {
	Policy             string   `json:"policy"`
	Eligible           bool     `json:"eligible"`
	Reasons            []string `json:"reasons"`
	BaselineSuccesses  int      `json:"baselineSuccesses"`
	CandidateSuccesses int      `json:"candidateSuccesses"`
	EvidenceHash       string   `json:"evidenceHash"`
}

type RunV2 struct {
	ID           string               `json:"id"`
	Kind         string               `json:"kind"`
	PackID       string               `json:"packId"`
	PackDigest   string               `json:"packDigest"`
	ArmIDs       []string             `json:"armIds"`
	Schedule     []ScheduledAttemptV2 `json:"schedule"`
	ScheduleHash string               `json:"scheduleHash"`
	Status       string               `json:"status"`
	Cursor       int                  `json:"cursor"`
	Results      []ResultV2           `json:"results"`
	BatchIDs     []string             `json:"batchIds"`
	Gate         GateV2               `json:"gate"`
	Review       *ReviewV2            `json:"review,omitempty"`
	Error        string               `json:"error,omitempty"`
	CreatedAt    string               `json:"createdAt"`
	FinishedAt   string               `json:"finishedAt,omitempty"`
}

type BatchLimits struct {
	// WallSeconds cannot exceed two hours. Attempts reserve the full envelope,
	// including grading and cleanup, before they may begin.
	WallSeconds int `json:"wallSeconds"`
	MaxAttempts int `json:"maxAttempts"`
}

type BatchV2 struct {
	ID           string      `json:"id"`
	RunID        string      `json:"runId"`
	ScheduleHash string      `json:"scheduleHash"`
	StartCursor  int         `json:"startCursor"`
	NextCursor   int         `json:"nextCursor"`
	Limits       BatchLimits `json:"limits"`
	Status       string      `json:"status"`
	StopReason   string      `json:"stopReason"`
	StartedAt    string      `json:"startedAt"`
	FinishedAt   string      `json:"finishedAt,omitempty"`
}
