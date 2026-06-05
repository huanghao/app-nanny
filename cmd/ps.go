package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List all registered services and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("ps", nil)
		if err != nil {
			if ipc.IsDaemonNotRunning(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return err
		}

		var result ipc.PSResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		if len(result.Processes) == 0 {
			fmt.Println("no processes registered")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tPROCESS\tSTATUS\tPID\tUPTIME\tRESTARTS\tPORTS")
		for _, p := range result.Processes {
			process := p.Process
			if process == "" {
				process = "-"
			}
			ports := formatPorts(p.DeclaredPort, p.ActualPorts)
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%d\t%s\n",
				p.Project, process, p.Status, p.PID, p.Uptime, p.Restarts, ports)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}

func formatPorts(declared int, actual []int) string {
	if len(actual) > 0 {
		s := ""
		for i, p := range actual {
			if i > 0 {
				s += ","
			}
			s += fmt.Sprintf("%d", p)
		}
		return s
	}
	if declared > 0 {
		return fmt.Sprintf("%d (declared)", declared)
	}
	return "-"
}
