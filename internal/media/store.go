package media

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
	"math"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
)

func openDatabase(root string) (*sql.DB, error) {
	db, e := sql.Open("sqlite", filepath.Join(root, "media.sqlite"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	var v string
	if e = db.QueryRow("SELECT sqlite_version()").Scan(&v); e != nil {
		return fail(e)
	}
	var a, b, c int
	if _, e = fmt.Sscanf(v, "%d.%d.%d", &a, &b, &c); e != nil || a != 3 || b < 51 || b == 51 && c < 3 {
		return fail(errors.New("media database requires SQLite 3.51.3 or newer"))
	}
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, e = db.Exec(q); e != nil {
			return fail(e)
		}
	}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		return fail(e)
	}
	if version > 1 {
		return fail(errors.New("media database schema is newer than this application"))
	}
	tx, e := db.Begin()
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS clips(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,body TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,clip_id TEXT NOT NULL,body TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS answers(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,clip_id TEXT NOT NULL,body TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS manifests(clip_id TEXT PRIMARY KEY,body TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS indexes(id TEXT PRIMARY KEY,clip_id TEXT NOT NULL,model TEXT NOT NULL,digest TEXT NOT NULL,runtime TEXT NOT NULL,extractor TEXT NOT NULL,preprocessing TEXT NOT NULL,status TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS frames(id TEXT PRIMARY KEY,index_id TEXT NOT NULL,ordinal INTEGER NOT NULL,pts INTEGER NOT NULL,seconds REAL NOT NULL,sha256 TEXT NOT NULL,path TEXT NOT NULL,vector BLOB NOT NULL,UNIQUE(index_id,ordinal),FOREIGN KEY(index_id) REFERENCES indexes(id))`,
		`CREATE INDEX IF NOT EXISTS frame_index ON frames(index_id)`, `PRAGMA user_version=1`} {
		if _, e = tx.Exec(q); e != nil {
			return fail(e)
		}
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	if e = os.Chmod(filepath.Join(root, "media.sqlite"), 0600); e != nil {
		return fail(e)
	}
	return db, nil
}
func putDocument(db interface {
	Exec(string, ...any) (sql.Result, error)
}, table, id, workspaceID, clipID string, value any) error {
	body, e := json.Marshal(value)
	if e != nil {
		return e
	}
	switch table {
	case "clips":
		_, e = db.Exec(`INSERT INTO clips(id,workspace_id,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, id, workspaceID, string(body))
	case "jobs", "answers":
		_, e = db.Exec(`INSERT INTO `+table+`(id,workspace_id,clip_id,body) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, id, workspaceID, clipID, string(body))
	default:
		return errors.New("invalid media document table")
	}
	return e
}
func getDocument[T any](db *sql.DB, table, workspaceID, id string) (T, error) {
	var result T
	var body string
	e := db.QueryRow(`SELECT body FROM `+table+` WHERE workspace_id=? AND id=?`, workspaceID, id).Scan(&body)
	if errors.Is(e, sql.ErrNoRows) {
		return result, errors.New("video item not found in this workspace")
	}
	if e == nil {
		e = json.Unmarshal([]byte(body), &result)
	}
	return result, e
}
func listDocuments[T any](db *sql.DB, table, workspaceID, clipID string) ([]T, error) {
	q := `SELECT body FROM ` + table + ` WHERE workspace_id=?`
	args := []any{workspaceID}
	if clipID != "" {
		q += ` AND clip_id=?`
		args = append(args, clipID)
	}
	q += ` ORDER BY rowid DESC LIMIT 200`
	rows, e := db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var body string
		var d T
		if e = rows.Scan(&body); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(body), &d); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (m *Manager) recoverJobs() error {
	rows, e := m.db.Query(`SELECT body FROM jobs`)
	if e != nil {
		return e
	}
	var interrupted []Job
	for rows.Next() {
		var body string
		var j Job
		if e = rows.Scan(&body); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(body), &j); e != nil {
			break
		}
		if j.Status == "queued" || j.Status == "running" || j.Status == "cancelling" {
			interrupted = append(interrupted, j)
		}
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, j := range interrupted {
		j.Status = "interrupted"
		j.Error = "The application stopped before this job finished. Import or index again explicitly; no job was replayed."
		j.FinishedAt = store.Now()
		if e = m.saveJob(j); e != nil {
			return e
		}
		if j.Kind != "answer" {
			clip, e := getDocument[Clip](m.db, "clips", j.WorkspaceID, j.ClipID)
			if e != nil {
				return e
			}
			if clip.IndexID != "" {
				clip.Status = "ready"
			} else {
				clip.Status = "interrupted"
			}
			clip.Error = j.Error
			if e = m.saveClip(clip); e != nil {
				return e
			}
		}
		if j.AnswerID != "" {
			answer, e := getDocument[Answer](m.db, "answers", j.WorkspaceID, j.AnswerID)
			if e != nil {
				return e
			}
			answer.Status = "interrupted"
			answer.Error = j.Error
			if e = m.saveAnswer(answer); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m *Manager) saveClip(c Clip) error {
	return putDocument(m.db, "clips", c.ID, c.WorkspaceID, "", c)
}
func (m *Manager) saveJob(j Job) error {
	return putDocument(m.db, "jobs", j.ID, j.WorkspaceID, j.ClipID, j)
}
func (m *Manager) saveAnswer(a Answer) error {
	return putDocument(m.db, "answers", a.ID, a.WorkspaceID, a.ClipID, a)
}
func vectorBytes(v []float32) ([]byte, error) {
	if len(v) != 768 {
		return nil, errors.New("embedding dimension must be 768")
	}
	b := make([]byte, len(v)*4)
	norm := 0.
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, errors.New("embedding contains nonfinite values")
		}
		norm += float64(x) * float64(x)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	if math.Abs(norm-1) > 0.02 {
		return nil, errors.New("embedding is not unit normalized")
	}
	return b, nil
}
func decodeVector(b []byte) ([]float32, error) {
	if len(b) != 768*4 {
		return nil, errors.New("stored embedding has invalid dimension")
	}
	v := make([]float32, 768)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	_, e := vectorBytes(v)
	return v, e
}
func (m *Manager) loadFrames(indexID string) ([]frame, error) {
	rows, e := m.db.Query(`SELECT id,index_id,ordinal,pts,seconds,sha256,path,vector FROM frames WHERE index_id=? ORDER BY seconds`, indexID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []frame{}
	for rows.Next() {
		var f frame
		var b []byte
		if e = rows.Scan(&f.ID, &f.IndexID, &f.Ordinal, &f.PTS, &f.Seconds, &f.SHA256, &f.Path, &b); e != nil {
			return nil, e
		}
		f.Vector, e = decodeVector(b)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (m *Manager) publishIndex(ctx context.Context, clip Clip, indexID string, expected int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	tx, e := m.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM frames WHERE index_id=?`, indexID).Scan(&n); e != nil {
		return e
	}
	if n != expected {
		return errors.New("cannot publish an incomplete frame index")
	}
	if _, e = tx.Exec(`UPDATE indexes SET status='ready' WHERE id=? AND status='building'`, indexID); e != nil {
		return e
	}
	clip.IndexID = indexID
	clip.Status = "ready"
	clip.Error = ""
	clip.FrameCount = n
	clip.EmbeddingModel = m.config.EmbeddingModel
	if e = putDocument(tx, "clips", clip.ID, clip.WorkspaceID, "", clip); e != nil {
		return e
	}
	return tx.Commit()
}
func safeID(s string) bool { return len(s) == 32 && strings.Trim(s, "0123456789abcdef") == "" }
