package app

import (
	"strings"
	"testing"
)

func TestLegacyDefaultInstructionsMapToCurrentDefault(t *testing.T) {
	for _, legacy := range legacyDefaultInstructions {
		if legacy == defaultInstructions {
			t.Fatal("legacy list repeats the current default")
		}
		if got := agentInstructions(Doc{"instructions": legacy}); got != defaultInstructions {
			t.Fatalf("stored legacy default became custom text: %q", got)
		}
	}
	if !strings.Contains(defaultInstructions, "write and run a short script") {
		t.Fatal("calculation guidance missing from default instructions")
	}
}

func TestCurrentCalculationDefaultDoesNotDuplicateOrDropCustomInstructions(t *testing.T) {
	for _, value := range []string{"", " \n\t", defaultInstructions} {
		if got := agentInstructions(Doc{"instructions": value}); got != defaultInstructions {
			t.Fatalf("default instructions duplicated: %q", got)
		}
	}
	// A user extension or near-match to a historical default is custom text.
	// It must remain byte-for-byte present after the current host instructions.
	for _, custom := range []string{
		"Use the customer's currency.\nDo not change source files.",
		legacyDefaultInstructions[0] + "\nUser-owned extension.",
		strings.Replace(legacyDefaultInstructions[0], "a careful local", "a concise local", 1),
	} {
		want := defaultInstructions + "\n\n" + custom
		if got := agentInstructions(Doc{"instructions": custom}); got != want {
			t.Fatalf("custom instructions were changed or mistaken for a historical default: %q", got)
		}
	}
}
