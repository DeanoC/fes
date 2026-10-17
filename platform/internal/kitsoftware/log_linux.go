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
	SupervisorLogName          = "supervisor.log"
	SupervisorLogMaxBytes      = 2 << 20
	supervisorMirrorQueue      = 32
	supervisorMirrorQueueBytes = 64 << 10
)

// SupervisorLog appends supervisor and child output, keeping one older file.
// The tmpfs write finishes under the log mutex. Console mirroring is a separate
// goroutine with a bounded queue: a stalled serial console drops mirror bytes
// and cannot block a child pipe. A rotation that fails to open the new file
// stays recoverable; later writes open the current log again.
type SupervisorLog struct {
	path   string
	max    int64
	mirror io.Writer
	// openLog overrides creation of the current log. Tests use it to fail a rotation open.
	openLog func(name string) (*os.File, error)
	mu      sync.Mutex
	file    *os.File
	size    int64
	closed  bool

	mirrorCh     chan []byte
	mirrorQueued int
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
	l := &SupervisorLog{path: path, max: max, mirror: mirror, file: file, size: info.Size()}
	if mirror != nil {
		l.mirrorCh = make(chan []byte, supervisorMirrorQueue)
		go l.drainMirror(l.mirrorCh)
	}
	return l, nil
}

func (l *SupervisorLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if l.closed {
		return 0, errors.New("software log is closed")
	}
	if err := l.ensureOpen(); err != nil {
		return 0, err
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			if l.file == nil && !l.closed {
				if openErr := l.ensureOpen(); openErr != nil {
					return 0, err
				}
			} else {
				return 0, err
			}
		}
	}
	if l.file == nil {
		if err := l.ensureOpen(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	if n > 0 {
		l.enqueueMirror(p[:n])
	}
	return n, err
}

func (l *SupervisorLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.mirrorCh != nil {
		close(l.mirrorCh)
		l.mirrorCh = nil
	}
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
	file, err := l.openCurrent()
	if err != nil {
		return err
	}
	l.file = file
	l.size = 0
	return nil
}

func (l *SupervisorLog) reopen(cause error) error {
	file, err := l.openCurrent()
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

func (l *SupervisorLog) ensureOpen() error {
	if l.file != nil {
		return nil
	}
	if l.closed {
		return errors.New("software log is closed")
	}
	file, err := l.openCurrent()
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	l.file = file
	l.size = info.Size()
	return nil
}

func (l *SupervisorLog) openCurrent() (*os.File, error) {
	if l.openLog != nil {
		return l.openLog(l.path)
	}
	return os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}

// enqueueMirror copies p onto the console queue. The caller holds l.mu.
// A full queue drops the copy; the tmpfs record is already written.
func (l *SupervisorLog) enqueueMirror(p []byte) {
	if l.mirrorCh == nil || len(p) == 0 {
		return
	}
	if len(p) > supervisorMirrorQueueBytes || l.mirrorQueued+len(p) > supervisorMirrorQueueBytes {
		return
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case l.mirrorCh <- buf:
		l.mirrorQueued += len(buf)
	default:
	}
}

func (l *SupervisorLog) drainMirror(ch <-chan []byte) {
	for buf := range ch {
		_, _ = l.mirror.Write(buf)
		l.mu.Lock()
		l.mirrorQueued -= len(buf)
		if l.mirrorQueued < 0 {
			l.mirrorQueued = 0
		}
		l.mu.Unlock()
	}
}
