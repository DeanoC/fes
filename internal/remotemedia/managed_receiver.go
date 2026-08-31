package remotemedia

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/mediasession"
)

var (
	ErrManagedReceiverStart = errors.New("managed receiver failed to start")
	ErrManagedReceiverStop  = errors.New("managed receiver failed to stop")
)

const defaultManagedReceiverStopTimeout = 2 * time.Second

// ManagedPacketConn is the UDP transport owned by ManagedReceiver.
type ManagedPacketConn interface {
	ReadFrom([]byte) (int, net.Addr, error)
	Close() error
	LocalAddr() net.Addr
}

// ManagedReceiverTransport is the authenticated RTP receiver owned by a
// ManagedReceiver. Receiver implementations must be safe for Close to race
// with Ingest.
type ManagedReceiverTransport interface {
	Ingest([]byte) ([]AccessUnit, error)
	AcceptControl(ControlMessage) error
	Close() error
}

// ManagedDecoder is the optional Annex-B H.264 decoder process owned by a
// ManagedReceiver.
type ManagedDecoder interface {
	Start() error
	Write([]byte) (int, error)
	Close() error
	Wait() error
	Kill() error
}

type ManagedReceiverConfig struct {
	Session        string
	Generation     uint64
	Token          string
	SSRC           uint32
	RTPAddress     string
	ControlAddress string
	Decoder        string // "none" or "ffplay"
}

type ManagedReceiverOption func(*ManagedReceiver)

type managedReceiverBind func(string, *net.UDPAddr) (ManagedPacketConn, error)
type managedReceiverControlListen func(string, string) (net.Listener, error)
type managedReceiverFactory func(ReceiverConfig) (ManagedReceiverTransport, error)
type managedDecoderFactory func(context.Context) (ManagedDecoder, error)

// WithManagedReceiverBind injects UDP binding for deterministic tests.
func WithManagedReceiverBind(bind func(string, *net.UDPAddr) (ManagedPacketConn, error)) ManagedReceiverOption {
	return func(r *ManagedReceiver) {
		if bind != nil {
			r.bind = bind
		}
	}
}

// WithManagedReceiverFactory injects authenticated receiver construction.
// WithManagedReceiverControlListen injects TCP control listener construction.
func WithManagedReceiverControlListen(listen func(string, string) (net.Listener, error)) ManagedReceiverOption {
	return func(r *ManagedReceiver) {
		if listen != nil {
			r.controlListen = listen
		}
	}
}

func WithManagedReceiverFactory(factory func(ReceiverConfig) (ManagedReceiverTransport, error)) ManagedReceiverOption {
	return func(r *ManagedReceiver) {
		if factory != nil {
			r.newReceiver = factory
		}
	}
}

// WithManagedReceiverDecoder injects decoder process construction. Returning
// nil disables decoding for that instance.
func WithManagedReceiverDecoder(factory func(context.Context) (ManagedDecoder, error)) ManagedReceiverOption {
	return func(r *ManagedReceiver) { r.newDecoder = factory }
}

func WithManagedReceiverStopTimeout(timeout time.Duration) ManagedReceiverOption {
	return func(r *ManagedReceiver) {
		if timeout > 0 {
			r.stopTimeout = timeout
		}
	}
}

type ManagedReceiver struct {
	config        ManagedReceiverConfig
	bind          managedReceiverBind
	controlListen managedReceiverControlListen
	newReceiver   managedReceiverFactory
	newDecoder    managedDecoderFactory
	stopTimeout   time.Duration
}

func NewManagedReceiver(config ManagedReceiverConfig, options ...ManagedReceiverOption) (*ManagedReceiver, error) {
	if config.Session == "" || config.Token == "" || config.RTPAddress == "" {
		return nil, errors.New("managed receiver configuration is invalid")
	}
	if config.Decoder != "" && config.Decoder != "none" && config.Decoder != "ffplay" {
		return nil, errors.New("managed receiver configuration is invalid")
	}
	r := &ManagedReceiver{
		config: config, bind: defaultManagedReceiverBind, controlListen: defaultManagedReceiverControlListen, newReceiver: defaultManagedReceiverFactory,
		stopTimeout: defaultManagedReceiverStopTimeout,
	}
	if config.Decoder == "ffplay" {
		r.newDecoder = defaultFFPlayDecoder
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r, nil
}

var _ mediasession.Component = (*ManagedReceiver)(nil)

func (r *ManagedReceiver) Start(ctx context.Context, _ string) (mediasession.ComponentHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrManagedReceiverStart
	}

	transport, err := r.newReceiver(ReceiverConfig{Session: r.config.Session, Generation: r.config.Generation, Token: r.config.Token, SSRC: r.config.SSRC})
	if err != nil || transport == nil {
		return nil, ErrManagedReceiverStart
	}
	address, err := resolveManagedReceiverAddress(r.config.RTPAddress)
	if err != nil {
		_ = transport.Close()
		return nil, ErrManagedReceiverStart
	}
	conn, err := r.bind("udp", address)
	if err != nil || conn == nil {
		_ = transport.Close()
		return nil, ErrManagedReceiverStart
	}
	var control net.Listener
	if r.config.ControlAddress != "" {
		control, err = r.controlListen("tcp", r.config.ControlAddress)
		if err != nil || control == nil {
			_ = conn.Close()
			_ = transport.Close()
			return nil, ErrManagedReceiverStart
		}
	}
	decoder, err := r.startDecoder(ctx)
	if err != nil {
		if control != nil {
			_ = control.Close()
		}
		_ = conn.Close()
		_ = transport.Close()
		return nil, ErrManagedReceiverStart
	}
	if err := ctx.Err(); err != nil {
		h := newManagedReceiverHandle(ctx, conn, control, transport, decoder, r.stopTimeout)
		_ = h.Stop(context.Background())
		return nil, ErrManagedReceiverStart
	}
	h := newManagedReceiverHandle(ctx, conn, control, transport, decoder, r.stopTimeout)
	return h, nil
}

func (r *ManagedReceiver) startDecoder(ctx context.Context) (ManagedDecoder, error) {
	if r.newDecoder == nil {
		return nil, nil
	}
	decoder, err := r.newDecoder(ctx)
	if err != nil || decoder == nil {
		return nil, err
	}
	if err := decoder.Start(); err != nil {
		cleanupDecoder(decoder, r.stopTimeout)
		return nil, err
	}
	return decoder, nil
}

type managedReceiverHandle struct {
	conn     ManagedPacketConn
	control  net.Listener
	receiver ManagedReceiverTransport
	decoder  ManagedDecoder
	cancel   context.CancelFunc
	loopDone chan struct{}
	timeout  time.Duration

	decoderMu    sync.Mutex // serializes decoder Write and Close
	cleanupOnce  sync.Once
	cleanupDone  chan struct{}
	cleanupMu    sync.Mutex
	cleanupErr   error
	controlWG    sync.WaitGroup
	controlMu    sync.Mutex
	controlConns map[net.Conn]struct{}
}

func newManagedReceiverHandle(parent context.Context, conn ManagedPacketConn, control net.Listener, receiver ManagedReceiverTransport, decoder ManagedDecoder, timeout time.Duration) *managedReceiverHandle {
	ctx, cancel := context.WithCancel(parent)
	h := &managedReceiverHandle{conn: conn, control: control, receiver: receiver, decoder: decoder, cancel: cancel, loopDone: make(chan struct{}), cleanupDone: make(chan struct{}), timeout: timeout, controlConns: make(map[net.Conn]struct{})}
	go h.run(ctx)
	if control != nil {
		h.controlWG.Add(1)
		go h.acceptControl(ctx)
	}
	go func() {
		<-ctx.Done()
		_ = h.Stop(context.Background())
	}()
	return h
}

func (h *managedReceiverHandle) acceptControl(ctx context.Context) {
	defer h.controlWG.Done()
	for {
		conn, err := h.control.Accept()
		if err != nil {
			return
		}
		h.controlMu.Lock()
		h.controlConns[conn] = struct{}{}
		h.controlMu.Unlock()
		h.controlWG.Add(1)
		go h.readControl(ctx, conn)
	}
}

func (h *managedReceiverHandle) readControl(ctx context.Context, conn net.Conn) {
	defer h.controlWG.Done()
	defer func() { h.controlMu.Lock(); delete(h.controlConns, conn); h.controlMu.Unlock(); _ = conn.Close() }()
	for {
		message, err := ReadControlMessage(conn)
		if err != nil {
			return
		}
		if err := h.receiver.AcceptControl(message); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (h *managedReceiverHandle) run(ctx context.Context) {
	defer close(h.loopDone)
	buffer := make([]byte, 64<<10)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, _, err := h.conn.ReadFrom(buffer)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		units, err := h.receiver.Ingest(buffer[:n])
		if err != nil || h.decoder == nil {
			continue
		}
		for _, unit := range units {
			var annexB []byte
			for _, nal := range unit.NALs {
				annexB = append(annexB, 0, 0, 0, 1)
				annexB = append(annexB, nal...)
			}
			h.decoderMu.Lock()
			_, _ = h.decoder.Write(annexB)
			h.decoderMu.Unlock()
		}
	}
}

func (h *managedReceiverHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.cleanupOnce.Do(func() { go h.cleanup() })
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case <-h.cleanupDone:
		h.cleanupMu.Lock()
		err := h.cleanupErr
		h.cleanupMu.Unlock()
		return err
	case <-ctx.Done():
		return ErrManagedReceiverStop
	case <-timer.C:
		return ErrManagedReceiverStop
	}
}

func (h *managedReceiverHandle) cleanup() {
	defer close(h.cleanupDone)
	h.cancel()
	_ = h.conn.Close()
	if h.control != nil {
		_ = h.control.Close()
	}
	h.controlMu.Lock()
	for conn := range h.controlConns {
		_ = conn.Close()
	}
	h.controlMu.Unlock()
	_ = h.receiver.Close()
	h.controlWG.Wait()

	deadline := time.Now().Add(h.timeout)
	if h.decoder != nil {
		closed := make(chan struct{})
		go func() {
			h.decoderMu.Lock()
			_ = h.decoder.Close()
			h.decoderMu.Unlock()
			close(closed)
		}()
		if !waitUntil(closed, deadline) {
			// Kill is safe to use while a decoder Write is blocked and causes
			// process-backed writers to return so the serialized Close can run.
			_ = h.decoder.Kill()
		}
	}
	if !waitUntil(h.loopDone, deadline) {
		h.setCleanupError(ErrManagedReceiverStop)
	}
	if h.decoder != nil {
		waitCtx, cancel := context.WithDeadline(context.Background(), deadline)
		err := waitDecoder(h.decoder, waitCtx)
		cancel()
		// waitDecoder kills the process on timeout and leaves its waiter
		// goroutine responsible for reaping it; cleanup remains bounded.
		_ = err
	}
}

func (h *managedReceiverHandle) setCleanupError(err error) {
	h.cleanupMu.Lock()
	if h.cleanupErr == nil {
		h.cleanupErr = err
	}
	h.cleanupMu.Unlock()
}

func waitUntil(done <-chan struct{}, deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func defaultManagedReceiverBind(network string, address *net.UDPAddr) (ManagedPacketConn, error) {
	return net.ListenUDP(network, address)
}
func defaultManagedReceiverControlListen(network, address string) (net.Listener, error) {
	return net.Listen(network, address)
}
func defaultManagedReceiverFactory(config ReceiverConfig) (ManagedReceiverTransport, error) {
	return NewReceiver(config)
}
func resolveManagedReceiverAddress(address string) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp", address)
}

func defaultFFPlayDecoder(ctx context.Context) (ManagedDecoder, error) {
	cmd := exec.CommandContext(ctx, "ffplay", "-loglevel", "warning", "-fflags", "nobuffer", "-flags", "low_delay", "-f", "h264", "-i", "pipe:0")
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	return &ffplayManagedDecoder{cmd: cmd, input: input}, nil
}

type ffplayManagedDecoder struct {
	cmd   *exec.Cmd
	input interface {
		Write([]byte) (int, error)
		Close() error
	}
}

func (d *ffplayManagedDecoder) Start() error                { return d.cmd.Start() }
func (d *ffplayManagedDecoder) Write(p []byte) (int, error) { return d.input.Write(p) }
func (d *ffplayManagedDecoder) Close() error                { return d.input.Close() }
func (d *ffplayManagedDecoder) Wait() error                 { return d.cmd.Wait() }
func (d *ffplayManagedDecoder) Kill() error {
	if d.cmd.Process == nil {
		return nil
	}
	return d.cmd.Process.Kill()
}

func cleanupDecoder(decoder ManagedDecoder, timeout time.Duration) {
	_ = decoder.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = waitDecoder(decoder, ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// The waiter continues in the background so a process that takes
		// longer than the startup budget is still reaped after Kill.
	}
}
func waitDecoder(decoder ManagedDecoder, ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- decoder.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = decoder.Kill()
		// Reap after killing whenever the decoder honors Kill. Callers that
		// need a bounded wait run this function asynchronously (as startup
		// cleanup does, and Stop's cleanup worker does).
		<-done
		return ctx.Err()
	}
}
