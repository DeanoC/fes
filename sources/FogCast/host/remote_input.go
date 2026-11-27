package host

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
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
	ErrRemoteInputClosed      = errors.New("remote input is closed")
	ErrRemoteInputBusy        = errors.New("remote input is already attached")
	ErrRemoteInputInvalid     = errors.New("remote input configuration is invalid")
	ErrRemoteInputNoStream    = errors.New("no live input stream")
	ErrRemoteInputUnconfirmed = errors.New("input processing was not confirmed")
	errTapUnsupported         = errors.New("target bridge does not support timed taps")
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
	LocalKeyboard string             `json:"local_keyboard,omitempty"`
	SessionID     string             `json:"session_id,omitempty"`
	Source        string             `json:"source,omitempty"`
	State         RemoteInputState   `json:"state"`
	Ready         bool               `json:"ready"`
	Metrics       RemoteInputMetrics `json:"metrics"`
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

	state            RemoteInputState
	ready            bool
	closed           bool
	bridge           BridgeHandle
	conn             net.Conn
	reader           *bufio.Reader
	receiptReady     bool
	tapReady         bool
	tapChordReady    bool
	heartbeatCancel  context.CancelFunc
	session          uint64
	token            []byte
	core             string
	sequence         uint32
	lastPing         uint64
	inputState       remoteinput.State
	keyboard         bool
	controllerPorts  bool
	keypadPorts      bool
	mouse            bool
	colecoOutput     map[remoteinput.Code]bool
	source           string
	sourceGeneration uint64
	metrics          remoteInputCounters
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

// Only an explicit ownership refusal survives input-start sanitization. Unknown
// transport, parsing and bridge errors retain their existing generic failure.
func remoteInputStartError(err error) error {
	var api *protocol.APIError
	if errors.As(err, &api) && (api.Code == protocol.CodeKitLeaseDenied || api.Code == protocol.CodeKitLeaseBusy) {
		return &protocol.APIError{Code: api.Code, Message: "session does not own the kit lease", Phase: "recovery"}
	}
	return ErrRemoteInputInvalid
}

func (r *RemoteInput) Status() RemoteInputStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

func (r *RemoteInput) Attach(ctx context.Context, core string) error {
	return r.attach(ctx, core, false, false, false, false)
}

// AttachWithCapabilities attaches one session with the exact capabilities
// established by the host session coordinator. keyboard is true only for the
// fes.keyboard 1.0 interface.
func (r *RemoteInput) AttachWithCapabilities(ctx context.Context, core string, keyboard bool) error {
	return r.attach(ctx, core, keyboard, false, false, false)
}

func (r *RemoteInput) AttachWithControllerPorts(ctx context.Context, core string, keyboard, ports, keypad bool) error {
	return r.attach(ctx, core, keyboard, ports, keypad, false)
}

func (r *RemoteInput) AttachWithMouse(ctx context.Context, core string, keyboard, ports, keypad, mouse bool) error {
	return r.attach(ctx, core, keyboard, ports, keypad, mouse)
}

func (r *RemoteInput) attach(ctx context.Context, core string, keyboard, ports, keypad, mouse bool) error {
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
	r.source = ""
	r.sourceGeneration++
	r.state = RemoteInputStarting
	r.ready = false
	r.metrics = remoteInputCounters{}
	r.sequence = 0
	r.inputState = remoteinput.State{}
	r.colecoOutput = nil
	r.session = session
	r.token = token
	r.core = core
	r.keyboard = keyboard
	r.controllerPorts = ports
	r.keypadPorts = keypad
	r.mouse = mouse

	bridge, err := r.starter.Start(ctx, BridgeSpec{Session: session, Token: append([]byte(nil), token...), Core: core})
	if err != nil || bridge == nil {
		r.failLocked("attach_failed")
		return remoteInputStartError(err)
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
		return remoteInputStartError(err)
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
	var localKeyboard string
	if h, ok := r.bridge.(interface{ LocalKeyboardAdmission() string }); ok {
		localKeyboard = h.LocalKeyboardAdmission()
	}
	return RemoteInputStatus{
		LocalKeyboard: localKeyboard,
		State:         r.state,
		Ready:         r.ready,
		SessionID:     r.sessionIDLocked(),
		Source:        r.source,
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
	return remoteinput.EventForCode(code, action)
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

// Invalidate discards a previous target boot/session locally. It intentionally
// does not call bridge Stop or emit input against a replacement target session.
func (r *RemoteInput) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.heartbeatCancel != nil {
		r.heartbeatCancel()
		r.heartbeatCancel = nil
	}
	r.bridge = nil
	r.closeConnLocked()
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
	r.metrics.shutdownReason = "target_changed"
}
