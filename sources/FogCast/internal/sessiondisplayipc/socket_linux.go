//go:build linux

package sessiondisplayipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const maxWire = 4096
const operationTimeout = 15 * time.Second

type wire struct {
	Transition *Transition `json:"transition,omitempty"`
	Outcome    Outcome     `json:"outcome,omitempty"`
	Error      string      `json:"error,omitempty"`
	OK         bool        `json:"ok,omitempty"`
}

func writeWire(conn net.Conn, v wire) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > maxWire {
		return errors.New("display IPC message too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	payload := append(header[:], body...)
	for len(payload) != 0 {
		n, err := conn.Write(payload)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}
func readWire(conn net.Conn) (wire, error) {
	var result wire
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return result, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxWire {
		return result, errors.New("invalid display IPC message size")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, errors.New("trailing display IPC data")
	}
	return result, nil
}
func peerCredentials(conn net.Conn) (*unix.Ucred, error) {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("not unix socket")
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *unix.Ucred
	var inner error
	if err = raw.Control(func(fd uintptr) { cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return nil, err
	}
	if inner != nil {
		return nil, inner
	}
	return cred, nil
}
func protectConn(ctx context.Context, conn net.Conn) func() {
	deadline := time.Now().Add(operationTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}

// Protect the socket and lock directory from untrusted replacement.
func checkParent(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0022 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("unsafe display socket directory")
	}
	return nil
}

// DisplayGuard serializes launcher socket publication against an agent's
// display transition, including its prepare and finish exchange.
type DisplayGuard struct {
	fd   int
	once sync.Once
	err  error
}

func AcquireDisplayGuard(ctx context.Context, path string) (*DisplayGuard, error) {
	if ctx == nil || path == "" {
		return nil, errors.New("guard context and path are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkParent(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&077 != 0 {
		_ = unix.Close(fd)
		return nil, errors.New("unsafe display guard file")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if ctx.Err() != nil {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = unix.Close(fd)
				return nil, ctx.Err()
			}
			return &DisplayGuard{fd: fd}, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			_ = unix.Close(fd)
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = unix.Close(fd)
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func (g *DisplayGuard) Release() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		g.err = unix.Flock(g.fd, unix.LOCK_UN)
		if e := unix.Close(g.fd); g.err == nil {
			g.err = e
		}
	})
	return g.err
}

// openListener publishes a fully permissioned socket atomically while the
// caller holds its publication guard. Only a proven-stale trusted socket is
// removed; live sockets, symlinks and ambiguous paths are never replaced.
func openListener(path string) (*net.UnixListener, os.FileInfo, error) {
	if path == "" {
		return nil, nil, errors.New("empty socket path")
	}
	parent := filepath.Dir(path)
	if err := checkParent(path); err != nil {
		return nil, nil, err
	}
	// Under the publication lock, a crash may leave an inode whose listener
	// is gone. Probe it and unlink only the unchanged, trusted socket on
	// ECONNREFUSED; a live, ambiguous, replaced or unsafe path blocks startup.
	old, err := os.Lstat(path)
	if err == nil {
		st, ok := old.Sys().(*syscall.Stat_t)
		if !ok || old.Mode()&os.ModeSocket == 0 || old.Mode().Perm() != 0600 || st.Uid != uint32(os.Geteuid()) {
			return nil, nil, errors.New("unsafe existing display socket")
		}
		c, dialErr := net.DialTimeout("unix", path, time.Second)
		if dialErr == nil {
			_ = c.Close()
			return nil, nil, errors.New("display socket already active")
		}
		if !errors.Is(dialErr, unix.ECONNREFUSED) {
			return nil, nil, fmt.Errorf("display socket probe: %w", dialErr)
		}
		current, statErr := os.Lstat(path)
		if statErr != nil || !os.SameFile(old, current) {
			return nil, nil, errors.New("display socket changed during probe")
		}
		if err := os.Remove(path); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	tmp, err := os.MkdirTemp(parent, ".display-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	p := filepath.Join(tmp, "s")
	addr := &net.UnixAddr{Name: p, Net: "unix"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, nil, err
	}
	listener.SetUnlinkOnClose(false)
	if err = os.Chmod(p, 0600); err == nil {
		err = unix.Renameat2(unix.AT_FDCWD, p, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE)
	}
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	return listener, info, nil
}
func closeListener(l *net.UnixListener, path string, original os.FileInfo) {
	_ = l.Close()
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(original, current) {
		_ = os.Remove(path)
	}
}
func signalReady(ctx context.Context, ready chan<- error, err error) {
	if ready == nil {
		return
	}
	// Production readiness channels are buffered. Prefer delivering the result
	// even when cancellation has already happened, so startup cannot hang.
	select {
	case ready <- err:
		return
	default:
	}
	if ctx == nil {
		return
	}
	select {
	case ready <- err:
	case <-ctx.Done():
	}
}
func serve(ctx context.Context, path string, allowedUID uint32, guarded bool, ready chan<- error, handle func(context.Context, net.Conn)) error {
	return serveWithOpener(ctx, path, allowedUID, guarded, ready, handle, openListener)
}

func serveWithOpener(ctx context.Context, path string, allowedUID uint32, guarded bool, ready chan<- error, handle func(context.Context, net.Conn), opener func(string) (*net.UnixListener, os.FileInfo, error)) error {
	signal := func(err error) { signalReady(ctx, ready, err) }
	if ctx == nil {
		err := errors.New("listener context required")
		signal(err)
		return err
	}
	if err := ctx.Err(); err != nil {
		signal(err)
		return err
	}
	var guard *DisplayGuard
	var err error
	if guarded {
		guard, err = AcquireDisplayGuard(ctx, path)
		if err != nil {
			signal(err)
			return err
		}
	}
	l, info, err := opener(path)
	if guard != nil {
		_ = guard.Release()
	}
	if err != nil {
		signal(err)
		return err
	}
	signal(nil)
	// An unexpected listener exit leaves a stale socket behind: the agent's
	// absent-listener path must not mistake a live but unprotected launcher
	// for a headless kit. Only an intentional context cancellation unlinks it.
	cleanShutdown := false
	defer func() {
		if !cleanShutdown {
			_ = l.Close()
			return
		}
		if guarded {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), operationTimeout)
			defer cancel()
			g, e := AcquireDisplayGuard(cleanupCtx, path)
			if e == nil {
				defer g.Release()
				closeListener(l, path, info)
				return
			}
			_ = l.Close() // Leave a stale path rather than race a guarded operation.
			return
		}
		closeListener(l, path, info)
	}()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-done:
		}
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				cleanShutdown = true
				return nil
			}
			return err
		}
		cred, err := peerCredentials(conn)
		if err != nil || cred.Uid != allowedUID {
			_ = conn.Close()
			continue
		}
		stop := protectConn(ctx, conn)
		handle(ctx, conn)
		stop()
		_ = conn.Close()
	}
}

type Prepared struct {
	conn     net.Conn
	stop     func()
	mu       sync.Mutex
	finished bool
}

func dial(ctx context.Context, path string) (net.Conn, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("context required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, nil, err
	}
	cred, err := peerCredentials(conn)
	if err != nil || cred.Uid != uint32(os.Geteuid()) {
		_ = conn.Close()
		return nil, nil, errors.New("display IPC peer UID mismatch")
	}
	return conn, protectConn(ctx, conn), nil
}
func Prepare(ctx context.Context, path string, t Transition) (*Prepared, bool, error) {
	if !t.Valid() {
		return nil, false, errors.New("invalid transition")
	}
	conn, stop, err := dial(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err = writeWire(conn, wire{Transition: &t}); err == nil {
		var result wire
		result, err = readWire(conn)
		if err == nil && !result.OK {
			err = fmt.Errorf("launcher refused display transition: %s", result.Error)
		}
	}
	if err != nil {
		stop()
		_ = conn.Close()
		return nil, true, err
	}
	return &Prepared{conn: conn, stop: stop}, true, nil
}
func (p *Prepared) Finish(ctx context.Context, outcome Outcome) error {
	if p == nil || ctx == nil || !outcome.valid() {
		return errors.New("invalid display transition outcome")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return errors.New("display transition already finished")
	}
	p.finished = true
	defer p.stop()
	defer p.conn.Close()
	stop := protectConn(ctx, p.conn)
	defer stop()
	if err := writeWire(p.conn, wire{Outcome: outcome}); err != nil {
		return err
	}
	result, err := readWire(p.conn)
	if err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("launcher did not finish transition: %s", result.Error)
	}
	return nil
}
func ListenLauncher(ctx context.Context, path string, allowedUID uint32, prepare func(context.Context, Transition) (func(context.Context, Outcome) error, error), ready chan<- error) error {
	if prepare == nil {
		err := errors.New("prepare callback required")
		signalReady(ctx, ready, err)
		return err
	}
	return serve(ctx, path, allowedUID, true, ready, func(parent context.Context, conn net.Conn) {
		request, err := readWire(conn)
		if err != nil || request.Transition == nil || !request.Transition.Valid() {
			_ = writeWire(conn, wire{Error: "invalid transition"})
			return
		}
		opctx, cancel := context.WithTimeout(parent, operationTimeout)
		defer cancel()
		finish, err := prepare(opctx, *request.Transition)
		if err != nil || finish == nil {
			_ = writeWire(conn, wire{Error: "launcher not ready"})
			return
		}
		outcome := Unknown
		if writeWire(conn, wire{OK: true}) == nil {
			if response, e := readWire(conn); e == nil && response.Outcome.valid() {
				outcome = response.Outcome
			}
		}
		// Unknown cleanup must run even if the peer disconnected or the listener
		// context was cancelled; it is independently bounded.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cleanupCancel()
		if err = finish(cleanupCtx, outcome); err != nil {
			_ = writeWire(conn, wire{Error: "launcher finish failed"})
			return
		}
		if outcome != Unknown {
			_ = writeWire(conn, wire{OK: true})
		}
	})
}
