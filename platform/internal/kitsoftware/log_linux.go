package kitsoftware

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	// SupervisorLogName is the rotated log inside the supervisor run directory.
	// It lives on tmpfs so a stalled data disk cannot block or swallow it.
	SupervisorLogName     = "supervisor.log"
	SupervisorLogMaxBytes = 2 << 20
)

// SupervisorLog appends supervisor and child output, keeping one older file.
// The file is written before the optional console mirror, and a mirror error
// is ignored so a stalled serial console cannot drop the record.
type SupervisorLog struct {
	path   string
	max    int64
	mirror io.Writer
	mu     sync.Mutex
	file   *os.File
	size   int64
}

func OpenSupervisorLog(path string, mirror io.Writer) (*SupervisorLog, error) {
	return newSupervisorLog(path, SupervisorLogMaxBytes, mirror)
}

func newSupervisorLog(path string, max int64, mirror io.Writer) (*SupervisorLog, error) {
	if max <= 0 {
		return nil, errors.New("software log limit must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &SupervisorLog{path: path, max: max, mirror: mirror, file: file, size: info.Size()}, nil
}

func (l *SupervisorLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if l.file == nil {
		return 0, errors.New("software log is closed")
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	if l.mirror != nil && n > 0 {
		_, _ = l.mirror.Write(p[:n])
	}
	return n, err
}

func (l *SupervisorLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *SupervisorLog) rotate() error {
	if err := l.file.Close(); err != nil {
		l.file = nil
		return err
	}
	l.file = nil
	backup := l.path + ".1"
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return l.reopen(err)
	}
	if err := os.Rename(l.path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return l.reopen(err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	l.file = file
	l.size = 0
	return nil
}

func (l *SupervisorLog) reopen(cause error) error {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return cause
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return cause
	}
	l.file = file
	l.size = info.Size()
	return cause
}
