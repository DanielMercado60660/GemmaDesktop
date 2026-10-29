package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// RunGraph returns only diagram metadata from the immutable run checkpoint.
// SQL projects bounded fields before decoding: neither the ADK snapshot nor
// prompts, agent instructions, tool arguments, or file bodies enter this view.
// Missing legacy metadata is explicit; mutable workflow entities are never a
// fallback for an executed run.
func (s *Store) RunGraph(id string) (Doc, error) {
	var workflowID string
	var projected sql.NullString
	err := s.db.QueryRow(`SELECT coalesce(json_extract(r.body,'$.workflowId'),''),
		CASE WHEN json_type(c.body,'$.workflow')='object'
		AND json_type(c.body,'$.workflow.nodes')='array'
		AND coalesce(json_type(c.body,'$.workflow.edges'),'null') IN ('array','null')
		AND json_array_length(c.body,'$.workflow.nodes') BETWEEN 1 AND 64
		AND coalesce(json_array_length(c.body,'$.workflow.edges'),0)<=256
		THEN json_object(
			'id',substr(json_extract(c.body,'$.workflow.id'),1,128),
			'name',substr(json_extract(c.body,'$.workflow.name'),1,256),
			'nodes',json((SELECT json_group_array(json_object(
				'id',substr(json_extract(n.value,'$.id'),1,128),
				'type',substr(json_extract(n.value,'$.type'),1,64),
				'label',substr(json_extract(n.value,'$.label'),1,256),
				'position',json_object(
					'x',CASE WHEN json_type(n.value,'$.position.x') IN ('integer','real') THEN json_extract(n.value,'$.position.x') END,
					'y',CASE WHEN json_type(n.value,'$.position.y') IN ('integer','real') THEN json_extract(n.value,'$.position.y') END)
			)) FROM json_each(c.body,'$.workflow.nodes') n)),
			'edges',json((SELECT json_group_array(json_object(
				'id',substr(json_extract(e.value,'$.id'),1,128),
				'source',substr(json_extract(e.value,'$.source'),1,128),
				'target',substr(json_extract(e.value,'$.target'),1,128),
				'condition',substr(json_extract(e.value,'$.condition'),1,64)
			)) FROM json_each(c.body,'$.workflow.edges') e))
		) END
		FROM entities r LEFT JOIN entities c ON c.kind='checkpoint' AND c.id=r.id
		WHERE r.kind='run' AND r.id=?`, id).Scan(&workflowID, &projected)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := Doc{"runId": id, "source": "events"}
	if workflowID == "" {
		result["reason"] = "This is an agent run; the graph shows recorded activity."
		return result, nil
	}
	if !projected.Valid {
		result["reason"] = "The saved workflow diagram is unavailable or exceeds its display bounds. Only recorded activity is shown."
		return result, nil
	}
	var workflow Doc
	if err := json.Unmarshal([]byte(projected.String), &workflow); err != nil {
		return nil, err
	}
	result["source"], result["workflow"] = "checkpoint", workflow
	return result, nil
}
