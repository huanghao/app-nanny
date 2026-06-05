package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nanny",
	Short: "Local dev service manager",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// SocketPath returns the Unix socket path for daemon IPC.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "app-nanny", "app-nanny.sock")
}

// DataDir returns the base data directory.
func DataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "app-nanny")
}

// readNameFromToml reads the project name from an app-nanny.toml.
func readNameFromToml(path string) (string, error) {
	cfg, err := config.LoadProject(path)
	if err != nil {
		return "", err
	}
	return cfg.Name, nil
}
