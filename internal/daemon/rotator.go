// internal/daemon/rotator.go
package daemon

import (
	"fmt"
	"os"
	"sync"
)

// RotatingFile is an append-only log file that rotates when size exceeds maxSize.
// It keeps at most maxFiles backup copies (app.log, app.log.1, app.log.2, …).
type RotatingFile struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	file     *os.File
	size     int64
}

// NewRotatingFile opens (or creates) the file at path and returns a RotatingFile.
func NewRotatingFile(path string, maxSizeBytes int64, maxFiles int) (*RotatingFile, error) {
	f, size, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &RotatingFile{
		path:     path,
		maxSize:  maxSizeBytes,
		maxFiles: maxFiles,
		file:     f,
		size:     size,
	}, nil
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotate: %w", err)
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

func (r *RotatingFile) rotate() error {
	r.file.Close()

	// Shift backups: from maxFiles-1 down to 1
	for i := r.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", r.path, i)
		dst := fmt.Sprintf("%s.%d", r.path, i+1)
		os.Remove(dst)
		os.Rename(src, dst) //nolint:errcheck
	}
	// Rename current → .1
	os.Rename(r.path, r.path+".1") //nolint:errcheck

	f, _, err := openAppend(r.path)
	if err != nil {
		return err
	}
	r.file = f
	r.size = 0
	return nil
}

func openAppend(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}
