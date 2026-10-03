package input

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
)

// DefaultLocalInputSocket is the kit-local feed. It is a root-only unix
// socket, mode 0600, and is not reachable from the network. Any root process
// on the kit can write it; that is the same authority that owns the hardware.
const DefaultLocalInputSocket = "/run/fogcast/local-input.sock"

var errNoCore = errors.New("runtime core is not bound")

// localCoreObserveTimeout bounds the runtime status read taken while
// deliverLocal holds the input lifecycle lock. ServeLocalInput's context
// lasts for the agent process, and the runtime client sets a socket deadline
// only when that context has one. 250ms matches the health timeout
// mister-agent uses for the same Protocol2 status read. A stalled reply
// drops the frame and releases the lock.
const localCoreObserveTimeout = 250 * time.Millisecond

// Cache idle and unbound active observations alike. A ports binding retains
// its exact package generation; unbound routes must recheck whether the core
// still runs, and idle polls must not issue IPC for each input frame.
const localCoreObserveTTL = 250 * time.Millisecond

// localCoreWriteTimeout bounds set_keyboard and set_controller calls made
// while deliverLocal still holds the lifecycle lock. It matches
// localCoreObserveTimeout: both calls sit on that lock, and a stalled
// runtime reply must drop the frame on the same budget as a stalled status
// read. The host stream does not use this bound. Controller posts there
// keep mister-agent's 2s deadline when the caller has no tighter one, and
// host keyboard posts are not taken under this lock.
const localCoreWriteTimeout = localCoreObserveTimeout

func listenLocalInput(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("local input socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return listener, nil
}

// ServeLocalInput accepts raw input frames on a unix socket and delivers them
// into the same sink as the host stream. It is not an HTTP route and it does
// not consult the kit lease. The feed drops frames while no runtime core is
// bound, and while core replacement holds the input lifecycle lock. Closing
// the connection releases only the local source. The listener serves one
// writer synchronously: that writer must close before its successor can send.
func (c *TargetController) ServeLocalInput(ctx context.Context, path string) error {
	if c == nil {
		return errors.New("input controller is closed")
	}
	if ctx == nil {
		return errors.New("local input context is required")
	}
	listener, err := listenLocalInput(path)
	if err != nil {
		return err
	}
	c.localMu.Lock()
	if c.localClosed {
		c.localMu.Unlock()
		_ = listener.Close()
		_ = os.Remove(path)
		return net.ErrClosed
	}
	c.localLn = listener
	c.localPath = path
	c.localMu.Unlock()
	go func() {
		<-ctx.Done()
		c.closeLocal()
	}()
	defer c.closeLocal()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || c.localIsClosed() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		c.readLocal(ctx, conn)
	}
}

func (c *TargetController) localIsClosed() bool {
	c.localMu.Lock()
	defer c.localMu.Unlock()
	return c.localClosed
}

func (c *TargetController) closeLocal() {
	c.localMu.Lock()
	c.localClosed = true
	listener := c.localLn
	conn := c.localConn
	path := c.localPath
	c.localLn = nil
	c.localConn = nil
	c.localPath = ""
	c.localMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	if path != "" {
		_ = os.Remove(path)
	}
}

func (c *TargetController) readLocal(ctx context.Context, conn net.Conn) {
	c.localMu.Lock()
	if c.localClosed {
		c.localMu.Unlock()
		_ = conn.Close()
		return
	}
	c.localConn = conn
	c.localMu.Unlock()
	defer func() {
		c.localMu.Lock()
		if c.localConn == conn {
			c.localConn = nil
		}
		c.localMu.Unlock()
		_ = conn.Close()
		if c.ports != nil {
			_ = c.ports.releaseSource(sourceLocal)
		}
	}()
	reader := bufio.NewReaderSize(conn, bridge.MaxFrameBytes)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		frame, err := protocol.DecodeInputFrame(reader, bridge.MaxFrameBytes)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}
		if frame.Header.Type == protocol.InputTypePing {
			frame.Header.Type = protocol.InputTypePong
			frame.ServerMonoNS = uint64(time.Now().UnixNano())
			wire, err := protocol.EncodeInputFrame(frame)
			if err != nil {
				return
			}
			if _, err := conn.Write(wire); err != nil {
				return
			}
			continue
		}
		if frame.Header.Type != protocol.InputTypeInput {
			continue
		}
		_ = c.deliverLocal(ctx, frame)
		c.warnLocalDrops()
	}
}

// deliverLocal binds and writes one kit-local frame. Host attach and core
// replacement already hold lifecycle across that work. BeginCoreReplacement
// keeps the lock from ReleaseAll until its finish callback. TryLock drops a
// frame that arrives in that window, so the frame cannot observe the retired
// package and rebind it. The status read under this lock uses
// localCoreObserveTimeout rather than the process-lifetime reader context.
// The following set_keyboard or set_controller post uses localCoreWriteTimeout
// for the same reason: the poster must not keep this lock with a context
// that has no deadline.
func (c *TargetController) deliverLocal(ctx context.Context, frame protocol.InputFrame) (err error) {
	if c == nil {
		return errNoCore
	}
	defer func() {
		if err != nil {
			c.localDrops.Add(1)
		}
	}()
	if !c.lifecycle.TryLock() {
		c.localDrops.Add(1)
		return nil
	}
	defer c.lifecycle.Unlock()
	if err := c.ensureLocalCore(ctx); err != nil {
		return err
	}
	if c.ports == nil {
		return errNoCore
	}
	writeCtx, cancel := context.WithTimeout(ctx, localCoreWriteTimeout)
	defer cancel()
	return c.ports.applyContext(writeCtx, sourceLocal, frame)
}

// LocalInputDrops counts local frames dropped during core replacement or
// rejected by observation or delivery, including idle and unavailable ports.
func (c *TargetController) LocalInputDrops() uint64 {
	if c == nil {
		return 0
	}
	return c.localDrops.Load()
}

func (c *TargetController) warnLocalDrops() {
	total := c.LocalInputDrops()
	now := time.Now()
	c.localMu.Lock()
	if total == c.localReported || (!c.localWarning.IsZero() && now.Sub(c.localWarning) < time.Second) {
		c.localMu.Unlock()
		return
	}
	c.localReported = total
	c.localWarning = now
	c.localMu.Unlock()
	slog.Warn("kit-local input frames dropped", "total", total)
}

func (c *TargetController) ensureLocalCore(ctx context.Context) error {
	if c == nil || c.ports == nil {
		return errNoCore
	}
	if c.ports.hasBinding() {
		return nil
	}
	if active, fresh := c.ports.cachedObservation(); fresh {
		if active {
			return nil
		}
		return errNoCore
	}
	c.mu.Lock()
	observe := c.observe
	c.mu.Unlock()
	if observe == nil {
		return errNoCore
	}
	observeCtx, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	defer cancel()
	obs, err := observe(observeCtx)
	if err != nil {
		if retireErr := c.setLocalObservation(ctx, CoreObservation{}); retireErr != nil {
			return retireErr
		}
		return errNoCore
	}
	if err := c.setLocalObservation(ctx, obs); err != nil {
		return err
	}
	if !obs.Active {
		return errNoCore
	}
	if obs.Binding != nil {
		if err := c.ports.bind(obs.Binding); err != nil {
			// An observed ports contract whose binding failed must be retried;
			// it cannot become a fresh active cache for the legacy route.
			c.ports.mu.Lock()
			c.ports.observed = false
			c.ports.mu.Unlock()
			return err
		}
	}
	return nil
}

func (c *TargetController) setLocalObservation(ctx context.Context, obs CoreObservation) error {
	// Refresh can retire a held joystick-to-matrix route. Its neutral post
	// sits under the lifecycle lock too, so it needs the local write budget.
	writeCtx, cancel := context.WithTimeout(ctx, localCoreWriteTimeout)
	defer cancel()
	return c.ports.setObservationContext(writeCtx, obs)
}
