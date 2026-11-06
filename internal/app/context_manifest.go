package app

import (
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/workspace"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type ContextManifest struct {
	Notice          string                 `json:"notice,omitempty"`
	Version         int                    `json:"version"`
	Digest          string                 `json:"digest"`
	Sources         []engine.ContextSource `json:"sources"`
	Bytes           int                    `json:"bytes"`
	EstimatedTokens int                    `json:"estimatedTokens"`
	EstimateMethod  string                 `json:"estimateMethod"`
	Omissions       []string               `json:"omissions"`
}

func (c *Core) freezeContext(w Doc, profile CapabilityProfile, attachments any) (ContextManifest, error) {
	result := ContextManifest{Version: 1, Sources: []engine.ContextSource{}, Omissions: []string{}, EstimateMethod: "heuristic: Unicode characters / 4, rounded up; not a tokenizer measurement"}
	root, e := workspace.Open(str(w, "path"))
	if e != nil {
		return result, e
	}
	defer root.Close()
	paths := map[string]string{}
	targets := []string{"."}
	private := append(append([]string{}, workspace.DefaultPrivatePaths...), profile.PrivatePaths...)
	if attachments != nil {
		values, ok := attachments.([]any)
		if !ok {
			return result, errors.New("attachments must be workspace paths")
		}
		if len(values) > 16 {
			return result, errors.New("at most 16 attachments")
		}
		for _, value := range values {
			p, ok := value.(string)
			if !ok {
				return result, errors.New("attachment path must be a string")
			}
			clean, err := contextWorkspacePath(p, private)
			if err != nil {
				return result, err
			}
			paths[clean] = "attachment"
			targets = append(targets, clean)
		}
	}
	if profile.ProjectGuidance {
		for _, target := range targets {
			candidates, err := guidanceAncestorPaths(target, false)
			if err != nil {
				return result, err
			}
			for _, path := range candidates {
				paths[path] = "project_guidance"
			}
		}
	}
	ordered := sortedKeys(paths)
	sort.SliceStable(ordered, func(i, j int) bool { return strings.Count(ordered[i], "/") < strings.Count(ordered[j], "/") })
	characters := 0
	for _, p := range ordered {
		kind := paths[p]
		f, err := root.Read(p)
		if err != nil {
			if kind == "project_guidance" && os.IsNotExist(err) {
				continue
			}
			return result, err
		}
		scope := "reference attachment"
		if kind == "project_guidance" {
			scope = filepath.ToSlash(filepath.Dir(p)) + "/**; applies only to files in this subtree; deeper guidance has precedence within its scope"
		}
		if len(result.Sources) >= 16 {
			return result, errors.New("context is limited to 16 actual sources; narrow attachments")
		}
		result.Sources = append(result.Sources, engine.ContextSource{Kind: kind, Path: filepath.ToSlash(p), SHA256: f.Hash, Content: f.Content, Scope: scope, TrustClass: "untrusted_source"})
		result.Bytes += len(f.Content)
		characters += utf8.RuneCountInString(f.Content)
	}
	if e = engine.ValidateContextSources(result.Sources); e != nil {
		return result, e
	}
	result.Digest = contentDigest(result.Sources)
	result.EstimatedTokens = (characters + 3) / 4
	return result, nil
}
func (c *Core) contextManifest(runID string) (any, error) {
	d, e := c.Store.Get("checkpoint", runID)
	if e != nil {
		return nil, e
	}
	if _, ok := d["contextManifest"]; ok {
		return c.contextAuditView(runID, d)
	}
	return Doc{"version": 0, "sources": []any{}, "notice": "This legacy run has no typed context manifest"}, nil
}
func sortedKeys(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
