package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func (r *RemoteInput) connectLocked(ctx context.Context) error {
	if r.bridge == nil {
		return ErrRemoteInputInvalid
	}
	if dialer, ok := r.bridge.(BridgeDialer); ok {
		conn, err := dialer.Dial(ctx)
		if err != nil {
			return streamDialError(err)
		}
		if err := r.handshakeLocked(conn); err != nil {
			_ = conn.Close()
			return ErrRemoteInputInvalid
		}
		r.conn = conn
		r.state = RemoteInputAttached
		r.ready = true
		return nil
	}
	endpoint := r.bridge.Endpoint()
	if len(endpoint) == 0 || len(endpoint) > maxBridgeAddressBytes {
		return ErrRemoteInputInvalid
	}
	dialer := net.Dialer{Timeout: r.dial}
	dialCtx, cancel := context.WithTimeout(ctx, r.dial)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", endpoint)
	if err != nil {
		return streamDialError(err)
	}
	if err := r.handshakeLocked(conn); err != nil {
		_ = conn.Close()
		return ErrRemoteInputInvalid
	}
	r.conn = conn
	r.state = RemoteInputAttached
	r.ready = true
	return nil
}

func (r *RemoteInput) handshakeLocked(conn net.Conn) error {
	if conn == nil {
		return ErrRemoteInputInvalid
	}
	if err := conn.SetWriteDeadline(r.now().Add(r.dial)); err != nil {
		return err
	}
	hello := struct {
		Version uint8  `json:"version"`
		Session uint64 `json:"session"`
		Core    string `json:"core"`
		Proof   string `json:"proof"`
	}{Version: protocol.InputVersion, Session: r.session, Core: r.core, Proof: hex.EncodeToString(r.token)}
	encoded, err := json.Marshal(hello)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := conn.Write(encoded); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(r.now().Add(r.dial)); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(conn, maxHandshakeBytes)
	line, err := readBoundedLine(reader, maxHandshakeBytes)
	if err != nil {
		return err
	}
	var welcome struct {
		OK bool `json:"ok"`
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&welcome); err != nil || !welcome.OK {
		return ErrRemoteInputInvalid
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	r.reader = reader
	return nil
}

func (r *RemoteInput) ensureConnectionLocked(ctx context.Context) error {
	if r.conn != nil {
		return nil
	}
	if r.state != RemoteInputReconnecting {
		r.state = RemoteInputReconnecting
		r.ready = false
	}
	return r.reconnectLocked(ctx)
}

func (r *RemoteInput) reconnectLocked(ctx context.Context) error {
	deadline := r.now().Add(r.grace)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.connectLocked(ctx); err == nil {
			if err := r.replayStateLocked(ctx); err != nil {
				r.closeConnLocked()
				lastErr = err
			} else {
				r.metrics.stateResyncs++
				r.startHeartbeatLocked()
				return nil
			}
		} else {
			lastErr = err
		}
		// A refused grant cannot recover by redialing the old input handle.
		// Release the input lock promptly so read-only ownership reconciliation
		// can invalidate the stale session instead of exhausting its deadline.
		var leaseErr *protocol.APIError
		if errors.As(lastErr, &leaseErr) && (leaseErr.Code == protocol.CodeKitLeaseDenied || leaseErr.Code == protocol.CodeKitLeaseBusy) {
			return lastErr
		}
		if r.now().After(deadline) {
			if lastErr == nil {
				lastErr = ErrRemoteInputInvalid
			}
			return lastErr
		}
		timer := time.NewTimer(minDuration(10*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *RemoteInput) replayStateLocked(ctx context.Context) error {
	if r.colecoInputEnabledLocked() {
		desired := r.colecoDesiredStateLocked()
		if err := r.writeColecoSnapshotLocked(ctx, desired); err != nil {
			return err
		}
		r.colecoOutput = desired
		return nil
	}
	for player := uint8(0); player < 2; player++ {
		snapshot := r.inputState.SnapshotForPlayer(player)
		if snapshot.Mouse && r.mouse {
			event := remoteinput.MouseEvent(0, 0, snapshot.MouseButtons)
			if err := r.writeFrameLocked(ctx, r.frameForEventLocked(event, r.now()), r.now()); err != nil {
				return err
			}
		}
		pressed := append([]remoteinput.Code(nil), snapshot.Pressed...)
		sort.Slice(pressed, func(i, j int) bool { return pressed[i] < pressed[j] })
		for _, code := range pressed {
			event := eventForCode(code, remoteinput.ActionPress)
			event.Player = player
			if err := r.writeFrameLocked(ctx, r.frameForEventLocked(event, r.now()), r.now()); err != nil {
				return err
			}
		}
		codes := make([]remoteinput.Code, 0, len(snapshot.Axes))
		for code := range snapshot.Axes {
			codes = append(codes, code)
		}
		sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
		for _, code := range codes {
			event := eventForCode(code, remoteinput.ActionAbsolute)
			event.Player = player
			event.Value = int32(snapshot.Axes[code])
			if err := r.writeFrameLocked(ctx, r.frameForEventLocked(event, r.now()), r.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RemoteInput) writeFrameLocked(ctx context.Context, frame protocol.InputFrame, capturedAt time.Time) error {
	if r.conn == nil {
		return ErrRemoteInputInvalid
	}
	deadline := r.now().Add(r.dial)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := r.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	wire, err := protocol.EncodeInputFrame(frame)
	if err != nil {
		return err
	}
	if _, err := r.conn.Write(wire); err != nil {
		return err
	}
	r.metrics.framesSent++
	latency := r.now().Sub(capturedAt)
	if !capturedAt.IsZero() && latency >= 0 {
		r.metrics.captureLatencies = appendBounded(r.metrics.captureLatencies, float64(latency)/float64(time.Millisecond), maxLatencySamples)
	}
	return nil
}

func (r *RemoteInput) closeConnLocked() {
	r.receiptReady = false
	r.tapReady = false
	r.tapChordReady = false
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
	r.reader = nil
}

func (r *RemoteInput) readPongLocked() {
	if r.reader == nil || r.conn == nil {
		return
	}
	_ = r.conn.SetReadDeadline(r.now().Add(r.dial))
	frame, err := protocol.DecodeInputFrame(r.reader, 4096)
	if err == nil && frame.Header.Session == r.session && frame.Header.Type == protocol.InputTypePong && frame.ServerMonoNS != 0 && frame.ClientMonoNS != 0 {
		r.metrics.rttMS = float64(r.now().UnixNano()-int64(frame.ClientMonoNS)) / float64(time.Millisecond)
		if r.metrics.rttMS <= 0 {
			r.metrics.rttMS = 0.001
		}
	}
	_ = r.conn.SetReadDeadline(time.Time{})
}

// nextPingLocked gives each heartbeat or processing barrier a distinct echoed
// value even when the configured clock has not advanced.
func (r *RemoteInput) nextPingLocked() uint64 {
	nonce := uint64(r.now().UnixNano())
	if nonce <= r.lastPing {
		nonce = r.lastPing + 1
	}
	r.lastPing = nonce
	return nonce
}

func (r *RemoteInput) confirmSinkLocked(ctx context.Context) error {
	if r.conn == nil || r.reader == nil {
		return ErrRemoteInputNoStream
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	nonce := r.nextPingLocked()
	// PING is not tracked as an input frame by the bridge. Reuse the last
	// input sequence so the next event does not report a spurious gap.
	ping := protocol.InputFrame{
		Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagReceiptRequested, Session: r.session},
		Seq:    r.sequence, ClientMonoNS: nonce,
	}
	if err := r.writeFrameLocked(ctx, ping, time.Time{}); err != nil {
		return err
	}
	return r.waitReceiptLocked(ctx, ping.Seq, nonce, 0, r.dial)
}

// waitReceiptLocked consumes only the matching response; old heartbeat
// PONGs may precede it. The caller owns r.mu and must invalidate uncertain
// attachments after a mutating request.
func (r *RemoteInput) waitReceiptLocked(ctx context.Context, sequence uint32, nonce uint64, flags uint16, timeout time.Duration) error {
	if r.conn == nil || r.reader == nil {
		return ErrRemoteInputNoStream
	}
	conn := r.conn
	deadline := r.now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetReadDeadline(time.Now())
		close(callbackDone)
	})
	defer func() {
		if !stop() {
			<-callbackDone
		}
		_ = conn.SetReadDeadline(time.Time{})
	}()
	// A delayed heartbeat may already be buffered. Ignore only older PONGs
	// from this session and require the exact receipt for success.
	for stale := 0; stale < 8; stale++ {
		pong, err := protocol.DecodeInputFrame(r.reader, 4096)
		if err != nil {
			return err
		}
		if pong.Header.Session != r.session {
			return ErrRemoteInputUnconfirmed
		}
		if pong.Header.Type == protocol.InputTypeReceipt && pong.Seq == sequence && pong.ClientMonoNS == nonce {
			if (flags == protocol.InputFlagTapSupported || flags == protocol.InputFlagTapChordSupported) && pong.Header.Flags == 0 {
				return errTapUnsupported
			}
			if pong.Header.Flags != flags {
				return ErrRemoteInputUnconfirmed
			}
			return ctx.Err()
		}
		if pong.Header.Type != protocol.InputTypePong || pong.ClientMonoNS >= nonce {
			return ErrRemoteInputUnconfirmed
		}
	}
	return ErrRemoteInputUnconfirmed
}

func (r *RemoteInput) startHeartbeatLocked() {
	if r.heartbeatCancel != nil {
		r.heartbeatCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.heartbeatCancel = cancel
	go r.heartbeatLoop(ctx)
}

func (r *RemoteInput) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			if r.closed || r.state == RemoteInputDetached || r.state == RemoteInputFailed || r.bridge == nil || r.conn == nil {
				r.mu.Unlock()
				return
			}
			r.sequence++
			frame := protocol.InputFrame{
				Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: r.session},
				Seq:    r.sequence,
				// Echoed by the bridge in PONG so the host can report a real
				// round-trip sample instead of an always-zero metric.
				ClientMonoNS: r.nextPingLocked(),
			}
			if err := r.writeFrameLocked(context.Background(), frame, time.Time{}); err != nil {
				r.closeConnLocked()
				r.state = RemoteInputReconnecting
				r.ready = false
				if err := r.reconnectLocked(context.Background()); err != nil {
					if _, lost := r.ownershipReconnectFailureLocked(err); !lost {
						r.failLocked("reconnect_timeout")
					}
				}
			} else {
				r.readPongLocked()
			}
			r.mu.Unlock()
		}
	}
}

func (r *RemoteInput) stopBridgeLocked(ctx context.Context) {
	if r.heartbeatCancel != nil {
		r.heartbeatCancel()
		r.heartbeatCancel = nil
	}
	if r.bridge != nil {
		_ = r.bridge.Stop(ctx)
		r.bridge = nil
	}
	r.closeConnLocked()
}

func (r *RemoteInput) detachLocked(ctx context.Context, reason string) {
	r.closeConnLocked()
	r.stopBridgeLocked(ctx)
	r.inputState.ReleaseAll()
	r.colecoOutput = nil
	r.source = ""
	r.sourceGeneration++
	r.session = 0
	r.token = nil
	r.core = ""
	r.keyboard = false
	r.sequence = 0
	r.ready = false
	r.state = RemoteInputDetached
	r.metrics.releases++
	r.metrics.shutdownReason = reason
}

func (r *RemoteInput) failLocked(reason string) {
	r.stopBridgeLocked(context.Background())
	r.ready = false
	r.state = RemoteInputFailed
	r.metrics.shutdownReason = reason
}
