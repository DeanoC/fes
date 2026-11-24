//go:build linux

package tenfoot

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// runNativeKeyboardWorker drives an injected, bounded step until its own
// lifetime ends. Display cancellation must not cancel this context before
// shutdown has attempted retirement. Idle and failed steps wait for a wake
// or retry timer; this helper starts no goroutine by itself.
func runNativeKeyboardWorker(ctx context.Context, wake <-chan struct{}, retry time.Duration, step func(context.Context) (bool, error)) error {
	if ctx == nil || wake == nil || retry <= 0 || step == nil {
		return errNativeGuestState
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := step(ctx)
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

type nativeRecoveryFeed interface {
	RecoverV2(context.Context, string, string, string, time.Duration) (localhid.Reply, localfeed.V2Handle, error)
	// PromoteRecovered must be nonblocking and must not call back into App.
	PromoteRecovered(localfeed.V2Handle, localhid.Reply) error
	CloseV2(localfeed.V2Handle)
}

type nativeConnectedFeed interface {
	RequestV2(context.Context, localfeed.V2Handle, localhid.Request) (localhid.Reply, error)
	CloseV2(localfeed.V2Handle)
}

// runNativeKeyboardConnection owns exactly one already-recovered connection.
// Its context is independent of the display lifetime. It starts no goroutine
// itself and is not installed by production. EOF is loss, never neutral.
func (a *App) runNativeKeyboardConnection(ctx context.Context, feed nativeConnectedFeed, handle localfeed.V2Handle, expected nativeGuestBinding, interval time.Duration) error {
	if ctx == nil || feed == nil || handle.Done() == nil || interval <= 0 {
		return errNativeGuestState
	}
	defer feed.CloseV2(handle)
	done := handle.Done()
	for {
		select {
		case <-ctx.Done():
			a.nativeKeyboardTransportLost(expected)
			return ctx.Err()
		case <-done:
			a.nativeKeyboardTransportLost(expected)
			return errNativeGuestState
		default:
		}
		current, observeErr := a.nativeKeyboardObserveBinding()
		if observeErr != nil || current != expected {
			a.nativeKeyboardAuthorityLost(expected)
		}
		worked, workErr := a.nativeKeyboardRunOne(ctx, expected, a.nativeKeyboardObserveBinding, func(ctx context.Context, req localhid.Request) (localhid.Reply, error) {
			return feed.RequestV2(ctx, handle, req)
		})
		select {
		case <-done:
			a.nativeKeyboardTransportLost(expected)
			return errNativeGuestState
		default:
		}
		// A ready indication is conditioned on fresh authority, a live exact
		// connection and a fully drained acknowledged queue.
		a.nativeKeyboardConfirmReady(expected)
		select {
		case <-done:
			a.nativeKeyboardTransportLost(expected)
			return errNativeGuestState
		default:
		}
		a.mu.Lock()
		same := a.nativeGuestBinding == expected
		phase := a.nativeGuest.phase
		waitingSnapshot := a.nativeGuestSnapshotAllowed
		pendingBegin := a.nativeGuestBeginRequest != nil
		a.mu.Unlock()
		if !same {
			return errNativeGuestState
		}
		if phase == nativeGuestUncertain {
			a.nativeKeyboardTransportLost(expected)
			return errNativeGuestState
		}
		if phase == nativeGuestIdle && !waitingSnapshot && !pendingBegin {
			return nil
		}
		if worked && workErr == nil {
			continue
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			a.nativeKeyboardTransportLost(expected)
			return ctx.Err()
		case <-done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			a.nativeKeyboardTransportLost(expected)
			return errNativeGuestState
		case <-a.nativeKeyboardWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

// nativeKeyboardRecoverOnce performs a synchronous recovery step. The
// caller owns the feed and must supply the original agent identity, never one
// inferred from a newer response. A successful return permits exactly one
// fresh physical snapshot; it does not claim BEGIN or guest input succeeded.
func (a *App) nativeKeyboardRecoverOnce(ctx context.Context, feed nativeRecoveryFeed, publisher [16]byte, previousAgent string, timeout time.Duration) (handle localfeed.V2Handle, binding nativeGuestBinding, err error) {
	if ctx == nil || feed == nil || timeout <= 0 {
		return handle, binding, errNativeGuestState
	}
	expected, err := a.nativeKeyboardPrepareBinding(publisher)
	if err != nil {
		return handle, binding, err
	}
	defer func() {
		if err != nil {
			feed.CloseV2(handle)
		}
	}()
	reply, handle, err := feed.RecoverV2(ctx, previousAgent, expected.TargetID, expected.BootID, timeout)
	if err != nil {
		return handle, binding, err
	}
	current, err := a.nativeKeyboardObserveBinding()
	if err != nil || current != expected || ctx.Err() != nil {
		if err == nil {
			err = errNativeGuestState
		}
		return handle, binding, err
	}
	// App authority cannot change between validation and the feed's
	// nonblocking promotion. A failure revokes the unobserved permit before
	// releasing App.mu; no snapshot can race the transaction.
	err = a.nativeKeyboardPromoteWithAuthority(ctx, feed, handle, expected, previousAgent, reply)
	if err != nil {
		return handle, binding, err
	}
	return handle, expected, nil
}

func (a *App) nativeKeyboardPromoteWithAuthority(ctx context.Context, feed nativeRecoveryFeed, handle localfeed.V2Handle, expected nativeGuestBinding, previousAgent string, reply localhid.Reply) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.nativeKeyboardPermitAfterRecoveryLocked(expected, previousAgent, reply); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		a.nativeGuestSnapshotAllowed = false
		return err
	}
	if err := feed.PromoteRecovered(handle, reply); err != nil {
		a.nativeGuestSnapshotAllowed = false
		return err
	}
	return nil
}
