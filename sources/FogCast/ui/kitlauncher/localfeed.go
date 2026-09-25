package kitlauncher

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// localInputUnavailableMessage is the footer when play input cannot reach the
// kit-local socket. It is not a host error and it is not cleared by reconnect.
const localInputUnavailableMessage = "Local input unavailable"

// errLocalInputUnavailable means the agent is not listening. Callers must not
// post the same pad to the host.
var errLocalInputUnavailable = errors.New("local input socket is not listening")

// localInputSession satisfies the input-frame encoding, which rejects session
// 0. The kit-local socket does not consult a lease or this value.
const localInputSession uint64 = 1

const localInputDial = 250 * time.Millisecond

// localInputRetry is how long a missing agent socket stays dark. One pad poll
// emits several events, and each failed dial can spend the full dial budget.
// Further sends wait until this elapses, then try once.
const localInputRetry = time.Second

// localFeed is one connection to mister-agent's kit-local input socket.
// Closing it releases only that local source.
type localFeed struct {
	path     string
	conn     net.Conn
	seq      uint32
	nextDial time.Time
	dial     func(network, address string, timeout time.Duration) (net.Conn, error)
}

func newLocalFeed(path string, dial func(string, string, time.Duration) (net.Conn, error)) *localFeed {
	return &localFeed{path: path, dial: dial}
}

func (c *Client) localInputSocket() string {
	if c == nil {
		return ""
	}
	return c.localInputPath
}

func (f *localFeed) Close() {
	if f == nil || f.conn == nil {
		return
	}
	_ = f.conn.Close()
	f.conn = nil
}

func (f *localFeed) send(e remoteinput.Event, now time.Time) error {
	if f == nil || f.path == "" {
		return errLocalInputUnavailable
	}
	if now.IsZero() {
		now = time.Now()
	}
	// A down socket must not dial again until the cooldown. Callers also skip
	// send entirely during that window; this is the backstop if they do not.
	if f.conn == nil && !f.nextDial.IsZero() && now.Before(f.nextDial) {
		return errLocalInputUnavailable
	}
	f.seq++
	frame := localInputFrame(f.seq, e, now)
	wire, err := protocol.EncodeInputFrame(frame)
	if err != nil {
		f.seq--
		return err
	}
	if f.conn == nil {
		conn, err := f.dialTimeout()
		if err != nil {
			f.seq--
			f.nextDial = now.Add(localInputRetry)
			return fmt.Errorf("%w: %v", errLocalInputUnavailable, err)
		}
		f.conn = conn
	}
	_ = f.conn.SetWriteDeadline(time.Now().Add(localInputDial))
	if _, err := f.conn.Write(wire); err != nil {
		f.Close()
		f.seq--
		f.nextDial = now.Add(localInputRetry)
		return fmt.Errorf("%w: %v", errLocalInputUnavailable, err)
	}
	f.nextDial = time.Time{}
	return nil
}

func (f *localFeed) dialTimeout() (net.Conn, error) {
	if f.dial != nil {
		return f.dial("unix", f.path, localInputDial)
	}
	return net.DialTimeout("unix", f.path, localInputDial)
}

// localInputFrame is the raw event the hub already produced. mister-agent
// applies playhid.StreamEvent from the runtime observation, so the kit keeps
// the player index and does not reshape keyboard frames here.
func localInputFrame(seq uint32, e remoteinput.Event, now time.Time) protocol.InputFrame {
	if now.IsZero() {
		now = time.Now()
	}
	return protocol.InputFrame{
		Header:       protocol.InputHeader{Type: protocol.InputTypeInput, Session: localInputSession},
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
