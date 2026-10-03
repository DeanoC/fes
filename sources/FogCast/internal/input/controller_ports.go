package input

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/joymatrix"
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
	CoreID   string
	// Generation distinguishes a reload of the same matrix core.
	Generation uint64
	// KeyboardHID names the generation when fes.keyboard.hid 1.0 is active.
	KeyboardHID *KeyboardHIDBinding
	Binding     *ControllerBinding
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
	publishSeq           [controllerPortCount]uint64
	inflight             [controllerPortCount]int
	haltPublish          bool
	keyboard             bool
	coreID               string
	localMatrix          remoteinput.State
	localKeys            map[remoteinput.Code]bool
	pendingMatrixNeutral bool
	coreGeneration       uint64
	keyboardHID          bool
	hidBinding           *KeyboardHIDBinding
	coreActive           bool
	observed             bool
	observedAt           time.Time
	displayFocused       bool
	keys                 *KeyboardSink
	hid                  *keyboardHIDSink
	pads                 *padMerge
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
	_ = s.setObservationContext(context.Background(), obs)
}

func (s *controllerPortsSink) setObservationContext(ctx context.Context, obs CoreObservation) error {
	return s.updateObservationContext(ctx, obs, true)
}

func (s *controllerPortsSink) invalidateObservationContext(ctx context.Context) error {
	return s.updateObservationContext(ctx, CoreObservation{}, false)
}

func (s *controllerPortsSink) updateObservationContext(ctx context.Context, obs CoreObservation, confirmed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hid != nil && !sameHIDBinding(s.hidBinding, obs.KeyboardHID) {
		if err := s.hid.bindContext(ctx, obs.KeyboardHID); err != nil {
			return err
		}
		s.hidBinding = nil
		if obs.KeyboardHID != nil {
			copy := *obs.KeyboardHID
			s.hidBinding = &copy
		}
	}
	sameMatrixCore := obs.Active && obs.Keyboard && obs.CoreID == s.coreID && obs.Generation == s.coreGeneration
	if confirmed && !sameMatrixCore && s.keys != nil {
		// Physical keyboard holds belong to that core too. Observation proves
		// its matrix has retired; discard both local contributions without a post.
		s.keys.forgetSource(sourceLocal)
		s.keys.forgetSource(sourceLocalMatrix)
	}
	if (len(s.localKeys) != 0 || len(s.localMatrixKeysLocked()) != 0 || s.pendingMatrixNeutral) &&
		(!confirmed || !sameMatrixCore || s.pendingMatrixNeutral) {
		// Stop replaying old pad holds as soon as observation becomes uncertain.
		// A failed neutral post still needs retry on the same observed core.
		s.localMatrix.ReleaseAll()
		if (!confirmed || sameMatrixCore) && s.keys != nil {
			s.pendingMatrixNeutral = true
			if err := s.keys.releaseSourceContext(ctx, sourceLocalMatrix); err != nil {
				s.observed = false
				return err
			}
		}
		s.localKeys = nil
		s.pendingMatrixNeutral = false
	}
	if confirmed {
		s.keyboard = obs.Keyboard
		s.coreID = obs.CoreID
		s.coreGeneration = obs.Generation
	}
	s.keyboardHID = obs.KeyboardHID != nil && s.hid != nil
	s.coreActive = obs.Active
	// A negative result can still be cached once no neutral write is pending.
	// Failed neutral writes return above with the cache invalidated.
	s.observed = true
	s.observedAt = time.Now()
	return nil
}

func sameHIDBinding(left, right *KeyboardHIDBinding) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// keyboardModeLocked selects the keyboard shaping from the observed contract.
func (s *controllerPortsSink) keyboardModeLocked() playhid.KeyboardMode {
	switch {
	case s.keyboardHID:
		return playhid.HIDKeys
	case s.keyboard:
		return playhid.MatrixKeys
	default:
		return playhid.NativeKeys
	}
}

func (s *controllerPortsSink) hasBinding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.binding != nil
}

func (s *controllerPortsSink) cachedObservation() (active, fresh bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coreActive, s.observed && time.Since(s.observedAt) < localCoreObserveTTL
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
	if remoteinput.IsLocalPlayerDeparture(frameEvent(f)) {
		defer s.mu.Unlock()
		if source != sourceLocal {
			return bridge.RejectInput("player departure requires the local input socket")
		}
		return s.releaseLocalPlayerLocked(ctx, f.Player)
	}
	shaped, ok := s.shapeLocked(source, f)
	if s.displayFocused && ok && !keyboardFrame(shaped) {
		s.mu.Unlock()
		return nil
	}
	if ok && s.keyboardHID && keyboardFrame(shaped) {
		// HID key state has its own ordered sink; a slow set_keyboard_hid
		// post must not hold the controller-port lock.
		hid := s.hid
		s.mu.Unlock()
		return hid.applyFrom(ctx, source, shaped)
	}
	defer s.mu.Unlock()
	if !ok {
		if source == sourceLocal {
			return bridge.RejectInput("unsupported local input")
		}
		return nil
	}
	if s.binding == nil {
		if source == sourceLocal && s.localMatrixRouteLocked() && shaped.Device == uint8(remoteinput.DeviceGamepad) {
			return s.applyLocalMatrixLocked(ctx, shaped)
		}
		return s.applyUnboundLocked(ctx, source, shaped)
	}
	return s.applyPortsLocked(ctx, source, shaped)
}

func (s *controllerPortsSink) releaseLocalPlayerLocked(ctx context.Context, player uint8) error {
	if s.binding == nil {
		if s.localMatrixRouteLocked() {
			// Releases preceding the marker normally emptied this player's state.
			// Clear any remaining contribution without touching the other pad.
			for _, code := range s.localMatrix.SnapshotForPlayer(player).Pressed {
				_ = s.localMatrix.Apply(remoteinput.Event{Player: player, Device: remoteinput.DeviceGamepad,
					Kind: remoteinput.KindButton, Action: remoteinput.ActionRelease, Code: code})
			}
			for _, code := range []remoteinput.Code{remoteinput.AxisLeftX, remoteinput.AxisLeftY} {
				_ = s.localMatrix.Apply(remoteinput.Event{Player: player, Device: remoteinput.DeviceGamepad,
					Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: code})
			}
			return s.publishLocalMatrixLocked(ctx)
		}
		return nil
	}
	// The hub sends this after the departed pad's releases and zero axes.
	// Keep the remaining source state: a physical keyboard can also contribute
	// shaped controls on this player, and it has not disconnected.
	s.localClaim[player] = false
	return s.publishAllLocked(ctx)
}

func (s *controllerPortsSink) applyLocalMatrixLocked(ctx context.Context, f protocol.InputFrame) error {
	if err := s.localMatrix.Apply(frameEvent(f)); err != nil {
		return err
	}
	return s.publishLocalMatrixLocked(ctx)
}

func (s *controllerPortsSink) localMatrixRouteLocked() bool {
	return s.binding == nil && s.coreActive && s.keyboard && joymatrix.Supports(s.coreID) && s.keys != nil
}

func (s *controllerPortsSink) localMatrixKeysLocked() map[remoteinput.Code]bool {
	desired := make(map[remoteinput.Code]bool)
	for player := uint8(0); player < controllerPortCount; player++ {
		for code := range joymatrix.Desired(s.coreID, s.localMatrix.SnapshotForPlayer(player)) {
			desired[code] = true
		}
	}
	return desired
}

func (s *controllerPortsSink) publishLocalMatrixLocked(ctx context.Context) error {
	desired := s.localMatrixKeysLocked()
	var removed, added []remoteinput.Code
	for code := range s.localKeys {
		if !desired[code] {
			removed = append(removed, code)
		}
	}
	for code := range desired {
		if !s.localKeys[code] {
			added = append(added, code)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	for _, transition := range []struct {
		codes  []remoteinput.Code
		action remoteinput.Action
	}{{removed, remoteinput.ActionRelease}, {added, remoteinput.ActionPress}} {
		for _, code := range transition.codes {
			frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(transition.action), Code: uint16(code)}
			if err := s.keys.ApplyFrom(ctx, sourceLocalMatrix, frame); err != nil {
				return err
			}
		}
	}
	s.localKeys = desired
	return nil
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
	if source == sourceLocal && s.binding == nil && !(s.localMatrixRouteLocked() && event.Device == remoteinput.DeviceGamepad) {
		event.Player = 0
	}
	shaped, ok := playhid.StreamEvent(event, s.keyboardModeLocked())
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
		// A local claim can temporarily exclude a previously mapped remote
		// player. Its releases still retire retained holds, so reopening a
		// port cannot replay a button or stick the sender already released.
		if event.Action == remoteinput.ActionRelease || (event.Kind == remoteinput.KindAxis && event.Value == 0) {
			if err := s.sources[source].Apply(event); err != nil {
				return err
			}
		}
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
	if s.hid != nil {
		_ = s.hid.releaseSource(source)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if source >= sourceCount {
		source = sourceRemote
	}
	s.sources[source].ReleaseAll()
	if source == sourceLocal {
		s.localMatrix.ReleaseAll()
		s.localClaim = [controllerPortCount]bool{}
		if s.binding != nil || s.keys == nil {
			s.localKeys = nil
			s.pendingMatrixNeutral = false
		}
	}
	if s.binding == nil {
		var result error
		if s.keys != nil {
			if source == sourceLocal {
				// One full matrix post retires pad and physical-keyboard holds.
				// Disconnect does not prove the core retired, so failure still
				// needs the same observation-driven retry as a local timeout.
				s.pendingMatrixNeutral = true
				result = s.keys.releaseSourcesContext(context.Background(), sourceLocal, sourceLocalMatrix)
				if result == nil {
					s.localKeys = nil
					s.pendingMatrixNeutral = false
				} else {
					s.observed = false
				}
			} else {
				_ = s.keys.ReleaseSource(source)
			}
		}
		if s.pads != nil {
			return errors.Join(result, s.pads.ReleaseSource(source))
		}
		if s.fallback != nil && s.keys == nil && s.pads == nil {
			return s.fallback.ReleaseAll()
		}
		return result
	}
	// Disconnect and local-socket close are not a frame held under the
	// lifecycle lock. The host poster keeps its own deadline.
	return s.publishAllLocked(context.Background())
}

func (s *controllerPortsSink) ReleaseAll() error {
	if s.hid != nil {
		s.hid.releaseAll()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initPublishLocked()
	s.displayFocused = false
	s.keyboardHID = false
	s.hidBinding = nil
	if s.binding == nil {
		s.resetSourcesLocked()
		s.localMatrix.ReleaseAll()
		s.localKeys = nil
		s.pendingMatrixNeutral = false
		s.coreActive = false
		s.observed = false
		s.keyboard = false
		s.coreID = ""
		s.coreGeneration = 0
		if s.keys != nil {
			_ = s.keys.ReleaseAll()
		}
		if s.pads != nil {
			_ = s.pads.ReleaseAll()
		}
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
		s.localMatrix.ReleaseAll()
		s.localKeys = nil
		s.pendingMatrixNeutral = false
		s.binding = nil
		s.coreActive = false
		s.observed = false
		s.keyboard = false
		s.coreID = ""
		s.coreGeneration = 0
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
