// internal/daemon/process.go
package daemon

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusCrashed  Status = "crashed"
)

type Process struct {
	mu        sync.Mutex
	name      string
	cfg       config.ProcessConfig
	workDir   string
	extraEnv  map[string]string
	status    Status
	pid       int
	pgid      int
	startedAt time.Time
	restarts  int
	cmd       *exec.Cmd
	crashCh   chan struct{}
	onCrash   func(name string)
}

func NewProcess(name string, cfg config.ProcessConfig, workDir string) *Process {
	return &Process{
		name:    name,
		cfg:     cfg,
		workDir: workDir,
		status:  StatusStopped,
		crashCh: make(chan struct{}),
	}
}

func (p *Process) SetEnv(env map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.extraEnv = env
}

func (p *Process) SetOnCrash(fn func(name string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onCrash = fn
}

func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == StatusRunning || p.status == StatusStarting {
		return fmt.Errorf("process %q is already running", p.name)
	}

	if strings.TrimSpace(p.cfg.Command) == "" {
		return fmt.Errorf("process %q: empty command", p.name)
	}

	cmd := exec.Command("sh", "-c", p.cfg.Command)
	cmd.Dir = p.workDir

	// Build env: start with inherited env, then inject extras
	cmd.Env = append(syscall.Environ(), func() []string {
		var extra []string
		for k, v := range p.extraEnv {
			extra = append(extra, k+"="+v)
		}
		return extra
	}()...)

	// Put child in its own process group for clean shutdown
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", p.name, err)
	}

	p.cmd = cmd
	p.pid = cmd.Process.Pid
	p.pgid, _ = syscall.Getpgid(p.pid)
	p.status = StatusRunning
	p.startedAt = time.Now()
	p.crashCh = make(chan struct{})

	go p.watch()
	return nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	if p.status != StatusRunning {
		p.mu.Unlock()
		return nil
	}
	p.status = StatusStopping
	pgid := p.pgid
	p.mu.Unlock()

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		log.Printf("process %q: SIGTERM error: %v", p.name, err)
	}

	done := make(chan struct{})
	go func() {
		p.cmd.Wait() //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck
		<-done
	}

	p.mu.Lock()
	p.status = StatusStopped
	p.pid = 0
	p.pgid = 0
	p.mu.Unlock()
	return nil
}

func (p *Process) watch() {
	err := p.cmd.Wait()

	p.mu.Lock()
	stopping := p.status == StatusStopping
	if !stopping {
		p.status = StatusCrashed
		log.Printf("process %q crashed: %v", p.name, err)
	}
	crashCh := p.crashCh
	onCrash := p.onCrash
	p.mu.Unlock()

	close(crashCh)
	if !stopping && onCrash != nil {
		onCrash(p.name)
	}
}

func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

func (p *Process) PGID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pgid
}

func (p *Process) StartedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt
}

func (p *Process) Restarts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restarts
}

func (p *Process) IncrRestarts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restarts++
}
