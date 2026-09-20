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
	lifecycle     sync.Mutex
	mu            sync.Mutex
	active        *lease
	listenAddress string
	listen        func(network, address string) (net.Listener, error)
	uinputPath    string
	persistent    bridge.Sink
	keyboard      *KeyboardSink
	ports         *controllerPortsSink
	portsBinding  func(context.Context) (*ControllerBinding, error)
	needsRelease  bool
	closed        bool
	pending       *lease
	closeOnce     sync.Once
	closeErr      error
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

func NewNativeTargetControllerWithConfig(listenAddress, uinputPath string) (*TargetController, error) {
	pads, err := bridge.CreateUInputGamepad(uinputPath)
	if err != nil {
		return nil, err
	}
	keys := NewKeyboardSink()
	ports := &controllerPortsSink{fallback: muxSink{keys: keys, pads: pads}}
	controller := newTargetControllerWithSink(listenAddress, ports)
	controller.ports = ports
	controller.keyboard = keys
	return controller, nil
}

func (c *TargetController) ConfigureControllerPorts(binding func(context.Context) (*ControllerBinding, error), poster ControllerPoster) {
	if c == nil || c.ports == nil {
		return
	}
	c.portsBinding = binding
	c.ports.poster = poster
}

func (c *TargetController) SetKeyboardPoster(poster func(uint64) error) {
	if c == nil || c.keyboard == nil {
		return
	}
	c.keyboard.SetPoster(poster)
}

func newTargetControllerWithSink(listenAddress string, sink bridge.Sink) *TargetController {
	if listenAddress == "" {
		listenAddress = "127.0.0.1:18183"
	}
	return &TargetController{listenAddress: listenAddress, persistent: sink}
}

func (c *TargetController) Attach(ctx context.Context, spec Spec) error {
	if c == nil || ctx == nil || spec.Session == 0 || len(spec.Token) < 16 || !validCore(spec.Core) {
		return errors.New("invalid input lease")
	}
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	return c.attachLocked(ctx, spec)
}

func (c *TargetController) attachLocked(ctx context.Context, spec Spec) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("input controller is closed")
	}
	if c.needsRelease {
		persistent := c.persistent
		c.mu.Unlock()
		if persistent == nil {
			return errors.New("input state could not be released")
		}
		if err := persistent.ReleaseAll(); err != nil {
			return err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return errors.New("input controller is closed")
		}
		c.needsRelease = false
	}
	current := c.active
	if current != nil && current.session == spec.Session && current.core == spec.Core &&
		subtle.ConstantTimeCompare(current.token, spec.Token) == 1 {
		c.mu.Unlock()
		return nil
	}
	c.active = nil
	c.mu.Unlock()
	if err := stopLease(current); err != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
		return err
	}

	var sink bridge.Sink
	if c.ports != nil && c.portsBinding != nil {
		binding, err := c.portsBinding(ctx)
		if err != nil {
			return err
		}
		if err := c.ports.bind(binding); err != nil {
			return err
		}
	}
	if c.persistent != nil {
		sink = retainedSink{Sink: c.persistent}
	} else {
		var err error
		sink, err = bridge.OpenUInput(c.uinputPath)
		if err != nil {
			return err
		}
	}
	server, err := bridge.New(bridge.Config{
		Addr:    c.listenAddress,
		Token:   append([]byte(nil), spec.Token...),
		Session: spec.Session,
		Core:    spec.Core,
		Listen:  c.listen,
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
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.Join(errors.New("input controller is closed"), stopLease(candidate))
	}
	c.pending = candidate
	c.mu.Unlock()
	go func() { _ = server.ListenAndServe(leaseCtx) }()
	var startupErr error
	select {
	case <-ctx.Done():
		startupErr = ctx.Err()
	case startupErr = <-server.Startup():
	}
	if startupErr != nil {
		stopErr := stopLease(candidate)
		c.mu.Lock()
		if c.pending == candidate {
			c.pending = nil
		}
		if stopErr != nil {
			c.needsRelease = c.persistent != nil
		}
		c.mu.Unlock()
		return errors.Join(startupErr, stopErr)
	}

	c.mu.Lock()
	if c.pending == candidate {
		c.pending = nil
	}
	if c.closed {
		c.mu.Unlock()
		return errors.Join(errors.New("input controller is closed"), stopLease(candidate))
	}
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

// BeginCoreReplacement closes the active producer, waits for its in-flight
// writes, and neutralizes the retained sink before the runtime may open a new
// input reader. The returned function must be called exactly once. Passing
// preserve reconstructs the same logical lease after a proven pre-mutation
// failure; false permanently retires it.
func (c *TargetController) BeginCoreReplacement(ctx context.Context) (func(context.Context, bool) error, error) {
	if c == nil || ctx == nil {
		return nil, errors.New("invalid input replacement barrier")
	}
	c.lifecycle.Lock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.lifecycle.Unlock()
		return nil, errors.New("input controller is closed")
	}
	current := c.active
	c.active = nil
	c.mu.Unlock()
	if err := stopLease(current); err != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
		c.lifecycle.Unlock()
		return nil, err
	}

	var once sync.Once
	var finishErr error
	finish := func(finishCtx context.Context, preserve bool) error {
		once.Do(func() {
			defer c.lifecycle.Unlock()
			if !preserve || current == nil {
				return
			}
			if finishCtx == nil {
				finishErr = errors.New("invalid input replacement completion")
				return
			}
			finishErr = c.attachLocked(finishCtx, Spec{
				Session: current.session,
				Token:   append([]byte(nil), current.token...),
				Core:    current.core,
			})
		})
		return finishErr
	}
	return finish, nil
}

func (c *TargetController) Detach(ctx context.Context, session uint64) error {
	if c == nil || session == 0 {
		return errors.New("invalid input session")
	}
	c.mu.Lock()
	pending := c.pending
	if pending != nil && pending.session == session {
		c.pending = nil
	} else {
		pending = nil
	}
	c.mu.Unlock()
	pendingErr := stopLease(pending)
	if pendingErr != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
	}

	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	current := c.active
	if current == nil || current.session != session {
		c.mu.Unlock()
		if pending == nil {
			return nil
		}
		if ctx == nil {
			return pendingErr
		}
		return errors.Join(pendingErr, ctx.Err())
	}
	c.active = nil
	c.mu.Unlock()
	releaseErr := errors.Join(pendingErr, stopLease(current))
	if releaseErr != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
	}
	if ctx == nil {
		return releaseErr
	}
	return errors.Join(releaseErr, ctx.Err())
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

// ReleaseAll ends remote input without destroying the retained uinput device.
// The kit lease calls this only after all admitted HTTP operations finish.
func (c *TargetController) ReleaseAll(ctx context.Context) error {
	c.mu.Lock()
	pending, current := c.pending, c.active
	c.mu.Unlock()
	var result error
	if pending != nil {
		result = errors.Join(result, c.Detach(ctx, pending.session))
	}
	if current != nil {
		result = errors.Join(result, c.Detach(ctx, current.session))
	}
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	persistent := c.persistent
	c.mu.Unlock()
	if persistent != nil {
		releaseErr := persistent.ReleaseAll()
		result = errors.Join(result, releaseErr)
		c.mu.Lock()
		c.needsRelease = releaseErr != nil
		c.mu.Unlock()
	}
	return result
}

func (c *TargetController) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		pending := c.pending
		c.pending = nil
		c.mu.Unlock()
		pendingErr := stopLease(pending)

		c.lifecycle.Lock()
		defer c.lifecycle.Unlock()
		c.mu.Lock()
		current := c.active
		c.active = nil
		persistent := c.persistent
		c.persistent = nil
		needsRelease := c.needsRelease
		c.needsRelease = false
		c.mu.Unlock()
		stopErr := stopLease(current)
		var releaseErr error
		var persistentErr error
		if persistent != nil {
			if needsRelease {
				releaseErr = persistent.ReleaseAll()
			}
			persistentErr = persistent.Close()
		}
		c.closeErr = errors.Join(pendingErr, stopErr, releaseErr, persistentErr)
	})
	return c.closeErr
}

type retainedSink struct {
	bridge.Sink
}

func (retainedSink) Close() error { return nil }

func stopLease(current *lease) error {
	if current == nil {
		return nil
	}
	if current.cancel != nil {
		current.cancel()
	}
	if current.server != nil {
		return current.server.Close()
	}
	return nil
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
