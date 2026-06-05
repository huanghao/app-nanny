package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/launchd"
	"github.com/spf13/cobra"
)

// jsonUnmarshal is a local alias to avoid shadowing issues.
var jsonUnmarshal = json.Unmarshal

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the nanny background daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the nanny daemon in the background",
	RunE: func(cmd *cobra.Command, args []string) error {
		if isDaemonRunning() {
			fmt.Println("daemon is already running")
			return nil
		}
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find executable: %w", err)
		}

		// Ensure data dir exists before opening log
		if err := os.MkdirAll(DataDir(), 0755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}

		proc := exec.Command(self, "serve")
		proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		logPath := DataDir() + "/daemon.log"
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open daemon log: %w", err)
		}
		proc.Stdout = logFile
		proc.Stderr = logFile

		if err := proc.Start(); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Printf("daemon started (pid %d), log: %s\n", proc.Process.Pid, logPath)
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the nanny daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("shutdown", nil)
		if err != nil {
			if ipc.IsDaemonNotRunning(err) {
				fmt.Println("daemon is not running")
				return nil
			}
			return err
		}
		fmt.Println("daemon stopped")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status and version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isDaemonRunning() {
			fmt.Println("daemon: stopped")
			fmt.Printf("cli:    nanny %s (commit %s)\n", buildVersion, buildCommit)
			return nil
		}

		// Query daemon version via IPC
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("version", nil)
		daemonVer, daemonCom := "unknown", "unknown"
		if err == nil && resp != nil {
			var info map[string]string
			if jsonErr := jsonUnmarshal(resp.Result, &info); jsonErr == nil {
				daemonVer = info["version"]
				daemonCom = info["commit"]
			}
		}

		fmt.Printf("daemon: running  nanny %s (commit %s)\n", daemonVer, daemonCom)
		fmt.Printf("cli:    nanny %s (commit %s)\n", buildVersion, buildCommit)
		if daemonVer != buildVersion {
			fmt.Println("⚠  version mismatch — run: nanny daemon stop && nanny daemon start")
		}
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install nanny as a macOS LaunchAgent (auto-start on login)",
	RunE: func(cmd *cobra.Command, args []string) error {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		if err := launchd.Install(self, DataDir()); err != nil {
			return err
		}
		fmt.Printf("installed LaunchAgent: %s\n", launchd.PlistPath())
		fmt.Println("nanny daemon will start automatically on next login.")
		fmt.Println("To start now: nanny daemon start")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the nanny LaunchAgent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := launchd.Uninstall(); err != nil {
			return err
		}
		fmt.Println("LaunchAgent removed.")
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

func isDaemonRunning() bool {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
