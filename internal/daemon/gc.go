// internal/daemon/gc.go
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/huanghao/app-nanny/internal/config"
)

// daemonLogMaxSize mirrors the per-process rotation cap in rotator.go.
// daemon.log itself isn't written through RotatingFile (it's launchd's
// StandardOutPath, redirected before this binary ever runs), so nothing
// enforces this cap automatically — GC is what applies it.
const daemonLogMaxSize = 50 * 1024 * 1024

// daemonLogKeepBytes is how much of daemon.log survives a cap: enough
// recent context to be useful, small enough that repeated GCs stay cheap.
const daemonLogKeepBytes = 10 * 1024 * 1024

// rotationBackups mirrors the maxFiles=3 passed to NewRotatingFile at this
// package's call sites — GC needs to treat "<key>.log.2" as belonging to
// <key> just like "<key>.log" does.
const rotationBackups = 3

// GCResult is returned to IPC callers as ipc.GCResult; kept as a distinct
// type in this package so daemon logic doesn't import internal/ipc.
type GCResult struct {
	RemovedLogs         []string
	FreedBytes          int64
	DaemonLogCapped     bool
	DaemonLogFreedBytes int64
}

// GC removes log files under logDir that no longer belong to any
// currently registered project or process (left behind by `nanny remove`,
// or by a project renaming a [processes.*] entry), and caps daemon.log if
// it's grown past daemonLogMaxSize. Both cases exist because nanny's
// per-process log rotation (rotator.go) only ever caps *known, currently
// active* log files — it has no notion of "this project doesn't exist
// anymore" or "this file isn't going through RotatingFile at all"
// (daemon.log is launchd's raw StandardOutPath redirect).
//
// It never touches anything outside dataDir/logs and dataDir/daemon.log:
// no project's own data (kolab's data/, md-viewer's annotations.db, …) is
// nanny's to manage.
func (m *Manager) GC(dryRun bool) (GCResult, error) {
	m.mu.Lock()
	reg := m.registry
	logDir := m.logDir
	m.mu.Unlock()

	result := GCResult{}

	valid := validLogBasenames(reg)

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return result, fmt.Errorf("read log dir: %w", err)
		}
		entries = nil
	}
	for _, e := range entries {
		if e.IsDir() || valid[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result.RemovedLogs = append(result.RemovedLogs, e.Name())
		result.FreedBytes += info.Size()
		if !dryRun {
			if err := os.Remove(filepath.Join(logDir, e.Name())); err != nil {
				return result, fmt.Errorf("remove %s: %w", e.Name(), err)
			}
		}
	}
	sort.Strings(result.RemovedLogs)

	daemonLogPath := filepath.Join(filepath.Dir(logDir), "daemon.log")
	if info, err := os.Stat(daemonLogPath); err == nil && info.Size() > daemonLogMaxSize {
		result.DaemonLogCapped = true
		result.DaemonLogFreedBytes = info.Size() - daemonLogKeepBytes
		if !dryRun {
			if err := truncateKeepTail(daemonLogPath, daemonLogKeepBytes); err != nil {
				return result, fmt.Errorf("cap daemon.log: %w", err)
			}
		}
	}

	return result, nil
}

// validLogBasenames returns every log filename (including rotation
// backups) that some currently registered project/process could still
// produce. Built from each project's own app-nanny.toml rather than
// parsing filenames back apart, because sanitized keys replace "/" with
// "-" (logPath) and project names themselves may contain "-"
// (mnl-workers-portal, my-repo-status, …), making that reverse mapping
// ambiguous.
//
// A project whose app-nanny.toml can no longer be read (directory moved
// or deleted since it was registered) falls back to treating its bare
// project name as the only valid key — its own [processes.*] names, if
// any, become unrecoverable and their logs get swept up as orphans. That
// matches this being a log cache, not data: worst case, a stale log for a
// project you can no longer even load gets deleted.
func validLogBasenames(reg *config.Registry) map[string]bool {
	valid := make(map[string]bool)
	for name, dir := range reg.List() {
		var keys []string
		cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err == nil && cfg.IsModeB() {
			for proc := range cfg.Processes {
				keys = append(keys, name+"/"+proc)
			}
		} else {
			keys = []string{name}
		}
		for _, key := range keys {
			base := strings.ReplaceAll(key, "/", "-")
			valid[base+".log"] = true
			for i := 1; i <= rotationBackups; i++ {
				valid[fmt.Sprintf("%s.log.%d", base, i)] = true
			}
		}
	}
	return valid
}

// truncateKeepTail shrinks the file at path to its last keepBytes,
// in place on the same inode (copytruncate-style) rather than
// remove-and-recreate. daemon.log is held open by launchd across the
// life of the daemon (StandardOutPath, not something this process
// reopens), so replacing the directory entry would leave launchd
// appending into an unlinked, invisible inode forever. Truncating and
// rewriting the same inode is safe for an O_APPEND writer because
// O_APPEND recomputes the write offset from the current file size on
// every write — there's a small race window between the truncate and the
// rewrite where a concurrent append could be overwritten, the same
// documented trade-off `logrotate`'s copytruncate makes; acceptable here
// since this is a diagnostic log, not data.
func truncateKeepTail(path string, keepBytes int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size <= keepBytes {
		return nil
	}

	tail := make([]byte, keepBytes)
	if _, err := f.ReadAt(tail, size-keepBytes); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(tail, 0); err != nil {
		return err
	}
	return nil
}
