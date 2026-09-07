// internal/config/project.go
package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// ProjectConfig is the parsed content of an app-nanny.toml file. Every
// project declares at least one [processes.<name>] block — a
// single-process project just declares one (conventionally named "main"),
// there is no separate flat-field shorthand for that case. (There used to
// be one — "Mode A" — see git history around the "unify to one mode"
// refactor; it doubled every code path that touches a process's config,
// for an ergonomics win only the project's own toml author ever saw.)
type ProjectConfig struct {
	Name          string                   `toml:"name"`
	AutoStart     bool                     `toml:"autostart"`
	Restart       string                   `toml:"restart"` // "always"|"on-failure"|"never"
	MaxRestarts   int                      `toml:"max_restarts"`
	Processes     map[string]ProcessConfig `toml:"processes"`
	ErrorPatterns []ErrorPattern           `toml:"error_patterns"`
}

// ProcessConfig is one entry under [processes.<name>].
type ProcessConfig struct {
	Command      string `toml:"command"`
	Port         int    `toml:"port"`
	WorkingDir   string `toml:"working_dir"`
	MemoryWarnMB int    `toml:"memory_warn_mb"`
	OtelService  string `toml:"otel_service_name"` // opt-in to local otel (see otel/README.md)
}

// LocalOtelEndpoint and LocalOtelProtocol are the fixed address of nanny's
// own otel/ project (see otel/README.md). A process that declares
// otel_service_name gets these injected as standard OTEL_EXPORTER_OTLP_*
// env vars — one place to change if that project's ports ever move, instead
// of every consumer hardcoding them. Whether anything is actually listening
// there is unrelated to nanny's own operation: otel is itself just another
// opt-in nanny project, and OTLP exporters fail silently (async, drop on
// error) when there's nothing to send to.
const (
	LocalOtelEndpoint = "http://localhost:4318"
	LocalOtelProtocol = "http/protobuf"
)

// ErrorPattern defines a custom error trigger rule.
type ErrorPattern struct {
	Match        string `toml:"match"`
	ContextAfter int    `toml:"context_after"`
}

// LoadProject reads and validates an app-nanny.toml at the given path.
func LoadProject(path string) (*ProjectConfig, error) {
	var cfg ProjectConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("%s: 'name' is required", path)
	}
	if len(cfg.Processes) == 0 {
		return nil, fmt.Errorf("%s: at least one [processes.<name>] block is required", path)
	}
	if cfg.Restart == "" {
		cfg.Restart = "on-failure"
	}
	if cfg.MaxRestarts == 0 {
		cfg.MaxRestarts = 5
	}
	return &cfg, nil
}

// DeclaredPorts returns all port numbers declared in this config.
func (c *ProjectConfig) DeclaredPorts() []int {
	var ports []int
	for _, proc := range c.Processes {
		if proc.Port > 0 {
			ports = append(ports, proc.Port)
		}
	}
	return ports
}
