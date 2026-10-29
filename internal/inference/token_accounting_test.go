package inference

import (
	"encoding/json"
	"testing"
)

func TestTokenAccountingValidatesReceiptsAndLegacyUnknown(t *testing.T) {
	if (*TokenAccounting)(nil).Complete() {
		t.Fatal("legacy absence complete")
	}
	good := NewTokenAccounting(2, 2, 2)
	if !good.Complete() {
		t.Fatal(good)
	}
	if empty := NewTokenAccounting(0, 0, 0); empty.Status != "unknown" || empty.Complete() || !MergeTokenAccounting(empty, good).Complete() {
		t.Fatal("empty ledger must be unknown but preserve new measured receipts", empty)
	}
	max := int(^uint(0) >> 1)
	if overflow := MergeTokenAccounting(NewTokenAccounting(max, max, max), good); overflow.Complete() || overflow.Status != "unknown" {
		t.Fatal("counter overflow claimed complete", overflow)
	}
	for _, mutate := range []func(*TokenAccounting){
		func(a *TokenAccounting) { a.Version = 2 }, func(a *TokenAccounting) { a.Source = "guessed" }, func(a *TokenAccounting) { a.Attempts = 0 }, func(a *TokenAccounting) { a.TerminalResponses = 3 }, func(a *TokenAccounting) { a.MeasuredResponses = -1 }, func(a *TokenAccounting) { a.Status = "unknown" }, func(a *TokenAccounting) { a.UnknownContributions = 1 },
	} {
		a := *good
		mutate(&a)
		if a.Complete() {
			t.Fatal("malformed complete receipt accepted", a)
		}
	}
	partial := MergeTokenAccounting(good, NewTokenAccounting(1, 0, 0))
	if partial.Complete() || partial.Status != "partial" || partial.Attempts != 3 {
		t.Fatal(partial)
	}
	legacy := MergeTokenAccounting(good, nil)
	if legacy.Complete() || legacy.Status != "partial" || legacy.UnknownContributions != 1 {
		t.Fatal(legacy)
	}
	var old struct {
		Usage           Metrics          `json:"usage"`
		TokenAccounting *TokenAccounting `json:"tokenAccounting,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"usage":{"prompt_eval_count":100,"eval_count":20}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.TokenAccounting != nil || old.TokenAccounting.Complete() {
		t.Fatal("legacy positive counts inferred complete")
	}
	raw, _ := json.Marshal(old)
	var restored map[string]any
	_ = json.Unmarshal(raw, &restored)
	if _, ok := restored["tokenAccounting"]; ok {
		t.Fatal("legacy record silently upgraded")
	}
}
