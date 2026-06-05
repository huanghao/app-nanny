package launchd

import (
	"fmt"
	"os"
	"path/filepath"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.app-nanny.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>Crashed</key>
        <true/>
    </dict>
    <key>StandardOutPath</key>
    <string>%s/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>%s/daemon.log</string>
</dict>
</plist>
`

// PlistPath returns the path where the LaunchAgent plist should be installed.
func PlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.app-nanny.daemon.plist")
}

// Install writes the LaunchAgent plist file.
func Install(binaryPath, dataDir string) error {
	plist := fmt.Sprintf(plistTemplate, binaryPath, dataDir, dataDir)
	path := PlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return nil
}

// Uninstall removes the LaunchAgent plist.
func Uninstall() error {
	path := PlistPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}
