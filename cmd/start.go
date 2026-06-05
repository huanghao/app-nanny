package cmd

import (
	"fmt"
	"strings"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <name>[/process]",
	Short: "Start a registered project or a single process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("start", ipc.StartParams{Name: project, Process: process})
		if err != nil {
			return err
		}
		fmt.Printf("started %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

// splitTarget splits "project/process" into ("project", "process").
func splitTarget(target string) (project, process string) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return target, ""
}
