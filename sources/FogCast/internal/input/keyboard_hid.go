package input

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

var errTapUnavailable = errors.New("target keyboard tap is unavailable")

// KeyboardHIDBinding is the active fes.computer generation that negotiated
// fes.keyboard.hid 1.0. Every set_keyboard_hid post carries this identity.
type KeyboardHIDBinding struct {
	PackageID  string
	Generation uint64
}

// KeyboardHIDPoster sends one complete nine-row key state.
type KeyboardHIDPoster func(context.Context, string, uint64, hidkeys.Rows) error

// keyboardHIDSink keeps each source's held usages; the posted state is their
// union, so a release from one source leaves the other's hold in place. Posts
// are serialized under mu so the runtime never sees an older state last.
// unconfirmed marks a state whose last post failed: a repeated press or
// release reposts it instead of reporting a success the runtime never saw.
type keyboardHIDSink struct {
	mu             sync.Mutex
	poster         KeyboardHIDPoster
	binding        *KeyboardHIDBinding
	pressed        [sourceCount]map[uint8]bool
	blocked        [sourceCount]map[uint8]bool
	shiftAmbiguous [sourceCount]bool
	tapHeld        map[uint8]bool
	tapInFlight    map[uint8]uint64
	tapSequence    uint64
	focused        bool
	focusFence     atomic.Bool
	fenceEpoch     atomic.Uint64
	focusEpoch     uint64
	dirty          bool
	unconfirmed    bool
}

func (s *keyboardHIDSink) setPoster(poster KeyboardHIDPoster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poster = poster
}

// bind adopts a new generation with every key released. Rebinding the same
// generation keeps the held state.
func (s *keyboardHIDSink) bind(binding *KeyboardHIDBinding) {
	_ = s.bindContext(context.Background(), binding)
}

func (s *keyboardHIDSink) bindContext(ctx context.Context, binding *KeyboardHIDBinding) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil {
		s.mu.Lock()
	} else {
		// A remote HID post holds mu across its runtime write. Observation
		// refresh under the local lifecycle lock must honor its own budget.
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for !s.mu.TryLock() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
			}
		}
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if binding != nil && s.binding != nil && *binding == *s.binding {
		return nil
	}
	s.binding = nil
	if binding != nil {
		copy := *binding
		s.binding = &copy
	}
	s.pressed = [sourceCount]map[uint8]bool{}
	// A source can hold a focus-suppressed key across a core replacement.
	// Keep that suppression until its real release or source disconnect.
	s.tapHeld = nil
	s.tapInFlight = nil
	s.dirty = false
	s.unconfirmed = false
	return nil
}

func (s *keyboardHIDSink) bound() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.binding != nil
}

func (s *keyboardHIDSink) unionLocked() map[uint8]bool {
	union := map[uint8]bool{}
	for source := range s.pressed {
		for usage := range s.pressed[source] {
			union[usage] = true
		}
	}
	for usage := range s.tapHeld {
		union[usage] = true
	}
	return union
}

func (s *keyboardHIDSink) postLocked(ctx context.Context) error {
	if s.binding == nil || s.poster == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	union := s.unionLocked()
	// A failed post may still have applied; keep the state dirty so release
	// always attempts neutral, and unconfirmed so a retry reposts it.
	s.dirty = true
	s.unconfirmed = true
	err := s.poster(ctx, s.binding.PackageID, s.binding.Generation, hidkeys.Encode(union))
	if err == nil {
		s.dirty = len(union) != 0
		s.unconfirmed = false
	}
	return err
}

// repostLocked answers a repeated press or release: nothing changed, but a
// state whose last post failed is posted again.
func (s *keyboardHIDSink) repostLocked(ctx context.Context) error {
	if !s.unconfirmed {
		return nil
	}
	return s.postLocked(ctx)
}

// replayUnconfirmed posts the current union when the last post failed and
// this is still that generation. TryLock gives up when a post already holds
// the sink: that post owns the snapshot, and waiting here would keep the
// local lifecycle lock behind it. A different generation is not replayed.
func (s *keyboardHIDSink) replayUnconfirmed(ctx context.Context, binding *KeyboardHIDBinding) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.mu.TryLock() {
		return nil
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.focused || s.focusFence.Load() || !s.unconfirmed || s.binding == nil || binding == nil || *s.binding != *binding {
		return nil
	}
	return s.postLocked(ctx)
}

// reconcileFocusNeutral waits for a concurrent HID operation to finish, then
// retries any unconfirmed neutral under the current binding. A changed binding
// clears unconfirmed state, so it cannot target the retired generation.
func (s *keyboardHIDSink) reconcileFocusNeutral(ctx context.Context) error {
	if err := s.lockTap(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.focused || s.focusFence.Load() || !s.unconfirmed || s.binding == nil {
		return nil
	}
	return s.postLocked(ctx)
}

// retireFocusKeysLocked retains a pending neutral because a refused or
// unknown physical transition may have left the old rows held.
func (s *keyboardHIDSink) retireFocusKeysLocked() {
	if s.dirty || len(s.tapHeld) != 0 {
		s.unconfirmed = true
	}
	s.tapHeld = nil
	for source, keys := range s.pressed {
		if s.blocked[source] == nil {
			s.blocked[source] = map[uint8]bool{}
		}
		for usage := range keys {
			s.blocked[source][usage] = true
		}
		s.pressed[source] = nil
	}
}

// settleUnknownFocus neutralizes a post that was already in flight when an
// unknown result latched the fence. An epoch or generation change cancels it.
func (s *keyboardHIDSink) settleUnknownFocus(ctx context.Context, epoch uint64, binding KeyboardHIDBinding) error {
	if err := s.lockTap(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if !s.focusFence.Load() || s.fenceEpoch.Load() != epoch || s.binding == nil || *s.binding != binding {
		return nil
	}
	s.retireFocusKeysLocked()
	s.focused = true
	s.focusEpoch = epoch
	if !s.unconfirmed {
		return nil
	}
	return s.postLocked(ctx)
}

// setDisplayFocus retires held usages without a post. The physical focus
// transition neutralizes the machine; each source must release before a fresh
// press is delivered. The caller holds the controller-port lifecycle fence.
func (s *keyboardHIDSink) setDisplayFocus(ctx context.Context, focused bool, epoch uint64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.lockTap(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if focused || (s.focusFence.Load() && !s.focused) {
		s.retireFocusKeysLocked()
	}
	s.focused = focused
	s.focusEpoch = epoch
	s.fenceEpoch.Store(epoch)
	s.focusFence.Store(focused)
	return nil
}

func (s *keyboardHIDSink) clearBlockedLocked(source inputSource, usage uint8) {
	delete(s.blocked[source], usage)
	if !s.blocked[source][0xe1] && !s.blocked[source][0xe5] {
		s.shiftAmbiguous[source] = false
	}
}

// observeOtherKeyboard tracks releases while another keyboard mode owns
// delivery. It never posts to the old HID generation.
func (s *keyboardHIDSink) observeOtherKeyboard(ctx context.Context, source inputSource, frame protocol.InputFrame) error {
	code := remoteinput.Code(frame.Code)
	usage, ok := hidkeys.Usage(code)
	if !ok {
		usage, ok = hidkeys.FromLegacy(code)
	}
	if !ok {
		return nil
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.lockTap(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if code == zx81keys.KeyShift {
		// Legacy Shift has no side identity. A new legacy press while a
		// side is suppressed makes later legacy releases ambiguous too.
		left, right := s.blocked[source][0xe1], s.blocked[source][0xe5]
		if frame.Action == uint8(remoteinput.ActionPress) {
			if left || right {
				s.shiftAmbiguous[source] = true
			}
		} else if !s.shiftAmbiguous[source] && left != right {
			delete(s.blocked[source], 0xe1)
			delete(s.blocked[source], 0xe5)
		}
		return nil
	}
	if frame.Action == uint8(remoteinput.ActionRelease) {
		s.clearBlockedLocked(source, usage)
	}
	return nil
}

// applyFrom records one HID key frame from a source and posts the new union.
func (s *keyboardHIDSink) applyFrom(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	return s.applyFromWithEpoch(ctx, source, f, nil, false)
}

// applyFromEpoch binds a frame to the focus state observed before the ports
// lock was released. A delayed frame cannot cross a complete focus cycle.
func (s *keyboardHIDSink) applyFromEpoch(ctx context.Context, source inputSource, f protocol.InputFrame, epoch uint64, focused bool) error {
	return s.applyFromWithEpoch(ctx, source, f, &epoch, focused)
}

func (s *keyboardHIDSink) applyFromWithEpoch(ctx context.Context, source inputSource, f protocol.InputFrame, epoch *uint64, admittedFocused bool) error {
	usage, ok := hidkeys.Usage(remoteinput.Code(f.Code))
	if !ok || (f.Action != uint8(remoteinput.ActionPress) && f.Action != uint8(remoteinput.ActionRelease)) {
		return nil
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != nil && (*epoch != s.focusEpoch || admittedFocused && !s.focused) {
		if f.Action == uint8(remoteinput.ActionPress) {
			if s.blocked[source] == nil {
				s.blocked[source] = map[uint8]bool{}
			}
			s.blocked[source][usage] = true
		} else {
			s.clearBlockedLocked(source, usage)
			if !s.focused && !s.focusFence.Load() {
				return s.repostLocked(ctx)
			}
		}
		return nil
	}
	if f.Action == uint8(remoteinput.ActionRelease) {
		s.clearBlockedLocked(source, usage)
	}
	if s.focused || s.focusFence.Load() || s.blocked[source][usage] {
		if (s.focused || s.focusFence.Load()) && f.Action == uint8(remoteinput.ActionPress) {
			if s.blocked[source] == nil {
				s.blocked[source] = map[uint8]bool{}
			}
			s.blocked[source][usage] = true
		}
		delete(s.pressed[source], usage)
		return nil
	}
	if s.pressed[source] == nil {
		s.pressed[source] = map[uint8]bool{}
	}
	if f.Action == uint8(remoteinput.ActionPress) {
		if s.pressed[source][usage] {
			return s.repostLocked(ctx)
		}
		s.pressed[source][usage] = true
	} else {
		if !s.pressed[source][usage] {
			return s.repostLocked(ctx)
		}
		delete(s.pressed[source], usage)
	}
	return s.postLocked(ctx)
}

// tap applies a session-owned remote key at the target. The runtime's row
// posts may themselves block or fail, so a successful return confirms only
// accepted row posts, not guest timing. A changed generation is never sent a
// release from the previous core.
func (s *keyboardHIDSink) tap(ctx context.Context, f protocol.InputFrame, afterPress func()) error {
	usage, ok := hidkeys.Usage(remoteinput.Code(f.Code))
	if !ok || f.ServerMonoNS == 0 || f.Value != protocol.InputTapHoldMS {
		return errTapUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pressCtx, cancelPress := context.WithTimeout(ctx, 500*time.Millisecond)
	if err := s.lockTap(pressCtx); err != nil {
		cancelPress()
		return err
	}
	if s.focused || s.focusFence.Load() || s.blocked[sourceRemote][usage] || s.binding == nil || s.binding.Generation != f.ServerMonoNS || s.poster == nil || s.heldUsageLocked(usage) || s.tapInFlight[usage] != 0 {
		s.mu.Unlock()
		cancelPress()
		return errTapUnavailable
	}
	binding := *s.binding
	if s.tapHeld == nil {
		s.tapHeld = make(map[uint8]bool)
	}
	if s.tapInFlight == nil {
		s.tapInFlight = make(map[uint8]uint64)
	}
	s.tapSequence++
	if s.tapSequence == 0 {
		s.tapSequence++
	}
	tapID := s.tapSequence
	s.tapHeld[usage] = true
	s.tapInFlight[usage] = tapID
	pressErr := s.postLocked(pressCtx)
	s.mu.Unlock()
	cancelPress()
	if afterPress != nil {
		afterPress()
	}
	if pressErr == nil {
		timer := time.NewTimer(time.Duration(protocol.InputTapHoldMS) * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			pressErr = ctx.Err()
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
	// Release even after a cancelled/uncertain press. Do not let caller
	// cancellation suppress it, and never affect a replacement binding.
	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRelease()
	if err := s.lockTap(releaseCtx); err != nil {
		return errors.Join(pressErr, err)
	}
	defer s.mu.Unlock()
	if s.binding == nil || *s.binding != binding || s.tapInFlight[usage] != tapID {
		return errors.Join(pressErr, errTapUnavailable)
	}
	heldByTap := s.tapHeld[usage]
	delete(s.tapHeld, usage)
	delete(s.tapInFlight, usage)
	if !heldByTap || s.focused || s.focusFence.Load() {
		return errors.Join(pressErr, errTapUnavailable)
	}
	if s.heldUsageLocked(usage) {
		// A local press began during the tap. Preserve it, but do not claim
		// that a release edge reached the target while the union is held.
		pressErr = errors.Join(pressErr, errTapUnavailable)
	}
	return errors.Join(pressErr, s.postLocked(releaseCtx))
}

func (s *keyboardHIDSink) heldUsageLocked(usage uint8) bool {
	for _, pressed := range s.pressed {
		if pressed[usage] {
			return true
		}
	}
	return false
}

func (s *keyboardHIDSink) lockTap(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !s.mu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}

// releaseSource drops one source's held keys, for example a closed kit
// socket or a disconnected host stream.
func (s *keyboardHIDSink) releaseSource(source inputSource) error {
	return s.releaseSourceContext(context.Background(), source, nil)
}

// releaseSourceContext optionally refuses to post into a replacement binding.
func (s *keyboardHIDSink) releaseSourceContext(ctx context.Context, source inputSource, expected *KeyboardHIDBinding) error {
	if source >= sourceCount {
		source = sourceRemote
	}
	if err := s.lockTap(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if expected != nil && (s.binding == nil || *s.binding != *expected) {
		return nil
	}
	s.blocked[source] = nil
	s.shiftAmbiguous[source] = false
	changed := len(s.pressed[source]) != 0
	s.pressed[source] = nil
	if source == sourceRemote && len(s.tapHeld) != 0 {
		// A timed tap belongs to the host stream. Disconnect retires its
		// contribution, but its in-flight token stays until its goroutine
		// observes retirement, so it cannot release a replacement tap.
		s.tapHeld = nil
		changed = true
	}
	if s.focused || s.focusFence.Load() {
		return nil
	}
	if !changed {
		return s.repostLocked(ctx)
	}
	return s.postLocked(ctx)
}

// releaseAll neutralizes every key and forgets the binding. Neutralization is
// best-effort: after Stop the runtime rejects the retired generation, and the
// runtime already treats its input state as neutral after Stop.
func (s *keyboardHIDSink) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pressed = [sourceCount]map[uint8]bool{}
	s.blocked = [sourceCount]map[uint8]bool{}
	s.shiftAmbiguous = [sourceCount]bool{}
	s.tapHeld = nil
	s.tapInFlight = nil
	s.focused = false
	s.focusFence.Store(false)
	if s.dirty {
		_ = s.postLocked(context.Background())
	}
	s.binding = nil
	s.dirty = false
	s.unconfirmed = false
}
