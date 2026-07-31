// internal/daemon/path.go
package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// ensureToolPath prepends standard user tool directories to PATH when the
// daemon was started with a bare environment — launchd starts agents with
// PATH=/usr/bin:/bin:/usr/sbin:/sbin, and managed commands run via
// `sh -c`, which never reads ~/.zshrc. Toolchains that only exist on the
// interactive-shell PATH (go via goenv shims, bun, node via nvm, cargo…)
// would otherwise be unresolvable ("sh: go: command not found").
//
// Only directories that exist on disk are added, and entries already on
// PATH are left alone, so this is a no-op when the daemon was started from
// a terminal with a full environment.
func ensureToolPath() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	candidates := []string{
		filepath.Join(home, "bin"),
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".goenv", "bin"),
		filepath.Join(home, ".goenv", "shims"),
		filepath.Join(home, ".bun", "bin"),
	}
	// nvm: resolve the default alias to its version's bin dir, mirroring the
	// ~/.zshrc logic.
	if alias, err := os.ReadFile(filepath.Join(home, ".nvm", "alias", "default")); err == nil {
		v := strings.TrimPrefix(strings.TrimSpace(string(alias)), "v")
		if matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "v"+v+"*", "bin")); len(matches) > 0 {
			candidates = append(candidates, matches[len(matches)-1])
		}
	}

	path := os.Getenv("PATH")
	var prepend []string
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if !strings.Contains(":"+path+":", ":"+dir+":") {
			prepend = append(prepend, dir)
		}
	}
	if len(prepend) > 0 {
		os.Setenv("PATH", strings.Join(prepend, ":")+":"+path)
	}
}
