package agent_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
)

type liveRuntime struct {
	fakeRuntime
	replace        int
	clear          int
	reconcileCalls int
	tearDown       bool
	err            *protocol.APIError
}

func (r *liveRuntime) ReplaceLiveMedia(_ context.Context, _ int64, _ io.Reader, _ protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.replace++
	return r.err
}

func (r *liveRuntime) ClearLiveMedia(_ context.Context, _ protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.clear++
	return r.err
}

func (r *liveRuntime) Reconcile(ctx context.Context) protocol.Status {
	r.reconcileCalls++
	status := r.fakeRuntime.Reconcile(ctx)
	if r.tearDown {
		status.State = protocol.StateFailed
		status.Recovery = protocol.RecoveryRebootRequired
		status.CorePackage = nil
	}
	return status
}

func TestLiveMediaCoordinatorAdmission(t *testing.T) {
	status := mediaStatus()
	status.CorePackage.Generation = 8
	runtime := &liveRuntime{fakeRuntime: fakeRuntime{reconciled: status}}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 8}
	if _, err := c.ReplaceLiveMedia(context.Background(), 1, strings.NewReader("x"), b); err != nil || runtime.replace != 1 {
		t.Fatalf("replace err=%v calls=%d", err, runtime.replace)
	}
	if _, err := c.ClearLiveMedia(context.Background(), b); err != nil || runtime.clear != 1 {
		t.Fatalf("clear err=%v calls=%d", err, runtime.clear)
	}
	stale := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 7}
	if _, err := c.ReplaceLiveMedia(context.Background(), 1, strings.NewReader("x"), stale); err == nil || runtime.replace != 1 {
		t.Fatalf("stale accepted: %v", err)
	}
	runtime.err = protocol.LiveMediaBusyError()
	if _, err := c.ReplaceLiveMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestClearLiveMediaUnavailableDoesNotReplaceSession(t *testing.T) {
	status := mediaStatus()
	status.CorePackage.Generation = 8
	runtime := &liveRuntime{fakeRuntime: fakeRuntime{reconciled: status}}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	runtime.reconcileCalls = 0
	runtime.tearDown = true
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 8}
	for _, apiErr := range []*protocol.APIError{
		{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable", Phase: "input"},
		{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable", Phase: "transfer"},
		protocol.LiveMediaBusyError(),
	} {
		runtime.err = apiErr
		before := runtime.reconcileCalls
		got, err := c.ClearLiveMedia(context.Background(), b)
		if err == nil || err.Code != apiErr.Code || runtime.reconcileCalls != before {
			t.Fatalf("err=%v reconciles=%d want code %s", err, runtime.reconcileCalls-before, apiErr.Code)
		}
		if got.State != protocol.StateActive || c.Status().State != protocol.StateActive || c.Status().CorePackage == nil {
			t.Fatalf("eject replaced session: %+v", c.Status())
		}
	}
	runtime.err = &protocol.APIError{Code: protocol.CodeInternal, Message: "clear exploded", Phase: "transfer"}
	got, err := c.ClearLiveMedia(context.Background(), b)
	if err == nil || err.Code != protocol.CodeInternal || runtime.reconcileCalls == 0 || got.State != protocol.StateFailed {
		t.Fatalf("non-eject failure should reconcile: err=%v status=%+v calls=%d", err, got, runtime.reconcileCalls)
	}
}
