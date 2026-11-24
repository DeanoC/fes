// Package localfeed writes one pad or keyboard stream to mister-agent's
// kit-local input socket. Tenfoot uses it so a core sees
// one frame encoding. A missing socket stays dark for a second between dials.
package localfeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	// DefaultSocket is the kit-local input socket mister-agent listens on.
	DefaultSocket = "/run/fogcast/local-input.sock"
	// Session satisfies the input-frame encoding, which rejects session 0.
	// The kit-local socket does not consult a lease or this value.
	Session uint64 = 1
	// Dial is the connect and write budget for one frame.
	Dial = 250 * time.Millisecond
	// Retry is how long a missing agent socket stays dark. One pad poll
	// emits several events, and each failed dial can spend the full dial budget.
	// Further sends wait until this elapses, then try once.
	Retry = time.Second
)

// ErrUnavailable means the agent is not listening. Callers must not post
// the same pad to the host.
var ErrUnavailable = errors.New("local input socket is not listening")

// Feed is one connection to mister-agent's kit-local input socket.
// Closing it releases only that local source.
type Feed struct {
	mu           sync.Mutex
	path         string
	conn         net.Conn
	seq          uint32
	nextDial     time.Time
	dial         func(network, address string, timeout time.Duration) (net.Conn, error)
	binding      *Binding
	v2           *V2Transport
	v2Used       bool
	v2Reserved   bool
	keyboardUsed bool
	legacyUsed   bool
	recovering   bool
	padUnion     padContributorUnion
}

// V2Handle identifies one connection, including after it has closed. Its
// unexported pointer prevents a later connection from inheriting its proof.
type V2Handle struct{ transport *V2Transport }

func (h V2Handle) Done() <-chan struct{} {
	if h.transport == nil {
		return nil
	}
	return h.transport.Done()
}

// Binding names the exact execution lease and HID core receiving physical
// keyboard events during an externally owned session.
type Binding struct {
	LeaseGeneration string `json:"lease_generation"`
	PackageID       string `json:"package_id"`
	CoreGeneration  uint64 `json:"core_generation"`
	// Epoch distinguishes local queue lifetimes on the same lease and core.
	Epoch uint64 `json:"epoch"`
}

type boundRecord struct {
	Operation string `json:"operation"`
	Binding
}

func writeBoundRecord(conn net.Conn, operation string, binding Binding, frame []byte) error {
	header, err := json.Marshal(boundRecord{Operation: operation, Binding: binding})
	if err != nil {
		return err
	}
	header = append(header, '\n')
	header = append(header, frame...)
	_ = conn.SetWriteDeadline(time.Now().Add(Dial))
	_, err = conn.Write(header)
	return err
}

// New dials path on the first send. An empty path fails each send with
// ErrUnavailable and does not dial. dial may be nil; the default is
// net.DialTimeout.
func New(path string, dial func(string, string, time.Duration) (net.Conn, error)) *Feed {
	return &Feed{path: path, dial: dial}
}

// ReserveV2 fences a pristine Feed before native input polling begins. It
// performs no I/O and allows no raw input until correlated recovery on the
// eventual connection succeeds. A failed reservation never changes ownership.
func (f *Feed) ReserveV2() error {
	if f == nil || f.path == "" {
		return ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn != nil || f.v2 != nil || f.v2Used || f.keyboardUsed || f.legacyUsed || f.binding != nil || f.recovering {
		return ErrUnavailable
	}
	f.v2Used = true
	f.v2Reserved = true
	return nil
}

// Close releases the current connection. A later Send dials again.
func (f *Feed) Close() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeLocked()
}

// CloseBound retires only this queue lifetime. A later session may already
// have opened a new connection when the old queue has finished draining.
func (f *Feed) CloseBound(binding Binding) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn != nil && f.binding != nil && *f.binding == binding {
		if err := writeBoundRecord(f.conn, "reset", binding, nil); err != nil {
			f.closeLocked()
		} else {
			f.binding = nil
		}
	}
}

func (f *Feed) closeLocked() {
	f.padUnion = padContributorUnion{}
	if f.v2 != nil {
		_ = f.v2.Close()
		f.v2 = nil
	}
	f.recovering = false
	if f.conn == nil {
		return
	}
	_ = f.conn.Close()
	f.conn = nil
	f.binding = nil
}

// RecoverV2 obtains a correlated reader recovery reply on a fresh connection.
// The connection remains recovery-only until its owner validates the reply and
// explicitly promotes this same transport. This operation is not enabled by
// itself and does not grant keyboard input or a physical snapshot.
func (f *Feed) RecoverV2(ctx context.Context, previousAgent, targetID, bootID string, timeout time.Duration) (localhid.Reply, V2Handle, error) {
	if f == nil || f.path == "" || ctx == nil || timeout <= 0 {
		return localhid.Reply{}, V2Handle{}, ErrUnavailable
	}
	f.mu.Lock()
	if err := ctx.Err(); err != nil {
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, err
	}
	if !f.v2Reserved || f.legacyUsed || f.keyboardUsed || f.conn != nil || f.v2 != nil || f.recovering || f.binding != nil {
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, ErrUnavailable
	}
	conn, err := f.dialTimeout()
	if err != nil {
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err = ctx.Err(); err != nil {
		_ = conn.Close()
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, err
	}
	t, err := NewV2RecoveryTransport(conn, timeout)
	if err != nil {
		_ = conn.Close()
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, err
	}
	f.conn, f.v2, f.v2Used, f.recovering = conn, t, true, true
	f.mu.Unlock()
	reply, err := t.Request(ctx, localhid.Request{Op: "recover", AgentInstance: previousAgent, TargetID: targetID, BootID: bootID})
	if err != nil {
		f.mu.Lock()
		if f.v2 == t {
			f.closeLocked()
		}
		f.mu.Unlock()
		return localhid.Reply{}, V2Handle{}, err
	}
	return reply, V2Handle{transport: t}, nil
}

// PromoteRecovered preserves the exact connection that produced the ACK.
// The caller must first validate the receiver's source-cleanup guarantee;
// guest keyboard still needs a fresh physical snapshot and BEGIN/ACK.
func (f *Feed) PromoteRecovered(handle V2Handle, reply localhid.Reply) error {
	return f.promoteRecovered(handle, reply, 0, nil)
}

// PromoteRecoveredPad binds mapped-key contributors to a non-reused App pad
// epoch while promoting the exact recovered connection, in one nonblocking
// transaction. The caller holds and validates App authority during the call.
func (f *Feed) PromoteRecoveredPad(handle V2Handle, reply localhid.Reply, padEpoch uint64, fence *PadWriteFence) error {
	if padEpoch == 0 || fence.Revoked() {
		return ErrUnavailable
	}
	return f.promoteRecovered(handle, reply, padEpoch, fence)
}

func (f *Feed) promoteRecovered(handle V2Handle, reply localhid.Reply, padEpoch uint64, fence *PadWriteFence) error {
	if f == nil {
		return ErrUnavailable
	}
	// The App holds its authority lock while promoting. Never wait for a
	// concurrent feed write or transport shutdown while holding that lock.
	if !f.mu.TryLock() {
		return ErrUnavailable
	}
	defer f.mu.Unlock()
	if !f.recovering || f.v2 == nil || f.v2 != handle.transport {
		return ErrUnavailable
	}
	if padEpoch != 0 && fence.Revoked() {
		return ErrUnavailable
	}
	if err := f.v2.PromoteRecovered(reply); err != nil {
		return err
	}
	if padEpoch != 0 && fence.Revoked() {
		return ErrUnavailable
	}
	f.padUnion.epoch = padEpoch
	f.padUnion.fence = fence
	f.recovering = false
	return nil
}

// CloseV2 closes only the exact connection a caller owns; a stale callback
// cannot tear down a newer recovery or source.
func (f *Feed) CloseV2(handle V2Handle) {
	if f == nil || handle.transport == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.v2 == handle.transport {
		f.closeLocked()
	}
}

// RequestV2 sends one correlated request on the active v2 transport. It does
// not hold the Feed lock while waiting, so pad and mouse writes can proceed.
func (f *Feed) RequestV2(ctx context.Context, handle V2Handle, req localhid.Request) (localhid.Reply, error) {
	if f == nil || ctx == nil {
		return localhid.Reply{}, ErrUnavailable
	}
	f.mu.Lock()
	t := f.v2
	if f.recovering || t != handle.transport {
		t = nil
	}
	f.mu.Unlock()
	if t == nil {
		return localhid.Reply{}, ErrUnavailable
	}
	return t.Request(ctx, req)
}

// Send writes one raw event. A down socket is not dialed again until Retry
// has elapsed. now is the frame timestamp; a zero time is time.Now.
func (f *Feed) Send(e remoteinput.Event, now time.Time) error {
	return f.send(context.Background(), e, now, nil)
}

// SendBound keeps an immutable binding for this frame even if the observed
// session changes while it waits behind an earlier local input.
func (f *Feed) SendBound(ctx context.Context, e remoteinput.Event, now time.Time, binding Binding) error {
	if binding.LeaseGeneration == "" || binding.PackageID == "" || binding.CoreGeneration == 0 {
		return ErrUnavailable
	}
	return f.send(ctx, e, now, &binding)
}

func (f *Feed) send(ctx context.Context, e remoteinput.Event, now time.Time, binding *Binding) error {
	if f == nil || f.path == "" {
		return ErrUnavailable
	}
	if ctx == nil {
		return ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if binding == nil && f.v2Used && f.v2 != nil && !f.recovering && e.Device == remoteinput.DeviceGamepad && e.Kind == remoteinput.KindButton {
		return f.sendPhysicalPadLocked(ctx, e, now)
	}
	if binding == nil && f.v2Used && f.v2 != nil && !f.recovering && e.Device == remoteinput.DeviceGamepad && e.Kind == remoteinput.KindAxis && e.Player < 2 && ctx.Err() == nil {
		f.padUnion.departure[e.Player] = false
		if err := f.sendLocked(ctx, e, now, nil); err != nil {
			f.closeLocked()
			return err
		}
		return nil
	}
	return f.sendLocked(ctx, e, now, binding)
}

func (f *Feed) sendLocked(ctx context.Context, e remoteinput.Event, now time.Time, binding *Binding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keyboard := binding != nil || e.Device == remoteinput.DeviceKeyboard || e.Kind == remoteinput.KindKey
	if f.v2Used && (keyboard || f.v2 == nil || f.recovering) {
		return ErrUnavailable
	}
	if !f.v2Used {
		f.legacyUsed = true
	}
	if keyboard {
		f.keyboardUsed = true
	}
	if now.IsZero() {
		now = time.Now()
	}
	if f.conn == nil && !f.nextDial.IsZero() && now.Before(f.nextDial) {
		return ErrUnavailable
	}
	f.seq++
	frame := Frame(f.seq, e, now)
	wire, err := protocol.EncodeInputFrame(frame)
	if err != nil {
		f.seq--
		return err
	}
	if f.conn == nil {
		conn, err := f.dialTimeout()
		if err != nil {
			f.seq--
			f.nextDial = now.Add(Retry)
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		f.conn = conn
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.v2 != nil {
		if !f.padConnectionValidLocked() {
			return f.failRevokedPadLocked()
		}
		if err := f.v2.WriteRaw(ctx, wire); err != nil {
			// Do not replay this ambiguous frame or silently reconnect. The
			// receiver may still be draining the old source.
			f.closeLocked()
			f.nextDial = now.Add(Retry)
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if !f.padConnectionValidLocked() {
			return f.failRevokedPadLocked()
		}
		return nil
	}
	if binding == nil && f.binding != nil && (e.Device == remoteinput.DeviceKeyboard || e.Kind == remoteinput.KindKey) {
		// A newer unbound keyboard mode must retire the old generation before
		// its first key. Pad and mouse frames leave the bound keyboard intact.
		if err := writeBoundRecord(f.conn, "reset", *f.binding, nil); err != nil {
			f.closeLocked()
			f.nextDial = now.Add(Retry)
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		f.binding = nil
	}
	_ = f.conn.SetWriteDeadline(time.Now().Add(Dial))
	if binding != nil {
		err = writeBoundRecord(f.conn, "key", *binding, wire)
	} else {
		_, err = f.conn.Write(wire)
	}
	if err != nil {
		f.closeLocked()
		f.seq--
		f.nextDial = now.Add(Retry)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if binding != nil {
		copy := *binding
		f.binding = &copy
	}
	f.nextDial = time.Time{}
	return nil
}

func (f *Feed) dialTimeout() (net.Conn, error) {
	if f.dial != nil {
		return f.dial("unix", f.path, Dial)
	}
	return net.DialTimeout("unix", f.path, Dial)
}

// Frame is the raw event the hub already produced. mister-agent applies
// playhid.StreamEvent from the runtime observation, so the caller keeps the
// player index and does not reshape keyboard frames here.
func Frame(seq uint32, e remoteinput.Event, now time.Time) protocol.InputFrame {
	if now.IsZero() {
		now = time.Now()
	}
	return protocol.InputFrame{
		Header:       protocol.InputHeader{Type: protocol.InputTypeInput, Session: Session},
		Seq:          seq,
		ClientMonoNS: uint64(now.UnixNano()),
		Player:       e.Player,
		Device:       uint8(e.Device),
		Kind:         uint8(e.Kind),
		Action:       uint8(e.Action),
		Code:         uint16(e.Code),
		Value:        e.Value,
	}
}
