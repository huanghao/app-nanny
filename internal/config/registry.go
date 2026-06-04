// internal/config/registry.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RegistryEntry is one project entry in the global index.
type RegistryEntry struct {
	Path string `json:"path"`
}

// Registry is the in-memory representation of registry.json.
// It is not safe for concurrent use — callers must synchronize.
type Registry struct {
	path     string
	projects map[string]RegistryEntry
}

// NewRegistry loads (or creates) the registry at the given path.
func NewRegistry(path string) *Registry {
	r := &Registry{path: path, projects: map[string]RegistryEntry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &r.projects)
	}
	return r
}

// Add registers a project name -> directory path and persists.
func (r *Registry) Add(name, dir string) error {
	r.projects[name] = RegistryEntry{Path: dir}
	return r.save()
}

// Remove deletes a project from the registry and persists.
func (r *Registry) Remove(name string) error {
	if _, ok := r.projects[name]; !ok {
		return fmt.Errorf("project %q not found in registry", name)
	}
	delete(r.projects, name)
	return r.save()
}

// Get returns the directory path for a project name.
func (r *Registry) Get(name string) (string, bool) {
	e, ok := r.projects[name]
	return e.Path, ok
}

// List returns all registered projects as name -> path map.
func (r *Registry) List() map[string]string {
	out := make(map[string]string, len(r.projects))
	for k, v := range r.projects {
		out[k] = v.Path
	}
	return out
}

func (r *Registry) save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}
