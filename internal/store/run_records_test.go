package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolSummaryBoundsPayloadButKeepsWholeRunCounts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 230; i++ {
		call := Doc{"id": fmt.Sprintf("call-%03d", i), "createdAt": fmt.Sprintf("%06d", i), "runId": "owned", "tool": "run_command", "status": "completed", "arguments": Doc{"executable": "python3", "args": []string{"-m", "unittest"}}, "result": Doc{"data": Doc{"exitCode": 0, "stdout": strings.Repeat("界", 2500) + "DIAGNOSTIC-END", "readOnly": true}}}
		if i == 0 {
			call["status"] = "failed"
			call["result"] = Doc{"data": Doc{"outcomeUnknown": true}}
		}
		if i == 229 {
			call["tool"] = "edit_file"
			call["arguments"] = Doc{"path": "large.py", "oldText": strings.Repeat("RAW-SOURCE", 60000), "newText": "RAW-SOURCE-NEW"}
			call["result"] = Doc{"content": strings.Repeat("RAW-RESULT", 60000), "data": Doc{"path": "large.py", "beforeHash": "old", "afterHash": "new"}, "artifacts": []Doc{{"before": strings.Repeat("RAW-ARTIFACT", 200000), "after": strings.Repeat("RAW-ARTIFACT", 200000)}}}
		}
		if err := s.Put("toolcall", call); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := s.Put("toolcall", Doc{"id": fmt.Sprintf("read-%d", i), "runId": "owned", "tool": "read_file", "status": "completed", "createdAt": "999999"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put("toolcall", Doc{"id": "outside", "runId": "other", "tool": "run_command", "status": "started"}); err != nil {
		t.Fatal(err)
	}
	summary, err := s.ToolSummary("owned")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ToolCalls != 240 || summary.FailedTools != 1 || summary.UnresolvedTools != 1 || summary.ReceiptEntries != 230 || summary.RecoveryActions != 230 {
		t.Fatalf("aggregate counts lost old/omitted outcome: %+v", summary)
	}
	encodedBytes := 0
	for _, record := range summary.Records {
		b, _ := json.Marshal(record)
		encodedBytes += len(b)
		if strings.Contains(string(b), "RAW-") || strings.Contains(string(b), "outside") {
			t.Fatal("raw source, artifacts or another run crossed projection")
		}
	}
	if len(summary.Records) == 0 || len(summary.Records) > 200 || encodedBytes > maxToolSummaryBytes {
		t.Fatalf("projection not bounded: records=%d bytes=%d", len(summary.Records), encodedBytes)
	}
	last := summary.Records[len(summary.Records)-1]
	if last["id"] != "call-229" {
		t.Fatalf("latest action missing: %v", last)
	}
	previous, _ := json.Marshal(summary.Records[len(summary.Records)-2])
	if !strings.Contains(string(previous), "DIAGNOSTIC-END") || !strings.Contains(string(previous), "[tail only]") || !strings.Contains(string(previous), `"readOnly":true`) {
		t.Fatalf("diagnostic tail or boolean lost: %s", previous)
	}
	full, err := s.Get("toolcall", "call-229")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(full["result"]), "RAW-ARTIFACT") {
		t.Fatal("projection modified durable source")
	}
}

func TestToolSummaryBoundsArgumentsAndErrors(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Put("toolcall", Doc{"id": "big", "runId": "run", "tool": "run_command", "status": "failed", "arguments": Doc{"executable": "/python", "args": []string{strings.Repeat("ARGUMENT-BODY", 10000)}}, "error": strings.Repeat("界", 10000)}); err != nil {
		t.Fatal(err)
	}
	summary, err := s.ToolSummary("run")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(summary.Records)
	if len(b) > 6000 || strings.Contains(string(b), "ARGUMENT-BODY") || summary.Records[0]["detailsLimited"] != true {
		t.Fatalf("unbounded projection: bytes=%d", len(b))
	}
	if !strings.Contains(string(b), "/python") || !strings.Contains(string(b), "[truncated]") {
		t.Fatal("omitted metadata not identified")
	}
}

func TestArtifactSummariesExcludeSourceWithoutChangingFullRecord(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc := Doc{"id": "artifact", "runId": "run", "workspaceId": "workspace", "name": "module.py", "path": "/workspace/module.py", "mediaType": "text/plain", "before": strings.Repeat("before", 100000), "after": strings.Repeat("after", 100000), "size": 500000}
	if err := s.Put("artifact", doc); err != nil {
		t.Fatal(err)
	}
	summaries, err := s.ArtifactSummaries()
	if err != nil || len(summaries) != 1 {
		t.Fatal(summaries, err)
	}
	if _, ok := summaries[0]["before"]; ok {
		t.Fatal("before leaked")
	}
	if _, ok := summaries[0]["after"]; ok {
		t.Fatal("after leaked")
	}
	if summaries[0]["workspaceId"] != "workspace" || summaries[0]["name"] != "module.py" {
		t.Fatal("metadata lost")
	}
	full, err := s.Get("artifact", "artifact")
	if err != nil || full["before"] != doc["before"] || full["after"] != doc["after"] {
		t.Fatal("full artifact mutated", err)
	}
}
