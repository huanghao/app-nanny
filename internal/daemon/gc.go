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

// logCapBytes is the size threshold past which a log file gets truncated
// to its tail. Mirrors the per-process rotation cap in rotator.go (that
// one keeps 3 size-capped backups instead, for logs actively written
// through this process — see the doc comment on Manager.GC for why this
// separate, coarser mechanism exists at all).
const logCapBytes = 50 * 1024 * 1024

// logCapKeepBytes is how much of an oversized file survives capping:
// enough recent context to be useful, small enough that repeated GCs
// stay cheap.
const logCapKeepBytes = 10 * 1024 * 1024

// rotationBackups mirrors the maxFiles=3 passed to NewRotatingFile at this
// package's call sites — GC needs to treat "<key>.log.2" as belonging to
// <key> just like "<key>.log" does.
const rotationBackups = 3

// GCResult is returned to IPC callers as ipc.GCResult; kept as a distinct
// type in this package so daemon logic doesn't import internal/ipc.
type GCResult struct {
	RemovedLogs []string // orphaned files/directories deleted outright
	FreedBytes  int64
	CappedLogs  []string // oversized files truncated to their tail in place
	CappedBytes int64    // total bytes trimmed across CappedLogs
}

// GC cleans up two things nanny's own per-process log rotation
// (rotator.go) doesn't cover, because that one only ever caps a *known,
// currently active* log file:
//
//  1. Log files under logDir with no owning registered project/process —
//     left behind by `nanny remove`, or by renaming a [processes.*] entry.
//  2. Oversized files anywhere nanny knows to look that aren't going
//     through RotatingFile at all: daemon.log (launchd's raw
//     StandardOutPath redirect), and logDir/<project>/ subdirectories —
//     the convention (see nanny skill docs) for a project's *own* log
//     files that nanny never captures because nanny didn't spawn the
//     writer (a companion GUI app, a browser-launched helper process,
//     …). Any file placed there is nanny's to size-cap on sight, same as
//     logrotate doesn't care who wrote the file it's rotating — but
//     whole-file identity inside a still-registered project's directory
//     is the project's own business, not something GC tries to validate
//     file-by-file the way it does for its own flat capture files.
//
// It never touches anything else: no project's own data (kolab's data/,
// md-viewer's annotations.db, …) is nanny's to manage, only its own log
// capture plus whatever a project deliberately opts into by writing under
// logDir/<project>/.
func (m *Manager) GC(dryRun bool) (GCResult, error) {
	m.mu.Lock()
	reg := m.registry
	logDir := m.logDir
	m.mu.Unlock()

	result := GCResult{}
	registered := reg.List()

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return result, fmt.Errorf("read log dir: %w", err)
		}
		entries = nil
	}

	valid := validLogBasenames(registered)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if valid[e.Name()] {
			continue
		}
		path := filepath.Join(logDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		result.RemovedLogs = append(result.RemovedLogs, e.Name())
		result.FreedBytes += info.Size()
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return result, fmt.Errorf("remove %s: %w", e.Name(), err)
			}
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projectDir := filepath.Join(logDir, e.Name())
		if _, ok := registered[e.Name()]; !ok {
			size, err := dirSize(projectDir)
			if err != nil {
				return result, fmt.Errorf("measure %s: %w", e.Name(), err)
			}
			result.RemovedLogs = append(result.RemovedLogs, e.Name()+"/")
			result.FreedBytes += size
			if !dryRun {
				if err := os.RemoveAll(projectDir); err != nil {
					return result, fmt.Errorf("remove %s: %w", e.Name(), err)
				}
			}
			continue
		}
		if err := capOversizedFiles(projectDir, e.Name()+"/", dryRun, &result); err != nil {
			return result, err
		}
	}

	sort.Strings(result.RemovedLogs)
	sort.Strings(result.CappedLogs)

	daemonLogPath := filepath.Join(filepath.Dir(logDir), "daemon.log")
	if err := capIfOversized(daemonLogPath, "daemon.log", dryRun, &result); err != nil {
		return result, err
	}

	return result, nil
}

// validLogBasenames returns every flat log filename (including rotation
// backups) that some currently registered project/process's captured
// stdout/stderr could still produce. Built from each project's own
// app-nanny.toml rather than parsing filenames back apart, because
// sanitized keys replace "/" with "-" (logPath) and project names
// themselves may contain "-" (mnl-workers-portal, my-repo-status, …),
// making that reverse mapping ambiguous.
//
// A project whose app-nanny.toml can no longer be read (directory moved
// or deleted since it was registered) falls back to treating its bare
// project name as the only valid key — its own [processes.*] names, if
// any, become unrecoverable and their logs get swept up as orphans. That
// matches this being a log cache, not data: worst case, a stale log for a
// project you can no longer even load gets deleted.
func validLogBasenames(registered map[string]string) map[string]bool {
	valid := make(map[string]bool)
	for name, dir := range registered {
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

// capOversizedFiles walks a still-registered project's own log directory
// (logDir/<project>/) and caps any file over logCapBytes in place. Unlike
// the flat capture files above, it doesn't try to validate individual
// filenames against anything — the directory itself being under a
// registered project is the only check; what a project names its own
// files inside it is that project's business.
func capOversizedFiles(dir, labelPrefix string, dryRun bool, result *GCResult) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // one level deep — matches the documented convention
		}
		if err := capIfOversized(filepath.Join(dir, e.Name()), labelPrefix+e.Name(), dryRun, result); err != nil {
			return err
		}
	}
	return nil
}

// capIfOversized truncates the file at path to its tail (logCapKeepBytes)
// if it's grown past logCapBytes, recording it under label in result.
func capIfOversized(path, label string, dryRun bool, result *GCResult) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() <= logCapBytes {
		return nil
	}
	freed := info.Size() - logCapKeepBytes
	result.CappedLogs = append(result.CappedLogs, label)
	result.CappedBytes += freed
	if dryRun {
		return nil
	}
	if err := truncateKeepTail(path, logCapKeepBytes); err != nil {
		return fmt.Errorf("cap %s: %w", label, err)
	}
	return nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}

// truncateKeepTail shrinks the file at path to its last keepBytes,
// in place on the same inode (copytruncate-style) rather than
// remove-and-recreate. A file capped by GC may still be actively held
// open by its writer (daemon.log by launchd across the daemon's whole
// life; a project's own companion process writing under logDir/<project>/
// for as long as it runs) — none of them reopen the path on rotation, so
// replacing the directory entry would leave the writer appending into an
// unlinked, invisible inode forever. Truncating and rewriting the same
// inode is safe for an O_APPEND writer because O_APPEND recomputes the
// write offset from the current file size on every write — there's a
// small race window between the truncate and the rewrite where a
// concurrent append could be overwritten, the same documented trade-off
// `logrotate`'s copytruncate makes; acceptable here since these are
// diagnostic logs, not data.
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
