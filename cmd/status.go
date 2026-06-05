// cmd/status.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show detailed status for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("status", ipc.StatusParams{Name: args[0]})
		if err != nil {
			return err
		}
		var result ipc.StatusResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}
		if len(result.Processes) == 0 {
			fmt.Printf("no processes found for %q\n", args[0])
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tSTATUS\tPID\tUPTIME\tMEM\tERRORS\tLOG")
		for _, p := range result.Processes {
			mem := fmt.Sprintf("%.0fM", p.MemMB)
			if p.MemMB == 0 {
				mem = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%d\t%s\n",
				p.Key, p.Status, p.PID, p.Uptime, mem, p.ErrorCount, p.LogPath)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
