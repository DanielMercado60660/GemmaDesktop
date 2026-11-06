package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func (s *Store) migrateV2() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE INDEX entities_session ON entities(kind,json_extract(body,'$.sessionId'),updated,id)`,
		`CREATE INDEX entities_workspace ON entities(kind,json_extract(body,'$.workspaceId'),updated,id)`,
		`CREATE TABLE entity_changes(seq INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,id TEXT NOT NULL)`,
		`CREATE INDEX entity_changes_identity ON entity_changes(kind,id,seq)`,
		`CREATE TRIGGER entities_insert_change AFTER INSERT ON entities BEGIN INSERT INTO entity_changes(kind,id) VALUES(NEW.kind,NEW.id); END`,
		`CREATE TRIGGER entities_update_change AFTER UPDATE ON entities BEGIN INSERT INTO entity_changes(kind,id) VALUES(NEW.kind,NEW.id); END`,
		`CREATE TRIGGER entities_delete_change AFTER DELETE ON entities BEGIN INSERT INTO entity_changes(kind,id) VALUES(OLD.kind,OLD.id); END`,
		`CREATE TRIGGER changes_bound AFTER INSERT ON entity_changes WHEN NEW.seq % 256 = 0 BEGIN DELETE FROM entity_changes WHERE seq < NEW.seq - 10000; END`,
		`PRAGMA user_version=2`,
	} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListScoped filters before decoding JSON. The field is an allowlist, never SQL
// supplied by a caller. This preserves chronological history without reading
// unrelated conversation bodies.
func (s *Store) ListScoped(kind, field, value string) ([]Doc, error) {
	if field != "sessionId" && field != "workspaceId" {
		return nil, errors.New("unsupported entity scope")
	}
	rows, err := s.db.Query(`SELECT body FROM entities WHERE kind=? AND json_extract(body,'$.`+field+`')=? ORDER BY updated,id`, kind, value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var d Doc
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Mutate serializes a read/validate/write in one database transaction, including
// creation. The callback must not call Store methods (the transaction owns the
// single connection). A nil result leaves the record unchanged.
func (s *Store) Mutate(kind, id string, fn func(Doc) (Doc, error)) (Doc, error) {
	if id == "" {
		return nil, errors.New("entity id is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var body string
	err = tx.QueryRow(`SELECT body FROM entities WHERE kind=? AND id=?`, kind, id).Scan(&body)
	var previous Doc
	if err == nil {
		if err = json.Unmarshal([]byte(body), &previous); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	next, err := fn(previous)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return previous, tx.Commit()
	}
	if String(next, "id") != id {
		return nil, errors.New("mutation cannot change entity identity")
	}
	b, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body,updated=excluded.updated`, kind, id, string(b), Now())
	if err != nil {
		return nil, err
	}
	return next, tx.Commit()
}

type EntityChange struct {
	Sequence int64  `json:"sequence"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Deleted  bool   `json:"deleted"`
	Entity   Doc    `json:"entity,omitempty"`
}
type ChangePage struct {
	Changes []EntityChange `json:"changes"`
	Cursor  int64          `json:"cursor"`
	HasMore bool           `json:"hasMore"`
	Resync  bool           `json:"resync"`
}

func (s *Store) EntityCursor() (int64, error) {
	var cursor int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM entity_changes`).Scan(&cursor)
	return cursor, err
}

// EntityChanges returns current values for changed identities, including
// deletions. Its cursor is independent of the display-event journal. Only
// presentation entities are returned; secret/checkpoint records never enter it.
func (s *Store) EntityChanges(after int64, limit int) (ChangePage, error) {
	p := ChangePage{Changes: []EntityChange{}, Cursor: after}
	if after < 0 {
		return p, errors.New("cursor must be nonnegative")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	tx, err := s.db.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var earliest, latest int64
	if err := tx.QueryRow(`SELECT COALESCE(MIN(seq),0),COALESCE(MAX(seq),0) FROM entity_changes`).Scan(&earliest, &latest); err != nil {
		return p, err
	}
	if after > latest || earliest > 0 && after < earliest-1 {
		p.Resync = true
		return p, nil
	}
	rows, err := tx.Query(`SELECT c.seq,c.kind,c.id,e.body FROM entity_changes c LEFT JOIN entities e ON e.kind=c.kind AND e.id=c.id AND e.kind IN ('workspace','session','run','agent','workflow','approval','settings') WHERE c.seq>? ORDER BY c.seq LIMIT ?`, after, limit+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	n, bytes := 0, 0
	for rows.Next() {
		var c EntityChange
		var body sql.NullString
		if err = rows.Scan(&c.Sequence, &c.Kind, &c.ID, &body); err != nil {
			return p, err
		}
		if n == limit || body.Valid && len(p.Changes) > 0 && bytes+len(body.String) > 2<<20 {
			p.HasMore = true
			break
		}
		n++
		p.Cursor = c.Sequence
		switch c.Kind {
		case "workspace", "session", "run", "agent", "workflow", "approval", "settings":
		case "artifact", "skill", "mcp", "experiment", "capability", "taskplan", "delegation":
			p.Resync = true
			continue
		default:
			continue
		}
		c.Deleted = !body.Valid
		if body.Valid {
			bytes += len(body.String)
			if err = json.Unmarshal([]byte(body.String), &c.Entity); err != nil {
				return p, fmt.Errorf("decode change: %w", err)
			}
		}
		p.Changes = append(p.Changes, c)
	}
	return p, rows.Err()
}

// Includes the item count so deletion of any message invalidates the view.
// Sequence changes detect edits even if the wall clock moves backwards.
func (s *Store) SessionMessageCursor(sessionID string) (string, error) {
	var count, revision int64
	err := s.db.QueryRow(`SELECT COUNT(DISTINCT e.id),COALESCE(MAX(c.seq),0) FROM entities e LEFT JOIN entity_changes c ON c.kind=e.kind AND c.id=e.id WHERE e.kind='message' AND json_extract(e.body,'$.sessionId')=?`, sessionID).Scan(&count, &revision)
	return fmt.Sprintf("%d:%d", count, revision), err
}
