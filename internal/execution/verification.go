package execution

import (
	"errors"
	"gemmadesktop.local/app/internal/workspace"
	"strings"
)

// VerificationContract is admitted by the host and frozen with a run. The model
// cannot select a different verifier, working directory or preservation policy.
type VerificationContract struct {
	Command               []string `json:"command"`
	WorkingDirectory      string   `json:"workingDirectory,omitempty"`
	ProtectedPaths        []string `json:"protectedPaths,omitempty"`
	RequireRegressionTest bool     `json:"requireRegressionTest"`
}

func (v VerificationContract) Validate() error {
	if len(v.Command) == 0 || len(v.Command) > 257 || strings.TrimSpace(v.Command[0]) == "" {
		return errors.New("verification requires a bounded declared command")
	}
	for _, a := range v.Command {
		if len(a) > 256<<10 || strings.ContainsRune(a, 0) {
			return errors.New("invalid verification command argument")
		}
	}
	if _, err := workspace.Clean(v.WorkingDirectory, false); err != nil {
		return err
	}
	return workspace.ValidateProtectedPaths(v.ProtectedPaths)
}
