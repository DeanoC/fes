package localfeed

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

var ErrV2TransportClosed = errors.New("local input v2 transport closed")

// Done closes when this exact transport begins teardown, including idle EOF.
// A caller must still correlate the notification to its current source.
func (t *V2Transport) Done() <-chan struct{} { return t.closed }

type v2Result struct {
	reply localhid.Reply
	err   error
}

// V2Transport owns one connection, one response reader and one write lane.
// Constructing it does not authorize a source or prove a physical snapshot.
type V2Transport struct {
	conn             net.Conn
	timeout          time.Duration
	writes           chan struct{}
	control          chan struct{}
	done             chan struct{}
	closed           chan struct{}
	once             sync.Once
	mu               sync.Mutex
	tracker          localhid.ReplyTracker
	pending          chan v2Result
	ping             *protocol.InputFrame
	pingTimer        *time.Timer
	pingSession      uint64
	lastPingSeq      uint32
	recoveryPromoted bool
	recoveryReply    *localhid.Reply
}

// NewV2RecoveryTransport begins restricted to correlated recovery requests.
// It permits no raw input until an explicit, one-way promotion on this same
// connection after a correlated positive recovery reply.
func NewV2RecoveryTransport(conn net.Conn, maxControlDuration time.Duration) (*V2Transport, error) {
	if conn == nil || maxControlDuration <= 0 {
		return nil, errors.New("invalid v2 transport configuration")
	}
	t := &V2Transport{conn: conn, timeout: maxControlDuration, writes: make(chan struct{}, 1), control: make(chan struct{}, 1), done: make(chan struct{}), closed: make(chan struct{})}
	go t.readLoop()
	return t, nil
}

func (t *V2Transport) breakTransport() {
	t.once.Do(func() {
		// Publish closure before waiting for any tracker or timer lock, so
		// recovery promotion cannot miss an already-started teardown.
		close(t.closed)
		t.tracker.Disconnect()
		t.mu.Lock()
		if t.pingTimer != nil {
			t.pingTimer.Stop()
		}
		t.mu.Unlock()
		_ = t.conn.Close()
	})
}

func (t *V2Transport) readLoop() {
	defer close(t.done)
	defer t.breakTransport()
	for {
		incoming, err := localhid.ReadIncoming(t.conn)
		if err != nil {
			return
		}
		if incoming.Pong != nil {
			t.mu.Lock()
			ping := t.ping
			pong := incoming.Pong
			valid := ping != nil && pong.Header.Session == ping.Header.Session && pong.Seq == ping.Seq && pong.ClientMonoNS == ping.ClientMonoNS
			if valid {
				t.ping = nil
				if t.pingTimer != nil {
					t.pingTimer.Stop()
					t.pingTimer = nil
				}
			}
			t.mu.Unlock()
			if !valid {
				return
			}
			continue
		}
		if incoming.Reply == nil {
			return
		}
		t.mu.Lock()
		pending := t.pending
		acceptErr := t.tracker.Accept(*incoming.Reply)
		if !t.recoveryPromoted && acceptErr == nil && incoming.Reply.Op == "recover" {
			copy := *incoming.Reply
			t.recoveryReply = &copy
		}
		if pending != nil {
			t.pending = nil
		}
		t.mu.Unlock()
		// Deliver only after releasing the state mutex: a caller can then
		// immediately perform nonblocking promotion without racing this reader.
		if pending != nil {
			pending <- v2Result{reply: *incoming.Reply, err: acceptErr}
		}
		if pending == nil || (acceptErr != nil && !(errors.Is(acceptErr, localhid.ErrQuarantined) && incoming.Reply.Op == "edge" && incoming.Reply.Status == "quarantined")) {
			return
		}
	}
}

// Request permits one outstanding control operation; a correlated edge
// quarantine permits only a same-token reset through the tracker.
func (t *V2Transport) Request(ctx context.Context, req localhid.Request) (localhid.Reply, error) {
	if ctx == nil {
		return localhid.Reply{}, errors.New("nil control context")
	}
	select {
	case t.control <- struct{}{}:
		defer func() { <-t.control }()
	default:
		return localhid.Reply{}, localhid.ErrPending
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	wait := make(chan v2Result, 1)
	t.mu.Lock()
	select {
	case <-t.closed:
		t.mu.Unlock()
		return localhid.Reply{}, ErrV2TransportClosed
	default:
	}
	if (req.Op == "recover") != !t.recoveryPromoted {
		t.mu.Unlock()
		return localhid.Reply{}, errors.New("invalid control for v2 transport mode")
	}
	var id uint64
	var err error
	if req.Op == "recover" {
		id, err = t.tracker.StartRecovery(req.AgentInstance, req.TargetID, req.BootID)
	} else {
		id, err = t.tracker.Start(req.Op, req.Token)
	}
	if err != nil {
		t.mu.Unlock()
		return localhid.Reply{}, err
	}
	t.pending = wait
	t.mu.Unlock()
	req.Version, req.Request = 2, id
	wire, err := localhid.EncodeRequest(req)
	if err != nil {
		t.breakTransport()
		return localhid.Reply{}, err
	}
	if err = t.write(ctx, wire, nil); err != nil {
		t.breakTransport()
		return localhid.Reply{}, err
	}
	select {
	case result := <-wait:
		return result.reply, result.err
	case <-ctx.Done():
		t.breakTransport()
		return localhid.Reply{}, ctx.Err()
	case <-t.closed:
		return localhid.Reply{}, ErrV2TransportClosed
	}
}

// WriteRaw allows existing pad/mouse frames and plain PINGs only. It cannot
// bypass the source ledger with an unacknowledged raw keyboard frame.
func (t *V2Transport) WriteRaw(ctx context.Context, wire []byte) error {
	t.mu.Lock()
	fenced := !t.recoveryPromoted
	t.mu.Unlock()
	if fenced {
		return errors.New("raw input unavailable on recovery transport")
	}
	if ctx == nil || len(wire) != 52 {
		return errors.New("invalid raw input frame")
	}
	f, err := protocol.DecodeInputFrame(bytes.NewReader(wire), 48)
	if err != nil {
		return err
	}
	var ping *protocol.InputFrame
	switch f.Header.Type {
	case protocol.InputTypePing:
		if f.Header.Flags != 0 || f.Seq == 0 || f.ClientMonoNS == 0 {
			return errors.New("invalid raw ping")
		}
		ping = &f
	case protocol.InputTypeInput:
		gamepad := f.Device == uint8(remoteinput.DeviceGamepad) &&
			((f.Kind == uint8(remoteinput.KindButton) && (f.Action == uint8(remoteinput.ActionPress) || f.Action == uint8(remoteinput.ActionRelease))) ||
				(f.Kind == uint8(remoteinput.KindAxis) && f.Action == uint8(remoteinput.ActionAbsolute)))
		mouse := f.Device == uint8(remoteinput.DeviceMouse) && f.Kind == uint8(remoteinput.KindRelative) && f.Action == uint8(remoteinput.ActionRelative)
		if !gamepad && !mouse {
			return errors.New("unsupported raw input; keyboard bypass is not permitted")
		}
	default:
		return errors.New("unsupported raw input frame")
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	return t.write(ctx, wire, ping)
}

// PromoteRecovered retains this connection's reader-slot proof. The caller
// must separately validate that the target's recovery reply proves the old
// source cleanup required by its use, and supply a fresh physical snapshot
// to a later BEGIN. No raw keyboard frame is ever allowed by this transport.
// This method is not used by a production path until that target guarantee is
// reviewed. Promotion is one-way and cannot be repeated on another socket.
func (t *V2Transport) PromoteRecovered(reply localhid.Reply) error {
	if !t.mu.TryLock() {
		return errors.New("recovery transport busy")
	}
	defer t.mu.Unlock()
	select {
	case <-t.closed:
		return ErrV2TransportClosed
	default:
	}
	if t.recoveryPromoted || t.recoveryReply == nil || *t.recoveryReply != reply || t.pending != nil {
		return errors.New("unconfirmed recovery promotion")
	}
	t.recoveryPromoted = true
	t.recoveryReply = nil
	return nil
}

func (t *V2Transport) write(ctx context.Context, wire []byte, ping *protocol.InputFrame) error {
	select {
	case t.writes <- struct{}{}:
		defer func() { <-t.writes }()
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		return ErrV2TransportClosed
	}
	t.mu.Lock()
	select {
	case <-t.closed:
		t.mu.Unlock()
		return ErrV2TransportClosed
	default:
	}
	if ping != nil {
		if t.ping != nil {
			t.mu.Unlock()
			return localhid.ErrPending
		}
		if (t.pingSession != 0 && ping.Header.Session != t.pingSession) || ping.Seq <= t.lastPingSeq {
			t.mu.Unlock()
			t.breakTransport()
			return errors.New("invalid ping sequence")
		}
		copy := *ping
		t.ping = &copy
		t.pingSession, t.lastPingSeq = ping.Header.Session, ping.Seq
		t.pingTimer = time.AfterFunc(t.timeout, t.breakTransport)
	}
	t.mu.Unlock()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.breakTransport()
		return errors.New("write deadline missing")
	}
	if err := t.conn.SetWriteDeadline(deadline); err != nil {
		t.breakTransport()
		return err
	}
	stop := context.AfterFunc(ctx, t.breakTransport)
	n, err := t.conn.Write(wire)
	stopped := stop()
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	if err == nil && (!stopped || ctx.Err() != nil) {
		err = ctx.Err()
		if err == nil {
			err = ErrV2TransportClosed
		}
	}
	if err == nil {
		err = t.conn.SetWriteDeadline(time.Time{})
	}
	if err != nil {
		t.breakTransport()
	}
	return err
}

func (t *V2Transport) Close() error {
	t.breakTransport()
	timer := time.NewTimer(t.timeout)
	defer timer.Stop()
	select {
	case <-t.done:
		return nil
	case <-timer.C:
		return errors.New("local input v2 reader did not stop")
	}
}
