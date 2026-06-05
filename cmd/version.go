// cmd/version.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show nanny version and build info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("nanny %s (commit %s)\n", buildVersion, buildCommit)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
