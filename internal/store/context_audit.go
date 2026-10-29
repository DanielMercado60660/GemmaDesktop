package store

import "encoding/json"

type ContextAudit struct {
	Events               []Doc `json:"events"`
	Truncated            bool  `json:"truncated"`
	TruncatedToolResults int   `json:"truncatedToolResults"`
}

// Read only bounded metadata events; long reasoning/text streams cannot push
// context diagnostics out of this filtered history.
func (s *Store) ContextAudit(runID string) (ContextAudit, error) {
	out := ContextAudit{Events: []Doc{}}
	rows, e := s.db.Query(`SELECT seq,body FROM events WHERE run_id=? AND json_extract(body,'$.type') IN ('context_prepared','generation_metrics','token_accounting','context_compacting','context_compacted','context_compaction_failed','context_restored') ORDER BY seq DESC LIMIT 1025`, runID)
	if e != nil {
		return out, e
	}
	bytes := 0
	for rows.Next() {
		var seq int64
		var raw string
		if e = rows.Scan(&seq, &raw); e != nil {
			rows.Close()
			return out, e
		}
		if len(out.Events) == 1024 || bytes+len(raw) > 2<<20 {
			out.Truncated = true
			break
		}
		bytes += len(raw)
		d := Doc{}
		if e = json.Unmarshal([]byte(raw), &d); e != nil {
			rows.Close()
			return out, e
		}
		d["seq"] = seq
		out.Events = append(out.Events, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i, j := 0, len(out.Events)-1; i < j; i, j = i+1, j-1 {
		out.Events[i], out.Events[j] = out.Events[j], out.Events[i]
	}
	e = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE run_id=? AND json_extract(body,'$.type') IN ('tool_completed','tool_failed') AND json_extract(body,'$.data.data.truncated')=1`, runID).Scan(&out.TruncatedToolResults)
	return out, e
}
