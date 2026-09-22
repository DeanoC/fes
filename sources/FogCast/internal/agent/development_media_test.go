package agent_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaRuntime struct {
	fakeRuntime
	calls            int
	started, release chan struct{}
	failure          *protocol.APIError
}

type ownedMediaRuntime struct {
	mediaRuntime
	stage func(context.Context, context.Context, int64, io.Reader) *protocol.APIError
}

func (r *ownedMediaRuntime) LoadDevelopmentMediaOwned(admission, owner context.Context, n int64, body io.Reader, b protocol.DevelopmentMediaBinding) *protocol.APIError {
	return r.stage(admission, owner, n, body)
}

func TestMediaStreamCoordinatorKeepsAdmissionCancellableAndOwnerAlive(t *testing.T) {
	status := mediaStatus()
	status.CorePackage.ActiveInterfaces = append(status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.MediaStreamInterface().ID, Major: 1})
	status.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
	runtime := &ownedMediaRuntime{mediaRuntime: mediaRuntime{fakeRuntime: fakeRuntime{reconciled: status}}}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	runtime.stage = func(admission, owner context.Context, n int64, body io.Reader) *protocol.APIError {
		close(started)
		<-admission.Done()
		if owner.Err() != nil {
			t.Error("request cancellation revoked mutation owner")
		}
		<-release
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Phase: "admission", Message: "cancelled", Cause: admission.Err()}
	}
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.LoadDevelopmentMedia(ctx, 32768, strings.NewReader(strings.Repeat("x", 32768)), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Stream: true})
		done <- err
	}()
	<-started
	cancel()
	if _, err := c.Stop(context.Background()); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("ownership lost: %v", err)
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cause lost: %v", err)
	}
}

func (r *mediaRuntime) LoadDevelopmentMedia(ctx context.Context, n int64, b io.Reader, binding protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.calls++
	if r.started != nil {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	}
	return r.failure
}

func TestDevelopmentMediaReportsExistingRuntimeRecovery(t *testing.T) {
	runtime := &mediaRuntime{fakeRuntime: fakeRuntime{reconciled: mediaStatus()}, failure: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "transport failed", Phase: "transport"}}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	runtime.reconciled.State = protocol.StateFailed
	runtime.reconciled.Recovery = protocol.RecoveryRebootRequired
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
	status, err := c.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b)
	if err == nil || status.Recovery != protocol.RecoveryRebootRequired || status.State != protocol.StateFailed || runtime.calls != 1 {
		t.Fatalf("status=%+v error=%v calls=%d", status, err, runtime.calls)
	}
	if _, err := c.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || runtime.calls != 1 {
		t.Fatal("recovery state admitted another media mutation")
	}
}
func mediaStatus() protocol.Status {
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
}
func TestDevelopmentMediaCoordinatorAdmissionAndSerialization(t *testing.T) {
	runtime := &mediaRuntime{fakeRuntime: fakeRuntime{reconciled: mediaStatus()}, started: make(chan struct{}), release: make(chan struct{})}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 8}
	if _, err := c.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || runtime.calls != 0 {
		t.Fatal("stale identity reached runtime")
	}
	b.Generation = 9
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b)
		done <- err
	}()
	<-runtime.started
	if _, err := c.Stop(context.Background()); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("Stop crossed media transfer: %v", err)
	}
	close(runtime.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status := c.Status(); !b.Matches(status) {
		t.Fatalf("media changed session: %+v", status)
	}
}
