package hostapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/internal/mediasession"
)

type adapterTestComponent struct {
	handle *adapterTestHandle
	err    error
}

func (c adapterTestComponent) Start(context.Context, string) (mediasession.ComponentHandle, error) {
	if c.handle == nil {
		return nil, c.err
	}
	return c.handle, c.err
}

type adapterTestHandle struct {
	mu      sync.Mutex
	stopErr error
	stops   int
}

func (h *adapterTestHandle) Stop(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stops++
	return h.stopErr
}

func TestMediaSessionAdapterPreservesPartialHandleOnStartFailure(t *testing.T) {
	cleanupErr := errors.New("initial cleanup failed")
	started := &adapterTestHandle{stopErr: cleanupErr}
	session := mediasession.New(
		adapterTestComponent{err: errors.New("receiver start failed")},
		adapterTestComponent{handle: started},
	)
	adapted := NewMediaSessionAdapter(session)
	handle, err := adapted.Start(context.Background(), "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle %v err %v, want retryable partial handle", handle, err)
	}
	started.mu.Lock()
	started.stopErr = nil
	started.mu.Unlock()
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("partial handle retry = %v", err)
	}
	started.mu.Lock()
	stops := started.stops
	started.mu.Unlock()
	if stops != 2 {
		t.Fatalf("component stop attempts = %d, want failed rollback plus retry", stops)
	}
}
