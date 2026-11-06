package app

import (
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/workspace"
)

func (c *Core) focusContinuation(p Doc) (any, error) {
	source, err := c.Store.Get("run", str(p, "runId"))
	if err != nil {
		return nil, err
	}
	request := clone(p)
	delete(request, "runId")
	request["sessionId"] = source["sessionId"]
	request["agentId"] = source["agentId"]
	request["focusedFromRunId"] = source["id"]
	return c.startRun(request, "")
}

// This is an explicit, deterministic projection. No model-generated summary
// can replace user constraints, claim new verification, or authorize replay.
func (c *Core) prepareFocusedContinuation(p, session Doc, agentID, workflowID string) (Doc, error) {
	id := str(p, "focusedFromRunId")
	if id == "" {
		return nil, nil
	}
	source, err := c.Store.Get("run", id)
	if err != nil {
		return nil, err
	}
	switch str(source, "status") {
	case "completed", "failed", "cancelled", "interrupted":
	default:
		return nil, errors.New("focus requires a stopped run")
	}
	if source["sessionId"] != session["id"] || source["workspaceId"] != session["workspaceId"] || str(source, "agentId") != agentID || str(source, "workflowId") != "" || workflowID != "" {
		return nil, errors.New("focus requires the same ordinary session, workspace and agent")
	}
	runs, err := c.Store.ListScoped("run", "sessionId", str(session, "id"))
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 || str(runs[len(runs)-1], "id") != id {
		return nil, errors.New("focus must continue the latest run so later user constraints are not omitted")
	}
	checkpoint, err := c.Store.Get("checkpoint", id)
	if err != nil {
		return nil, err
	}
	frozen, err := decodeFrozenAgents(checkpoint)
	if err != nil {
		return nil, err
	}
	var previous engine.Request
	b, _ := json.Marshal(checkpoint["request"])
	if err = json.Unmarshal(b, &previous); err != nil {
		return nil, err
	}
	primary, ok := frozen[previous.CapabilityID]
	if !ok {
		return nil, errors.New("source run lacks an intact authority snapshot")
	}
	if previous.VerificationRequired && checkpoint["verificationContract"] == nil {
		return nil, errors.New("source verification contract is missing")
	}
	if supplied := p["verificationContract"]; supplied != nil && contentDigest(obj(supplied)) != contentDigest(obj(checkpoint["verificationContract"])) {
		return nil, errors.New("focused continuation must retain the original verification contract")
	}
	messages, err := c.Store.ListScoped("message", "sessionId", str(session, "id"))
	if err != nil {
		return nil, err
	}
	requests := []string{}
	total := 0
	for _, message := range messages {
		if str(message, "role") == "user" {
			text := str(message, "content")
			total += len(text)
			if total > 64<<10 {
				return nil, errors.New("exact user constraints exceed the focus bound; no constraints were summarized or dropped")
			}
			requests = append(requests, text)
		}
	}
	if len(requests) == 0 {
		return nil, errors.New("source user constraints unavailable")
	}
	receipt, err := c.runReceipt(id)
	if err != nil {
		return nil, err
	}
	guidance, err := c.guidanceReceipts(id)
	if err != nil {
		return nil, err
	}
	receiptIDs := []string{}
	for _, r := range guidance {
		receiptIDs = append(receiptIDs, str(r, "id"))
	}
	payload := Doc{"sourceRunId": id, "priorUserRequests": requests, "observedToolReceipt": receipt, "guidanceReceiptIds": receiptIDs, "notice": "Explicit focused continuation. Preserve these exact prior user constraints unless the current user changes them. Tool receipts are historical observations, not fresh verification or permission to replay effects. Re-read affected files. Previous assistant prose and execution state are not resumed. The complete original tool journal remains attached to sourceRunId."}
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(text) > 96<<10 {
		return nil, errors.New("focused evidence exceeds the safe bound; nothing was silently truncated")
	}
	contextSource := engine.ContextSource{Kind: "focused_continuation", Path: "run/" + id, SHA256: workspace.Hash(text), Content: string(text), Scope: "Historical user constraints and immutable receipt references; current user request takes precedence", TrustClass: "historical_evidence"}
	return Doc{"sourceRunId": id, "sourceCheckpointHash": contentDigest(checkpoint), "userArchiveHash": contentDigest(requests), "receiptHash": contentDigest(receipt), "authorityHash": harnessAuthorityHash(primary), "verificationContract": checkpoint["verificationContract"], "contextSource": contextSource}, nil
}

func focusedContextSource(focus Doc) (engine.ContextSource, error) {
	var out engine.ContextSource
	b, err := json.Marshal(focus["contextSource"])
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}
