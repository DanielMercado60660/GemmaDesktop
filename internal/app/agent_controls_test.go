package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
)

func TestAgentGenerationControlsValidateBeforePersisting(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Core{Store: s}
	base := func() Doc { return Doc{"id": "profile", "name": "Profile", "maxSteps": 4, "mode": "approval"} }
	if _, err := c.saveAgent(base()); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Get("agent", "profile")
	if err != nil || saved["thinking"] != "auto" || number(saved, "maxOutputTokens", 0) != 8192 || saved["temperature"] != 0.2 || saved["toolProtocol"] != "native" {
		t.Fatalf("missing field defaults: %+v %v", saved, err)
	}
	before, _ := json.Marshal(saved)
	for _, field := range []string{"thinking", "maxOutputTokens", "temperature", "toolProtocol"} {
		values := []any{"", "automatic", true, nil, 3}
		if field == "maxOutputTokens" {
			values = []any{0, -1, 255, 32769, 256.5, "8192", true, nil, math.NaN(), math.Inf(1), json.Number("invalid")}
		}
		if field == "temperature" {
			values = []any{-0.01, 2.0001, "0", true, nil, math.NaN(), math.Inf(1), math.Inf(-1), json.Number("invalid")}
		}
		if field == "toolProtocol" {
			values = []any{"", "structured", "native ", "NATIVE", true, nil, 3}
		}
		for _, value := range values {
			p := base()
			p[field] = value
			if _, err := c.saveAgent(p); err == nil {
				t.Fatalf("invalid %s=%v accepted", field, value)
			}
			after, err := s.Get("agent", "profile")
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(after)
			if string(encoded) != string(before) {
				t.Fatalf("invalid update persisted: %s=%v", field, value)
			}
		}
	}
	for _, tc := range []struct {
		thinking string
		limit    any
		want     int
	}{{"auto", 256, 256}, {"on", 32768.0, 32768}, {"off", json.Number("8192"), 8192}} {
		p := base()
		p["thinking"], p["maxOutputTokens"] = tc.thinking, tc.limit
		if _, err := c.saveAgent(p); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get("agent", "profile")
		if got["thinking"] != tc.thinking || number(got, "maxOutputTokens", 0) != tc.want {
			t.Fatalf("valid fields lost: %+v", got)
		}
	}
	for _, temperature := range []any{0, 1.0, json.Number("2.0")} {
		p := base()
		p["temperature"] = temperature
		if _, err := c.saveAgent(p); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get("agent", "profile")
		want, _ := finiteNumber(temperature)
		if err != nil || got["temperature"] != want {
			t.Fatalf("valid temperature changed: want=%v got=%+v err=%v", want, got, err)
		}
	}
	for _, protocol := range []string{"native", "structured_json"} {
		p := base()
		p["toolProtocol"] = protocol
		if _, err := c.saveAgent(p); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get("agent", "profile")
		if err != nil || got["toolProtocol"] != protocol {
			t.Fatalf("valid tool protocol changed: want=%s got=%+v err=%v", protocol, got, err)
		}
	}
}

func TestIntegrationAgentGenerationControlsReachPlainChat(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fields      Doc
		think       any
		limit       float64
		temperature float64
		protocol    string
	}{
		{"legacy", Doc{}, nil, 8192, 0.2, "native"},
		{"auto", Doc{"thinking": "auto", "maxOutputTokens": 512, "temperature": 0}, nil, 512, 0, "native"},
		{"on", Doc{"thinking": "on", "maxOutputTokens": 256, "temperature": 1}, true, 256, 1, "native"},
		{"off", Doc{"thinking": "off", "maxOutputTokens": 32768, "temperature": 2}, false, 32768, 2, "native"},
		{"structured", Doc{"toolProtocol": "structured_json"}, nil, 8192, 0.2, "structured_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan inference.ChatRequest, 1)
			h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
				received <- req
				writeControlsFixtureAnswer(w, req)
			})
			p := Doc{"id": "default", "name": "Profile", "mode": "approval", "maxSteps": 4}
			for k, v := range tc.fields {
				p[k] = v
			}
			callIntegration(t, h.core, "agent.save", p)
			if tc.name == "legacy" {
				if _, err := h.core.Store.Update("agent", "default", func(p Doc) error {
					delete(p, "temperature")
					delete(p, "toolProtocol")
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Describe current controls"})
			finishedIntegration(t, h.core, str(run, "id"), "completed")
			var req inference.ChatRequest
			select {
			case req = <-received:
			case <-time.After(time.Second):
				t.Fatal("no model request")
			}
			if req.Think != tc.think || req.Options["num_predict"] != tc.limit || req.Options["temperature"] != tc.temperature {
				t.Fatalf("wire controls: think=%v options=%+v", req.Think, req.Options)
			}
			if (req.Format != nil) != (tc.protocol == "structured_json") || (req.Format != nil && len(req.Tools) != 0) {
				t.Fatalf("tool protocol not applied to wire request: %+v", req)
			}
			cp, err := h.core.Store.Get("checkpoint", str(run, "id"))
			if err != nil {
				t.Fatal(err)
			}
			frozen := obj(cp["request"])
			if frozen["think"] != tc.think || number(frozen, "maxOutputTokens", 0) != int(tc.limit) || frozen["temperature"] != tc.temperature || frozen["toolProtocol"] != tc.protocol {
				t.Fatalf("request controls not frozen: %+v", frozen)
			}
		})
	}
}

func writeControlsFixtureAnswer(w http.ResponseWriter, req inference.ChatRequest) {
	if req.Format != nil {
		fmt.Fprintln(w, `{"message":{"content":"{\"action\":\"final\",\"answer\":\"complete\"}"},"done":true}`)
		return
	}
	fmt.Fprintln(w, `{"message":{"content":"complete"},"done":true}`)
}

func TestIntegrationWorkflowFreezesReferencedGenerationControls(t *testing.T) {
	received := make(chan inference.ChatRequest, 3)
	entered, release := make(chan struct{}), make(chan struct{})
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		received <- req
		if req.Think == nil {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		writeControlsFixtureAnswer(w, req)
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Base", "mode": "approval", "maxSteps": 8, "thinking": "on", "maxOutputTokens": 4096})
	a := callIntegration(t, h.core, "agent.save", Doc{"name": "Automatic", "mode": "approval", "maxSteps": 2, "thinking": "auto", "maxOutputTokens": 512, "temperature": 0})
	b := callIntegration(t, h.core, "agent.save", Doc{"name": "Explicit off", "mode": "approval", "maxSteps": 2, "thinking": "off", "maxOutputTokens": 256, "temperature": 1, "toolProtocol": "structured_json"})
	wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Frozen controls", "nodes": []Doc{{"id": "first", "type": "agent", "agentId": a["id"]}, {"id": "second", "type": "agent", "agentId": b["id"]}}, "edges": []Doc{{"source": "first", "target": "second"}}})
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "Use referenced profiles"})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("workflow did not reach automatic profile")
	}
	callIntegration(t, h.core, "agent.save", Doc{"id": b["id"], "name": "Edited later", "mode": "approval", "maxSteps": 2, "thinking": "on", "maxOutputTokens": 32768, "temperature": 2})
	close(release)
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	first, second := <-received, <-received
	if first.Think != nil || first.Options["num_predict"] != float64(512) || second.Think != false || second.Options["num_predict"] != float64(256) || first.Options["temperature"] != float64(0) || second.Options["temperature"] != float64(1) {
		t.Fatalf("workflow lost frozen controls: first=%+v second=%+v", first, second)
	}
	if first.Format != nil || second.Format == nil || len(second.Tools) != 0 {
		t.Fatal("workflow lost frozen referenced tool protocols")
	}
}

func TestIntegrationEvaluationFreezesGenerationControlsAndRequest(t *testing.T) {
	received := make(chan inference.ChatRequest, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		received <- req
		if req.Think == true {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		writeControlsFixtureAnswer(w, req)
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "On baseline", "mode": "approval", "maxSteps": 2, "thinking": "on", "maxOutputTokens": 512, "temperature": 0})
	candidate := callIntegration(t, h.core, "agent.save", Doc{"name": "Off candidate", "mode": "approval", "maxSteps": 2, "thinking": "off", "maxOutputTokens": 256, "temperature": 1, "toolProtocol": "structured_json"})
	experiment := callIntegration(t, h.core, "eval.start", Doc{"workspaceId": h.workID, "baselineAgentId": "default", "candidateAgentId": candidate["id"], "tasks": []string{"Describe current controls"}})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not start")
	}
	callIntegration(t, h.core, "agent.save", Doc{"id": candidate["id"], "name": "Changed later", "mode": "approval", "maxSteps": 2, "thinking": "auto", "maxOutputTokens": 32768, "temperature": 2})
	close(release)
	var completed Doc
	waitIntegration(t, func() bool {
		completed, _ = h.core.Store.Get("experiment", str(experiment, "id"))
		return completed["status"] != "running"
	})
	first, second := <-received, <-received
	if first.Think != true || first.Options["num_predict"] != float64(512) || second.Think != false || second.Options["num_predict"] != float64(256) || first.Options["temperature"] != float64(0) || second.Options["temperature"] != float64(1) {
		t.Fatalf("evaluation controls drifted: first=%+v second=%+v", first, second)
	}
	if first.Format != nil || second.Format == nil || len(second.Tools) != 0 || obj(completed["baseline"])["toolProtocol"] != "native" || obj(completed["candidate"])["toolProtocol"] != "structured_json" {
		t.Fatal("evaluation tool protocol changed after dispatch or was not frozen")
	}
	results, _ := completed["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results=%+v", results)
	}
	if obj(completed["baseline"])["temperature"] != float64(0) || obj(completed["candidate"])["temperature"] != float64(1) {
		t.Fatalf("effective experiment profile temperature not frozen: %+v", completed)
	}
	for i, raw := range results {
		r := obj(raw)
		req := obj(r["request"])
		wantLimit, wantThink := 512, true
		wantProtocol := "native"
		if i == 1 {
			wantLimit, wantThink = 256, false
			wantProtocol = "structured_json"
		}
		if r["status"] != "completed" || req["think"] != wantThink || number(req, "maxOutputTokens", 0) != wantLimit || req["readOnly"] != true || req["temperature"] != float64(i) || req["toolProtocol"] != wantProtocol {
			t.Fatalf("missing frozen request: %+v", r)
		}
	}
}
