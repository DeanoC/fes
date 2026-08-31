package host

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	defaultReconnectGrace    = 3 * time.Second
	defaultHeartbeatInterval = 250 * time.Millisecond
	defaultDialTimeout       = 2 * time.Second
	maxBridgeAddressBytes    = 256
	maxHandshakeBytes        = 4096
	maxLatencySamples        = 256
)

var (
	ErrRemoteInputClosed  = errors.New("remote input is closed")
	ErrRemoteInputBusy    = errors.New("remote input is already attached")
	ErrRemoteInputInvalid = errors.New("remote input configuration is invalid")
)

// BridgeSpec is the private identity handed to a per-session bridge. It is
// intentionally not serializable as part of the host API response.
type BridgeSpec struct {
	Session uint64
	Token   []byte
	Core    string
}

// BridgeHandle represents a bridge process owned by one remote-input session.
// Endpoint is consumed only by the host-side client; it is never exposed by
// RemoteInputStatus.
type BridgeHandle interface {
	Ready() <-chan struct{}
	Endpoint() string
	Stop(context.Context) error
}

type bridgeReadyError interface {
	ReadyError() error
}

// BridgeStarter starts and supervises a bridge for exactly one session.
type BridgeStarter interface {
	Start(context.Context, BridgeSpec) (BridgeHandle, error)
}

// BridgeStarterFunc adapts a function to BridgeStarter.
type BridgeStarterFunc func(context.Context, BridgeSpec) (BridgeHandle, error)

func (f BridgeStarterFunc) Start(ctx context.Context, spec BridgeSpec) (BridgeHandle, error) {
	return f(ctx, spec)
}

// RemoteInputController is the narrow lifecycle surface used by host sessions.
type RemoteInputController interface {
	Attach(context.Context, string) error
	Detach(context.Context, string) error
	Status() RemoteInputStatus
}

type RemoteInputState string

const (
	RemoteInputDetached     RemoteInputState = "detached"
	RemoteInputStarting     RemoteInputState = "starting"
	RemoteInputAttached     RemoteInputState = "attached"
	RemoteInputReconnecting RemoteInputState = "reconnecting"
	RemoteInputFailed       RemoteInputState = "failed"
)

// RemoteInputMetrics contains privacy-safe transport and delivery counters.
// Bridge-to-uinput is zero until a bridge reports that stage through a future
// metrics channel; this client cannot infer kernel delivery from a TCP write.
type RemoteInputMetrics struct {
	FramesSent               uint64  `json:"frames_sent"`
	StateResyncs             uint64  `json:"state_resyncs"`
	SequenceGaps             uint64  `json:"sequence_gaps"`
	Releases                 uint64  `json:"releases"`
	CaptureToBridgeP95MS     float64 `json:"capture_to_bridge_p95_ms"`
	BridgeToUInputP95MS      float64 `json:"bridge_to_uinput_p95_ms"`
	RTTMS                    float64 `json:"rtt_ms"`
	BridgeToUInputMeasurable bool    `json:"bridge_to_uinput_measurable"`
	ShutdownReason           string  `json:"shutdown_reason,omitempty"`
}

type RemoteInputStatus struct {
	State   RemoteInputState   `json:"state"`
	Ready   bool               `json:"ready"`
	Metrics RemoteInputMetrics `json:"metrics"`
}

type RemoteInputConfig struct {
	Starter           BridgeStarter
	ReconnectGrace    time.Duration
	HeartbeatInterval time.Duration
	DialTimeout       time.Duration
	Random            io.Reader
	Now               func() time.Time
}

type RemoteInput struct {
	mu        sync.Mutex
	starter   BridgeStarter
	grace     time.Duration
	heartbeat time.Duration
	dial      time.Duration
	random    io.Reader
	now       func() time.Time

	state           RemoteInputState
	ready           bool
	closed          bool
	bridge          BridgeHandle
	conn            net.Conn
	reader          *bufio.Reader
	heartbeatCancel context.CancelFunc
	session         uint64
	token           []byte
	core            string
	sequence        uint32
	inputState      remoteinput.State
	metrics         remoteInputCounters
}

type remoteInputCounters struct {
	framesSent              uint64
	stateResyncs            uint64
	sequenceGaps            uint64
	releases                uint64
	rttMS                   float64
	captureLatencies        []float64
	captureToBridgeP95      float64
	bridgeToUInputP95       float64
	bridgeMetricsMeasurable bool
	shutdownReason          string
}

// NewRemoteInput constructs a host-side, per-session bridge client. A starter
// is required so bridge process ownership stays explicit at the application
// boundary instead of silently spawning an unrelated process.
func NewRemoteInput(config RemoteInputConfig) (*RemoteInput, error) {
	if config.Starter == nil {
		return nil, ErrRemoteInputInvalid
	}
	if config.ReconnectGrace <= 0 {
		config.ReconnectGrace = defaultReconnectGrace
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = defaultHeartbeatInterval
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultDialTimeout
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &RemoteInput{
		starter:   config.Starter,
		grace:     config.ReconnectGrace,
		heartbeat: config.HeartbeatInterval,
		dial:      config.DialTimeout,
		random:    config.Random,
		now:       config.Now,
		state:     RemoteInputDetached,
	}, nil
}

func (r *RemoteInput) Status() RemoteInputStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

func (r *RemoteInput) Attach(ctx context.Context, core string) error {
	if err := validateBridgeCore(core); err != nil {
		return ErrRemoteInputInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRemoteInputClosed
	}
	if r.state != RemoteInputDetached && r.state != RemoteInputFailed {
		return ErrRemoteInputBusy
	}

	session, token, err := r.newIdentity()
	if err != nil {
		return ErrRemoteInputInvalid
	}
	r.state = RemoteInputStarting
	r.ready = false
	r.metrics = remoteInputCounters{}
	r.sequence = 0
	r.inputState = remoteinput.State{}
	r.session = session
	r.token = token
	r.core = core

	bridge, err := r.starter.Start(ctx, BridgeSpec{Session: session, Token: append([]byte(nil), token...), Core: core})
	if err != nil || bridge == nil {
		r.failLocked("attach_failed")
		return ErrRemoteInputInvalid
	}
	r.bridge = bridge
	if err := waitReady(ctx, bridge.Ready()); err != nil {
		r.stopBridgeLocked(context.Background())
		r.failLocked("attach_failed")
		return ErrRemoteInputInvalid
	}
	if ready, ok := bridge.(bridgeReadyError); ok && ready.ReadyError() != nil {
		r.stopBridgeLocked(context.Background())
		r.failLocked("attach_failed")
		return ErrRemoteInputInvalid
	}
	if err := r.connectLocked(ctx); err != nil {
		r.stopBridgeLocked(context.Background())
		r.failLocked("attach_failed")
		return ErrRemoteInputInvalid
	}
	r.state = RemoteInputAttached
	r.ready = true
	r.startHeartbeatLocked()
	return nil
}

// Detach closes the authenticated transport before stopping the bridge. The
// bridge's authenticated connection owner performs release-all on close.
func (r *RemoteInput) Detach(ctx context.Context, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRemoteInputClosed
	}
	if r.state == RemoteInputDetached {
		r.metrics.shutdownReason = safeShutdownReason(reason)
		return nil
	}
	r.detachLocked(ctx, safeShutdownReason(reason))
	return nil
}

func (r *RemoteInput) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.state != RemoteInputDetached {
		r.detachLocked(context.Background(), "close")
	}
	return nil
}

var _ RemoteInputController = (*RemoteInput)(nil)

// SendEvent sends a normalized host event. capturedAt should be the local
// capture timestamp; it is used only for monotonic latency metrics.
func (r *RemoteInput) SendEvent(ctx context.Context, event remoteinput.Event, capturedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRemoteInputClosed
	}
	if r.state != RemoteInputAttached && r.state != RemoteInputReconnecting {
		return ErrRemoteInputInvalid
	}
	if err := r.inputState.Apply(event); err != nil {
		return ErrRemoteInputInvalid
	}
	if err := r.ensureConnectionLocked(ctx); err != nil {
		return err
	}
	frame := r.frameForEventLocked(event, capturedAt)
	if err := r.writeFrameLocked(ctx, frame, capturedAt); err == nil {
		r.state = RemoteInputAttached
		r.ready = true
		return nil
	}

	// A TCP write can fail after the bridge has applied the event. Keep the
	// authoritative local state, reconnect using the same identity, and replay
	// it as a snapshot. This is idempotent and avoids stuck controls.
	r.closeConnLocked()
	r.state = RemoteInputReconnecting
	r.ready = false
	if err := r.reconnectLocked(ctx); err != nil {
		r.failLocked("reconnect_timeout")
		return err
	}
	return nil
}

// Send is a convenience form for callers that do not have a separate capture
// timestamp.
func (r *RemoteInput) Send(ctx context.Context, event remoteinput.Event) error {
	return r.SendEvent(ctx, event, r.now())
}

func (r *RemoteInput) RecordSequenceGap() {
	r.mu.Lock()
	r.metrics.sequenceGaps++
	r.mu.Unlock()
}

func (r *RemoteInput) RecordBridgeMetrics(sequenceGaps, releases uint64, bridgeToUInputP95 float64, measurable bool) {
	r.mu.Lock()
	r.metrics.sequenceGaps = sequenceGaps
	r.metrics.releases = releases
	r.metrics.bridgeToUInputP95 = bridgeToUInputP95
	r.metrics.bridgeMetricsMeasurable = measurable
	r.mu.Unlock()
}

// RecordCaptureToBridgeP95 updates the bridge-side latency estimate when the
// bridge reports a monotonic capture timestamp sample.
func (r *RemoteInput) RecordCaptureToBridgeP95(milliseconds float64) {
	if milliseconds < 0 {
		milliseconds = 0
	}
	r.mu.Lock()
	r.metrics.captureToBridgeP95 = milliseconds
	r.mu.Unlock()
}

func (r *RemoteInput) RecordRTT(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	r.mu.Lock()
	r.metrics.rttMS = float64(duration) / float64(time.Millisecond)
	r.mu.Unlock()
}

func (r *RemoteInput) statusLocked() RemoteInputStatus {
	return RemoteInputStatus{
		State: r.state,
		Ready: r.ready,
		Metrics: RemoteInputMetrics{
			FramesSent:               r.metrics.framesSent,
			StateResyncs:             r.metrics.stateResyncs,
			SequenceGaps:             r.metrics.sequenceGaps,
			Releases:                 r.metrics.releases,
			CaptureToBridgeP95MS:     r.captureToBridgeP95Locked(),
			BridgeToUInputP95MS:      r.bridgeToUInputP95Locked(),
			RTTMS:                    r.metrics.rttMS,
			BridgeToUInputMeasurable: r.bridgeToUInputMeasurableLocked(),
			ShutdownReason:           r.metrics.shutdownReason,
		},
	}
}

func (r *RemoteInput) captureToBridgeP95Locked() float64 {
	if r.metrics.captureToBridgeP95 > 0 {
		return r.metrics.captureToBridgeP95
	}
	return percentile(r.metrics.captureLatencies, 0.95)
}

func (r *RemoteInput) bridgeToUInputP95Locked() float64 {
	return r.metrics.bridgeToUInputP95
}

func (r *RemoteInput) bridgeToUInputMeasurableLocked() bool {
	return r.metrics.bridgeMetricsMeasurable
}

func (r *RemoteInput) newIdentity() (uint64, []byte, error) {
	var raw [8]byte
	if _, err := io.ReadFull(r.random, raw[:]); err != nil {
		return 0, nil, err
	}
	session := binary.LittleEndian.Uint64(raw[:])
	if session == 0 {
		session = 1
	}
	token := make([]byte, 32)
	if _, err := io.ReadFull(r.random, token); err != nil {
		return 0, nil, err
	}
	return session, token, nil
}

func (r *RemoteInput) connectLocked(ctx context.Context) error {
	if r.bridge == nil {
		return ErrRemoteInputInvalid
	}
	if dialer, ok := r.bridge.(BridgeDialer); ok {
		conn, err := dialer.Dial(ctx)
		if err != nil {
			return ErrRemoteInputInvalid
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
		return ErrRemoteInputInvalid
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
	snapshot := r.inputState.Snapshot()
	pressed := append([]remoteinput.Code(nil), snapshot.Pressed...)
	sort.Slice(pressed, func(i, j int) bool { return pressed[i] < pressed[j] })
	for _, code := range pressed {
		event := eventForCode(code, remoteinput.ActionPress)
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
		event.Value = int32(snapshot.Axes[code])
		if err := r.writeFrameLocked(ctx, r.frameForEventLocked(event, r.now()), r.now()); err != nil {
			return err
		}
	}
	return nil
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
		Device:       uint8(event.Device),
		Kind:         uint8(event.Kind),
		Action:       uint8(event.Action),
		Code:         uint16(event.Code),
		Value:        event.Value,
	}
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
				ClientMonoNS: uint64(r.now().UnixNano()),
			}
			if err := r.writeFrameLocked(context.Background(), frame, time.Time{}); err != nil {
				r.closeConnLocked()
				r.state = RemoteInputReconnecting
				r.ready = false
				if err := r.reconnectLocked(context.Background()); err != nil {
					r.failLocked("reconnect_timeout")
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
	r.session = 0
	r.token = nil
	r.core = ""
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

func validateBridgeCore(core string) error {
	if core == "" || len(core) > 128 {
		return ErrRemoteInputInvalid
	}
	for _, char := range core {
		if char < 0x20 || char == 0x7f || char == '/' || char == '\\' {
			return ErrRemoteInputInvalid
		}
	}
	return nil
}

func safeShutdownReason(reason string) string {
	switch reason {
	case "session_stop", "session_replace", "watchdog", "reconnect_timeout", "operator_detach", "attach_failed", "bridge_error", "close", "detach":
		return reason
	default:
		return "operator_detach"
	}
}

func waitReady(ctx context.Context, ready <-chan struct{}) error {
	if ready == nil {
		return ErrRemoteInputInvalid
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

func readBoundedLine(reader *bufio.Reader, max int) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if len(line) > max {
		return nil, ErrRemoteInputInvalid
	}
	if err != nil {
		return nil, err
	}
	return line, nil
}

func eventForCode(code remoteinput.Code, action remoteinput.Action) remoteinput.Event {
	event := remoteinput.Event{Code: code, Action: action}
	switch {
	case code < 100:
		event.Device = remoteinput.DeviceKeyboard
		event.Kind = remoteinput.KindKey
	case code < 200:
		event.Device = remoteinput.DeviceGamepad
		event.Kind = remoteinput.KindButton
	default:
		event.Device = remoteinput.DeviceGamepad
		event.Kind = remoteinput.KindAxis
	}
	return event
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	index := int(float64(len(copyValues)-1) * fraction)
	return copyValues[index]
}

func appendBounded(values []float64, value float64, max int) []float64 {
	values = append(values, value)
	if len(values) > max {
		values = values[len(values)-max:]
	}
	return values
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
