// cmd/errors.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var errorsLast bool
var errorsCopy bool

var errorsCmd = &cobra.Command{
	Use:   "errors <name>[/process]",
	Short: "Show captured error events",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])

		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("errors", ipc.ErrorsParams{
			Name:    project,
			Process: process,
			Last:    errorsLast,
		})
		if err != nil {
			return err
		}
		var result ipc.ErrorsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		if len(result.Events) == 0 {
			fmt.Println("no error events recorded")
			return nil
		}

		var sb strings.Builder
		for _, e := range result.Events {
			fmt.Fprintf(&sb, "── %s [%s] ──\n", e.Time, e.Key)
			for _, l := range e.Lines {
				fmt.Fprintf(&sb, "  %s\n", l)
			}
			fmt.Fprintln(&sb)
		}

		output := sb.String()
		fmt.Print(output)

		if errorsCopy {
			return copyToClipboard(output)
		}
		return nil
	},
}

func init() {
	errorsCmd.Flags().BoolVar(&errorsLast, "last", false, "Show only the most recent error event")
	errorsCmd.Flags().BoolVar(&errorsCopy, "copy", false, "Copy the last error event to clipboard (macOS pbcopy)")
	rootCmd.AddCommand(errorsCmd)
}

func copyToClipboard(text string) error {
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader(text)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("pbcopy failed: %w", err)
	}
	fmt.Println("(copied to clipboard)")
	return nil
}
