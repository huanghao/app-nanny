// cmd/logs.go
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var logsFollow bool
var logsLines int

var logsCmd = &cobra.Command{
	Use:   "logs <name>[/process]",
	Short: "Show captured stdout/stderr logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])

		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("logs", ipc.LogsParams{
			Name:    project,
			Process: process,
			Lines:   logsLines,
		})
		if err != nil {
			return err
		}
		var result ipc.LogsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		for _, l := range result.Lines {
			fmt.Println(l)
		}

		if !logsFollow {
			return nil
		}

		if result.Path == "" {
			return fmt.Errorf("no log file path returned by daemon")
		}
		return tailFollow(result.Path)
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of historical lines to show")
	rootCmd.AddCommand(logsCmd)
}

// tailFollow polls the file at path for new content and writes it to stdout.
func tailFollow(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	defer f.Close()

	// Seek to end (history already printed above)
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n]) //nolint:errcheck
		}
		if err == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}
