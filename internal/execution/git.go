package execution

import (
	"os"
	"path/filepath"
)

// Apple's /usr/bin/git is a launcher that may fork xcodebuild. Prefer the
// installed tool itself so read-only Git does not require a fork allowance.
func GitExecutable() string {
	for _, path := range []string{"/Applications/Xcode.app/Contents/Developer/usr/bin/git", "/Library/Developer/CommandLineTools/usr/bin/git", "/opt/homebrew/bin/git"} {
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				return resolved
			}
		}
	}
	return "/usr/bin/git"
}

// GitProtectedExclusions prevents diff from reconstructing protected content
// from Git's object database, where filesystem pathname denial cannot see it.
func GitProtectedExclusions() []string {
	return []string{
		":(glob,icase,exclude)**/.env*",
		":(glob,icase,exclude)**/.ssh/**",
		":(glob,icase,exclude)**/.aws/**",
		":(glob,icase,exclude)**/.gnupg/**",
		":(glob,icase,exclude)**/.codex/**",
		":(glob,icase,exclude)**/.gemmadesktop/**",
		":(glob,icase,exclude)**/.gemma-desktop/**",
		":(glob,icase,exclude)**/.npmrc",
		":(glob,icase,exclude)**/.pypirc",
		":(glob,icase,exclude)**/.netrc",
		":(glob,icase,exclude)**/.git-credentials",
		":(glob,icase,exclude)**/.docker/**",
		":(glob,icase,exclude)**/.kube/**",
		":(glob,icase,exclude)**/.azure/**",
	}
}
