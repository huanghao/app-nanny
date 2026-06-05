package cmd

import (
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("remove", ipc.RemoveParams{Name: args[0]})
		if err != nil {
			return err
		}
		fmt.Printf("removed %q\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
