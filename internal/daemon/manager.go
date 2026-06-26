// internal/daemon/manager.go
package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	configs    map[string]*config.ProjectConfig
	activeToml map[string]string // raw toml content used at last Start()
	logDir     string
	loggers    map[string]*Logger
	errRing    *ErrorRing
	metrics    *Metrics
}

func NewManager(reg *config.Registry, rt *Runtime, logDir string) *Manager {
	m := &Manager{
		registry:   reg,
		runtime:    rt,
		processes:  make(map[string]*Process),
		configs:    make(map[string]*config.ProjectConfig),
		activeToml: make(map[string]string),
		logDir:     logDir,
		loggers:    make(map[string]*Logger),
		errRing:    NewErrorRing(),
		metrics:    NewMetrics(),
	}
	m.startMetricsLoop()
	return m
}

func (m *Manager) startMetricsLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m.mu.Lock()
			type kp struct {
				key string
				pid int
			}
			var running []kp
			for key, proc := range m.processes {
				if proc.Status() == StatusRunning {
					running = append(running, kp{key, proc.PID()})
				}
			}
			m.mu.Unlock()
			for _, r := range running {
				m.metrics.Update(r.key, r.pid)
			}
		}
	}()
}

// AdoptProcess reconnects the Manager to a process that survived a daemon restart.
// The process is tracked but its stdout/stderr are not captured (no pipe was set up).
func (m *Manager) AdoptProcess(key string, entry RuntimeEntry) {
	parts := strings.SplitN(key, "/", 2)
	projectName := parts[0]

	m.mu.Lock()
	cfg := m.configs[projectName]
	m.mu.Unlock()

	var procCfg config.ProcessConfig
	projDir, _ := m.registry.Get(projectName)
	workDir := projDir
	if cfg != nil && len(parts) == 2 {
		if pc, ok := cfg.Processes[parts[1]]; ok {
			procCfg = pc
			if pc.WorkingDir != "" {
				workDir = filepath.Join(projDir, pc.WorkingDir)
			}
		}
	} else if cfg != nil {
		procCfg = config.ProcessConfig{Command: cfg.Command}
	}

	proc := NewAdoptedProcess(key, procCfg, workDir, entry.PID, entry.PGID, entry.StartedAt)
	if cfg != nil {
		proc.SetOnCrash(func(k string) { m.onCrash(k, cfg) })
	}

	m.mu.Lock()
	m.processes[key] = proc
	m.runtime.Set(key, entry)
	m.mu.Unlock()
	m.runtime.Save() //nolint:errcheck

	log.Printf("manager: re-adopted %q (pid=%d) — logs not captured until restart", key, entry.PID)
}

// logPath returns the log file path for a given process key.
func (m *Manager) logPath(key string) string {
	sanitized := strings.ReplaceAll(key, "/", "-")
	return filepath.Join(m.logDir, sanitized+".log")
}

// LogPath returns the log file path for a key.
// Returns "" for Mode B project names (multiple processes — no single file to tail).
func (m *Manager) LogPath(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Direct active logger
	if _, ok := m.loggers[key]; ok {
		return m.logPath(key)
	}
	// Adopted process: log file may exist from a previous run
	if _, ok := m.processes[key]; ok {
		return m.logPath(key)
	}
	// Mode B project name: check for subprocesses
	prefix := key + "/"
	for k := range m.processes {
		if strings.HasPrefix(k, prefix) {
			return "" // multiple sub-processes, no single path
		}
	}
	return m.logPath(key)
}

// SubProcessKeys returns sub-process keys for a Mode B project.
// Checks both active loggers AND adopted processes (no logger but known PID).
func (m *Manager) SubProcessKeys(project string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := project + "/"
	seen := make(map[string]bool)
	var keys []string
	for k := range m.loggers {
		if strings.HasPrefix(k, prefix) && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range m.processes {
		if strings.HasPrefix(k, prefix) && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// LogLines returns the last n lines for a key.
//   - Direct key with active logger → in-memory ring buffer (freshest data)
//   - Direct key without logger (adopted) → read log file from disk
//   - Mode B project name → aggregate from all subprocesses (logger or file)
func (m *Manager) LogLines(key string, n int) []string {
	m.mu.Lock()
	logger, hasLogger := m.loggers[key]
	m.mu.Unlock()

	// 1. Direct in-memory logger (process started by this daemon instance)
	if hasLogger {
		return logger.TailLines(n)
	}

	// 2. Check if this is a Mode B project name (no subprocess suffix)
	prefix := key + "/"
	m.mu.Lock()
	type sub struct {
		name   string
		key    string
		logger *Logger // nil for adopted processes
	}
	seen := make(map[string]bool)
	var subs []sub
	for k, lg := range m.loggers {
		if strings.HasPrefix(k, prefix) {
			name := strings.TrimPrefix(k, prefix)
			if !seen[name] {
				seen[name] = true
				subs = append(subs, sub{name, k, lg})
			}
		}
	}
	// Include adopted processes that have no logger but may have a log file
	for k := range m.processes {
		if strings.HasPrefix(k, prefix) {
			name := strings.TrimPrefix(k, prefix)
			if !seen[name] {
				seen[name] = true
				subs = append(subs, sub{name, k, nil})
			}
		}
	}
	m.mu.Unlock()

	if len(subs) > 0 {
		sort.Slice(subs, func(i, j int) bool { return subs[i].name < subs[j].name })
		perProc := n / len(subs)
		if perProc < 20 {
			perProc = 20
		}
		var all []string
		for _, s := range subs {
			var lines []string
			if s.logger != nil {
				lines = s.logger.TailLines(perProc)
			} else {
				lines = tailLogFile(m.logPath(s.key), perProc)
			}
			for _, line := range lines {
				all = append(all, "["+s.name+"] "+line)
			}
		}
		if len(all) > n {
			all = all[len(all)-n:]
		}
		return all
	}

	// 3. Direct key, no logger — read log file (adopted process or previous run)
	return tailLogFile(m.logPath(key), n)
}

// tailLogFile reads the last n lines from a log file on disk.
func tailLogFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// RecentErrors returns the most recent error events for key.
func (m *Manager) RecentErrors(key string, n int) []ErrorEvent {
	return m.errRing.RecentForKey(key, n)
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
	// Remove all stopped/crashed process entries for this project
	for key := range m.processes {
		if strings.HasPrefix(key, name) {
			delete(m.processes, key)
		}
	}
	for key := range m.loggers {
		if strings.HasPrefix(key, name) {
			delete(m.loggers, key)
		}
	}
	delete(m.configs, name)
	return m.registry.Remove(name)
}

func (m *Manager) Start(projectName, processName string) error {
	m.mu.Lock()
	dir, found := m.registry.Get(projectName)
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("project %q not registered", projectName)
	}

	// Always re-read the toml so edits take effect on next start/restart
	tomlPath := filepath.Join(dir, "app-nanny.toml")
	cfg, err := config.LoadProject(tomlPath)
	if err != nil {
		return err
	}
	// Store raw toml so we can later compare against disk version
	rawBytes, _ := os.ReadFile(tomlPath)

	m.mu.Lock()
	m.configs[projectName] = cfg
	m.activeToml[projectName] = string(rawBytes)
	m.mu.Unlock()

	if cfg.IsModeB() {
		return m.startModeB(projectName, processName, cfg, dir)
	}
	return m.startModeA(projectName, cfg, dir)
}

func (m *Manager) startModeA(name string, cfg *config.ProjectConfig, dir string) error {
	m.mu.Lock()
	// Idempotent: skip if already running
	if existing, ok := m.processes[name]; ok && existing.Status() == StatusRunning {
		m.mu.Unlock()
		return nil
	}
	for envVar, port := range cfg.Ports {
		if err := m.checkPortConflictLocked(name, envVar, port); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()

	proc := NewProcess(name, config.ProcessConfig{Command: cfg.Command}, dir)
	if err := os.MkdirAll(m.logDir, 0755); err == nil {
		logPath := m.logPath(name)
		needSep := fileHasContent(logPath)
		if rf, err := NewRotatingFile(logPath, 50*1024*1024, 3); err == nil {
			lg := NewLogger(rf, m.errRing, name, cfg.ErrorPatterns)
			if needSep {
				lg.WriteSeparator(time.Now())
			}
			proc.SetStdio(lg)
			m.mu.Lock()
			m.loggers[name] = lg
			m.mu.Unlock()
		}
	}
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
		// Idempotent: skip processes already running
		if existing, ok := m.processes[key]; ok && existing.Status() == StatusRunning {
			m.mu.Unlock()
			continue
		}
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
		if err := os.MkdirAll(m.logDir, 0755); err == nil {
			logPath := m.logPath(key)
			needSep := fileHasContent(logPath)
			if rf, err := NewRotatingFile(logPath, 50*1024*1024, 3); err == nil {
				lg := NewLogger(rf, m.errRing, key, cfg.ErrorPatterns)
				if needSep {
					lg.WriteSeparator(time.Now())
				}
				proc.SetStdio(lg)
				m.mu.Lock()
				m.loggers[key] = lg
				m.mu.Unlock()
			}
		}
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
	seen := make(map[string]bool) // projects already represented in out

	// 1. All tracked processes (running / stopped / crashed)
	for key, proc := range m.processes {
		parts := strings.SplitN(key, "/", 2)
		project := parts[0]
		process := ""
		if len(parts) == 2 {
			process = parts[1]
		}
		seen[project] = true
		uptime := ""
		if proc.Status() == StatusRunning {
			uptime = formatDuration(time.Since(proc.StartedAt()))
		}
		snap := m.metrics.Get(key)
		errs := m.errRing.RecentForKey(key, 50)
		errCount := len(errs)
		lastErrTime := ""
		if len(errs) > 0 {
			lastErrTime = errs[0].Time.Format(time.RFC3339)
		}
		out = append(out, ipc.ProcessInfo{
			Project:       project,
			Process:       process,
			Status:        string(proc.Status()),
			PID:           proc.PID(),
			Uptime:        uptime,
			Restarts:      proc.Restarts(),
			DeclaredPort:  m.declaredPortForKey(key),
			ActualPorts:   ActualPorts(proc.PID(), proc.PGID()),
			MemMB:         snap.MemMB,
			WorkDir:       proc.WorkDir(),
			ErrorCount:    errCount,
			LastErrorTime: lastErrTime,
			LastLogTime:   m.lastLogTimeStrLocked(key),
		})
	}

	// 2. Registered projects not yet started — show as stopped so they're visible
	for name, projDir := range m.registry.List() {
		if seen[name] {
			continue
		}
		cfg := m.configs[name]
		if cfg == nil {
			out = append(out, ipc.ProcessInfo{Project: name, Status: "stopped", WorkDir: projDir})
			continue
		}
		if cfg.IsModeB() {
			for pName, pCfg := range cfg.Processes {
				wd := projDir
				if pCfg.WorkingDir != "" {
					wd = filepath.Join(projDir, pCfg.WorkingDir)
				}
				out = append(out, ipc.ProcessInfo{
					Project:      name,
					Process:      pName,
					Status:       "stopped",
					DeclaredPort: pCfg.Port,
					WorkDir:      wd,
				})
			}
		} else {
			var firstPort int
			for _, p := range cfg.Ports {
				firstPort = p
				break
			}
			out = append(out, ipc.ProcessInfo{
				Project:      name,
				Status:       "stopped",
				DeclaredPort: firstPort,
				WorkDir:      projDir,
			})
		}
	}

	// Stable order: sort by "project/process" so the list never jumps around
	sort.Slice(out, func(i, j int) bool {
		ki := out[i].Project + "\x00" + out[i].Process
		kj := out[j].Project + "\x00" + out[j].Process
		return ki < kj
	})

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
// For Mode B processes (key = "project/process"), only that process's own port is checked.
// For Mode A processes (key = "project"), all ports in [ports] are checked.
func (m *Manager) checkPortConflictLocked(claimant, envVar string, port int) error {
	if port == 0 {
		return nil
	}
	for key, proc := range m.processes {
		if key == claimant || proc.Status() != StatusRunning {
			continue
		}
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			// Mode B: running process only owns its own declared port
			projectCfg := m.configs[parts[0]]
			if projectCfg == nil {
				continue
			}
			if procCfg, ok := projectCfg.Processes[parts[1]]; ok {
				if procCfg.Port == port {
					return fmt.Errorf("port %d (%s) conflicts with running service %q", port, envVar, key)
				}
			}
		} else {
			// Mode A: running process owns all ports in [ports] table
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
	}
	return nil
}

func (m *Manager) projectConfigForKeyLocked(key string) *config.ProjectConfig {
	parts := strings.SplitN(key, "/", 2)
	return m.configs[parts[0]]
}

// ProjectToml returns the current on-disk contents of a project's app-nanny.toml.
func (m *Manager) ProjectToml(name string) (string, error) {
	m.mu.Lock()
	dir, found := m.registry.Get(name)
	m.mu.Unlock()
	if !found {
		return "", fmt.Errorf("project %q not registered", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, "app-nanny.toml"))
	if err != nil {
		return "", fmt.Errorf("read toml: %w", err)
	}
	return string(data), nil
}

// ProjectTomlActive returns the toml content that was used when the service was last started.
// Returns empty string if the service has never been started by this daemon instance.
func (m *Manager) ProjectTomlActive(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeToml[name]
}

// fileHasContent reports whether the file at path exists and has non-zero size.
func fileHasContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// lastLogTimeStrLocked returns the RFC3339 timestamp of the last log line for a key.
// Must be called with m.mu held.
func (m *Manager) lastLogTimeStrLocked(key string) string {
	if logger, ok := m.loggers[key]; ok {
		if t := logger.LastLineTime(); !t.IsZero() {
			return t.Format(time.RFC3339)
		}
	}
	// Adopted process or no lines yet — try log file mtime (outside lock is fine for stat)
	if info, err := os.Stat(m.logPath(key)); err == nil && info.Size() > 0 {
		return info.ModTime().Format(time.RFC3339)
	}
	return ""
}

// declaredPortForKey returns the configured port for a process key.
// For Mode B ("project/process") returns that process's declared port.
// For Mode A ("project") returns the first port in [ports], or 0 if none.
func (m *Manager) declaredPortForKey(key string) int {
	parts := strings.SplitN(key, "/", 2)
	cfg := m.configs[parts[0]]
	if cfg == nil {
		return 0
	}
	if len(parts) == 2 {
		if procCfg, ok := cfg.Processes[parts[1]]; ok {
			return procCfg.Port
		}
		return 0
	}
	// Mode A: return first port value
	for _, port := range cfg.Ports {
		return port
	}
	return 0
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

// DetailedStatus returns per-process status for a named project.
func (m *Manager) DetailedStatus(projectName string) ipc.StatusResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []ipc.ProcessStatus
	for key, proc := range m.processes {
		parts := strings.SplitN(key, "/", 2)
		if parts[0] != projectName {
			continue
		}
		snap := m.metrics.Get(key)
		errCount := len(m.errRing.RecentForKey(key, 50))
		uptime := ""
		if proc.Status() == StatusRunning {
			uptime = formatDuration(time.Since(proc.StartedAt()))
		}
		statuses = append(statuses, ipc.ProcessStatus{
			Key:         key,
			Status:      string(proc.Status()),
			PID:         proc.PID(),
			Uptime:      uptime,
			Restarts:    proc.Restarts(),
			MemMB:       snap.MemMB,
			ActualPorts: ActualPorts(proc.PID(), proc.PGID()),
			ErrorCount:  errCount,
			LogPath:     m.logPath(key),
		})
	}
	return ipc.StatusResult{Processes: statuses}
}

// ActualPorts returns TCP listening ports for a process and all its children.
// We scan by PGID because the managed command runs inside `sh -c "..."`,
// so the actual server (Flask, Node, etc.) is a child of the shell we started.
func ActualPorts(pid, pgid int) []int {
	if pid == 0 {
		return nil
	}

	// Collect all PIDs in the process group via pgrep
	pidList := fmt.Sprintf("%d", pid) // fallback: just the direct PID
	if pgid > 0 {
		if pgrpOut, err := exec.Command("pgrep", "-g", fmt.Sprintf("%d", pgid)).Output(); err == nil {
			lines := strings.Fields(string(pgrpOut))
			if len(lines) > 0 {
				pidList = strings.Join(lines, ",")
			}
		}
	}

	// lsof flags:
	//   -a    AND conditions (-p and -i both apply, not OR)
	//   -P    do NOT convert port numbers to service names (e.g. show 3011 not "trusted-web")
	out, err := exec.Command("lsof", "-a", "-P", "-p", pidList, "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	if err != nil {
		return nil
	}

	seen := make(map[int]bool)
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
		if port > 0 && !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	return ports
}
