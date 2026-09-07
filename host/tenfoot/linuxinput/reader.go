package linuxinput

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Reader fans out non-blocking Poll from one or more evdev/js files.
type Reader struct {
	ch        chan Mapped
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	mu        sync.Mutex
	devs      []*device
}

type device struct {
	f    *os.File
	kind Kind
	path string
	name string
}

// DeviceInfo is a live node the Reader opened.
type DeviceInfo struct {
	Path string
	Name string
	Kind Kind
}

// NewReader returns an idle reader. Call Add or OpenPaths, then Poll.
func NewReader() *Reader {
	return &Reader{
		ch:   make(chan Mapped, 64),
		done: make(chan struct{}),
	}
}

// Add starts a read loop on f. The Reader takes ownership of f.
func (r *Reader) Add(f *os.File, kind Kind, path, name string) error {
	if r == nil || f == nil {
		return fmt.Errorf("linuxinput: nil reader or file")
	}
	if kind == KindUnknown {
		kind = KindFromPath(path)
	}
	if kind == KindUnknown {
		kind = KindEvdev
	}
	if name == "" {
		name = filepath.Base(path)
	}
	_ = unix.SetNonblock(int(f.Fd()), true)
	d := &device{f: f, kind: kind, path: path, name: name}
	r.mu.Lock()
	r.devs = append(r.devs, d)
	r.mu.Unlock()
	r.wg.Add(1)
	go r.readLoop(d)
	return nil
}

// OpenPaths opens every path that can be opened. It succeeds if at least
// one device is live.
func OpenPaths(paths []string) (*Reader, error) {
	r := NewReader()
	var last error
	n := 0
	for _, p := range paths {
		if p == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			last = err
			continue
		}
		name := filepath.Base(p)
		if nm, err := deviceName(f); err == nil && nm != "" {
			name = nm
		}
		if err := r.Add(f, KindFromPath(p), p, name); err != nil {
			_ = f.Close()
			last = err
			continue
		}
		n++
	}
	if n == 0 {
		_ = r.Close()
		if last != nil {
			return nil, last
		}
		return nil, fmt.Errorf("linuxinput: no devices")
	}
	return r, nil
}

// Info lists opened nodes.
func (r *Reader) Info() []DeviceInfo {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DeviceInfo, 0, len(r.devs))
	for _, d := range r.devs {
		out = append(out, DeviceInfo{Path: d.path, Name: d.name, Kind: d.kind})
	}
	return out
}

// Poll drains pending mapped events without blocking.
func (r *Reader) Poll() []Mapped {
	if r == nil {
		return nil
	}
	var out []Mapped
	for {
		select {
		case m := <-r.ch:
			if m.Action == ActionNone {
				continue
			}
			out = append(out, m)
		default:
			return out
		}
	}
}

// Close stops read loops and closes files.
func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	var err error
	r.closeOnce.Do(func() {
		close(r.done)
		r.mu.Lock()
		devs := r.devs
		r.mu.Unlock()
		for _, d := range devs {
			if d.f != nil {
				if e := d.f.Close(); e != nil && err == nil {
					err = e
				}
			}
		}
		waited := make(chan struct{})
		go func() {
			r.wg.Wait()
			close(waited)
		}()
		select {
		case <-waited:
		case <-time.After(time.Second):
		}
	})
	return err
}

func (r *Reader) readLoop(d *device) {
	defer r.wg.Done()
	recSize := EvdevSize
	if d.kind == KindJoystick {
		recSize = JSSize
	}
	buf := make([]byte, recSize)
	pending := make([]byte, 0, recSize*2)
	for {
		n, err := d.f.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for len(pending) >= recSize {
				rec := pending[:recSize]
				pending = pending[recSize:]
				m := mapRecord(d.kind, rec)
				if m.Action == ActionNone {
					continue
				}
				m.Source = d.name
				select {
				case r.ch <- m:
				case <-r.done:
					return
				}
			}
		}
		if err == nil && n == 0 {
			return
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				select {
				case <-r.done:
					return
				case <-time.After(8 * time.Millisecond):
				}
				continue
			}
			return
		}
	}
}
