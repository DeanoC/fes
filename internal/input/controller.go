// Package input owns target-side remote-input leases and streams.
package input

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
)

const streamDialTimeout = 2 * time.Second

type Spec struct {
	Session uint64
	Token   []byte
	Core    string
}

type Controller interface {
	Attach(context.Context, Spec) error
	Detach(context.Context, uint64) error
	OpenStream(context.Context, uint64) (net.Conn, error)
	Close() error
}

type TargetController struct {
	mu            sync.Mutex
	active        *lease
	listenAddress string
	uinputPath    string
}

type lease struct {
	session uint64
	token   []byte
	core    string
	server  *bridge.Server
	cancel  context.CancelFunc
}

func NewTargetController() *TargetController {
	return NewTargetControllerWithConfig("127.0.0.1:18183", "/dev/uinput")
}

func NewTargetControllerWithConfig(listenAddress, uinputPath string) *TargetController {
	if listenAddress == "" {
		listenAddress = "127.0.0.1:18183"
	}
	if uinputPath == "" {
		uinputPath = "/dev/uinput"
	}
	return &TargetController{listenAddress: listenAddress, uinputPath: uinputPath}
}

func (c *TargetController) Attach(ctx context.Context, spec Spec) error {
	if c == nil || ctx == nil || spec.Session == 0 || len(spec.Token) < 16 || !validCore(spec.Core) {
		return errors.New("invalid input lease")
	}

	c.mu.Lock()
	current := c.active
	if current != nil && current.session == spec.Session && current.core == spec.Core &&
		subtle.ConstantTimeCompare(current.token, spec.Token) == 1 {
		c.mu.Unlock()
		return nil
	}
	c.active = nil
	c.mu.Unlock()
	stopLease(current)

	sink, err := bridge.OpenUInput(c.uinputPath)
	if err != nil {
		return err
	}
	server, err := bridge.New(bridge.Config{
		Addr:    c.listenAddress,
		Token:   append([]byte(nil), spec.Token...),
		Session: spec.Session,
		Core:    spec.Core,
	}, sink)
	if err != nil {
		_ = sink.Close()
		return err
	}
	leaseCtx, cancel := context.WithCancel(context.Background())
	candidate := &lease{
		session: spec.Session,
		token:   append([]byte(nil), spec.Token...),
		core:    spec.Core,
		server:  server,
		cancel:  cancel,
	}
	go func() { _ = server.ListenAndServe(leaseCtx) }()
	select {
	case <-ctx.Done():
		stopLease(candidate)
		return ctx.Err()
	case <-server.Ready():
	}

	c.mu.Lock()
	if c.active != nil {
		previous := c.active
		c.active = candidate
		c.mu.Unlock()
		stopLease(previous)
		return nil
	}
	c.active = candidate
	c.mu.Unlock()
	return nil
}

func (c *TargetController) Detach(ctx context.Context, session uint64) error {
	if c == nil || session == 0 {
		return errors.New("invalid input session")
	}
	c.mu.Lock()
	current := c.active
	if current == nil || current.session != session {
		c.mu.Unlock()
		return nil
	}
	c.active = nil
	c.mu.Unlock()
	stopLease(current)
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (c *TargetController) OpenStream(ctx context.Context, session uint64) (net.Conn, error) {
	if c == nil || ctx == nil || session == 0 {
		return nil, errors.New("invalid input session")
	}
	c.mu.Lock()
	current := c.active
	if current == nil || current.session != session || current.server == nil || current.server.Addr() == nil {
		c.mu.Unlock()
		return nil, errors.New("input session is unavailable")
	}
	endpoint := current.server.Addr().String()
	c.mu.Unlock()
	dialer := net.Dialer{Timeout: streamDialTimeout}
	return dialer.DialContext(ctx, "tcp", endpoint)
}

func (c *TargetController) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	current := c.active
	c.active = nil
	c.mu.Unlock()
	stopLease(current)
	return nil
}

func stopLease(current *lease) {
	if current == nil {
		return
	}
	if current.cancel != nil {
		current.cancel()
	}
	if current.server != nil {
		_ = current.server.Close()
	}
}

func validCore(core string) bool {
	if core == "" || len(core) > 128 {
		return false
	}
	for _, char := range core {
		if char < 0x20 || char == 0x7f || char == '/' || char == '\\' {
			return false
		}
	}
	return true
}

var _ Controller = (*TargetController)(nil)
