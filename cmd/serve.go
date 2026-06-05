package cmd

import (
	"log"

	"github.com/huanghao/app-nanny/internal/daemon"
	"github.com/spf13/cobra"
)

// serveCmd is the internal subcommand run by the daemon process.
// It is hidden from normal help output.
var serveCmd = &cobra.Command{
	Use:    "serve",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.SetPrefix("[nanny-daemon] ")
		return daemon.Run(SocketPath(), DataDir())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
