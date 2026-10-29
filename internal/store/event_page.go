package store

import (
	"encoding/json"
	"errors"
)

// EventPage is a forward-only cursor over presentation events. Raw ADK
// checkpoints, reasoning tokens, and generation metrics remain in the journal,
// not in this view.
type EventPage struct {
	Events  []Doc `json:"events"`
	Cursor  int64 `json:"eventCursor"`
	HasMore bool  `json:"hasMore"`
}

func (s *Store) SessionEvents(sessionID string, after int64, limit int) (EventPage, error) {
	p := EventPage{Events: []Doc{}, Cursor: after}
	if after < 0 {
		return p, errors.New("Event cursor must be nonnegative")
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 2000 {
		limit = 2000
	}
	rows, err := s.db.Query(`SELECT e.seq,e.body FROM events e
		JOIN entities r ON r.kind='run' AND r.id=e.run_id
		WHERE json_extract(r.body,'$.sessionId')=? AND e.seq>?
		AND json_extract(e.body,'$.type') NOT IN ('adk_event','thinking','generation_metrics','token_accounting','context_prepared')
		ORDER BY e.seq LIMIT ?`, sessionID, after, limit+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var seq int64
		var body string
		if err = rows.Scan(&seq, &body); err != nil {
			return p, err
		}
		if len(p.Events) == limit || len(p.Events) > 0 && bytes+len(body) > 2<<20 {
			p.HasMore = true
			break
		}
		d := Doc{}
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return p, err
		}
		d["seq"] = seq
		p.Events = append(p.Events, d)
		p.Cursor = seq
		bytes += len(body)
	}
	return p, rows.Err()
}

const maxActivityBytes = 2 << 20

// Activity returns the newest dashboard tail, in chronological display order.
// It includes at most 500 events and approximately 2 MiB of stored JSON. The
// byte cap is soft only for one oversized newest event, which is kept whole;
// splitting it would lose JSON integrity and the latest diagnostic. Token
// streams and generation metrics are excluded. Older presentation history remains
// available through SessionEvents; raw metrics stay in the audit journal.
func (s *Store) Activity() ([]Doc, error) {
	rows, err := s.db.Query(`SELECT seq,body FROM events
		WHERE json_extract(body,'$.type') NOT IN ('adk_event','thinking','text','generation_metrics','token_accounting','context_prepared')
		ORDER BY seq DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	bytes := 0
	for rows.Next() {
		var seq int64
		var body string
		if err = rows.Scan(&seq, &body); err != nil {
			return nil, err
		}
		if len(out) > 0 && bytes+len(body) > maxActivityBytes {
			break
		}
		d := Doc{}
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return nil, err
		}
		d["seq"] = seq
		out = append(out, d)
		bytes += len(body)
		if bytes >= maxActivityBytes {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
