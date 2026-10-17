package kitsoftware

import (
	"context"
	"log"
	"time"
)

// SuperviseUntilSignal runs once until it returns nil or ctx is cancelled.
// A startup error is logged and retried with backoff. The process stays up so
// a stalled data disk cannot exit the supervisor and take the error with it.
func SuperviseUntilSignal(ctx context.Context, once func(context.Context) error, wait func(context.Context, time.Duration) error) error {
	var backoff time.Duration
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		err := once(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		log.Printf("software supervisor: %v", err)
		backoff = NextSupervisorBackoff(backoff)
		if err := wait(ctx, backoff); err != nil {
			return nil
		}
	}
}

// NextSupervisorBackoff grows from one second and stops at thirty seconds.
func NextSupervisorBackoff(current time.Duration) time.Duration {
	if current < time.Second {
		return time.Second
	}
	next := current * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}
