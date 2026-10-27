// Package store owns the durable application journal. Model-generated data never
// becomes executable during recovery; unfinished operations remain interrupted.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Doc map[string]any

var ErrNotFound = errors.New("entity not found")

type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	Path    string
	Version string
}

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Now() string                   { return time.Now().UTC().Format(time.RFC3339Nano) }
func String(d Doc, k string) string { v, _ := d[k].(string); return v }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Path: path}
	if err = db.QueryRow("SELECT sqlite_version()").Scan(&s.Version); err != nil {
		db.Close()
		return nil, err
	}
	if !walVersionSupported(s.Version) {
		db.Close()
		return nil, fmt.Errorf("SQLite %s is not qualified for WAL; require 3.51.3 or newer", s.Version)
	}
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version > 1 {
		db.Close()
		return nil, fmt.Errorf("database schema %d is newer than this application", version)
	}
	if version == 0 {
		tx, e := db.Begin()
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		for _, q := range []string{
			`CREATE TABLE entities(kind TEXT NOT NULL,id TEXT NOT NULL,body TEXT NOT NULL,updated TEXT NOT NULL,PRIMARY KEY(kind,id))`,
			`CREATE TABLE events(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,run_id TEXT NOT NULL,body TEXT NOT NULL)`,
			`CREATE INDEX events_run ON events(run_id,seq)`,
			`PRAGMA user_version=1`} {
			if _, e = tx.Exec(q); e != nil {
				db.Close()
				return nil, e
			}
		}
		if e = tx.Commit(); e != nil {
			db.Close()
			return nil, e
		}
	}
	if err = os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func walVersionSupported(version string) bool {
	var major, minor, patch int
	n, e := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	return e == nil && n == 3 && major == 3 && (minor > 51 || minor == 51 && patch >= 3)
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(kind string, d Doc) error {
	id := String(d, "id")
	if id == "" {
		return errors.New("entity id is required")
	}
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(`INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body,updated=excluded.updated`, kind, id, string(b), Now())
	return e
}
func (s *Store) Get(kind, id string) (Doc, error) {
	var b string
	e := s.db.QueryRow(`SELECT body FROM entities WHERE kind=? AND id=?`, kind, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
	}
	if e != nil {
		return nil, e
	}
	d := Doc{}
	e = json.Unmarshal([]byte(b), &d)
	return d, e
}
func (s *Store) List(kind string) ([]Doc, error) {
	rows, e := s.db.Query(`SELECT body FROM entities WHERE kind=? ORDER BY updated`, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		d := Doc{}
		if e = json.Unmarshal([]byte(b), &d); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) Delete(kind, id string) error {
	_, e := s.db.Exec(`DELETE FROM entities WHERE kind=? AND id=?`, kind, id)
	return e
}
func (s *Store) Update(kind, id string, fn func(Doc) error) (Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, e := s.Get(kind, id)
	if e != nil {
		return nil, e
	}
	if e = fn(d); e != nil {
		return nil, e
	}
	return d, s.Put(kind, d)
}
func (s *Store) Event(d Doc) (Doc, error) {
	// The journal retains bounded display payloads; authoritative complete tool
	// results and ADK checkpoints live in their separate records.
	if detail, ok := d["detail"].(string); ok && len(detail) > 32768 {
		d["detail"] = strings.ToValidUTF8(detail[:32768], "�") + "\n[Display truncated; inspect the recorded tool result or artifact.]"
	}
	if data, err := json.Marshal(d["data"]); err == nil && len(data) > 32768 {
		d["data"] = Doc{"omittedBytes": len(data), "note": "Large payload retained in the tool/checkpoint record"}
	}
	if d["id"] == nil {
		d["id"] = ID()
	}
	if d["createdAt"] == nil {
		d["createdAt"] = Now()
	}
	b, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	r, e := s.db.Exec(`INSERT INTO events(id,run_id,body) VALUES(?,?,?)`, d["id"], String(d, "runId"), string(b))
	if e != nil {
		return nil, e
	}
	seq, e := r.LastInsertId()
	d["seq"] = seq
	return d, e
}
func (s *Store) Events(runID string, after int64) ([]Doc, error) {
	rows, e := s.db.Query(`SELECT seq,body FROM (SELECT seq,body FROM events WHERE (?='' OR run_id=?) AND seq>? ORDER BY seq DESC LIMIT 3000) ORDER BY seq`, runID, runID, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		var seq int64
		var b string
		if e = rows.Scan(&seq, &b); e != nil {
			return nil, e
		}
		d := Doc{}
		if e = json.Unmarshal([]byte(b), &d); e != nil {
			return nil, e
		}
		d["seq"] = seq
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) Recover() error {
	runs, e := s.List("run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		switch String(r, "status") {
		case "queued", "running", "waiting_approval", "cancelling":
			r["status"] = "interrupted"
			r["finishedAt"] = Now()
			r["error"] = "The application stopped before a final outcome was recorded. Inspect files and tool history before starting another run; external effects were not replayed."
			if e = s.Put("run", r); e != nil {
				return e
			}
			if _, e = s.Event(Doc{"runId": r["id"], "type": "status", "title": "Run interrupted", "detail": r["error"]}); e != nil {
				return e
			}
		}
	}
	approvals, e := s.List("approval")
	if e != nil {
		return e
	}
	for _, a := range approvals {
		if String(a, "status") == "pending" {
			a["status"] = "expired"
			if e = s.Put("approval", a); e != nil {
				return e
			}
		}
	}
	intents, e := s.List("toolcall")
	if e != nil {
		return e
	}
	for _, intent := range intents {
		if String(intent, "status") == "started" {
			intent["status"] = "outcome_unknown"
			if e = s.Put("toolcall", intent); e != nil {
				return e
			}
		}
	}
	experiments, e := s.List("experiment")
	if e != nil {
		return e
	}
	for _, d := range experiments {
		if String(d, "status") == "running" {
			d["status"] = "interrupted"
			if e = s.Put("experiment", d); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) Backup(path string) error {
	if _, e := os.Stat(path); e == nil {
		return errors.New("backup target exists")
	}
	_, e := s.db.Exec("VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'")
	if e == nil {
		e = os.Chmod(path, 0600)
	}
	return e
}
