// Package localfeed writes one pad or keyboard stream to mister-agent's
// kit-local input socket. The kit grid and tenfoot share it so a core sees
// one frame encoding. A missing socket stays dark for a second between dials.
package localfeed

import (
	"errors"
	"fmt"
	"net"
	"time"

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
	path     string
	conn     net.Conn
	seq      uint32
	nextDial time.Time
	dial     func(network, address string, timeout time.Duration) (net.Conn, error)
}

// New dials path on the first send. An empty path fails each send with
// ErrUnavailable and does not dial. dial may be nil; the default is
// net.DialTimeout.
func New(path string, dial func(string, string, time.Duration) (net.Conn, error)) *Feed {
	return &Feed{path: path, dial: dial}
}

// Close releases the current connection. A later Send dials again.
func (f *Feed) Close() {
	if f == nil || f.conn == nil {
		return
	}
	_ = f.conn.Close()
	f.conn = nil
}

// Send writes one raw event. A down socket is not dialed again until Retry
// has elapsed. now is the frame timestamp; a zero time is time.Now.
func (f *Feed) Send(e remoteinput.Event, now time.Time) error {
	if f == nil || f.path == "" {
		return ErrUnavailable
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
	_ = f.conn.SetWriteDeadline(time.Now().Add(Dial))
	if _, err := f.conn.Write(wire); err != nil {
		f.Close()
		f.seq--
		f.nextDial = now.Add(Retry)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
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
