package cmd

import (
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name>[/process]",
	Short: "Restart a project or a single process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("restart", ipc.RestartParams{Name: project, Process: process})
		if err != nil {
			return err
		}
		fmt.Printf("restarted %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
