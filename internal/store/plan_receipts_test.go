package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanReceiptProjectsBoundedSameRunObservations(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	secret := strings.Repeat("PRIVATE-OUTPUT", 100000)
	row := Doc{"id": "receipt", "runId": "own", "tool": "run_command", "status": "failed", "arguments": Doc{"executable": "python3", "args": []string{"--token", "PRIVATE-CREDENTIAL"}, "workingDirectory": "nested"}, "error": secret, "result": Doc{"content": secret, "data": Doc{"stdout": secret, "stderr": secret, "exitCode": 1, "publication": "not_applied"}}}
	if err = s.Put("toolcall", row); err != nil {
		t.Fatal(err)
	}
	got, err := s.PlanReceipt("own", "receipt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if len(b) > 2048 || strings.Contains(string(b), "PRIVATE-") || got["executable"] != "python3" || got["exitCode"] != float64(1) || got["publication"] != "not_applied" {
		t.Fatal("unsafe or incomplete receipt projection", got)
	}
	for _, run := range []string{"other", "unknown"} {
		if _, err = s.PlanReceipt(run, "receipt"); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-run lookup accepted", err)
		}
	}
	full, _ := s.Get("toolcall", "receipt")
	if full["error"] != secret {
		t.Fatal("projection changed stored evidence")
	}
	if err = s.Put("toolcall", Doc{"id": "long-path", "runId": "own", "tool": "read_file", "status": "failed", "arguments": Doc{"path": strings.Repeat("a", 600)}}); err != nil {
		t.Fatal(err)
	}
	limited, err := s.PlanReceipt("own", "long-path")
	if err != nil || limited["detailsLimited"] != true || len(String(limited, "path")) != 512 {
		t.Fatal("metadata truncation was not explicit", limited, err)
	}
}
