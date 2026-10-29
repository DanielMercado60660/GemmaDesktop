package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// ListHarnessMetadata projects payloads inside SQLite, before Scan or Go JSON
// decoding. It intentionally shares List's row ordering and returns every
// metadata row; callers fetch full immutable detail with Get on demand.
// The kind and JSON paths are controlled here, never supplied as SQL fragments.
func (s *Store) ListHarnessMetadata(kind string) ([]Doc, error) {
	switch kind {
	case "experiment", "revision", "packet", "trial", "lesson", "evaluation", "promotion":
	default:
		return nil, errors.New("unknown harness collection")
	}
	rows, e := s.db.Query(`SELECT json_remove(body,
  '$.context','$.result','$.response','$.content','$.results','$.library','$.approvedWorkflow',
  '$.spec.agent','$.spec.frozenCapability','$.spec.frozenCapabilities',
  '$.spec.approvedSkills','$.spec.skills','$.spec.approvedWorkflows','$.spec.workflows',
  '$.spec.frozenAgents','$.spec.agents'),
  CASE WHEN kind='harness_evaluation' THEN COALESCE(json_array_length(body,'$.results'),0) END
  FROM entities WHERE kind=? ORDER BY updated`, "harness_"+kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		var body string
		var recorded sql.NullInt64
		if e = rows.Scan(&body, &recorded); e != nil {
			return nil, e
		}
		d := Doc{}
		if e = json.Unmarshal([]byte(body), &d); e != nil {
			return nil, e
		}
		if recorded.Valid {
			d["recordedResults"] = recorded.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
