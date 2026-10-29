package app

import (
	"encoding/json"
	"math"

	"gemmadesktop.local/app/internal/harness"
)

const (
	harnessObservationTextLimit     = 32768
	harnessObservationMetadataLimit = 16 << 10
	harnessObservationChangesLimit  = 8
)

// Bound only the durable observation, never the result returned to the model.
// Process-output truncation (data.truncated) and observation truncation are
// separate facts: a long, complete output can still need a shorter audit view.
func boundHarnessObservation(entry harness.ToolObservation) harness.ToolObservation {
	if len(entry.Content) <= harnessObservationTextLimit {
		return entry
	}
	entry.Content = entry.Content[:harnessObservationTextLimit] + "\n[Observation truncated]"
	if entry.Tool == "run_command" {
		entry.Data = harnessCommandObservationMetadata(entry.Data)
	} else {
		entry.Data = nil
	}
	return entry
}

// Input is the JSON-normalized tools.Manager result. Retain only a fixed
// metadata allowlist; arbitrary stdout/stderr/body maps never enter this view.
// Oversized identities are omitted explicitly, not clipped into false IDs.
func harnessCommandObservationMetadata(data harness.Doc) harness.Doc {
	out := harness.Doc{"observationDataTruncated": true, "observationPayloadOmitted": true}
	omitted := []string{}
	seen := 0
	put := func(key string, value any) bool {
		out[key] = value
		b, err := json.Marshal(out)
		// Reserve space for fixed omission flags/counts added after projection.
		if err != nil || len(b) > harnessObservationMetadataLimit-1024 {
			delete(out, key)
			return false
		}
		return true
	}
	for _, key := range []string{"readOnly", "cancelled", "truncated", "workspaceChanged", "changesLimited"} {
		if value, exists := data[key]; exists {
			seen++
			if flag, ok := value.(bool); !ok || !put(key, flag) {
				omitted = append(omitted, key)
			}
		}
	}
	for _, key := range []string{"exitCode", "durationMs", "changeCount"} {
		if value, exists := data[key]; exists {
			seen++
			if n, ok := value.(float64); !ok || math.IsNaN(n) || math.IsInf(n, 0) || !put(key, n) {
				omitted = append(omitted, key)
			}
		}
	}
	for _, field := range []struct {
		key   string
		limit int
	}{
		{"receiptId", 1024}, {"commandId", 256}, {"image", 512},
		{"snapshotDigest", 128}, {"dependencyDigest", 128}, {"publication", 128},
		{"signal", 128}, {"workingDirectory", 1024}, {"environment", 512}, {"journalPath", 2048},
	} {
		if value, exists := data[field.key]; exists {
			seen++
			text, ok := value.(string)
			if !ok || len(text) > field.limit || !put(field.key, text) {
				omitted = append(omitted, field.key)
			}
		}
	}
	if raw, exists := data["changes"]; exists {
		seen++
		changes, ok := raw.([]any)
		if ok || raw == nil {
			total := float64(len(changes))
			if n, valid := out["changeCount"].(float64); valid && n >= total && math.Trunc(n) == n {
				total = n
			} else if _, exists := data["changeCount"]; !exists {
				out["changeCount"] = total
			}
			kept := []any{}
			for _, rawChange := range changes[:min(len(changes), harnessObservationChangesLimit)] {
				change, ok := rawChange.(map[string]any)
				path, pathOK := change["path"].(string)
				if !ok || !pathOK || len(path) > 1024 {
					continue
				}
				summary := map[string]any{"path": path}
				complete := true
				for _, key := range []string{"beforeHash", "afterHash"} {
					if value, exists := change[key]; exists {
						text, valid := value.(string)
						if !valid || len(text) > 128 {
							complete = false
							break
						}
						summary[key] = text
					}
				}
				if !complete {
					continue
				}
				if n, ok := change["size"].(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) {
					summary["size"] = n
				}
				if deleted, ok := change["deleted"].(bool); ok {
					summary["deleted"] = deleted
				}
				candidate := append(kept, summary)
				if put("changes", candidate) {
					kept = candidate
				}
			}
			out["changes"] = kept
			out["observationChangesAvailable"] = len(changes)
			out["observationChangesRetained"] = len(kept)
			out["observationChangesOmitted"] = total - float64(len(kept))
		} else {
			omitted = append(omitted, "changes")
		}
	}
	payloadFields := []string{}
	for _, key := range []string{"stdout", "stderr", "content", "body"} {
		if _, exists := data[key]; exists {
			seen++
			payloadFields = append(payloadFields, key)
		}
	}
	out["observationOmittedPayloadFields"] = payloadFields
	out["observationOmittedMetadataFields"] = omitted
	out["observationUnknownFieldsOmitted"] = len(data) > seen
	return out
}
