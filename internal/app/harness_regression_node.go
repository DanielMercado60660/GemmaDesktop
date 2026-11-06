package app

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
)

func nodeProductionPaths(task harness.Task) []string {
	paths := []string{}
	for name := range task.Public {
		ext := filepath.Ext(name)
		if (ext != ".js" && ext != ".mjs" && ext != ".cjs") || contains(task.ProtectedFiles, name) {
			continue
		}
		base := filepath.Base(name)
		test := strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "spec_")
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(name)), "/") {
			test = test || part == "test" || part == "tests" || part == "__tests__"
		}
		for _, pattern := range task.RegressionTestGlobs {
			match, _ := filepath.Match(pattern, base)
			test = test || match
		}
		if !test {
			paths = append(paths, filepath.ToSlash(name))
		}
	}
	sort.Strings(paths)
	return paths
}

var nodeFailureHeader = regexp.MustCompile(`(?m)^( *)not ok [0-9]+ - ([^\r\n]+)$`)
var nodeFileLocation = regexp.MustCompile(`^(.+):[1-9][0-9]*:[1-9][0-9]*$`)
var nodeNamedFrame = regexp.MustCompile(`^(?:at )?(.*?) \((.+):[1-9][0-9]*:[1-9][0-9]*\)$`)
var nodeNativeIterationFrame = regexp.MustCompile(`^(?:at )?Array\.(?:map|filter|forEach|reduce|reduceRight|some|every|find|findIndex|flatMap|sort) \(<anonymous>\)$`)

func nodeWorkspacePath(name string) string {
	if strings.HasPrefix(name, "file:") {
		u, err := url.Parse(name)
		if err != nil || u.Scheme != "file" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
			return ""
		}
		name = u.Path
	}
	if !strings.HasPrefix(name, "/workspace/") {
		return ""
	}
	return pythonFramePath(name)
}

func nodeScalar(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if strings.HasPrefix(value, `"`) {
		var decoded string
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return decoded
		}
	}
	return value
}

func nodeSummaryCount(out, key string) int {
	re := regexp.MustCompile(`(?m)^# ` + regexp.QuoteMeta(key) + ` ([0-9]+)$`)
	rows := re.FindAllStringSubmatch(out, -1)
	if len(rows) != 1 {
		return -1
	}
	value, err := strconv.Atoi(rows[0][1])
	if err != nil {
		return -1
	}
	return value
}

// Interpret bounded TAP diagnostics, not exception words printed by a test.
// Unknown formats, module wrappers, hooks and ambiguous throw sites fail closed.
func nodeRegressionFailure(r execution.Result, selectedTests, originalPaths []string) bool {
	if r.ExitCode <= 0 || r.Truncated || r.Cancelled {
		return false
	}
	out := strings.ReplaceAll(r.Stdout, "\r\n", "\n")
	headers := nodeFailureHeader.FindAllStringSubmatchIndex(out, -1)
	if !strings.HasPrefix(out, "TAP version 13\n") || len(headers) == 0 || nodeSummaryCount(out, "tests") <= 0 || nodeSummaryCount(out, "fail") <= 0 || nodeSummaryCount(out, "cancelled") != 0 {
		return false
	}
	type acceptedFailure struct {
		position, indent int
		path             string
	}
	accepted := []acceptedFailure{}
	for _, header := range headers {
		indent, name := out[header[2]:header[3]], out[header[4]:header[5]]
		lines := strings.Split(out[header[1]:], "\n")
		fields := map[string]string{}
		stack := []string{}
		started, ended, inStack := false, false, false
		for _, line := range lines {
			if !started {
				if line == indent+"  ---" {
					started = true
				} else if strings.TrimSpace(line) != "" {
					return false
				}
				continue
			}
			if line == indent+"  ..." {
				ended = true
				break
			}
			if inStack && strings.HasPrefix(line, indent+"    ") {
				stack = append(stack, strings.TrimPrefix(line, indent+"    "))
				continue
			}
			inStack = false
			if !strings.HasPrefix(line, indent+"  ") || strings.HasPrefix(line, indent+"   ") {
				continue
			}
			key, value, ok := strings.Cut(strings.TrimPrefix(line, indent+"  "), ": ")
			if !ok {
				continue
			}
			if _, duplicate := fields[key]; duplicate {
				return false
			}
			fields[key] = nodeScalar(value)
			inStack = key == "stack" && (value == "|-" || value == "|")
		}
		location := nodeFileLocation.FindStringSubmatch(fields["location"])
		if !ended || len(location) == 0 || !contains(selectedTests, nodeWorkspacePath(location[1])) || fields["code"] != "ERR_ASSERTION" && fields["code"] != "ERR_TEST_FAILURE" {
			return false
		}
		path := nodeWorkspacePath(location[1])
		if fields["failureType"] == "subtestsFailed" {
			// A failed suite/parent may summarize already validated nested tests;
			// it cannot independently supply red evidence.
			subtests := regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(indent+"# Subtest: "+name)+`$`).FindAllStringIndex(out[:header[0]], -1)
			if len(subtests) == 0 {
				return false
			}
			found := false
			for _, child := range accepted {
				found = found || child.indent > len(indent) && child.position > subtests[len(subtests)-1][0] && child.path == path
			}
			if !found {
				return false
			}
			continue
		}
		if fields["failureType"] != "testCodeFailure" || fields["type"] != "test" || fields["name"] == "" || fields["name"] == "SyntaxError" || fields["name"] == "AbortError" || fields["name"] == "TimeoutError" || fields["name"] == "SystemError" {
			return false
		}
		executed, origin := false, ""
		for _, line := range stack {
			line = strings.TrimSpace(line)
			// Native iteration frames cannot supply or replace a throw origin.
			// They may appear after an already attributed production callback.
			if origin != "" && nodeNativeIterationFrame.MatchString(line) {
				continue
			}
			function, source := "", ""
			if frame := nodeNamedFrame.FindStringSubmatch(line); len(frame) > 0 {
				function, source = frame[1], frame[2]
			} else if frame := nodeFileLocation.FindStringSubmatch(strings.TrimPrefix(line, "at ")); len(frame) > 0 {
				source = frame[1]
			} else {
				return false
			}
			if strings.HasPrefix(source, "node:internal/modules/") {
				return false
			}
			if strings.HasPrefix(source, "node:") {
				continue
			}
			framePath := nodeWorkspacePath(source)
			if framePath == "" {
				return false
			}
			if origin == "" {
				origin = framePath
			}
			executed = executed || framePath == path && strings.HasPrefix(strings.TrimPrefix(function, "async "), "TestContext.")
		}
		if !executed || origin == "" || fields["code"] == "ERR_ASSERTION" && fields["name"] != "AssertionError" || fields["code"] == "ERR_TEST_FAILURE" && !contains(originalPaths, origin) {
			return false
		}
		accepted = append(accepted, acceptedFailure{header[0], len(indent), path})
	}
	return len(accepted) > 0
}
