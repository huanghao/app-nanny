package main

import "github.com/huanghao/app-nanny/cmd"

// version and commit are injected at build time via -ldflags.
// See justfile for the build command.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	cmd.SetVersion(version, commit)
	cmd.Execute()
}
