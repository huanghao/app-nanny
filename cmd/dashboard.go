// cmd/dashboard.go
package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the web console in the browser (http://localhost:7070)",
	RunE: func(cmd *cobra.Command, args []string) error {
		url := "http://localhost:7070/static/index.html"
		fmt.Printf("Opening %s\n", url)
		return exec.Command("open", url).Start()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
