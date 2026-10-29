package media

import "testing"

func TestCitationGroupsValidateEveryMemberAndDeduplicate(t *testing.T) {
	evidence := []Evidence{{FrameID: "frame-a", Label: "F001", TimeSeconds: 2}, {FrameID: "frame-b", Label: "F002", TimeSeconds: 5}, {FrameID: "frame-c", Label: "F003", TimeSeconds: 8}}
	for _, test := range []struct {
		name, text string
		want       []string
	}{
		{"single", "A red shape [F001].", []string{"F001"}},
		{"grouped", "Two views [F001, F002].", []string{"F001", "F002"}},
		{"all grouped", "The clip contains these scenes [F001, F002, F003].", []string{"F001", "F002", "F003"}},
		{"repeated", "First [F002, F001, F002], later [F001] [F003, F002].", []string{"F002", "F001", "F003"}},
		{"whitespace", "Views [ \tF001 ,\n F002\t ].", []string{"F001", "F002"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateCitations(test.text, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("citation count=%d want=%d", len(got), len(test.want))
			}
			for i, label := range test.want {
				if got[i].Label != label {
					t.Fatalf("citation order=%v want=%v", got, test.want)
				}
				found := false
				for _, e := range evidence {
					if e.Label == label {
						found = got[i].FrameID == e.FrameID && got[i].TimeSeconds == e.TimeSeconds
					}
				}
				if !found {
					t.Fatalf("citation did not retain host identity/timestamp: %+v", got[i])
				}
			}
		})
	}
}

func TestCitationGroupsRejectUnknownAndMalformedWithoutPartialSuccess(t *testing.T) {
	evidence := []Evidence{{FrameID: "frame-a", Label: "F001", TimeSeconds: 2}, {FrameID: "frame-b", Label: "F002", TimeSeconds: 5}}
	for _, text := range []string{
		"No citations.", "Bare F001 is not a citation.", "Unknown [F999].", "Mixed [F001, F999].", "Mixed [F999, F001].",
		"Already valid [F001] then unknown [F002, F999].", "Empty [].", "Trailing [F001,].", "Leading [,F001].", "Double comma [F001,,F002].",
		"Partial [F01].", "Extra digit [F0001].", "Lowercase [f001].", "Missing prefix [001].", "Extra member [F001, text].", "Missing comma [F001 F002].", "Conjunction [F001 and F002].", "Range [F001-F002].", "Unicode range [F001–F002].",
		"Nested [F001,[F002]].", "Unclosed [F001, F002", "Already valid [F001] then partial [F002", "Extra closing [F001]].",
		"External https://example.com [F001, F002].", "File file:///tmp/video [F001].", "Data data:text/plain,secret [F001].", "Script javascript:alert(1) [F001].",
	} {
		t.Run(text, func(t *testing.T) {
			got, err := validateCitations(text, evidence)
			if err == nil || got != nil {
				t.Fatalf("invalid citations accepted or partially returned: citations=%+v err=%v", got, err)
			}
		})
	}
}
