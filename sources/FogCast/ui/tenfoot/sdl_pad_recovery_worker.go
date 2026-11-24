//go:build sdl3

package tenfoot

import (
	"context"
	"time"
)

type sdlPadRecovery struct {
	*padOnlyRecovery
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *sdlPadRecovery) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		previous := ""
		for ctx.Err() == nil {
			binding, err := s.app.padOnlyAuthority(s.native)
			if err == nil {
				if previous == "" {
					previous = binding.AgentInstance
				}
				handle, recoverErr := s.recover(ctx, binding, previous)
				if recoverErr == nil {
					previous = binding.AgentInstance
					tick := time.NewTicker(250 * time.Millisecond)
					alive := true
					for alive {
						select {
						case <-ctx.Done():
							alive = false
						case <-handle.Done():
							alive = false
						case <-tick.C:
							current, err := s.app.padOnlyAuthority(s.native)
							if err != nil || current != binding {
								alive = false
							}
						}
					}
					tick.Stop()
					s.feed.CloseV2(handle)
				}
			}
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-timer.C:
			}
		}
	}()
}

func (s *sdlPadRecovery) stop(ctx context.Context) error {
	if s == nil || s.cancel == nil {
		return nil
	}
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
