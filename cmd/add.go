package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [dir]",
	Short: "Register a project (reads app-nanny.toml in dir)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		tomlPath := filepath.Join(abs, "app-nanny.toml")
		if _, err := os.Stat(tomlPath); err != nil {
			return fmt.Errorf("no app-nanny.toml found in %s", abs)
		}
		name, err := readNameFromToml(tomlPath)
		if err != nil {
			return err
		}
		client := ipc.NewClient(SocketPath())
		_, err = client.Call("add", ipc.AddParams{Name: name, Path: abs})
		if err != nil {
			return err
		}
		fmt.Printf("registered %q (%s)\n", name, abs)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
