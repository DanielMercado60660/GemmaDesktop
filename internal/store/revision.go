package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Revise atomically compares a document revision and records its replacement.
func (s *Store) Revise(kind, id string, expected int, d Doc) (Doc, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var raw string
	e = tx.QueryRow("SELECT body FROM entities WHERE kind=? AND id=?", kind, id).Scan(&raw)
	revision := 0
	if e == nil {
		old := Doc{}
		if e = json.Unmarshal([]byte(raw), &old); e != nil {
			return nil, e
		}
		if v, ok := old["revision"].(float64); ok {
			revision = int(v)
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if revision != expected {
		return nil, fmt.Errorf("revision conflict: expected %d, current %d", expected, revision)
	}
	out := Doc{}
	for k, v := range d {
		out[k] = v
	}
	out["id"] = id
	out["revision"] = revision + 1
	b, e := json.Marshal(out)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec("INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body,updated=excluded.updated", kind, id, string(b), Now()); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// PublishVersion commits an immutable revision and its editable head together.
func (s *Store) PublishVersion(kind, id string, expected int, d Doc) (Doc, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var raw string
	revision := 0
	e = tx.QueryRow("SELECT body FROM entities WHERE kind=? AND id=?", kind, id).Scan(&raw)
	if e == nil {
		old := Doc{}
		if e = json.Unmarshal([]byte(raw), &old); e != nil {
			return nil, e
		}
		if v, ok := old["revision"].(float64); ok {
			revision = int(v)
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if revision != expected {
		return nil, fmt.Errorf("revision conflict: expected %d, current %d", expected, revision)
	}
	d["id"] = id
	d["revision"] = revision + 1
	b, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec("INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?)", kind+"_revision", fmt.Sprintf("%s:%d", id, revision+1), string(b), Now()); e != nil {
		return nil, e
	}
	if _, e = tx.Exec("INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body,updated=excluded.updated", kind, id, string(b), Now()); e != nil {
		return nil, e
	}
	return d, tx.Commit()
}

// CreateOnce claims a durable operation identity without overwriting a prior
// outcome. Concurrent duplicate admissions cannot both begin execution.
func (s *Store) CreateOnce(kind, id string, d Doc) error {
	out := Doc{}
	for k, v := range d {
		out[k] = v
	}
	out["id"] = id
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?)", kind, id, string(raw), Now())
	return err
}
