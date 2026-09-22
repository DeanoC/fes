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
	replace int
	clear   int
	err     *protocol.APIError
}

func (r *liveRuntime) ReplaceLiveMedia(_ context.Context, _ int64, _ io.Reader, _ protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.replace++
	return r.err
}

func (r *liveRuntime) ClearLiveMedia(_ context.Context, _ protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.clear++
	return r.err
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
