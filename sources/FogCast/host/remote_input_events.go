package host

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/joymatrix"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// SendEvent sends a normalized host event. capturedAt should be the local
// capture timestamp; it is used only for monotonic latency metrics.
func (r *RemoteInput) SendEvent(ctx context.Context, event remoteinput.Event, capturedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sendDesktopEventLocked(ctx, event, capturedAt, true)
}

// SendEventConfirmed waits for the bridge sink to accept the emitted frames.
// A valid mapping can emit no frames; a receipt does not prove guest consumption.
func (r *RemoteInput) SendEventConfirmed(ctx context.Context, event remoteinput.Event, capturedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.confirmEventLocked(ctx, r.sendDesktopEventLocked(ctx, event, capturedAt, false))
}

// confirmEventLocked closes the attachment on uncertain delivery so no held
// state or relative motion is replayed into a replacement connection.
func (r *RemoteInput) confirmEventLocked(ctx context.Context, err error) error {
	if err == nil {
		err = r.confirmSinkLocked(ctx)
		if err != nil {
			err = ErrRemoteInputUnconfirmed
		}
	}
	if errors.Is(err, ErrRemoteInputUnconfirmed) {
		// The event may already have reached the bridge. Close before stopping
		// the bridge so it releases its source; never replay ambiguous motion or
		// retained holds into a later attachment.
		r.closeConnLocked()
		r.inputState.ReleaseAll()
		r.colecoOutput = nil
		stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		r.stopBridgeLocked(stopCtx)
		cancel()
		r.state = RemoteInputFailed
		r.ready = false
		r.metrics.shutdownReason = "bridge_error"
	}
	return err
}

func (r *RemoteInput) sendDesktopEventLocked(ctx context.Context, event remoteinput.Event, capturedAt time.Time, reconnect bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.source != "" && r.source != "desktop" {
		return ErrRemoteInputBusy
	}
	if r.closed {
		return ErrRemoteInputClosed
	}
	if r.state != RemoteInputAttached && r.state != RemoteInputReconnecting {
		return ErrRemoteInputNoStream
	}
	newClaim := r.source == ""
	r.source = "desktop"
	err := r.sendEventModeLocked(ctx, event, capturedAt, reconnect)
	if err != nil && newClaim && !reconnect && !r.receiptReady {
		r.source = ""
	}
	return err
}

func (r *RemoteInput) sendEventModeLocked(ctx context.Context, event remoteinput.Event, capturedAt time.Time, reconnect bool) error {
	if event.Device == remoteinput.DeviceMouse {
		if _, _, _, ok := remoteinput.MouseVector(event); !ok || !r.mouse {
			return ErrRemoteInputInvalid
		}
	}
	if event.Player > 1 || (event.Player != 0 && !r.controllerPorts) ||
		(event.Code >= remoteinput.Keypad0 && event.Code <= remoteinput.KeypadHash && !r.keypadPorts) {
		return ErrRemoteInputInvalid
	}
	if r.closed {
		return ErrRemoteInputClosed
	}
	if r.state != RemoteInputAttached && r.state != RemoteInputReconnecting {
		return ErrRemoteInputNoStream
	}
	// Reconnection and state sync here precede the new event. Once that event
	// is sent, the confirmed path must not reconnect or replay it.
	if err := r.ensureLiveStreamLocked(ctx); err != nil {
		return err
	}
	if !reconnect && !r.receiptReady {
		// Probe the bridge before mutating local state or emitting this event.
		// Older bridges close on the unknown receipt request. Ordinary attach
		// remains usable, but confirmed input fails closed before dispatch.
		if err := r.confirmSinkLocked(ctx); err != nil {
			r.closeConnLocked()
			r.state = RemoteInputReconnecting
			r.ready = false
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrRemoteInputNoStream
		}
		r.receiptReady = true
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.inputState.Apply(event); err != nil {
		return ErrRemoteInputInvalid
	}
	if err := r.ensureConnectionLocked(ctx); err != nil {
		if refusal, lost := r.ownershipReconnectFailureLocked(err); lost {
			return refusal
		}
		return streamUnavailable(err)
	}
	var sendErr error
	if r.colecoInputEnabledLocked() {
		sendErr = r.sendColecoDiffLocked(ctx, capturedAt)
	} else {
		frame := r.frameForEventLocked(event, capturedAt)
		sendErr = r.writeFrameLocked(ctx, frame, capturedAt)
	}
	if sendErr == nil {
		r.state = RemoteInputAttached
		r.ready = true
		return nil
	}
	if !reconnect {
		return ErrRemoteInputUnconfirmed
	}

	// A TCP write can fail after the bridge has applied the event. Keep the
	// authoritative local state, reconnect using the same identity, and replay
	// it as a snapshot. This is idempotent and avoids stuck controls.
	r.closeConnLocked()
	r.state = RemoteInputReconnecting
	r.ready = false
	if err := r.reconnectLocked(ctx); err != nil {
		if refusal, lost := r.ownershipReconnectFailureLocked(err); lost {
			return refusal
		}
		r.failLocked("reconnect_timeout")
		return streamUnavailable(err)
	}
	if event.Device == remoteinput.DeviceMouse {
		// The original write may have applied. Reconnect only restored buttons;
		// report its uncertainty without repeating motion.
		return streamUnavailable(sendErr)
	}
	return nil
}

// TapKey asks a capable target to apply both edges in its own input session.
// The returned duration is host processing time, not guest-visible timing.
func (r *RemoteInput) TapKey(ctx context.Context, sessionID string, generation uint64, event remoteinput.Event, checkOwnership func() error) (time.Duration, error) {
	return r.tapKey(ctx, sessionID, generation, event, 0, checkOwnership)
}

// TapKeyChord applies a non-modifier key together with a HID modifier mask.
func (r *RemoteInput) TapKeyChord(ctx context.Context, sessionID string, generation uint64, event remoteinput.Event, modifiers uint8, checkOwnership func() error) (time.Duration, error) {
	usage, ok := hidkeys.Usage(event.Code)
	if modifiers == 0 || !ok || usage >= 0xe0 {
		return 0, ErrRemoteInputInvalid
	}
	return r.tapKey(ctx, sessionID, generation, event, modifiers, checkOwnership)
}

func (r *RemoteInput) tapKey(ctx context.Context, sessionID string, generation uint64, event remoteinput.Event, modifiers uint8, checkOwnership func() error) (time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	_, validUsage := hidkeys.Usage(event.Code)
	if event.Player != 0 || event.Device != remoteinput.DeviceKeyboard || event.Kind != remoteinput.KindKey ||
		event.Action != remoteinput.ActionPress || event.Value != 0 || !validUsage {
		return 0, ErrRemoteInputInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.closed {
		return 0, ErrRemoteInputClosed
	}
	if sessionID == "" || sessionID != r.sessionIDLocked() || generation == 0 {
		return 0, ErrRemoteInputInvalid
	}
	if r.source != "" && r.source != "desktop" {
		return 0, ErrRemoteInputBusy
	}
	if r.inputState.Pressed(event.Code) {
		return 0, ErrRemoteInputBusy
	}
	if checkOwnership == nil {
		return 0, ErrRemoteInputInvalid
	}
	if err := checkOwnership(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Do not reconnect or replay during a tap: an uncertain press must never
	// be replayed as held while a reconnect handshake stalls.
	if r.state != RemoteInputAttached || !r.connectionLiveLocked() {
		return 0, ErrRemoteInputNoStream
	}
	started := time.Now()
	failTap := func(err error) (time.Duration, error) {
		// Disconnect first: the bridge releases its held controls. Clear the
		// local state so no held key can be replayed into another attachment.
		r.closeConnLocked()
		r.inputState.ReleaseAll()
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 100*time.Millisecond)
		r.stopBridgeLocked(stopCtx)
		cancelStop()
		r.ready = false
		r.state = RemoteInputFailed
		return time.Since(started), err
	}
	// A failed probe or unconfirmed tap closes this remote source. Require it
	// to be neutral so that cleanup cannot release another remote hold. Holds
	// from kit-local sources are maintained separately by the target.
	for player := uint8(0); player < 2; player++ {
		snapshot := r.inputState.SnapshotForPlayer(player)
		if len(snapshot.Pressed) != 0 || snapshot.MouseButtons != 0 {
			return time.Since(started), ErrRemoteInputBusy
		}
		for _, value := range snapshot.Axes {
			if value != 0 {
				return time.Since(started), ErrRemoteInputBusy
			}
		}
	}
	for _, pressed := range r.colecoOutput {
		if pressed {
			return time.Since(started), ErrRemoteInputBusy
		}
	}
	ready, probeFlag, supportedFlag := r.tapReady, uint16(protocol.InputFlagTapProbe), uint16(protocol.InputFlagTapSupported)
	if modifiers != 0 {
		ready, probeFlag, supportedFlag = r.tapChordReady, protocol.InputFlagTapChordProbe, protocol.InputFlagTapChordSupported
	}
	if !ready {
		// Optional capability probing is deferred until this explicit tap.
		// An older bridge closes on the probe before input is dispatched.
		nonce := r.nextPingLocked()
		probe := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: probeFlag, Session: r.session}, Seq: r.sequence, ClientMonoNS: nonce}
		probeFailed := func(err error) (time.Duration, error) {
			// This probe carried no input. A legacy bridge may close on the
			// new flag; permit later ordinary input to reconnect, but never
			// replay holds which that disconnect may already have released.
			r.closeConnLocked()
			r.inputState.ReleaseAll()
			r.colecoOutput = nil
			r.source = ""
			r.sourceGeneration++
			r.state = RemoteInputReconnecting
			r.ready = false
			if ctx.Err() != nil {
				return time.Since(started), ctx.Err()
			}
			return time.Since(started), err
		}
		if err := r.writeFrameLocked(ctx, probe, time.Time{}); err != nil {
			return probeFailed(ErrRemoteInputNoStream)
		}
		if err := r.waitReceiptLocked(ctx, probe.Seq, nonce, supportedFlag, r.dial); err != nil {
			if errors.Is(err, errTapUnsupported) {
				return time.Since(started), ErrRemoteInputNoStream
			}
			return probeFailed(ErrRemoteInputNoStream)
		}
		if modifiers == 0 {
			r.tapReady = true
		} else {
			r.tapChordReady = true
		}
	}
	// The probe can block; recheck ownership before the first mutating frame.
	if err := checkOwnership(); err != nil {
		return time.Since(started), err
	}
	if err := ctx.Err(); err != nil {
		return time.Since(started), err
	}
	r.source = "desktop"
	frame := r.frameForEventLocked(event, time.Now())
	frame.Header.Type = protocol.InputTypeTap
	if modifiers != 0 {
		frame.Header.Type = protocol.InputTypeTapChord
		frame.Reserved = uint16(modifiers)
	}
	frame.ServerMonoNS = generation
	frame.Value = protocol.InputTapHoldMS
	if err := r.writeFrameLocked(ctx, frame, time.Time{}); err != nil {
		return failTap(ErrRemoteInputUnconfirmed)
	}
	// Wait for target sink completion, not just the TCP write. On a lost or
	// canceled receipt the tap may have applied: disconnect, never replay.
	if err := r.waitReceiptLocked(ctx, frame.Seq, frame.ClientMonoNS, 0, 3*time.Second); err != nil {
		return failTap(ErrRemoteInputUnconfirmed)
	}
	return time.Since(started), nil
}

// Send is a convenience form for callers that do not have a separate capture
// timestamp.
func (r *RemoteInput) Send(ctx context.Context, event remoteinput.Event) error {
	return r.SendEvent(ctx, event, r.now())
}

func (r *RemoteInput) colecoInputEnabledLocked() bool {
	return joymatrix.Supports(r.core) && r.keyboard
}

func (r *RemoteInput) colecoDesiredStateLocked() map[remoteinput.Code]bool {
	snapshot := r.inputState.Snapshot()
	desired := joymatrix.Desired(r.core, snapshot)
	for _, code := range snapshot.Pressed {
		if _, ok := joymatrix.ButtonKey(code); !ok || ((r.core == "fes.sms" || r.core == "fes.sg1000") && code == remoteinput.ButtonB) {
			desired[code] = true
		}
	}
	return desired
}

func (r *RemoteInput) sendColecoDiffLocked(ctx context.Context, capturedAt time.Time) error {
	desired := r.colecoDesiredStateLocked()
	removed := make([]remoteinput.Code, 0)
	for code := range r.colecoOutput {
		if !desired[code] {
			removed = append(removed, code)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	for _, code := range removed {
		if err := r.writeColecoTransitionLocked(ctx, capturedAt, code, remoteinput.ActionRelease); err != nil {
			return err
		}
	}

	added := make([]remoteinput.Code, 0)
	for code := range desired {
		if !r.colecoOutput[code] {
			added = append(added, code)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	for _, code := range added {
		if err := r.writeColecoTransitionLocked(ctx, capturedAt, code, remoteinput.ActionPress); err != nil {
			return err
		}
	}
	r.colecoOutput = desired
	return nil
}

func (r *RemoteInput) writeColecoSnapshotLocked(ctx context.Context, desired map[remoteinput.Code]bool) error {
	codes := make([]remoteinput.Code, 0, len(desired))
	for code := range desired {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	for _, code := range codes {
		if err := r.writeColecoTransitionLocked(ctx, time.Time{}, code, remoteinput.ActionPress); err != nil {
			return err
		}
	}
	return nil
}

func (r *RemoteInput) writeColecoTransitionLocked(ctx context.Context, capturedAt time.Time, code remoteinput.Code, action remoteinput.Action) error {
	event := eventForCode(code, action)
	return r.writeFrameLocked(ctx, r.frameForEventLocked(event, capturedAt), capturedAt)
}

func (r *RemoteInput) frameForEventLocked(event remoteinput.Event, capturedAt time.Time) protocol.InputFrame {
	if capturedAt.IsZero() {
		capturedAt = r.now()
	}
	r.sequence++
	return protocol.InputFrame{
		Header:       protocol.InputHeader{Type: protocol.InputTypeInput, Session: r.session},
		Seq:          r.sequence,
		ClientMonoNS: uint64(capturedAt.UnixNano()),
		Player:       event.Player,
		Device:       uint8(event.Device),
		Kind:         uint8(event.Kind),
		Action:       uint8(event.Action),
		Code:         uint16(event.Code),
		Value:        event.Value,
	}
}
