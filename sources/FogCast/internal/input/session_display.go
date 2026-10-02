package input

import (
	"context"
	"errors"
	"sync"
)

// BeginSessionDisplay waits for local delivery and keyboard posts, then keeps
// the same input lifecycle fence through the physical display transition.
// It does not detach a host lease or program a core.
func (c *TargetController) BeginSessionDisplay(ctx context.Context) (func(bool), error) {
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
	c.ports.setDisplayFocus(true)
	var once sync.Once
	return func(focused bool) {
		once.Do(func() {
			c.ports.setDisplayFocus(focused)
			c.lifecycle.Unlock()
		})
	}, nil
}

func (s *controllerPortsSink) setDisplayFocus(focused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.displayFocused = focused
	if s.keys != nil {
		s.keys.setDisplayFocus(focused)
	}
}
