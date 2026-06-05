// internal/daemon/daemon.go
package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/web"
)

// Run is the daemon entry point. It blocks until SIGTERM or SIGINT.
func Run(socketPath, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	rt := NewRuntime(filepath.Join(dataDir, "runtime.json"))
	reg := config.NewRegistry(filepath.Join(dataDir, "registry.json"))

	// Crash recovery: kill orphan processes from previous run
	killed, err := CleanupOrphans(rt)
	if err != nil {
		log.Printf("daemon: orphan cleanup error: %v", err)
	}
	if len(killed) > 0 {
		log.Printf("daemon: cleaned up orphans: %v", killed)
	}

	logDir := filepath.Join(dataDir, "logs")
	mgr := NewManager(reg, rt, logDir)
	mgr.LoadAll()

	// Auto-start projects marked autostart=true
	for name, dir := range reg.List() {
		cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			continue
		}
		if cfg.AutoStart {
			log.Printf("daemon: auto-starting %q", name)
			if err := mgr.Start(name, ""); err != nil {
				log.Printf("daemon: auto-start %q failed: %v", name, err)
			}
		}
	}

	// Remove stale socket
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}

	log.Printf("daemon: listening on %s", socketPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	srv := ipc.NewServer(socketPath)
	registerHandlers(srv, mgr, sigCh)

	go srv.Serve(ln)

	// Start web console HTTP server on :7070
	webMux := web.NewMux(mgr)
	web.RegisterSSERoute(webMux, mgr)
	webSrv := web.NewServer(":7070", webMux)
	go func() {
		if err := webSrv.Start(); err != nil {
			log.Printf("web: server error: %v", err)
		}
	}()

	<-sigCh
	log.Println("daemon: shutting down")
	ln.Close()
	os.Remove(socketPath)

	// Stop all running processes
	for name := range reg.List() {
		_ = mgr.Stop(name, "")
	}
	return nil
}

// registerHandlers wires IPC methods to Manager operations.
func registerHandlers(srv *ipc.Server, mgr *Manager, sigCh chan<- os.Signal) {
	srv.Handle("add", func(params json.RawMessage) (any, error) {
		var p ipc.AddParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if err := mgr.Add(p.Name, p.Path); err != nil {
			return nil, err
		}
		return ipc.AddResult{Name: p.Name, Path: p.Path}, nil
	})

	srv.Handle("remove", func(params json.RawMessage) (any, error) {
		var p ipc.RemoveParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Remove(p.Name)
	})

	srv.Handle("start", func(params json.RawMessage) (any, error) {
		var p ipc.StartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Start(p.Name, p.Process)
	})

	srv.Handle("stop", func(params json.RawMessage) (any, error) {
		var p ipc.StopParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Stop(p.Name, p.Process)
	})

	srv.Handle("restart", func(params json.RawMessage) (any, error) {
		var p ipc.RestartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Restart(p.Name, p.Process)
	})

	srv.Handle("ps", func(_ json.RawMessage) (any, error) {
		return ipc.PSResult{Processes: mgr.PS()}, nil
	})

	srv.Handle("shutdown", func(_ json.RawMessage) (any, error) {
		go func() { sigCh <- syscall.SIGTERM }()
		return "ok", nil
	})

	srv.Handle("logs", func(params json.RawMessage) (any, error) {
		var p ipc.LogsParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		key := p.Name
		if p.Process != "" {
			key = p.Name + "/" + p.Process
		}
		n := p.Lines
		if n <= 0 {
			n = 100
		}
		return ipc.LogsResult{
			Lines: mgr.LogLines(key, n),
			Path:  mgr.LogPath(key),
		}, nil
	})

	srv.Handle("errors", func(params json.RawMessage) (any, error) {
		var p ipc.ErrorsParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		key := p.Name
		if p.Process != "" {
			key = p.Name + "/" + p.Process
		}
		n := 10
		if p.Last {
			n = 1
		}
		raw := mgr.RecentErrors(key, n)
		events := make([]ipc.ErrorEvent, len(raw))
		for i, e := range raw {
			events[i] = ipc.ErrorEvent{
				Time:  e.Time.Format("15:04:05"),
				Key:   e.Key,
				Lines: e.Lines,
			}
		}
		return ipc.ErrorsResult{Events: events}, nil
	})

	srv.Handle("status", func(params json.RawMessage) (any, error) {
		var p ipc.StatusParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return mgr.DetailedStatus(p.Name), nil
	})
}
