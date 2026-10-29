// Package media owns bounded, local sampled-frame video retrieval. It never
// exposes model tools, remote media, or implicit model downloads.
package media

import (
	"context"
	"gemmadesktop.local/app/internal/inference"
	"os"
	"time"
)

const EmbeddingModel = "embeddinggemma-2:440m-mxfp8-text-vision"
const MaxSourceBytes int64 = 256 << 20
const preprocessing = "visual-cfr-h264-v1;sample=1Hz-original-PTS;maxedge=768;jpeg=q3;dimensions=768"

type Config struct {
	DataDir        string
	WorkspacePath  func(string) (string, error)
	Client         func() *inference.Client
	EmbeddingModel string
	Answer         func(context.Context, AnswerRequest) (string, error)
	Emit           func(Event)
}
type Event struct {
	JobID       string `json:"jobId"`
	ClipID      string `json:"clipId"`
	WorkspaceID string `json:"workspaceId"`
	Type        string `json:"type"`
	Detail      string `json:"detail"`
}
type AnswerRequest struct {
	Model  string
	Prompt string
	Images []string
}
type StartResult struct {
	ClipID   string `json:"clipId"`
	JobID    string `json:"jobId"`
	AnswerID string `json:"answerId,omitempty"`
}
type Clip struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspaceId"`
	Name            string  `json:"name"`
	SHA256          string  `json:"sha256"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"durationSeconds"`
	Status          string  `json:"status"`
	FrameCount      int     `json:"frameCount"`
	EmbeddingModel  string  `json:"embeddingModel"`
	CreatedAt       string  `json:"createdAt"`
	Error           string  `json:"error,omitempty"`
	IndexID         string  `json:"indexId,omitempty"`
}
type Job struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	ClipID      string `json:"clipId"`
	AnswerID    string `json:"answerId,omitempty"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Stage       string `json:"stage"`
	Completed   int    `json:"completed"`
	Total       int    `json:"total"`
	Error       string `json:"error,omitempty"`
	CreatedAt   string `json:"createdAt"`
	FinishedAt  string `json:"finishedAt,omitempty"`
}
type Evidence struct {
	FrameID     string  `json:"frameId"`
	TimeSeconds float64 `json:"timeSeconds"`
	Score       float64 `json:"score"`
	SHA256      string  `json:"sha256"`
	Label       string  `json:"label"`
}
type Citation struct {
	FrameID     string  `json:"frameId"`
	TimeSeconds float64 `json:"timeSeconds"`
	Label       string  `json:"label"`
}
type Answer struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspaceId"`
	ClipID         string     `json:"clipId"`
	JobID          string     `json:"jobId"`
	Question       string     `json:"question"`
	Text           string     `json:"text"`
	Model          string     `json:"model"`
	ModelDigest    string     `json:"modelDigest,omitempty"`
	RuntimeVersion string     `json:"runtimeVersion,omitempty"`
	IndexID        string     `json:"indexId"`
	Status         string     `json:"status"`
	Error          string     `json:"error,omitempty"`
	Evidence       []Evidence `json:"evidence"`
	Citations      []Citation `json:"citations"`
	CreatedAt      string     `json:"createdAt"`
}
type ClipDetail struct {
	Clip          Clip     `json:"clip"`
	Jobs          []Job    `json:"jobs"`
	RecentAnswers []Answer `json:"recentAnswers"`
}
type Asset struct {
	File        *os.File
	Name        string
	ContentType string
	SHA256      string
	Size        int64
	ModTime     time.Time
}
type sample struct {
	Ordinal     int     `json:"ordinal"`
	PTS         int64   `json:"pts"`
	TimeSeconds float64 `json:"timeSeconds"`
}
type manifest struct {
	ProbeIdentity string   `json:"probeIdentity"`
	TimeBaseNum   int64    `json:"timeBaseNum"`
	TimeBaseDen   int64    `json:"timeBaseDen"`
	Duration      float64  `json:"duration"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Samples       []sample `json:"samples"`
}
type frame struct {
	ID      string
	IndexID string
	Ordinal int
	PTS     int64
	Seconds float64
	SHA256  string
	Path    string
	Vector  []float32
}
