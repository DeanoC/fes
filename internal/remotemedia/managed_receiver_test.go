package remotemedia

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type managedFakeConn struct {
	mu     sync.Mutex
	closed int
	readCh chan error
}

func newManagedFakeConn() *managedFakeConn { return &managedFakeConn{readCh: make(chan error, 1)} }
func (c *managedFakeConn) ReadFrom([]byte) (int, net.Addr, error) {
	err, ok := <-c.readCh
	if !ok {
		return 0, nil, net.ErrClosed
	}
	return 0, nil, err
}
func (c *managedFakeConn) Close() error {
	c.mu.Lock()
	c.closed++
	c.mu.Unlock()
	select {
	case c.readCh <- net.ErrClosed:
	default:
	}
	return nil
}
func (c *managedFakeConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5004}
}

func (c *managedFakeConn) closeCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }

type managedFakeReceiver struct {
	mu     sync.Mutex
	closed int
}

func (r *managedFakeReceiver) Ingest([]byte) ([]AccessUnit, error) {
	return nil, errors.New("malformed packet token=secret /private")
}
func (r *managedFakeReceiver) Close() error    { r.mu.Lock(); r.closed++; r.mu.Unlock(); return nil }
func (r *managedFakeReceiver) closeCount() int { r.mu.Lock(); defer r.mu.Unlock(); return r.closed }

type managedFakeDecoder struct {
	mu       sync.Mutex
	started  int
	closed   int
	waited   int
	startErr error
}

type managedBlockingDecoder struct {
	writeStarted         chan struct{}
	writeRelease         chan struct{}
	closeStarted         chan struct{}
	kill                 chan struct{}
	reaped               chan struct{}
	waitStarted          chan struct{}
	closeBeforeWriteDone chan struct{}
	writeDone            chan struct{}
}

func newManagedBlockingDecoder() *managedBlockingDecoder {
	return &managedBlockingDecoder{
		writeStarted: make(chan struct{}), writeRelease: make(chan struct{}),
		closeStarted: make(chan struct{}), kill: make(chan struct{}),
		reaped: make(chan struct{}), waitStarted: make(chan struct{}), closeBeforeWriteDone: make(chan struct{}),
		writeDone: make(chan struct{}),
	}
}
func (d *managedBlockingDecoder) Start() error { return nil }
func (d *managedBlockingDecoder) Write([]byte) (int, error) {
	close(d.writeStarted)
	<-d.writeRelease
	close(d.writeDone)
	return 0, nil
}
func (d *managedBlockingDecoder) Close() error {
	close(d.closeStarted)
	select {
	case <-d.writeDone:
	default:
		close(d.closeBeforeWriteDone)
	}
	return nil
}
func (d *managedBlockingDecoder) Wait() error {
	close(d.waitStarted)
	<-d.kill
	close(d.reaped)
	return nil
}
func (d *managedBlockingDecoder) Kill() error {
	select {
	case <-d.kill:
	default:
		close(d.kill)
	}
	return nil
}

type managedUnitReceiver struct{}

func (managedUnitReceiver) Ingest([]byte) ([]AccessUnit, error) {
	return []AccessUnit{{NALs: [][]byte{{1}}}}, nil
}
func (managedUnitReceiver) Close() error { return nil }

func (d *managedFakeDecoder) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started++
	return d.startErr
}
func (d *managedFakeDecoder) Write([]byte) (int, error) { return 0, nil }
func (d *managedFakeDecoder) Close() error              { d.mu.Lock(); d.closed++; d.mu.Unlock(); return nil }
func (d *managedFakeDecoder) Wait() error               { d.mu.Lock(); d.waited++; d.mu.Unlock(); return nil }
func (d *managedFakeDecoder) Kill() error               { return nil }
func (d *managedFakeDecoder) counts() (int, int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.started, d.closed, d.waited
}

func managedConfig() ManagedReceiverConfig {
	return ManagedReceiverConfig{Session: "session", Generation: 7, Token: "secret-token", RTPAddress: "127.0.0.1:5004"}
}

func TestManagedReceiverStartIsReadyOnlyAfterBindAndDecoderStartup(t *testing.T) {
	conn := newManagedFakeConn()
	receiver := &managedFakeReceiver{}
	decoder := &managedFakeDecoder{}
	component, err := NewManagedReceiver(managedConfig(),
		WithManagedReceiverBind(func(string, *net.UDPAddr) (ManagedPacketConn, error) { return conn, nil }),
		WithManagedReceiverFactory(func(ReceiverConfig) (ManagedReceiverTransport, error) { return receiver, nil }),
		WithManagedReceiverDecoder(func(context.Context) (ManagedDecoder, error) { return decoder, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}

	handle, err := component.Start(context.Background(), "game")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started, _, _ := decoder.counts()
	if conn.closeCount() != 0 || started != 1 {
		t.Fatalf("not ready after Start: conn closes=%d decoder starts=%d", conn.closeCount(), started)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestManagedReceiverDecoderStartupFailureCleansUp(t *testing.T) {
	conn := newManagedFakeConn()
	receiver := &managedFakeReceiver{}
	decoder := &managedFakeDecoder{startErr: errors.New("ffplay failed token=secret /private/bin")}
	component, err := NewManagedReceiver(managedConfig(),
		WithManagedReceiverBind(func(string, *net.UDPAddr) (ManagedPacketConn, error) { return conn, nil }),
		WithManagedReceiverFactory(func(ReceiverConfig) (ManagedReceiverTransport, error) { return receiver, nil }),
		WithManagedReceiverDecoder(func(context.Context) (ManagedDecoder, error) { return decoder, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = component.Start(context.Background(), "game")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/private") {
		t.Fatalf("unsafe startup error: %v", err)
	}
	if conn.closeCount() != 1 || receiver.closeCount() != 1 {
		t.Fatalf("cleanup conn=%d receiver=%d", conn.closeCount(), receiver.closeCount())
	}
	_, closed, waited := decoder.counts()
	if closed != 1 || waited != 1 {
		t.Fatalf("decoder cleanup closed=%d waited=%d", closed, waited)
	}
}

func TestManagedReceiverCancellationAndStopDoNotLeak(t *testing.T) {
	conn := newManagedFakeConn()
	receiver := &managedFakeReceiver{}
	component, err := NewManagedReceiver(managedConfig(),
		WithManagedReceiverBind(func(string, *net.UDPAddr) (ManagedPacketConn, error) { return conn, nil }),
		WithManagedReceiverFactory(func(ReceiverConfig) (ManagedReceiverTransport, error) { return receiver, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := component.Start(ctx, "game")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if conn.closeCount() != 1 || receiver.closeCount() != 1 {
		t.Fatalf("cleanup conn=%d receiver=%d", conn.closeCount(), receiver.closeCount())
	}
}

func TestManagedReceiverSanitizesConstructionErrors(t *testing.T) {
	secret := "token=top-secret /Users/private/capture"
	component, err := NewManagedReceiver(managedConfig(),
		WithManagedReceiverFactory(func(ReceiverConfig) (ManagedReceiverTransport, error) { return nil, errors.New(secret) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = component.Start(context.Background(), "game")
	if err == nil || strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "/Users/private") {
		t.Fatalf("unsafe receiver error: %v", err)
	}
}

func TestManagedReceiverStopIsIdempotent(t *testing.T) {
	conn := newManagedFakeConn()
	receiver := &managedFakeReceiver{}
	decoder := &managedFakeDecoder{}
	component, err := NewManagedReceiver(managedConfig(),
		WithManagedReceiverBind(func(string, *net.UDPAddr) (ManagedPacketConn, error) { return conn, nil }),
		WithManagedReceiverFactory(func(ReceiverConfig) (ManagedReceiverTransport, error) { return receiver, nil }),
		WithManagedReceiverDecoder(func(context.Context) (ManagedDecoder, error) { return decoder, nil }),
		WithManagedReceiverStopTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := component.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if conn.closeCount() != 1 || receiver.closeCount() != 1 {
		t.Fatalf("stop not idempotent conn=%d receiver=%d", conn.closeCount(), receiver.closeCount())
	}
	_, closed, waited := decoder.counts()
	if closed != 1 || waited != 1 {
		t.Fatalf("decoder stop not idempotent closed=%d waited=%d", closed, waited)
	}
}

func TestManagedReceiverStopHonorsExpiredContextAndCanRetryCleanup(t *testing.T) {
	conn := newManagedFakeConn()
	decoder := newManagedBlockingDecoder()
	conn.readCh <- nil
	h := newManagedReceiverHandle(context.Background(), conn, managedUnitReceiver{}, decoder, time.Second)
	<-decoder.writeStarted

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := h.Stop(expired); !errors.Is(err, ErrManagedReceiverStop) {
		t.Fatalf("Stop error = %v, want %v", err, ErrManagedReceiverStop)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("expired Stop took %v", elapsed)
	}
	close(decoder.writeRelease)
	select {
	case <-decoder.writeDone:
	case <-time.After(time.Second):
		t.Fatal("decoder write was not released")
	}
	select {
	case <-decoder.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("decoder close was not attempted")
	}
	select {
	case <-decoder.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("decoder Wait was not started")
	}
	select {
	case <-decoder.reaped:
	case <-time.After(2 * time.Second):
		t.Fatal("decoder was not reaped")
	}
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
}

func TestManagedReceiverStopSerializesDecoderWriteAndClose(t *testing.T) {
	conn := newManagedFakeConn()
	decoder := newManagedBlockingDecoder()
	conn.readCh <- nil
	h := newManagedReceiverHandle(context.Background(), conn, managedUnitReceiver{}, decoder, 50*time.Millisecond)
	<-decoder.writeStarted

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.Stop(ctx); !errors.Is(err, ErrManagedReceiverStop) {
		t.Fatalf("Stop error = %v, want %v", err, ErrManagedReceiverStop)
	}
	select {
	case <-decoder.closeBeforeWriteDone:
		t.Fatal("decoder Close raced with Write")
	case <-time.After(25 * time.Millisecond):
	}
	close(decoder.writeRelease)
	select {
	case <-decoder.reaped:
	case <-time.After(time.Second):
		t.Fatal("decoder was not reaped")
	}
}

func TestWaitDecoderKillsAndReapsAfterTimeout(t *testing.T) {
	decoder := newManagedBlockingDecoder()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitDecoder(decoder, ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitDecoder error = %v", err)
	}
	select {
	case <-decoder.reaped:
	case <-time.After(time.Second):
		t.Fatal("decoder was not reaped after kill")
	}
}
