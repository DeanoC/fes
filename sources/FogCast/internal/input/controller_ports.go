package input

import (
	"context"
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// ControllerBinding is observed from the runtime while the input lifecycle
// excludes core replacement. Every write retains this exact identity.
type ControllerBinding struct {
	PackageID  string
	Generation uint64
	Keypad     bool
}

// CoreObservation is what mister-agent can learn from the runtime without a
// host session: whether a core is bound, whether it wants the keyboard matrix,
// and the controller-port identity when that contract is active.
type CoreObservation struct {
	Active   bool
	Keyboard bool
	Binding  *ControllerBinding
}

type ControllerPoster func(context.Context, string, uint64, uint8, uint8, uint16) error

type portLevel struct {
	buttons uint8
	keypad  uint16
	known   bool
}

type controllerPortsSink struct {
	mu          sync.Mutex
	publishOnce sync.Once
	publish     sync.Cond
	fallback    bridge.Sink
	poster      ControllerPoster
	binding     *ControllerBinding
	sources     [sourceCount]remoteinput.State
	localClaim  [controllerPortCount]bool
	level       [controllerPortCount]portLevel
	dirty       [controllerPortCount]bool
	// publishSeq is the newest set_controller decision for a port.
	// inflight counts poster calls that have not rejoined mu.
	// haltPublish rejects new posts while ReleaseAll neutralizes.
	publishSeq  [controllerPortCount]uint64
	inflight    [controllerPortCount]int
	haltPublish bool
	keyboard    bool
	coreActive  bool
	observed    bool
	keys        *KeyboardSink
	pads        *padMerge
}

func (s *controllerPortsSink) bind(binding *ControllerBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if binding != nil && s.binding != nil && sameBinding(s.binding, binding) {
		return nil
	}
	if s.dirty[0] || s.dirty[1] {
		return errors.New("controller state needs release")
	}
	if binding != nil {
		if binding.PackageID == "" || binding.Generation == 0 || s.poster == nil {
			return errors.New("invalid controller binding")
		}
		copy := *binding
		binding = &copy
	}
	s.binding = binding
	s.resetSourcesLocked()
	return nil
}

func sameBinding(left, right *ControllerBinding) bool {
	return left.PackageID == right.PackageID && left.Generation == right.Generation && left.Keypad == right.Keypad
}

func (s *controllerPortsSink) setObservation(obs CoreObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyboard = obs.Keyboard
	s.coreActive = obs.Active
	s.observed = true
}

func (s *controllerPortsSink) hasBinding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.binding != nil
}

func (s *controllerPortsSink) cachedActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observed && s.coreActive && s.binding == nil
}

func (s *controllerPortsSink) resetSourcesLocked() {
	s.sources[sourceRemote].ReleaseAll()
	s.sources[sourceLocal].ReleaseAll()
	s.localClaim = [controllerPortCount]bool{}
	s.level = [controllerPortCount]portLevel{}
}

// Apply is the host stream. Local frames enter through applyContext with the
// deadline deliverLocal created while it holds the lifecycle lock. The host
// stream has no such deadline here; mister-agent keeps the 2s controller
// bound when this context has none. set_controller itself runs without mu, so
// a slow host post cannot block kit-local delivery on this lock.
func (s *controllerPortsSink) Apply(f protocol.InputFrame) error {
	return s.apply(sourceRemote, f)
}

func (s *controllerPortsSink) apply(source inputSource, f protocol.InputFrame) error {
	return s.applyContext(context.Background(), source, f)
}

func (s *controllerPortsSink) applyContext(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	shaped, ok := s.shapeLocked(source, f)
	if !ok {
		if source == sourceLocal {
			return bridge.RejectInput("unsupported local input")
		}
		return nil
	}
	if s.binding == nil {
		return s.applyUnboundLocked(ctx, source, shaped)
	}
	return s.applyPortsLocked(ctx, source, shaped)
}

// shapeLocked rewrites raw frames with the same playhid mapping the host used
// to apply before the stream. Gamepad frames pass through, so a host that
// already shaped its events is unchanged. Local frames always go through it.
// Remote frames do too once a runtime observation has stored the keyboard bit.
func (s *controllerPortsSink) shapeLocked(source inputSource, f protocol.InputFrame) (protocol.InputFrame, bool) {
	if source != sourceLocal && !s.observed {
		return f, true
	}
	event := frameEvent(f)
	if source == sourceLocal && s.binding == nil {
		event.Player = 0
	}
	shaped, ok := playhid.StreamEvent(event, s.keyboard)
	if !ok {
		return protocol.InputFrame{}, false
	}
	f.Player = shaped.Player
	f.Device = uint8(shaped.Device)
	f.Kind = uint8(shaped.Kind)
	f.Action = uint8(shaped.Action)
	f.Code = uint16(shaped.Code)
	f.Value = shaped.Value
	return f, true
}

func (s *controllerPortsSink) applyUnboundLocked(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	if keyboardFrame(f) {
		if s.keys != nil && f.Code >= uint16(zx81keys.KeyShift) {
			return s.keys.ApplyFrom(ctx, source, f)
		}
		if s.keys != nil {
			return nil
		}
		if s.fallback != nil {
			return s.fallback.Apply(f)
		}
		return nil
	}
	if s.pads != nil {
		return s.pads.ApplyFrom(source, f)
	}
	if s.fallback != nil {
		return s.fallback.Apply(f)
	}
	return nil
}

func (s *controllerPortsSink) applyPortsLocked(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	if f.Player > 1 || f.Device != uint8(remoteinput.DeviceGamepad) {
		return bridge.RejectInput("unsupported controller event")
	}
	event := frameEvent(f)
	if !validControllerEvent(event, s.binding.Keypad) {
		return bridge.RejectInput("unsupported controller control")
	}
	if s.haltPublish {
		return nil
	}
	if source == sourceLocal {
		before := s.localClaim
		s.localClaim[event.Player] = true
		if err := s.sources[source].Apply(event); err != nil {
			s.localClaim = before
			return err
		}
		if before != s.localClaim {
			return s.publishAllLocked(ctx)
		}
		return s.publishPortLocked(ctx, event.Player, true)
	}
	port, ok := remotePort(event.Player, s.localClaim)
	if !ok {
		return bridge.RejectInput("no free controller port")
	}
	if err := s.sources[source].Apply(event); err != nil {
		return err
	}
	return s.publishPortLocked(ctx, port, true)
}

func validControllerEvent(e remoteinput.Event, keypad bool) bool {
	if e.Kind == remoteinput.KindAxis {
		return e.Action == remoteinput.ActionAbsolute && (e.Code == remoteinput.AxisLeftX || e.Code == remoteinput.AxisLeftY) && e.Value >= -32768 && e.Value <= 32767
	}
	return e.Kind == remoteinput.KindButton && (e.Action == remoteinput.ActionPress || e.Action == remoteinput.ActionRelease) && e.Value == 0 &&
		((e.Code >= remoteinput.ButtonDPadUp && e.Code <= remoteinput.ButtonSelect) || (keypad && e.Code >= remoteinput.Keypad0 && e.Code <= remoteinput.KeypadHash))
}

// Keypad-port cores (Coleco) have no Start or Select on the original
// controller, and a modern pad has no keypad. On those cores Start also
// presses keypad 1 (the usual one-player game select) and Select also presses
// keypad * (the usual replay key). The Start/Select bitmap bits stay set.
const (
	keypadAliasStart  = uint16(1) << 1 // keypad 1
	keypadAliasSelect = uint16(1) << (remoteinput.KeypadStar - remoteinput.Keypad0)
)

func controllerSnapshot(snapshot remoteinput.Snapshot, keypadPorts bool) (buttons uint8, keypad uint16) {
	// Shared bitmap is Up,Down,Left,Right,A,B,Select,Start.
	for _, code := range snapshot.Pressed {
		if code >= remoteinput.ButtonDPadUp && code <= remoteinput.ButtonB {
			buttons |= 1 << (code - remoteinput.ButtonDPadUp)
		}
		if code == remoteinput.ButtonSelect {
			buttons |= 1 << 6
			if keypadPorts {
				keypad |= keypadAliasSelect
			}
		}
		if code == remoteinput.ButtonStart {
			buttons |= 1 << 7
			if keypadPorts {
				keypad |= keypadAliasStart
			}
		}
		if code >= remoteinput.Keypad0 && code <= remoteinput.KeypadHash {
			keypad |= 1 << (code - remoteinput.Keypad0)
		}
	}
	if x := snapshot.Axes[remoteinput.AxisLeftX]; x < -8000 {
		buttons |= 1 << 2
	} else if x > 8000 {
		buttons |= 1 << 3
	}
	if y := snapshot.Axes[remoteinput.AxisLeftY]; y < -8000 {
		buttons |= 1
	} else if y > 8000 {
		buttons |= 1 << 1
	}
	return
}

func (s *controllerPortsSink) mergedSnapshotLocked(port uint8) remoteinput.Snapshot {
	local := s.sources[sourceLocal].SnapshotForPlayer(port)
	remotePlayer, ok := remotePlayerForPort(port, s.localClaim)
	if !ok {
		return mergeSnapshots(local, remoteinput.Snapshot{})
	}
	return mergeSnapshots(local, s.sources[sourceRemote].SnapshotForPlayer(remotePlayer))
}

func (s *controllerPortsSink) publishAllLocked(ctx context.Context) error {
	var result error
	for port := uint8(0); port < controllerPortCount; port++ {
		if err := s.publishPortLocked(ctx, port, false); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// publishPortLocked posts one port. The caller holds mu on entry and on
// return. mu is not held across poster: a host set_controller must not block
// kit-local apply on the same lock the local socket needs drained. A post
// that finishes behind a newer decision does not commit, and the last stale
// completion on that port republishes the current snapshot.
func (s *controllerPortsSink) publishPortLocked(ctx context.Context, port uint8, force bool) error {
	s.initPublishLocked()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if s.haltPublish {
			return nil
		}
		if s.binding == nil || s.poster == nil {
			return errors.New("controller binding is inactive")
		}
		buttons, keypad := controllerSnapshot(s.mergedSnapshotLocked(port), s.binding.Keypad)
		unchanged := s.level[port].known && s.level[port].buttons == buttons && s.level[port].keypad == keypad
		if !force && (unchanged || (!s.level[port].known && buttons == 0 && keypad == 0)) {
			s.level[port].known = true
			s.dirty[port] = buttons != 0 || keypad != 0
			return nil
		}
		binding := *s.binding
		s.publishSeq[port]++
		seq := s.publishSeq[port]
		s.inflight[port]++
		// A failed request can have applied: release must still attempt zero.
		s.dirty[port] = true
		poster := s.poster
		s.mu.Unlock()

		err := poster(ctx, binding.PackageID, binding.Generation, port, buttons, keypad)

		s.mu.Lock()
		s.inflight[port]--
		// ReleaseAll waits until every port is idle. A stale republish below
		// is per port and must not wait for the other port.
		if s.inflight[0] == 0 && s.inflight[1] == 0 {
			s.publish.Broadcast()
		}
		if err != nil {
			return err
		}
		if s.haltPublish || s.binding == nil || !sameBinding(s.binding, &binding) {
			return nil
		}
		if seq == s.publishSeq[port] {
			s.level[port] = portLevel{buttons: buttons, keypad: keypad, known: true}
			s.dirty[port] = buttons != 0 || keypad != 0
			return nil
		}
		// This post is stale. If it was the last in flight for this port, the
		// runtime may now be showing it. Republish even while the other port
		// still has a post in flight.
		if s.inflight[port] == 0 {
			force = true
			continue
		}
		return nil
	}
}

func (s *controllerPortsSink) initPublishLocked() {
	s.publishOnce.Do(func() { s.publish.L = &s.mu })
}

func (s *controllerPortsSink) releaseSource(source inputSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source >= sourceCount {
		source = sourceRemote
	}
	s.sources[source].ReleaseAll()
	if source == sourceLocal {
		s.localClaim = [controllerPortCount]bool{}
	}
	if s.binding == nil {
		if s.keys != nil {
			_ = s.keys.ReleaseSource(source)
		}
		if s.pads != nil {
			return s.pads.ReleaseSource(source)
		}
		if s.fallback != nil && s.keys == nil && s.pads == nil {
			return s.fallback.ReleaseAll()
		}
		return nil
	}
	// Disconnect and local-socket close are not a frame held under the
	// lifecycle lock. The host poster keeps its own deadline.
	return s.publishAllLocked(context.Background())
}

func (s *controllerPortsSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initPublishLocked()
	if s.binding == nil {
		s.resetSourcesLocked()
		s.coreActive = false
		s.observed = false
		s.keyboard = false
		if s.fallback == nil {
			return nil
		}
		return s.fallback.ReleaseAll()
	}
	// In-flight host posts must finish before zeros, and must not commit
	// over them. Waiting drops mu so kit-local delivery is not stuck behind
	// that set_controller.
	s.haltPublish = true
	defer func() { s.haltPublish = false }()
	for s.inflight[0] > 0 || s.inflight[1] > 0 {
		s.publish.Wait()
	}
	var result error
	for port := uint8(0); port < controllerPortCount; port++ {
		if !s.dirty[port] {
			continue
		}
		binding := *s.binding
		poster := s.poster
		s.mu.Unlock()
		err := poster(context.Background(), binding.PackageID, binding.Generation, port, 0, 0)
		s.mu.Lock()
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		s.dirty[port] = false
		s.level[port] = portLevel{known: true}
	}
	if result == nil {
		s.resetSourcesLocked()
		s.binding = nil
		s.coreActive = false
		s.observed = false
		s.keyboard = false
	}
	if s.keys != nil {
		_ = s.keys.ReleaseAll()
	}
	if s.pads != nil {
		result = errors.Join(result, s.pads.ReleaseAll())
	}
	return result
}

func (s *controllerPortsSink) Close() error {
	err := s.ReleaseAll()
	if s.fallback == nil {
		return err
	}
	return errors.Join(err, s.fallback.Close())
}

// remoteSourceSink is the host bridge's view of the shared sink. Disconnect
// releases only the remote source, so a kit-local hold survives a host
// reconnect. Lease expiry and core replacement call ReleaseAll on the sink.
type remoteSourceSink struct {
	ports *controllerPortsSink
}

func (s remoteSourceSink) Apply(f protocol.InputFrame) error {
	return s.ports.apply(sourceRemote, f)
}

func (s remoteSourceSink) ReleaseAll() error {
	return s.ports.releaseSource(sourceRemote)
}

func (s remoteSourceSink) Close() error { return nil }
