package input

import (
	"context"
	"sync"
	"time"
)

type matrixGuardContextKey struct{}

func withMatrixGuard(ctx context.Context, guard func(context.Context) error) context.Context {
	return context.WithValue(ctx, matrixGuardContextKey{}, guard)
}

func matrixGuardFrom(ctx context.Context) func(context.Context) error {
	if ctx == nil {
		return nil
	}
	guard, _ := ctx.Value(matrixGuardContextKey{}).(func(context.Context) error)
	return guard
}

// lockInputMutex bounds lock acquisition by the calling lifecycle operation.
func lockInputMutex(ctx context.Context, mu *sync.Mutex) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil {
		mu.Lock()
		return nil
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !mu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	if err := ctx.Err(); err != nil {
		mu.Unlock()
		return err
	}
	return nil
}
