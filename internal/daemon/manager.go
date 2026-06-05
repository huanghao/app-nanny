// internal/daemon/manager.go
package daemon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/ipc"
)

type Manager struct {
	mu        sync.Mutex
	registry  *config.Registry
	runtime   *Runtime
	processes map[string]*Process
	configs   map[string]*config.ProjectConfig
}

func NewManager(reg *config.Registry, rt *Runtime) *Manager {
	return &Manager{
		registry:  reg,
		runtime:   rt,
		processes: make(map[string]*Process),
		configs:   make(map[string]*config.ProjectConfig),
	}
}

func (m *Manager) Add(name, dir string) error {
	cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
	if err != nil {
		return err
	}
	if err := m.registry.Add(name, dir); err != nil {
		return err
	}
	m.mu.Lock()
	m.configs[name] = cfg
	m.mu.Unlock()
	return nil
}

func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, proc := range m.processes {
		if strings.HasPrefix(key, name) && proc.Status() == StatusRunning {
			return fmt.Errorf("project %q has running processes; stop it first", name)
		}
	}
	delete(m.configs, name)
	return m.registry.Remove(name)
}

func (m *Manager) Start(projectName, processName string) error {
	m.mu.Lock()
	cfg, ok := m.configs[projectName]
	if !ok {
		dir, found := m.registry.Get(projectName)
		if !found {
			m.mu.Unlock()
			return fmt.Errorf("project %q not registered", projectName)
		}
		loaded, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			m.mu.Unlock()
			return err
		}
		cfg = loaded
		m.configs[projectName] = cfg
	}
	dir, _ := m.registry.Get(projectName)
	m.mu.Unlock()

	if cfg.IsModeB() {
		return m.startModeB(projectName, processName, cfg, dir)
	}
	return m.startModeA(projectName, cfg, dir)
}

func (m *Manager) startModeA(name string, cfg *config.ProjectConfig, dir string) error {
	m.mu.Lock()
	for envVar, port := range cfg.Ports {
		if err := m.checkPortConflictLocked(name, envVar, port); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()

	proc := NewProcess(name, config.ProcessConfig{Command: cfg.Command}, dir)
	env := make(map[string]string)
	for k, v := range cfg.Ports {
		env[k] = fmt.Sprintf("%d", v)
	}
	proc.SetEnv(env)
	proc.SetOnCrash(func(key string) { m.onCrash(key, cfg) })

	if err := proc.Start(); err != nil {
		return err
	}

	m.mu.Lock()
	m.processes[name] = proc
	m.runtime.Set(name, RuntimeEntry{
		PID: proc.PID(), PGID: proc.PGID(), StartedAt: proc.StartedAt(),
	})
	m.mu.Unlock()
	return m.runtime.Save()
}

func (m *Manager) startModeB(projectName, processName string, cfg *config.ProjectConfig, dir string) error {
	for pName, pCfg := range cfg.Processes {
		if processName != "" && pName != processName {
			continue
		}
		key := projectName + "/" + pName

		m.mu.Lock()
		err := m.checkPortConflictLocked(key, "PORT", pCfg.Port)
		m.mu.Unlock()
		if err != nil {
			return err
		}

		workDir := dir
		if pCfg.WorkingDir != "" {
			workDir = filepath.Join(dir, pCfg.WorkingDir)
		}
		proc := NewProcess(key, pCfg, workDir)
		proc.SetEnv(map[string]string{"PORT": fmt.Sprintf("%d", pCfg.Port)})
		proc.SetOnCrash(func(k string) { m.onCrash(k, cfg) })
		if err := proc.Start(); err != nil {
			return fmt.Errorf("start %s: %w", key, err)
		}
		m.mu.Lock()
		m.processes[key] = proc
		m.runtime.Set(key, RuntimeEntry{
			PID: proc.PID(), PGID: proc.PGID(), Port: pCfg.Port, StartedAt: proc.StartedAt(),
		})
		m.mu.Unlock()
	}
	return m.runtime.Save()
}

func (m *Manager) Stop(projectName, processName string) error {
	m.mu.Lock()
	var targets []string
	for key := range m.processes {
		if strings.HasPrefix(key, projectName) {
			if processName == "" || key == projectName+"/"+processName || key == projectName {
				targets = append(targets, key)
			}
		}
	}
	m.mu.Unlock()

	for _, key := range targets {
		m.mu.Lock()
		proc := m.processes[key]
		m.mu.Unlock()
		if proc != nil {
			if err := proc.Stop(); err != nil {
				return err
			}
		}
		m.mu.Lock()
		m.runtime.Delete(key)
		m.mu.Unlock()
	}
	return m.runtime.Save()
}

func (m *Manager) Restart(projectName, processName string) error {
	if err := m.Stop(projectName, processName); err != nil {
		return err
	}
	return m.Start(projectName, processName)
}

func (m *Manager) PS() []ipc.ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []ipc.ProcessInfo
	for key, proc := range m.processes {
		parts := strings.SplitN(key, "/", 2)
		project := parts[0]
		process := ""
		if len(parts) == 2 {
			process = parts[1]
		}
		uptime := ""
		if proc.Status() == StatusRunning {
			uptime = formatDuration(time.Since(proc.StartedAt()))
		}
		actualPorts := ActualPorts(proc.PID())
		out = append(out, ipc.ProcessInfo{
			Project:     project,
			Process:     process,
			Status:      string(proc.Status()),
			PID:         proc.PID(),
			Uptime:      uptime,
			Restarts:    proc.Restarts(),
			ActualPorts: actualPorts,
		})
	}
	return out
}

func (m *Manager) LoadAll() {
	for name, dir := range m.registry.List() {
		cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			continue
		}
		m.mu.Lock()
		m.configs[name] = cfg
		m.mu.Unlock()
	}
}

// checkPortConflictLocked must be called with m.mu held.
func (m *Manager) checkPortConflictLocked(claimant, envVar string, port int) error {
	if port == 0 {
		return nil
	}
	for key, proc := range m.processes {
		if key == claimant || proc.Status() != StatusRunning {
			continue
		}
		cfg := m.projectConfigForKeyLocked(key)
		if cfg == nil {
			continue
		}
		for _, p := range cfg.DeclaredPorts() {
			if p == port {
				return fmt.Errorf("port %d (%s) conflicts with running service %q", port, envVar, key)
			}
		}
	}
	return nil
}

func (m *Manager) projectConfigForKeyLocked(key string) *config.ProjectConfig {
	parts := strings.SplitN(key, "/", 2)
	return m.configs[parts[0]]
}

func (m *Manager) onCrash(key string, cfg *config.ProjectConfig) {
	m.mu.Lock()
	proc, ok := m.processes[key]
	m.mu.Unlock()
	if !ok || cfg.Restart == "never" {
		return
	}
	if cfg.MaxRestarts > 0 && proc.Restarts() >= cfg.MaxRestarts {
		return
	}
	go func() {
		time.Sleep(1 * time.Second)
		parts := strings.SplitN(key, "/", 2)
		project := parts[0]
		process := ""
		if len(parts) == 2 {
			process = parts[1]
		}
		proc.IncrRestarts()
		_ = m.Start(project, process)
	}()
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	min := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, min)
	}
	if min > 0 {
		return fmt.Sprintf("%dm%ds", min, s)
	}
	return fmt.Sprintf("%ds", s)
}

func ActualPorts(pid int) []int {
	if pid == 0 {
		return nil
	}
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", pid), "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	if err != nil {
		return nil
	}
	var ports []int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		var port int
		fmt.Sscanf(parts[len(parts)-1], "%d", &port)
		if port > 0 {
			ports = append(ports, port)
		}
	}
	return ports
}
