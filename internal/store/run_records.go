package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const maxToolSummaryRecords = 200
const maxToolSummaryBytes = 256 << 10

// ToolRunSummary keeps counts across the whole run while returning only recent,
// projected action records. Neither artifact snapshots nor raw file text crosses
// the SQL boundary into this presentation/recovery path.
type ToolRunSummary struct {
	ToolCalls, FailedTools, UnresolvedTools int
	ReceiptEntries, RecoveryActions         int
	Records                                 []Doc
}

const actionPredicate = `COALESCE(json_extract(body,'$.tool'),'') NOT IN ('read_file','list_files','search_files')`

func (s *Store) ToolSummary(runID string) (ToolRunSummary, error) {
	summary := ToolRunSummary{Records: []Doc{}}
	// Counts and records describe one SQLite snapshot, even if another run is
	// writing concurrently. The aggregate never returns full entity bodies.
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return summary, err
	}
	defer tx.Rollback()
	err = tx.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN json_extract(body,'$.status')='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN COALESCE(json_extract(body,'$.status'),'') NOT IN ('failed','completed') OR json_extract(body,'$.result.data.outcomeUnknown')=1 OR json_extract(body,'$.result.data.publication')='recovery_required' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN json_extract(body,'$.tool') IN ('run_command','setup_dependencies') OR (json_extract(body,'$.status')='completed' AND json_extract(body,'$.tool') IN ('write_file','edit_file','move_file','delete_file','skill_materialize')) THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN `+actionPredicate+` THEN 1 ELSE 0 END),0)
		FROM entities WHERE kind='toolcall' AND json_extract(body,'$.runId')=?`, runID).Scan(&summary.ToolCalls, &summary.FailedTools, &summary.UnresolvedTools, &summary.ReceiptEntries, &summary.RecoveryActions)
	if err != nil {
		return summary, err
	}
	rows, err := tx.Query(`SELECT `+toolSummaryProjection()+` FROM entities
		WHERE kind='toolcall' AND json_extract(body,'$.runId')=? AND `+actionPredicate+`
		ORDER BY json_extract(body,'$.createdAt') DESC,id DESC LIMIT ?`, runID, maxToolSummaryRecords)
	if err != nil {
		return summary, err
	}
	bytes := 0
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			break
		}
		if bytes+len(body) > maxToolSummaryBytes {
			continue
		}
		d := Doc{}
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			break
		}
		summary.Records = append(summary.Records, d)
		bytes += len(body)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return summary, err
	}
	// The SQL limit selects the latest actions; consumers display journal order.
	for i, j := 0, len(summary.Records)-1; i < j; i, j = i+1, j-1 {
		summary.Records[i], summary.Records[j] = summary.Records[j], summary.Records[i]
	}
	return summary, tx.Commit()
}

func toolSummaryProjection() string {
	// Paths below are fixed program constants, never interpolated user input.
	value := func(path string) string { return "json_extract(body,'$." + path + "')" }
	short := func(path string, chars int) string {
		v := value(path)
		return fmt.Sprintf("CASE WHEN length(%s)>%d THEN substr(%s,1,%d)||'[truncated]' ELSE %s END", v, chars, v, chars, v)
	}
	boolValue := func(path string) string { return "json(body -> '$." + path + "')" }
	args := `json_remove(COALESCE(json_extract(body,'$.arguments'),'{}'),'$.content','$.oldText','$.newText','$.arguments.content','$.arguments.oldText','$.arguments.newText')`
	argsTooLarge := "length(CAST(" + args + " AS BLOB))>4096"
	fallback := "json_object('detailsOmitted',json('true')"
	for _, key := range []string{"path", "destination", "executable", "workingDirectory", "expectedHash", "serverId", "tool"} {
		fallback += ",'" + key + "'," + short("arguments."+key, 256)
	}
	fallback += ")"
	fields := []string{
		"'id'," + short("id", 256), "'callId'," + short("callId", 256),
		"'tool'," + short("tool", 200), "'status'," + short("status", 32),
		"'arguments',json(CASE WHEN " + argsTooLarge + " THEN " + fallback + " ELSE " + args + " END)",
		"'error'," + short("error", 1024),
		"'detailsLimited',json(CASE WHEN " + argsTooLarge + " OR length(" + value("error") + ")>1024 OR length(" + value("result.data.changes") + ")>8192 THEN 'true' ELSE 'false' END)",
	}
	data := []string{}
	for _, key := range []string{"path", "source", "workingDirectory", "image", "packageManager"} {
		data = append(data, "'"+key+"',"+short("result.data."+key, 1024))
	}
	for _, key := range []string{"beforeHash", "afterHash", "signal", "commandId", "snapshotDigest", "dependencyDigest", "publication", "lockHash"} {
		data = append(data, "'"+key+"',"+short("result.data."+key, 128))
	}
	for _, key := range []string{"exitCode", "durationMs", "packageCount", "downloadedBytes"} {
		data = append(data, "'"+key+"',"+value("result.data."+key))
	}
	for _, key := range []string{"readOnly", "cancelled", "truncated", "outcomeUnknown", "workspaceChanged"} {
		data = append(data, "'"+key+"',"+boolValue("result.data."+key))
	}
	changes := value("result.data.changes")
	data = append(data, "'changes',json(CASE WHEN length("+changes+")<=8192 THEN "+changes+" ELSE NULL END)")
	for _, key := range []string{"stdout", "stderr"} {
		v := value("result.data." + key)
		data = append(data, "'"+key+"',CASE WHEN length("+v+")>1000 THEN '[tail only] '||substr("+v+",-1000) ELSE "+v+" END")
	}
	fields = append(fields, "'result',json_object('data',json_object("+strings.Join(data, ",")+"))")
	return "json_object(" + strings.Join(fields, ",") + ")"
}

// ArtifactSummaries deliberately leaves durable before/after contents in the
// artifact record. Full contents are fetched explicitly through artifact.get.
func (s *Store) ArtifactSummaries() ([]Doc, error) {
	rows, err := s.db.Query(`SELECT json_remove(body,'$.before','$.after') FROM entities WHERE kind='artifact' ORDER BY updated`)
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
		d := Doc{}
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ForRun reads only records owned by this run, in journal order.
func (s *Store) ForRun(kind, runID string) ([]Doc, error) {
	rows, err := s.db.Query(`SELECT body FROM entities WHERE kind=? AND json_extract(body,'$.runId')=? ORDER BY json_extract(body,'$.createdAt'),id`, kind, runID)
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
		d := Doc{}
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
