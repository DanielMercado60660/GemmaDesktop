package engine

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Optional local corpus replay: fixtures remain in ignored .runtime evidence,
// not source control. Ordinary test runs use the synthetic cases above.
func TestThinkingCycleFrozenCorpusReplay(t *testing.T) {
	input, output := os.Getenv("GEMMA_THINKING_CYCLE_REPLAY_INPUT"), os.Getenv("GEMMA_THINKING_CYCLE_REPLAY_OUTPUT")
	if input == "" {
		t.Skip("set the fixed offline corpus path to replay saved thinking deltas")
	}
	var cases []struct {
		ID       string              `json:"id"`
		Deltas   []string            `json:"deltas"`
		Expected *thinkingCycleMatch `json:"expected"`
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var results []struct {
		ID    string              `json:"id"`
		Match *thinkingCycleMatch `json:"match"`
	}
	triggers := 0
	for _, c := range cases {
		g := thinkingCycleGuard{}
		var match *thinkingCycleMatch
		for _, delta := range c.Deltas {
			if match = g.Add(delta); match != nil {
				break
			}
		}
		if match != nil {
			triggers++
		}
		if !reflect.DeepEqual(match, c.Expected) {
			t.Errorf("%s differs from frozen prototype: got %+v want %+v", c.ID, match, c.Expected)
		}
		results = append(results, struct {
			ID    string              `json:"id"`
			Match *thinkingCycleMatch `json:"match"`
		}{c.ID, match})
	}
	if output != "" {
		data, err = json.MarshalIndent(map[string]any{"version": thinkingCycleVersion, "generations": len(cases), "triggeredGenerations": triggers, "results": results}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("replayed %d ordinary thinking generations; %d triggers", len(cases), triggers)
}
