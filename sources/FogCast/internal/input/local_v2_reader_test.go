package input

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func v2BeginFor(f *v2BackendFixture) localhid.Request {
	return localhid.Request{Version: 2, Op: "begin", Request: 1, Publisher: strings.Repeat("1", 32), AgentInstance: f.req.AgentInstance, QueueEpoch: 1, LeaseGeneration: f.req.LeaseGeneration, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, CoreGeneration: 7, Devices: []localhid.DeviceSnapshot{{ID: localhid.DeviceID{1}, Held: []uint8{}}}}
}
func v2Exchange(t *testing.T, c net.Conn, req localhid.Request) localhid.Reply {
	t.Helper()
	wire, err := localhid.EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(wire); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	incoming, err := localhid.ReadIncoming(c)
	if err != nil || incoming.Reply == nil {
		t.Fatalf("reply %+v %v", incoming, err)
	}
	return *incoming.Reply
}
func sendV2Ping(t *testing.T, c net.Conn, seq uint32) {
	t.Helper()
	frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: seq, ClientMonoNS: uint64(seq)}
	if err := bridge.WriteFrame(c, frame); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	r, err := localhid.ReadIncoming(c)
	if err != nil || r.Pong == nil {
		t.Fatalf("pong %+v %v", r, err)
	}
}

// These focused reader tests start after a separately verified startup
// retirement; recovery integration tests exercise the actual startup gate.
func testEnableRecovery(t *testing.T, c *TargetController) {
	t.Helper()
	if err := c.SetLocalTargetIdentity("test-kit", "test-boot"); err != nil {
		t.Fatal(err)
	}
	c.localRecoveryMu.Lock()
	c.localRecoveryReady = true
	c.localRecoveryMu.Unlock()
	if err := c.EnableLocalInputRecovery(); err != nil {
		t.Fatal(err)
	}
}

func testRecoverConnection(t *testing.T, c *TargetController, conn net.Conn) {
	t.Helper()
	agent, err := c.LocalAgentInstance()
	if err != nil {
		t.Fatal(err)
	}
	reply := v2Exchange(t, conn, localhid.Request{Version: 2, Op: "recover", Request: 1, AgentInstance: agent, TargetID: "test-kit", BootID: "test-boot"})
	if reply.Status != "reader-drained" {
		t.Fatalf("recovery: %+v", reply)
	}
}

func TestReadLocalV2DisabledCannotLatchRawGuard(t *testing.T) {
	f := newV2BackendFixture(t)
	valid, err := localhid.EncodeRequest(v2BeginFor(f))
	if err != nil {
		t.Fatal(err)
	}
	for name, record := range map[string]string{
		"valid":           string(valid),
		"malformed":       "{\"version\":2,\"op\":\n",
		"unknown-version": "{\"version\":3}\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := NewTargetController()
			a, b := net.Pipe()
			done := make(chan struct{})
			go func() { c.readLocal(context.Background(), a, false); close(done) }()
			if _, err := b.Write([]byte(record)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("disabled receiver did not close")
			}
			_ = b.Close()
			if c.localV2Guard.Load() {
				t.Fatal("disabled v2 traffic latched the raw keyboard guard")
			}
		})
	}
}

func TestReadLocalV2DisabledPreservesOrdinaryKeyboard(t *testing.T) {
	f := newV2BackendFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, writer := net.Pipe()
	done := make(chan struct{})
	go func() { f.c.readLocal(ctx, first, false); close(done) }()
	wire, err := localhid.EncodeRequest(v2BeginFor(f))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(wire); err != nil {
		t.Fatal(err)
	}
	<-done
	_ = writer.Close()

	second, keyboard := net.Pipe()
	done = make(chan struct{})
	go func() { f.c.readLocal(ctx, second, false); close(done) }()
	code, _ := hidkeys.Code(4)
	frame := keyboardFrameFor(code, remoteinput.ActionPress)
	frame.Seq = 1
	if err := bridge.WriteFrame(keyboard, frame); err != nil {
		t.Fatal(err)
	}
	sendV2Ping(t, keyboard, 2)
	f.mu.Lock()
	pressed := len(f.rows) > 0 && f.rows[len(f.rows)-1][0]&(1<<4) != 0
	f.mu.Unlock()
	if !pressed {
		t.Fatal("ordinary keyboard did not pass after rejected v2 traffic")
	}
	_ = keyboard.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("legacy reader did not close")
	}
}

// net.Pipe is an in-memory connection; no local socket, device or kit is used.
func TestReadLocalV2RejectsRawKeyboardAndRetainsGuardAcrossEOF(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { f.c.readLocal(ctx, a, true); close(done) }()
	testRecoverConnection(t, f.c, b)
	request := v2BeginFor(f)
	request.Request = 2
	begin := v2Exchange(t, b, request)
	if begin.Status != "ready" {
		t.Fatalf("begin %+v", begin)
	}
	code, _ := hidkeys.Code(4)
	raw := keyboardFrameFor(code, remoteinput.ActionPress)
	raw.Device = uint8(remoteinput.DeviceGamepad)
	raw.Seq = 1
	if err := bridge.WriteFrame(b, raw); err != nil {
		t.Fatal(err)
	}
	sendV2Ping(t, b, 2)
	f.mu.Lock()
	count := len(f.rows)
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("disguised raw keyboard posted")
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if r := v2Exchange(t, b, edge); r.Status != "accepted" {
		t.Fatalf("edge %+v", r)
	}
	reset := localhid.Request{Version: 2, Op: "reset", Request: 4, Token: begin.Token, LastEventSeq: 1}
	if r := v2Exchange(t, b, reset); r.Status != "neutral-confirmed" {
		t.Fatalf("reset %+v", r)
	}
	_ = b.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not close")
	}
	if !f.c.localV2Guard.Load() {
		t.Fatal("v2 guard cleared on EOF")
	}
	x, y := net.Pipe()
	done2 := make(chan struct{})
	go func() { f.c.readLocal(ctx, x, true); close(done2) }()
	raw.Device = uint8(remoteinput.DeviceKeyboard)
	raw.Seq = 1
	if err := bridge.WriteFrame(y, raw); err != nil {
		t.Fatal(err)
	}
	sendV2Ping(t, y, 2)
	f.mu.Lock()
	last := f.rows[len(f.rows)-1]
	f.mu.Unlock()
	if last[0]&(1<<4) != 0 {
		t.Fatal("raw keyboard bypassed persistent guard")
	}
	_ = y.Close()
	select {
	case <-done2:
	case <-time.After(time.Second):
		t.Fatal("second reader did not close")
	}
}
func TestReadLocalV2UncertainNeutralRetainsReaderSlot(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { f.c.readLocal(ctx, a, true); close(done) }()
	testRecoverConnection(t, f.c, b)
	request := v2BeginFor(f)
	request.Request = 2
	begin := v2Exchange(t, b, request)
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if r := v2Exchange(t, b, edge); r.Status != "accepted" {
		t.Fatal(r)
	}
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return context.DeadlineExceeded })
	_ = b.Close()
	select {
	case <-done:
		t.Fatal("reader released while neutral uncertain")
	case <-time.After(100 * time.Millisecond):
	}
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return nil })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not release after confirmed neutral")
	}
	if !f.c.localV2Guard.Load() {
		t.Fatal("raw guard cleared after cleanup")
	}
}

func TestReadLocalV2StaleLegacyRecordsDoNotRetireOwnedSource(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { f.c.readLocal(ctx, a, true); close(done) }()
	defer b.Close()
	testRecoverConnection(t, f.c, b)
	request := v2BeginFor(f)
	request.Request = 2
	begin := v2Exchange(t, b, request)
	press := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if r := v2Exchange(t, b, press); r.Status != "accepted" {
		t.Fatal(r)
	}
	old := localDelegation{Operation: "reset", LeaseGeneration: f.req.LeaseGeneration, PackageID: f.req.PackageID, CoreGeneration: 7, Epoch: 1}
	header, _ := json.Marshal(old)
	header = append(header, '\n')
	if _, err := b.Write(header); err != nil {
		t.Fatal(err)
	}
	old.Operation = "key"
	header, _ = json.Marshal(old)
	header = append(header, '\n')
	if _, err := b.Write(header); err != nil {
		t.Fatal(err)
	}
	code, _ := hidkeys.Code(4)
	if err := bridge.WriteFrame(b, keyboardFrameFor(code, remoteinput.ActionRelease)); err != nil {
		t.Fatal(err)
	}
	release := localhid.Request{Version: 2, Op: "edge", Request: 4, Token: begin.Token, EventSeq: 2, Device: localhid.DeviceID{1}, Usage: 4, Down: false}
	if r := v2Exchange(t, b, release); r.Status != "accepted" {
		t.Fatalf("stale legacy record retired active source: %+v", r)
	}
	f.mu.Lock()
	if len(f.rows) != 2 {
		f.mu.Unlock()
		t.Fatalf("unexpected HID writes %d", len(f.rows))
	}
	f.mu.Unlock()
	reset := localhid.Request{Version: 2, Op: "reset", Request: 5, Token: begin.Token, LastEventSeq: 2}
	if r := v2Exchange(t, b, reset); r.Status != "neutral-confirmed" {
		t.Fatal(r)
	}
	_ = b.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not exit")
	}
}
func TestServeLocalInputRejectsSecondOwnerBeforeListening(t *testing.T) {
	c := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	c.localStarting = true
	if err := c.ServeLocalInput(context.Background(), "/path/never/opened/local-input.sock"); err == nil {
		t.Fatal("second listener allowed")
	}
}
