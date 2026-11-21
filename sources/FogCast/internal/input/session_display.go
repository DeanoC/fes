package input

import (
	"context"
	"errors"
	"sync"
	"time"
)

// BeginSessionDisplay waits for local delivery and keyboard posts, then keeps
// the same input lifecycle fence through the physical display transition.
// Only a request for launcher focus retires held keys and neutralizes the
// mouse, matching the runtime's neutral post; a surround shown or closed under
// machine focus keeps held machine input. It does not detach a host lease or
// program a core.
func (c *TargetController) BeginSessionDisplay(ctx context.Context, launcher bool) (func(bool), error) {
	if c == nil || ctx == nil || c.ports == nil || c.keyboard == nil {
		return nil, errors.New("session display input focus is unavailable")
	}
	c.lifecycle.Lock()
	if ctx.Err() != nil {
		c.lifecycle.Unlock()
		return nil, ctx.Err()
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		c.lifecycle.Unlock()
		return nil, errors.New("input controller is closed")
	}
	if launcher {
		if err := c.ports.setDisplayFocusContext(ctx, true); err != nil {
			c.lifecycle.Unlock()
			return nil, err
		}
		if c.ports.mouse != nil {
			if err := c.ports.mouse.neutral(ctx); err != nil {
				c.ports.finishDisplayFocus(false)
				c.lifecycle.Unlock()
				return nil, err
			}
		}
	}
	var once sync.Once
	return func(focused bool) {
		once.Do(func() {
			c.ports.finishDisplayFocus(focused)
			c.lifecycle.Unlock()
		})
	}, nil
}

// finishDisplayFocus reconciles a pending neutral when machine focus is
// reported. A refused focus may have left old physical rows held even if the
// matching key-up arrived while the transition was fenced. Failed posts stay
// unconfirmed for the existing observation/release retry path.
func (s *controllerPortsSink) finishDisplayFocus(focused bool) {
	s.mu.Lock()
	wasFocused := s.displayFocused
	if focused && !wasFocused {
		// Unknown physical outcomes must fence immediately, even if a
		// previously admitted HID post holds its mutex past our deadline.
		s.displayFocused = true
		s.displayFocusEpoch++
		if s.hid != nil {
			s.hid.fenceEpoch.Store(s.displayFocusEpoch)
			s.hid.focusFence.Store(true)
		}
		if s.keys != nil {
			s.keys.setDisplayFocus(true)
		}
	}
	epoch := s.displayFocusEpoch
	var binding *KeyboardHIDBinding
	if s.hidBinding != nil {
		copy := *s.hidBinding
		binding = &copy
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	settleLater := func() {
		if focused && !wasFocused && s.hid != nil && binding != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = s.hid.settleUnknownFocus(ctx, epoch, *binding)
			}()
		}
	}
	if err := s.setDisplayFocusContext(ctx, focused); err != nil {
		// A previous HID post can finish after an unknown physical result.
		// Keep the fence and neutralize once that post drains, bounded and
		// only while the same focus epoch and generation remain active.
		settleLater()
		return
	}
	if focused && !wasFocused && s.hid != nil && binding != nil {
		// A post may also have drained just before the bounded wait ended.
		if err := s.hid.settleUnknownFocus(ctx, epoch, *binding); err != nil {
			settleLater()
		}
	}
	if wasFocused && !focused && s.hid != nil {
		_ = s.hid.reconcileFocusNeutral(ctx)
	}
}

func (s *controllerPortsSink) setDisplayFocus(focused bool) {
	_ = s.setDisplayFocusContext(context.Background(), focused)
}

func (s *controllerPortsSink) setDisplayFocusContext(ctx context.Context, focused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	epoch := s.displayFocusEpoch
	if s.displayFocused != focused {
		epoch++
	}
	if s.hid != nil {
		if err := s.hid.setDisplayFocus(ctx, focused, epoch); err != nil {
			return err
		}
	}
	s.displayFocusEpoch = epoch
	s.displayFocused = focused
	if s.keys != nil {
		s.keys.setDisplayFocus(focused)
	}
	return nil
}
