//go:build linux

package tenfoot

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// nativeKeyboardService is created only by the explicit kit-host opt-in.
// Its context is independent of the display; shutdown retires sources and
// awaits confirmation before canceling it. It owns one worker goroutine.
type nativeKeyboardService struct {
	app       *App
	feed      *localfeed.Feed
	publisher [16]byte
	cancel    context.CancelFunc
	done      chan error
}

// configureNativeKeyboardService performs no device or network I/O beyond
// cryptographic identity generation. Inputs are already open by the owner.
// Feed reservation and gate installation happen before the first input poll.
func configureNativeKeyboardService(a *App, inputs *nativeInputs, enabled bool) (*nativeKeyboardService, error) {
	if !enabled {
		return nil, nil
	}
	if a == nil || inputs == nil {
		return nil, errNativeGuestState
	}
	a.mu.Lock()
	feed, ok := a.localFeed.(*localfeed.Feed)
	allowed := a.kitHost && a.client != nil && !a.client.paired && ok && feed != nil
	a.mu.Unlock()
	if !allowed {
		return nil, errors.New("native keyboard v2 requires the local kit host")
	}
	var publisher [16]byte
	if _, err := rand.Read(publisher[:]); err != nil || publisher == ([16]byte{}) {
		return nil, errors.New("native keyboard publisher unavailable")
	}
	if err := feed.ReserveV2(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeKeyboardQuarantined = true
	if a.nativeKeyboardGeneration == 0 {
		a.nativeKeyboardGeneration = 1
	}
	a.mu.Unlock()
	gate := &nativeKeyboardGate{boundary: a, publisher: publisher}
	if err := inputs.installNativeKeyboardGate(gate); err != nil {
		return nil, err
	}
	return &nativeKeyboardService{app: a, feed: feed, publisher: publisher}, nil
}

func (s *nativeKeyboardService) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan error, 1)
	go func() {
		previousAgent := ""
		err := runNativeKeyboardWorker(ctx, s.app.nativeKeyboardWake, 250*time.Millisecond, func(ctx context.Context) (bool, error) {
			s.app.mu.Lock()
			padPending, padEpoch, padBinding := s.app.nativePadRetiring, s.app.nativePadRetirementEpoch, s.app.nativePadRetirementBinding
			if padPending {
				s.app.mu.Unlock()
				agent, err := s.drainPad(ctx, padEpoch, padBinding)
				if err != nil {
					return false, err
				}
				previousAgent = agent
				return true, nil
			}
			if s.app.nativeKeyboardClosing {
				s.app.mu.Unlock()
				return false, errNativeGuestState
			}
			if s.app.nativeGuest.phase == nativeGuestUncertain {
				previousAgent = s.app.nativeGuest.binding.InputAgentInstance
			}
			if previousAgent == "" {
				previousAgent = s.app.session.InputAgentInstance
			}
			agent := previousAgent
			s.app.mu.Unlock()
			if !protocol.ValidInputAgentInstance(agent) {
				return false, errNativeGuestState
			}
			if pad, err := s.app.padOnlyAuthority(true); err == nil {
				runner := &padOnlyRecovery{app: s.app, feed: s.feed, native: true}
				handle, err := runner.recover(ctx, pad, agent)
				if err != nil {
					return false, err
				}
				previousAgent = pad.AgentInstance
				s.app.mu.Lock()
				current, authorityErr := s.app.padOnlyAuthorityLocked(time.Now(), true)
				if authorityErr == nil && current == pad {
					authorityErr = s.app.nativePadStartMappingLocked(s.app.nativePadRetirementEpoch, pad)
				}
				if authorityErr != nil || current != pad {
					s.app.nativePadRevokeConnectionLocked()
					s.app.mu.Unlock()
					s.feed.CloseV2(handle)
					return true, nil
				}
				s.app.mu.Unlock()
				s.monitorPad(ctx, handle, pad)
				return true, nil
			}
			handle, binding, err := s.app.nativeKeyboardRecoverOnce(ctx, s.feed, s.publisher, agent, time.Second)
			if err != nil {
				return false, err
			}
			previousAgent = binding.InputAgentInstance
			err = s.app.runNativeKeyboardConnection(ctx, s.feed, handle, binding, 250*time.Millisecond)
			return false, err
		})
		s.done <- err
	}()
}

// monitorPad owns the promoted pad-only connection until authority changes,
// shutdown or EOF. Its exact source remains marked retiring after close.
func (s *nativeKeyboardService) monitorPad(ctx context.Context, handle localfeed.V2Handle, expected padOnlyAuthority) {
	defer s.feed.CloseV2(handle)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		s.app.mu.Lock()
		current, err := s.app.padOnlyAuthorityLocked(time.Now(), true)
		epoch := s.app.nativePadRetirementEpoch
		if err != nil || current != expected || !s.app.nativePadRetiring || s.app.nativePadRetirementBinding != expected || s.app.nativePadConnectionFence.Revoked() {
			s.app.nativePadRevokeConnectionLocked()
			s.app.mu.Unlock()
			return
		}
		if !s.app.nativeKeyboardGuestFocusLocked() && (len(s.app.nativeMappedPad.keys) != 0 || len(s.app.nativeMappedPad.queue) != 0) {
			s.app.nativePadFenceMappingLocked()
		}
		action, ready := s.app.nativeMappedPad.next(epoch, expected)
		s.app.mu.Unlock()
		if ready {
			if action.retire {
				err = s.feed.RetireNativeMappedPad(ctx, handle, epoch, action.device)
			} else {
				err = s.feed.SendNativeMappedPad(ctx, handle, epoch, action.mappingFence, action.deviceFence, action.device, action.raw, action.event)
			}
			s.app.mu.Lock()
			completed := s.app.nativePadCompleteMappedLocked(action, err)
			s.app.mu.Unlock()
			if err != nil || !completed {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-handle.Done():
			return
		case <-s.app.nativeKeyboardWake:
		case <-tick.C:
		}
	}
}

// drainPad connects only after the pad handle was closed. Positive same-agent
// recovery is correlated with the exact old promotion epoch and identity;
// timeout or changed-agent ambiguity leaves nativePadRetiring set.
func (s *nativeKeyboardService) drainPad(ctx context.Context, epoch uint64, previous padOnlyAuthority) (string, error) {
	reply, handle, err := s.feed.RecoverV2(ctx, previous.AgentInstance, previous.TargetID, previous.BootID, time.Second)
	if err != nil {
		return "", err
	}
	defer s.feed.CloseV2(handle)
	if err := s.app.nativeKeyboardConfirmPadDrain(epoch, previous, reply); err != nil {
		return "", err
	}
	return reply.AgentInstance, nil
}

// stop is called only after the bounded retirement attempt. Cancellation of
// the worker never counts as source retirement or a neutral acknowledgment.
func (s *nativeKeyboardService) stop(ctx context.Context) error {
	if s == nil || s.cancel == nil || s.done == nil {
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
