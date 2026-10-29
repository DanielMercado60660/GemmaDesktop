package app_test

import (
	"encoding/json"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/tools"
)

func TestStructuredActionsAcceptGovernedBuiltinCatalogues(t *testing.T) {
	for _, mode := range []string{"approval", "read-only"} {
		t.Run(mode, func(t *testing.T) {
			manager, err := tools.New(t.TempDir(), tools.Options{Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Close() })
			request := inference.ChatRequest{Model: "catalogue-fixture"}
			for _, definition := range manager.List() {
				request.Tools = append(request.Tools, inference.ToolDefinition{
					Type: "function", Function: inference.FunctionDefinition{
						Name: definition.Name, Description: definition.Description, Parameters: definition.Parameters,
					},
				})
			}
			if len(request.Tools) == 0 {
				t.Fatal("governed catalogue is empty")
			}
			prepared, protocol, err := engine.PrepareStructuredActions(request)
			if err != nil {
				t.Fatalf("actual %s catalogue is incompatible: %v", mode, err)
			}
			if protocol == nil || len(prepared.Tools) != 0 || prepared.Format == nil {
				t.Fatal("catalogue did not produce a structured response schema")
			}
			encoded, err := json.Marshal(prepared.Format)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				OneOf []json.RawMessage `json:"oneOf"`
			}
			if err := json.Unmarshal(encoded, &schema); err != nil {
				t.Fatal(err)
			}
			if len(schema.OneOf) != len(request.Tools)+1 {
				t.Fatal("structured schema omitted governed tools or its final-answer branch")
			}
		})
	}
}
