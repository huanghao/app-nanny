// cmd/gc.go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var gcDryRun bool

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Remove log files orphaned by removed projects, and cap daemon.log",
	Long: `gc cleans up disk state nanny's own log rotation doesn't cover:

  - log files under ~/.local/share/app-nanny/logs/ left behind by a
    project (or one of its [processes.*]) that was removed with
    "nanny remove" or renamed — nanny's per-process rotation caps an
    active log's size, but has no notion of "this project no longer
    exists", so these just accumulate forever otherwise.
  - daemon.log, which launchd writes to directly and nanny's rotator
    never touches, capped to its most recent ~10MB if it's grown past 50MB.

It never touches a project's own data (kolab's data/, md-viewer's
annotations.db, etc.) — only nanny's own log capture.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("gc", ipc.GCParams{DryRun: gcDryRun})
		if err != nil {
			return err
		}
		var result ipc.GCResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		removeVerb, capVerb := "removed", "capped"
		if gcDryRun {
			removeVerb, capVerb = "would remove", "would cap"
		}

		if len(result.RemovedLogs) == 0 {
			fmt.Println("no orphaned logs found")
		} else {
			fmt.Printf("%s %d orphaned log file(s), freeing %s:\n", removeVerb, len(result.RemovedLogs), formatBytes(result.FreedBytes))
			for _, name := range result.RemovedLogs {
				fmt.Printf("  %s\n", name)
			}
		}

		if result.DaemonLogCapped {
			fmt.Printf("%s daemon.log, freeing %s\n", capVerb, formatBytes(result.DaemonLogFreedBytes))
		}

		return nil
	},
}

func init() {
	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "Show what would be cleaned without touching disk")
	rootCmd.AddCommand(gcCmd)
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
