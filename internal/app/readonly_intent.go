package app

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"gemmadesktop.local/app/internal/store"
)

// This is deliberately a small recognizer for unambiguous, whole-workspace
// directives, not a general interpretation of natural language. Path-specific
// prohibitions, exceptions, quoted examples and conditional requests must not
// silently disable an otherwise authorized repair.
var workspaceNoChange = regexp.MustCompile(`(?i)^(?:please\s+)?(?:do not|don't|don’t|never)\s+(?:change|modify|edit|write(?: to)?)(?:\s*(?:,|and|or)\s*(?:change|modify|edit|write(?: to)?|create|delete|move|rename))*\s+(?:(?:any|all|the)\s+)?files(?:\s+(?:in (?:this|the) (?:workspace|project)|at all))?(?:,?\s+please)?$`)
var workspaceUnchanged = regexp.MustCompile(`(?i)^(?:please\s+)?(?:(?:leave|keep)\s+(?:(?:all|the)\s+)?files\s+(?:unchanged|untouched)|(?:leave|keep)\s+(?:this|the)\s+(?:workspace|project)\s+(?:unchanged|untouched|read-only)|(?:do not|don't|don’t|never)\s+(?:change|modify)\s+(?:this|the)\s+(?:workspace|project))$`)
var simpleContinuation = regexp.MustCompile(`(?i)^(?:please\s+)?(?:continue|keep going|go on|try again|retry|resume)(?:\s+please)?[.!]?$`)
var competingWorkspaceAction = regexp.MustCompile(`(?i)^(?:please\s+)?(?:(?:actually|instead|now|but|except)[,:]?\s+)?(?:you (?:can|may|should)\s+)?(?:create|write|modify|edit|delete|move|rename|change|fix)\b`)
var workspaceActionTarget = regexp.MustCompile(`(?i)\b(?:files?|workspace|project|directory|folder)\b|[a-z0-9_-]+\.[a-z0-9_]+\b|[a-z0-9_.-]+/[a-z0-9_.-]+`)
var readOnlyExplanation = regexp.MustCompile(`(?i)^i (?:just|only) need (?:the )?(?:numbers|answer|results|analysis|summary|explanation)$`)

func explicitWorkspaceReadOnly(prompt string) string {
	constraint, _ := parseWorkspaceReadOnly(prompt)
	return constraint
}

func parseWorkspaceReadOnly(prompt string) (string, bool) {
	fence := ""
	constraint := ""
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if marker == fence {
				fence = ""
			}
			continue
		}
		if fence != "" || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
		for _, sentence := range directiveStatements(trimmed) {
			if sentence.question {
				continue
			}
			text := strings.TrimSpace(sentence.text)
			// Decline ambiguous requests with another imperative or explicit
			// exception, rather than silently preventing a requested repair.
			if competingWorkspaceAction.MatchString(text) && workspaceActionTarget.MatchString(text) {
				return "", true
			}
			if first, rest, ok := strings.Cut(text, ","); ok {
				rest = strings.ToLower(strings.TrimSpace(rest))
				if readOnlyExplanation.MatchString(rest) {
					text = strings.TrimSpace(first)
				}
			}
			if constraint == "" && (workspaceNoChange.MatchString(text) || workspaceUnchanged.MatchString(text)) {
				constraint = text
			}
		}
	}
	return constraint, false
}

type directiveStatement struct {
	text     string
	question bool
}

// Replace quoted spans with a nonmatching marker without disturbing sentence
// boundaries outside them. Apostrophes inside words (shop's, don't) are ordinary
// text. A period inside a path such as files.csv is not a sentence boundary.
func directiveStatements(line string) []directiveStatement {
	var out []directiveStatement
	var text strings.Builder
	var quote rune
	runes := []rune(line)
	for i, r := range runes {
		wordApostrophe := (r == '\'' || r == '’') && i > 0 && i+1 < len(runes) && unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1])
		if quote != 0 {
			if r == quote && !wordApostrophe && (i == 0 || runes[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if !wordApostrophe && strings.ContainsRune("\"'`“‘", r) {
			quote = r
			if r == '“' {
				quote = '”'
			}
			if r == '‘' {
				quote = '’'
			}
			text.WriteByte(0)
			continue
		}
		if strings.ContainsRune(".!?;", r) && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
			out = append(out, directiveStatement{text: text.String(), question: r == '?'})
			text.Reset()
			continue
		}
		text.WriteRune(r)
	}
	if text.Len() > 0 {
		out = append(out, directiveStatement{text: text.String()})
	}
	return out
}

// A normal new task is admitted afresh. An explicit retry/continue (including
// focused continuation) keeps the prior run's frozen constraint, even if a
// model summary omitted it. Resuming an interrupted workflow uses its existing
// checkpoint and never passes through this inference path.
func (c *Core) workspaceReadOnlyConstraint(prompt string, previous []Doc, focused bool) (string, error) {
	constraint, currentWriteRequest := parseWorkspaceReadOnly(prompt)
	if constraint != "" {
		return constraint, nil
	}
	if currentWriteRequest {
		return "", nil
	}
	if !focused && !simpleContinuation.MatchString(strings.TrimSpace(prompt)) {
		return "", nil
	}
	if len(previous) == 0 {
		return "", nil
	}
	checkpoint, err := c.Store.Get("checkpoint", str(previous[len(previous)-1], "id"))
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if constraint := str(obj(checkpoint["workspaceReadOnly"]), "constraint"); constraint != "" {
		return constraint, nil
	}
	// Older checkpoints predate this policy but retain the exact user prompt.
	return explicitWorkspaceReadOnly(str(previous[len(previous)-1], "prompt")), nil
}

func workspaceReadOnlyInstructions(constraint string, commands bool) string {
	if constraint == "" {
		return ""
	}
	text := "\n\nWorkspace constraint (exact user wording): " + constraint + ". The host enforces a read-only workspace for this run. Do not create, edit, delete, rename, or move workspace files."
	if commands {
		text += " Use run_command with readOnly:true for calculations; use python3 -c with inline code instead of creating a helper script. Commands still require approval and cannot write workspace files."
	}
	return text
}
