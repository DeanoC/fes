package input

import (
	"bufio"
	"context"
	"errors"
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
// bound. Closing the connection releases only the local source.
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
	}
}

func (c *TargetController) deliverLocal(ctx context.Context, frame protocol.InputFrame) error {
	if err := c.ensureLocalCore(ctx); err != nil {
		return err
	}
	if c.ports == nil {
		return errNoCore
	}
	return c.ports.apply(sourceLocal, frame)
}

func (c *TargetController) ensureLocalCore(ctx context.Context) error {
	if c == nil || c.ports == nil {
		return errNoCore
	}
	if c.ports.hasBinding() {
		return nil
	}
	c.mu.Lock()
	observe := c.observe
	c.mu.Unlock()
	if observe == nil {
		if c.ports.cachedActive() {
			return nil
		}
		return errNoCore
	}
	if c.ports.cachedActive() {
		return nil
	}
	obs, err := observe(ctx)
	if err != nil || !obs.Active {
		return errNoCore
	}
	c.ports.setObservation(obs)
	if obs.Binding != nil {
		if err := c.ports.bind(obs.Binding); err != nil {
			return err
		}
	}
	return nil
}
