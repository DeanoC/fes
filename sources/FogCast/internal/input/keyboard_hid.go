package input

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
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
	mu          sync.Mutex
	poster      KeyboardHIDPoster
	binding     *KeyboardHIDBinding
	pressed     [sourceCount]map[uint8]bool
	dirty       bool
	unconfirmed bool
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
	if !s.unconfirmed || s.binding == nil || binding == nil || *s.binding != *binding {
		return nil
	}
	return s.postLocked(ctx)
}

// applyFrom records one HID key frame from a source and posts the new union.
func (s *keyboardHIDSink) applyFrom(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	usage, ok := hidkeys.Usage(remoteinput.Code(f.Code))
	if !ok || (f.Action != uint8(remoteinput.ActionPress) && f.Action != uint8(remoteinput.ActionRelease)) {
		return nil
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if s.binding == nil || s.binding.Generation != f.ServerMonoNS || s.poster == nil || s.pressed[sourceRemote][usage] {
		s.mu.Unlock()
		cancelPress()
		return errTapUnavailable
	}
	binding := *s.binding
	if s.pressed[sourceRemote] == nil {
		s.pressed[sourceRemote] = make(map[uint8]bool)
	}
	s.pressed[sourceRemote][usage] = true
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
	if s.binding == nil || *s.binding != binding {
		return errors.Join(pressErr, errTapUnavailable)
	}
	delete(s.pressed[sourceRemote], usage)
	return errors.Join(pressErr, s.postLocked(releaseCtx))
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
	if source >= sourceCount {
		source = sourceRemote
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pressed[source]) == 0 {
		return s.repostLocked(context.Background())
	}
	s.pressed[source] = map[uint8]bool{}
	return s.postLocked(context.Background())
}

// releaseAll neutralizes every key and forgets the binding. Neutralization is
// best-effort: after Stop the runtime rejects the retired generation, and the
// runtime already treats its input state as neutral after Stop.
func (s *keyboardHIDSink) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pressed = [sourceCount]map[uint8]bool{}
	if s.dirty {
		_ = s.postLocked(context.Background())
	}
	s.binding = nil
	s.dirty = false
	s.unconfirmed = false
}
