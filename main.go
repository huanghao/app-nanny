package main

import (
	"github.com/huanghao/app-nanny/cmd"
	"github.com/huanghao/app-nanny/internal/daemon"
)

// version and commit are injected at build time via -ldflags.
// See justfile for the build command.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	cmd.SetVersion(version, commit)
	daemon.SetVersion(version, commit) // so daemon can report its own version via IPC
	cmd.Execute()
}
