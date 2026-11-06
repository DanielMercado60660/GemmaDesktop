package app

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/harness"
	"strings"
)

func (c *Core) harnessV2Call(ctx context.Context, method string, p Doc) (any, error) {
	switch method {
	case "harness.packs.list":
		return Doc{"items": c.harness.Packs()}, nil
	case "harness.arms.create":
		if str(p, "provenance") == "product:frozen" {
			if p["content"] != nil || str(p, "revisionId") != "" {
				return nil, errors.New("current-product baseline resolves its active revision automatically")
			}
			p = clone(p)
			for _, key := range []string{"model", "runtimeId", "thinking", "temperature", "maxOutputTokens", "maxSteps", "toolProtocol", "contextSize", "harnessMechanisms", "verificationFeedback"} {
				delete(p, key)
			}
		}
		spec, base, err := c.freezeHarnessSpec(ctx, p, true)
		if err != nil {
			return nil, err
		}
		revision := harness.Revision{ID: str(p, "revisionId"), Content: base}
		if p["content"] != nil {
			if revision.ID != "" {
				return nil, errors.New("choose frozen revision or content, not both")
			}
			b, err := json.Marshal(p["content"])
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(b, &revision.Content); err != nil {
				return nil, err
			}
			if err = decodeHarnessV2(p["authoredBy"], &revision.AuthoredBy); err != nil {
				return nil, err
			}
			if revision.AuthoredBy.Kind != "codex" || strings.TrimSpace(revision.AuthoredBy.Identity) == "" {
				return nil, errors.New("custom candidate content requires Codex authorship")
			}
		}
		return c.harness.RegisterArmV2(harness.ArmV2{Name: str(p, "name"), Provenance: str(p, "provenance"), Spec: spec, Revision: revision})
	case "harness.arms.get":
		return c.harness.GetArmV2(str(p, "id"))
	case "harness.arms.list":
		rows, err := c.harness.ListArmsV2()
		return Doc{"items": rows}, err
	case "harness.batchesV2.get":
		return c.harness.GetBatchV2(str(p, "id"))
	case "harness.batchesV2.list":
		run, err := c.harness.GetRunV2(str(p, "runId"))
		if err != nil {
			return nil, err
		}
		rows := []harness.BatchV2{}
		for _, id := range run.BatchIDs {
			b, e := c.harness.GetBatchV2(id)
			if e != nil {
				return nil, e
			}
			rows = append(rows, b)
		}
		return Doc{"items": rows}, nil
	case "harness.diagnostics.create":
		var req harness.DiagnosticRequest
		if err := decodeHarnessV2(p, &req); err != nil {
			return nil, err
		}
		return c.harness.CreateDiagnosticV2(req)
	case "harness.evaluationsV2.create":
		var req harness.EvaluationV2Request
		if err := decodeHarnessV2(p, &req); err != nil {
			return nil, err
		}
		baseline, err := c.harness.GetArmV2(req.BaselineArmID)
		if err != nil {
			return nil, err
		}
		if baseline.Provenance != "product:frozen" || baseline.Spec.ProfileBindingHash == "" || baseline.Spec.AuthorityHash == "" {
			return nil, errors.New("qualification requires a frozen current-product baseline")
		}
		return c.harness.CreateEvaluationV2(req)
	case "harness.evaluationsV2.promote":
		return c.promoteHarnessV2(ctx, str(p, "id"))
	case "harness.promotionsV2.rollback":
		return c.rollbackHarnessV2(ctx, str(p, "id"))
	case "harness.promotionsV2.get":
		return c.getHarnessPromotionV2(str(p, "id"))
	case "harness.promotionsV2.list":
		items, err := c.listHarnessPromotionsV2()
		return Doc{"items": items}, err
	case "harness.diagnostics.advance", "harness.evaluationsV2.advance", "harness.diagnostics.resume", "harness.evaluationsV2.resume":
		c.videoGuard.Lock()
		defer c.videoGuard.Unlock()
		if c.videoBusy() {
			return nil, errors.New("wait for the current video job")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.runs) > 0 || len(c.evals) > 0 {
			return nil, errors.New("wait for active chats and comparisons")
		}
		if err := c.requireHarnessRunKind(method, str(p, "id")); err != nil {
			return nil, err
		}
		defaultAttempts := 3
		if strings.Contains(method, "evaluationsV2") {
			defaultAttempts = 2
		}
		limits := harness.BatchLimits{WallSeconds: number(p, "wallSeconds", 7200), MaxAttempts: number(p, "maxAttempts", defaultAttempts)}
		if strings.HasSuffix(method, ".resume") {
			return c.harness.ResumeRunV2(str(p, "id"), limits)
		}
		return c.harness.AdvanceRunV2(str(p, "id"), limits)
	case "harness.diagnostics.get", "harness.evaluationsV2.get":
		if err := c.requireHarnessRunKind(method, str(p, "id")); err != nil {
			return nil, err
		}
		return c.harness.GetRunV2(str(p, "id"))
	case "harness.diagnostics.list", "harness.evaluationsV2.list":
		rows, err := c.harness.ListRunsV2()
		if err != nil {
			return nil, err
		}
		kind := "diagnostic"
		if strings.Contains(method, "evaluationsV2") {
			kind = "qualification"
		}
		out := []Doc{}
		for _, row := range rows {
			if row["kind"] == kind {
				out = append(out, row)
			}
		}
		return Doc{"items": out}, nil
	case "harness.diagnostics.export", "harness.evaluationsV2.export":
		if err := c.requireHarnessRunKind(method, str(p, "id")); err != nil {
			return nil, err
		}
		return c.harness.ExportRunV2(str(p, "id"))
	case "harness.diagnostics.cancel", "harness.evaluationsV2.cancel":
		if err := c.requireHarnessRunKind(method, str(p, "id")); err != nil {
			return nil, err
		}
		return Doc{}, c.harness.CancelRunV2(str(p, "id"))
	case "harness.evaluationsV2.review":
		var review harness.ReviewV2
		if err := decodeHarnessV2(p["review"], &review); err != nil {
			return nil, err
		}
		return c.harness.ReviewRunV2(str(p, "id"), review)
	case "harness.evaluationsV2.qualifiedArm":
		return c.harness.QualifiedArmV2(str(p, "id"))
	}
	return nil, errors.New("unknown versioned harness operation")
}

func (c *Core) requireHarnessRunKind(method, id string) error {
	run, err := c.harness.GetRunV2(id)
	if err != nil {
		return err
	}
	kind := "diagnostic"
	if strings.Contains(method, "evaluationsV2") {
		kind = "qualification"
	}
	if run.Kind != kind {
		return errors.New("operation does not match run kind")
	}
	return nil
}
func decodeHarnessV2(input any, out any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
