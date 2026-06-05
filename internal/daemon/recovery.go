// internal/daemon/recovery.go
package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type RuntimeEntry struct {
	PID       int       `json:"pid"`
	PGID      int       `json:"pgid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

type Runtime struct {
	path    string
	entries map[string]RuntimeEntry
}

func NewRuntime(path string) *Runtime {
	r := &Runtime{path: path, entries: map[string]RuntimeEntry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &r.entries)
	}
	return r
}

func (r *Runtime) Set(key string, e RuntimeEntry) {
	r.entries[key] = e
}

func (r *Runtime) Delete(key string) {
	delete(r.entries, key)
}

func (r *Runtime) All() map[string]RuntimeEntry {
	out := make(map[string]RuntimeEntry, len(r.entries))
	for k, v := range r.entries {
		out[k] = v
	}
	return out
}

func (r *Runtime) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}

func CleanupOrphans(rt *Runtime) ([]string, error) {
	var killed []string
	for key, entry := range rt.All() {
		alive := processAlive(entry.PID)
		if alive && entry.PGID > 0 {
			log.Printf("recovery: killing orphan %q (pid=%d pgid=%d)", key, entry.PID, entry.PGID)
			_ = syscall.Kill(-entry.PGID, syscall.SIGTERM)
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(-entry.PGID, syscall.SIGKILL)
			killed = append(killed, key)
		}
		rt.Delete(key)
	}
	return killed, rt.Save()
}

// LoadOrphans reads runtime.json and separates alive vs dead entries.
// Alive entries are returned for adoption into the Manager (NOT killed).
// Dead entries are cleared from runtime.
// This is the new behavior: dev services survive daemon restarts.
func LoadOrphans(rt *Runtime) map[string]RuntimeEntry {
	alive := make(map[string]RuntimeEntry)
	for key, entry := range rt.All() {
		if processAlive(entry.PID) {
			alive[key] = entry
			log.Printf("recovery: found running process %q (pid=%d)", key, entry.PID)
		}
		// Always clear from runtime — it will be re-added when adopted
		rt.Delete(key)
	}
	rt.Save() //nolint:errcheck
	return alive
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
