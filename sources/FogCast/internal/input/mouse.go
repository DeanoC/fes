package input

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type MouseBinding struct {
	PackageID  string
	Generation uint64
}
type MousePoster func(context.Context, string, uint64, int16, int16, uint8) error

// Mouse motion is transient. Only button snapshots may be retried or released.
// The two existing input sources contribute their button holds independently.
type mouseReconciliation struct {
	cancel context.CancelFunc
}

type mouseSink struct {
	mu          sync.Mutex
	poster      MousePoster
	binding     *MouseBinding
	buttons     [sourceCount]uint8
	unconfirmed bool
	reconcile   *mouseReconciliation
}

func (s *mouseSink) lock(ctx context.Context) error {
	if ctx == nil || ctx.Done() == nil {
		s.mu.Lock()
		return nil
	}
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
func (s *mouseSink) bindContext(ctx context.Context, binding *MouseBinding) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if binding != nil && s.binding != nil && *binding == *s.binding {
		return nil
	}
	s.cancelReconciliationLocked()
	s.binding = nil
	if binding != nil {
		copy := *binding
		s.binding = &copy
	}
	s.buttons = [sourceCount]uint8{}
	s.unconfirmed = false
	return nil
}
func (s *mouseSink) postLocked(ctx context.Context, dx, dy int16) error {
	if s.binding == nil || s.poster == nil {
		return nil
	}
	s.unconfirmed = true
	err := s.poster(ctx, s.binding.PackageID, s.binding.Generation, dx, dy, s.buttons[0]|s.buttons[1])
	if err == nil {
		s.unconfirmed = false
		s.cancelReconciliationLocked()
	} else if mouseBusy(err) {
		s.scheduleReconciliationLocked()
	} else {
		// An ambiguous transport failure must not start an automatic retry.
		s.cancelReconciliationLocked()
	}
	return err
}

func mouseBusy(err error) bool {
	var rejected *protocol.APIError
	return errors.As(err, &rejected) && rejected.Code == protocol.CodeBusy
}

func (s *mouseSink) cancelReconciliationLocked() {
	if s.reconcile != nil {
		s.reconcile.cancel()
		s.reconcile = nil
	}
}

// Busy is a confirmed pre-dispatch rejection. Retain only the latest absolute
// buttons while a disk capture runs; discarded relative motion is never queued.
// Every attempt keeps the original package generation. The lifetime covers the
// longest admitted disk replacement; each individual call remains bounded.
func (s *mouseSink) scheduleReconciliationLocked() {
	if s.reconcile != nil || s.binding == nil || s.poster == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Second)
	pending := &mouseReconciliation{cancel: cancel}
	binding := *s.binding
	s.reconcile = pending
	go func() {
		defer cancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		defer func() {
			s.mu.Lock()
			if s.reconcile == pending {
				s.reconcile = nil
			}
			s.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if s.lock(ctx) != nil {
				return
			}
			if s.reconcile != pending || s.binding == nil || *s.binding != binding {
				s.mu.Unlock()
				return
			}
			attempt, stop := context.WithTimeout(ctx, 2*time.Second)
			err := s.postLocked(attempt, 0, 0)
			stop()
			s.mu.Unlock()
			if !mouseBusy(err) {
				return
			}
		}
	}()
}
func (s *mouseSink) applyFrom(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	dx, dy, buttons, ok := remoteinput.MouseVector(frameEvent(f))
	if !ok {
		return nil
	}
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if source >= sourceCount {
		source = sourceRemote
	}
	if s.binding == nil {
		return nil
	}
	changed := s.buttons[source] != buttons
	s.buttons[source] = buttons
	if dx == 0 && dy == 0 && !changed && !s.unconfirmed {
		return nil
	}
	return s.postLocked(ctx, dx, dy)
}
func (s *mouseSink) releaseSource(source inputSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelReconciliationLocked()
	if source >= sourceCount {
		source = sourceRemote
	}
	changed := s.buttons[source] != 0
	s.buttons[source] = 0
	if !changed && !s.unconfirmed {
		return nil
	}
	return s.postLocked(context.Background(), 0, 0)
}
func (s *mouseSink) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelReconciliationLocked()
	dirty := s.buttons[0]|s.buttons[1] != 0 || s.unconfirmed
	s.buttons = [sourceCount]uint8{}
	if dirty {
		_ = s.postLocked(context.Background(), 0, 0)
	}
	s.cancelReconciliationLocked()
	s.binding = nil
	s.unconfirmed = false
}

// neutral waits for any earlier post, then clears both input sources while
// keeping the generation binding for play after a display-focus transition.
func (s *mouseSink) neutral(ctx context.Context) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	s.cancelReconciliationLocked()
	dirty := s.buttons[0]|s.buttons[1] != 0 || s.unconfirmed
	s.buttons = [sourceCount]uint8{}
	if dirty {
		return s.postLocked(ctx, 0, 0)
	}
	return nil
}
