package store

import (
	"database/sql"
	"encoding/json"
)

// PlanReceipt projects only bounded observation metadata before Go decoding.
// Commands deliberately omit argument values, output, error text, environment,
// source bytes and MCP payloads. Both identifiers scope every lookup.
func (s *Store) PlanReceipt(runID, receiptID string) (Doc, error) {
	var body string
	err := s.db.QueryRow(`SELECT json_object(
 'receiptId',id,
 'tool',substr(json_extract(body,'$.tool'),1,128),
 'status',substr(json_extract(body,'$.status'),1,32),
 'createdAt',substr(json_extract(body,'$.createdAt'),1,64),
 'finishedAt',substr(json_extract(body,'$.finishedAt'),1,64),
 'path',CASE WHEN json_extract(body,'$.tool') IN ('read_file','write_file','edit_file','move_file','delete_file') THEN substr(json_extract(body,'$.arguments.path'),1,512) WHEN json_extract(body,'$.tool')='skill_materialize' THEN substr(json_extract(body,'$.arguments.targetPath'),1,512) END,
 'destination',CASE WHEN json_extract(body,'$.tool')='move_file' THEN substr(json_extract(body,'$.arguments.destination'),1,512) END,
 'beforeHash',CASE WHEN json_extract(body,'$.tool') IN ('write_file','edit_file','move_file','delete_file','skill_materialize') THEN substr(json_extract(body,'$.result.data.beforeHash'),1,128) END,
 'afterHash',CASE WHEN json_extract(body,'$.tool') IN ('write_file','edit_file','move_file','delete_file','skill_materialize') THEN substr(json_extract(body,'$.result.data.afterHash'),1,128) END,
 'executable',CASE WHEN json_extract(body,'$.tool')='run_command' THEN substr(json_extract(body,'$.arguments.executable'),1,256) END,
 'workingDirectory',CASE WHEN json_extract(body,'$.tool')='run_command' THEN substr(json_extract(body,'$.arguments.workingDirectory'),1,512) END,
 'exitCode',CASE WHEN json_extract(body,'$.tool')='run_command' AND json_type(body,'$.result.data.exitCode')='integer' THEN json_extract(body,'$.result.data.exitCode') END,
 'publication',CASE WHEN json_extract(body,'$.tool')='run_command' THEN substr(json_extract(body,'$.result.data.publication'),1,128) END,
 'cancelled',CASE WHEN json_extract(body,'$.tool')='run_command' THEN json_extract(body,'$.result.data.cancelled')=1 END,
 'detailsLimited',json(CASE WHEN length(json_extract(body,'$.arguments.path'))>512 OR length(json_extract(body,'$.arguments.targetPath'))>512 OR length(json_extract(body,'$.arguments.destination'))>512 OR length(json_extract(body,'$.arguments.workingDirectory'))>512 OR length(json_extract(body,'$.arguments.executable'))>256 THEN 'true' ELSE 'false' END),
 'note','Bounded metadata only; command arguments, output and error text are omitted. Status and exit code are observations, not a correctness grade.'
) FROM entities WHERE kind='toolcall' AND id=? AND json_extract(body,'$.runId')=?`, receiptID, runID).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := Doc{}
	if err = json.Unmarshal([]byte(body), &d); err != nil {
		return nil, err
	}
	for key, value := range d {
		if value == nil {
			delete(d, key)
		}
	}
	return d, nil
}
