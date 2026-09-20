package input

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type barrierPackageControl struct {
	beforeLoad func()
}

func (*barrierPackageControl) Status(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (*barrierPackageControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected launch")
}
func (*barrierPackageControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected raw load")
}
func (*barrierPackageControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (*barrierPackageControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (*barrierPackageControl) InspectCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		InspectedPackage: &misterruntime.Protocol2Inspection{PackageID: packageID,
			Descriptor: inspection.Descriptor, Compatible: true}}, nil
}
func (c *barrierPackageControl) LoadCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if c.beforeLoad != nil {
		c.beforeLoad()
	}
	generation := uint64(1)
	core := inspection.Descriptor.Core.ID
	build := inspection.Descriptor.Build.ID
	return misterruntime.Protocol2Response{Protocol: 2, OK: true,
		State: "running_development", Execution: "development", Version: "test",
		Core: &core, Generation: &generation,
		Capabilities: misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{
			{ID: "fes.gamepad", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1},
		}},
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: packageID,
			Descriptor: inspection.Descriptor, Observed: misterruntime.Protocol2Observed{
				ABI: &misterruntime.Protocol2Contract{ID: "fes.simple-game", Major: 1}, BuildID: &build}},
	}, nil
}

type persistentTestSink struct {
	mu              sync.Mutex
	pressed         bool
	events          int
	released        int
	releaseAttempts int
	closed          int
	releaseErr      error
}

type blockingPersistentSink struct {
	persistentTestSink
	entered chan struct{}
	allow   chan struct{}
	once    sync.Once
}

func (s *blockingPersistentSink) Apply(frame protocol.InputFrame) error {
	s.once.Do(func() { close(s.entered) })
	<-s.allow
	return s.persistentTestSink.Apply(frame)
}

func (s *persistentTestSink) Apply(protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pressed = true
	s.events++
	return nil
}

func (s *persistentTestSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseAttempts++
	if s.releaseErr != nil {
		return s.releaseErr
	}
	s.pressed = false
	s.released++
	return nil
}

func TestPersistentTargetControllerBlocksSuccessorUntilFailedReleaseRecovers(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: token, Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Detach(context.Background(), 1); !errors.Is(err, releaseErr) {
		t.Fatalf("Detach error = %v, want release failure", err)
	}
	if err := controller.Attach(context.Background(), Spec{Session: 2, Token: token, Core: "MegaDrive"}); !errors.Is(err, releaseErr) {
		t.Fatalf("successor Attach error = %v, want pending release failure", err)
	}
	if _, err := controller.OpenStream(context.Background(), 2); err == nil {
		t.Fatal("successor lease became active before neutralization")
	}
	sink.mu.Lock()
	sink.releaseErr = nil
	sink.mu.Unlock()
	if err := controller.Attach(context.Background(), Spec{Session: 2, Token: token, Core: "MegaDrive"}); err != nil {
		t.Fatalf("successor Attach after neutralization = %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentTargetControllerCloseReportsReleaseFailure(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("Close error = %v, want release failure", err)
	}
}

func TestTargetControllerReplacementBarrierRetiresAndRestoresExactLease(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	t.Cleanup(func() { _ = controller.Close() })
	spec := Spec{Session: 41, Token: []byte("0123456789abcdef"), Core: "Pong"}
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	finish, err := controller.BeginCoreReplacement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.OpenStream(context.Background(), spec.Session); err == nil {
		t.Fatal("retired stream remained open during replacement")
	}
	sink.mu.Lock()
	releases := sink.released
	sink.mu.Unlock()
	if releases == 0 {
		t.Fatal("replacement did not neutralize the persistent sink")
	}
	if err := finish(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	connection, err := controller.OpenStream(context.Background(), spec.Session)
	if err != nil {
		t.Fatalf("exact lease was not restored: %v", err)
	}
	_ = connection.Close()

	finish, err = controller.BeginCoreReplacement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.OpenStream(context.Background(), spec.Session); err == nil {
		t.Fatal("retired replacement lease became available again")
	}
}

func TestTargetControllerReplacementBarrierWaitsForInflightProducerAndRejectsOldFrames(t *testing.T) {
	sink := &blockingPersistentSink{entered: make(chan struct{}), allow: make(chan struct{})}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	t.Cleanup(func() { _ = controller.Close() })
	token := []byte("0123456789abcdef")
	spec := Spec{Session: 51, Token: token, Core: "Pong"}
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	connection, err := controller.OpenStream(context.Background(), spec.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection,
		"{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n",
		spec.Session, spec.Core, hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	frame := protocol.InputFrame{Header: protocol.InputHeader{
		Type: protocol.InputTypeInput, Session: spec.Session},
		Seq: 1, Device: 1, Kind: 1, Action: 1, Code: protocol.InputCodeButtonC}
	if err := bridge.WriteFrame(connection, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("old producer did not enter the persistent sink")
	}
	type barrierResult struct {
		finish func(context.Context, bool) error
		err    error
	}
	barrier := make(chan barrierResult, 1)
	go func() {
		finish, err := controller.BeginCoreReplacement(context.Background())
		barrier <- barrierResult{finish: finish, err: err}
	}()
	select {
	case result := <-barrier:
		t.Fatalf("barrier returned before in-flight sink write: %v", result.err)
	case <-time.After(20 * time.Millisecond):
	}
	close(sink.allow)
	var result barrierResult
	select {
	case result = <-barrier:
	case <-time.After(time.Second):
		t.Fatal("barrier did not finish after the sink write completed")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	frame.Seq = 2
	_ = bridge.WriteFrame(connection, frame)
	time.Sleep(20 * time.Millisecond)
	sink.mu.Lock()
	events, released := sink.events, sink.released
	sink.mu.Unlock()
	if events != 1 || released == 0 {
		t.Fatalf("old producer crossed barrier: events=%d releases=%d", events, released)
	}
	if err := result.finish(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCoreReplacementFencesActualTargetProducerUntilFreshLease(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	t.Cleanup(func() { _ = controller.Close() })
	token := []byte("0123456789abcdef")
	old := Spec{Session: 61, Token: token, Core: "MegaDrive"}
	if err := controller.Attach(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	connection, err := controller.OpenStream(context.Background(), old.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection,
		"{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n",
		old.Session, old.Core, hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	frame := protocol.InputFrame{Header: protocol.InputHeader{
		Type: protocol.InputTypeInput, Session: old.Session},
		Seq: 1, Device: 1, Kind: 1, Action: 1, Code: protocol.InputCodeButtonC}
	if err := bridge.WriteFrame(connection, frame); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return s.events == 1 })

	archive := barrierCoreArchive(t)
	control := &barrierPackageControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithCorePackageRoot(t.TempDir()),
		misterruntime.WithCoreReplacementBarrier(controller))
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background()) })
	if _, attempted, apiErr := runtime.LoadCoreOwned(context.Background(),
		context.Background(), context.Background(), 3, bytes.NewReader([]byte("bad"))); apiErr == nil || attempted {
		t.Fatalf("invalid package attempted=%t error=%#v", attempted, apiErr)
	}
	frame.Seq = 2
	if err := bridge.WriteFrame(connection, frame); err != nil {
		t.Fatalf("invalid package retired the old connection: %v", err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return s.events == 2 })
	control.beforeLoad = func() {
		if _, err := controller.OpenStream(context.Background(), old.Session); err == nil {
			t.Error("old input lease was available during runtime mutation")
		}
		frame.Seq = 3
		_ = bridge.WriteFrame(connection, frame)
		time.Sleep(20 * time.Millisecond)
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if sink.events != 2 || sink.pressed || sink.released == 0 {
			t.Errorf("old producer crossed runtime barrier: events=%d pressed=%t releases=%d",
				sink.events, sink.pressed, sink.released)
		}
	}
	activation, attempted, apiErr := runtime.LoadCoreOwned(context.Background(),
		context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || !attempted || activation.Generation != 1 {
		t.Fatalf("activation=%#v attempted=%t error=%#v", activation, attempted, apiErr)
	}
	if _, err := controller.OpenStream(context.Background(), old.Session); err == nil {
		t.Fatal("old lease was restored after successful replacement")
	}

	fresh := Spec{Session: 62, Token: token, Core: "Pong"}
	if err := controller.Attach(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	freshConnection, err := controller.OpenStream(context.Background(), fresh.Session)
	if err != nil {
		t.Fatal(err)
	}
	defer freshConnection.Close()
	if _, err := fmt.Fprintf(freshConnection,
		"{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n",
		fresh.Session, fresh.Core, hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	frame.Header.Session = fresh.Session
	frame.Seq = 1
	if err := bridge.WriteFrame(freshConnection, frame); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return s.events == 3 })
}

func TestTargetControllerReplacementBarrierFailsClosedOnPauseAndResume(t *testing.T) {
	t.Run("pause", func(t *testing.T) {
		sink := &persistentTestSink{}
		controller := newTargetControllerWithSink("127.0.0.1:0", sink)
		t.Cleanup(func() { _ = controller.Close() })
		spec := Spec{Session: 7, Token: []byte("0123456789abcdef"), Core: "Pong"}
		if err := controller.Attach(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		sink.mu.Lock()
		sink.releaseErr = errors.New("neutralization failed")
		sink.mu.Unlock()
		if _, err := controller.BeginCoreReplacement(context.Background()); err == nil {
			t.Fatal("replacement ignored failed neutralization")
		}
		if _, err := controller.OpenStream(context.Background(), spec.Session); err == nil {
			t.Fatal("failed pause left old stream eligible")
		}
	})

	t.Run("resume", func(t *testing.T) {
		controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
		t.Cleanup(func() { _ = controller.Close() })
		spec := Spec{Session: 8, Token: []byte("0123456789abcdef"), Core: "Pong"}
		if err := controller.Attach(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		finish, err := controller.BeginCoreReplacement(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		controller.listen = func(string, string) (net.Listener, error) {
			return nil, errors.New("resume listen failed")
		}
		if err := finish(context.Background(), true); err == nil {
			t.Fatal("replacement ignored failed lease restoration")
		}
		if _, err := controller.OpenStream(context.Background(), spec.Session); err == nil {
			t.Fatal("failed resume published an input stream")
		}
	})
}

func TestPersistentTargetControllerCloseRetriesAndReportsPendingNeutralization(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Detach(context.Background(), 1); !errors.Is(err, releaseErr) {
		t.Fatalf("Detach error = %v, want release failure", err)
	}
	sink.mu.Lock()
	attemptsAfterDetach := sink.releaseAttempts
	sink.mu.Unlock()
	if err := controller.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("Close error = %v, want pending release failure", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.releaseAttempts != attemptsAfterDetach+1 {
		t.Fatalf("shutdown release attempts = %d, want %d", sink.releaseAttempts, attemptsAfterDetach+1)
	}
}

func TestTargetControllerConcurrentClosePublishesCancellationBeforeLifecycleWait(t *testing.T) {
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.lifecycle.Lock()
	closeResult := make(chan error, 1)
	go func() { closeResult <- controller.Close() }()

	deadline := time.Now().Add(time.Second)
	published := false
	for time.Now().Before(deadline) {
		controller.mu.Lock()
		published = controller.closed
		controller.mu.Unlock()
		if published {
			break
		}
		time.Sleep(time.Millisecond)
	}
	controller.lifecycle.Unlock()
	if !published {
		t.Fatal("Close waited for the lifecycle lock before publishing cancellation")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the lifecycle barrier was released")
	}
}

func TestTargetControllerReportsBindFailure(t *testing.T) {
	bindErr := errors.New("bind failed")
	pendingAtListen := make(chan bool, 1)
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.listen = func(string, string) (net.Listener, error) {
		controller.mu.Lock()
		pending := controller.pending
		controller.mu.Unlock()
		pendingAtListen <- pending != nil && pending.session == 1
		return nil, bindErr
	}
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); !errors.Is(err, bindErr) {
		t.Fatalf("Attach error = %v, want bind failure", err)
	}
	if !<-pendingAtListen {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetControllerCloseCancelsAttachAfterPendingStartup(t *testing.T) {
	bindErr := errors.New("late bind failure")
	listenEntered := make(chan struct{})
	allowListenReturn := make(chan struct{})
	listenReturned := make(chan struct{})
	var allowOnce sync.Once
	allowReturn := func() { allowOnce.Do(func() { close(allowListenReturn) }) }
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.listen = func(string, string) (net.Listener, error) {
		close(listenEntered)
		<-allowListenReturn
		close(listenReturned)
		return nil, bindErr
	}
	defer func() {
		allowReturn()
		_ = controller.Close()
	}()

	attachResult := make(chan error, 1)
	go func() {
		attachResult <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"})
	}()
	select {
	case <-listenEntered:
	case err := <-attachResult:
		t.Fatalf("Attach returned before reaching pending startup: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Attach did not reach pending startup")
	}
	controller.mu.Lock()
	pending := controller.pending
	controller.mu.Unlock()
	if pending == nil || pending.session != 1 {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- controller.Close() }()
	select {
	case err := <-attachResult:
		if !errors.Is(err, net.ErrClosed) || errors.Is(err, bindErr) {
			t.Fatalf("Attach error = %v, want Close outcome before late bind result", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not release Attach from pending startup")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind pending Attach")
	}
	allowReturn()
	select {
	case <-listenReturned:
	case <-time.After(time.Second):
		t.Fatal("listener call did not return after test barrier opened")
	}
	sink.mu.Lock()
	closed := sink.closed
	sink.mu.Unlock()
	if closed != 1 {
		t.Fatalf("persistent sink close calls = %d, want 1", closed)
	}
}

func TestTargetControllerDetachCancelsAttachAfterPendingStartup(t *testing.T) {
	listenEntered := make(chan struct{})
	allowListenReturn := make(chan struct{})
	var allowOnce sync.Once
	allowReturn := func() { allowOnce.Do(func() { close(allowListenReturn) }) }
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.listen = func(string, string) (net.Listener, error) {
		close(listenEntered)
		<-allowListenReturn
		return nil, errors.New("late bind failure")
	}
	defer func() {
		allowReturn()
		_ = controller.Close()
	}()

	attachResult := make(chan error, 1)
	go func() {
		attachResult <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"})
	}()
	select {
	case <-listenEntered:
	case err := <-attachResult:
		t.Fatalf("Attach returned before reaching pending startup: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Attach did not reach pending startup")
	}
	controller.mu.Lock()
	pending := controller.pending
	controller.mu.Unlock()
	if pending == nil || pending.session != 1 {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}

	detachResult := make(chan error, 1)
	go func() { detachResult <- controller.Detach(context.Background(), 1) }()
	select {
	case err := <-attachResult:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Attach error = %v, want pending lease closed by Detach", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Detach did not release Attach from pending startup")
	}
	select {
	case err := <-detachResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Detach blocked behind pending Attach")
	}
	allowReturn()
}

func (s *persistentTestSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

func TestTargetControllerRejectsInvalidLease(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("short"), Core: "SNES"}); err == nil {
		t.Fatal("short token accepted")
	}
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "/private"}); err == nil {
		t.Fatal("unsafe core accepted")
	}
}

func TestTargetControllerDetachIsIdempotent(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Detach(context.Background(), 99); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetControllerOpenStreamRequiresActiveLease(t *testing.T) {
	controller := NewTargetController()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := controller.OpenStream(ctx, 1); err == nil {
		t.Fatal("stream opened without lease")
	}
}

func TestPersistentTargetControllerDoesNotExposeStreamWithoutLease(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	t.Cleanup(func() { _ = controller.Close() })
	if _, err := controller.OpenStream(context.Background(), 1); err == nil {
		t.Fatal("persistent input stream opened without a lease")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 0 || sink.released != 0 {
		t.Fatalf("unleased sink activity = events:%d releases:%d", sink.events, sink.released)
	}
}

func TestPersistentTargetControllerDetachReleasesWithoutDestroyingDevice(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")

	attachAndPress := func(session uint64, core string, code uint16) net.Conn {
		t.Helper()
		if err := controller.Attach(context.Background(), Spec{Session: session, Token: token, Core: core}); err != nil {
			t.Fatal(err)
		}
		connection, err := controller.OpenStream(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n", session, core, hex.EncodeToString(token)); err != nil {
			t.Fatal(err)
		}
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: session}, Seq: 1, Device: 1, Kind: 1, Action: 1, Code: code}
		if err := bridge.WriteFrame(connection, frame); err != nil {
			t.Fatal(err)
		}
		waitForSink(t, sink, func(s *persistentTestSink) bool { return s.pressed })
		return connection
	}

	first := attachAndPress(1, "MegaDrive", protocol.InputCodeButtonC)
	if err := controller.Detach(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return !s.pressed && s.released > 0 })
	_ = first.Close()
	sink.mu.Lock()
	closedAfterDetach := sink.closed
	sink.mu.Unlock()
	if closedAfterDetach != 0 {
		t.Fatalf("device close calls after detach = %d, want 0", closedAfterDetach)
	}

	second := attachAndPress(2, "SNES", protocol.InputCodeButtonX)
	if err := controller.Detach(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 2 || sink.closed != 1 {
		t.Fatalf("persistent lifecycle = events:%d closes:%d, want 2 events and 1 close", sink.events, sink.closed)
	}
}

func TestPersistentTargetControllerReleaseAllPreservesDeviceForNextOwner(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")

	attachAndPress := func(session uint64, core string, code uint16) net.Conn {
		t.Helper()
		if err := controller.Attach(context.Background(), Spec{Session: session, Token: token, Core: core}); err != nil {
			t.Fatal(err)
		}
		connection, err := controller.OpenStream(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n", session, core, hex.EncodeToString(token)); err != nil {
			t.Fatal(err)
		}
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: session}, Seq: 1, Device: 1, Kind: 1, Action: 1, Code: code}
		if err := bridge.WriteFrame(connection, frame); err != nil {
			t.Fatal(err)
		}
		waitForSink(t, sink, func(s *persistentTestSink) bool { return s.pressed })
		return connection
	}

	first := attachAndPress(1, "MegaDrive", protocol.InputCodeButtonC)
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return !s.pressed && s.released > 0 })
	_ = first.Close()
	sink.mu.Lock()
	closedAfterDetach := sink.closed
	sink.mu.Unlock()
	if closedAfterDetach != 0 {
		t.Fatalf("device close calls after detach = %d, want 0", closedAfterDetach)
	}

	second := attachAndPress(2, "SNES", protocol.InputCodeButtonX)
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 2 || sink.closed != 1 {
		t.Fatalf("persistent lifecycle = events:%d closes:%d, want 2 events and 1 close", sink.events, sink.closed)
	}
}

func waitForSink(t *testing.T, sink *persistentTestSink, ready func(*persistentTestSink) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		ok := ready(sink)
		sink.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	t.Fatalf("timed out waiting for sink: events=%d pressed=%t releases=%d closes=%d", sink.events, sink.pressed, sink.released, sink.closed)
}

func barrierCoreArchive(t *testing.T) []byte {
	t.Helper()
	base := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:108], "0000644\x00")
		copy(header[108:116], "0000000\x00")
		copy(header[116:124], "0000000\x00")
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(entry.data)))
		copy(header[136:148], "00000000000\x00")
		for index := 148; index < 156; index++ {
			header[index] = ' '
		}
		header[156] = '0'
		copy(header[257:263], "ustar\x00")
		copy(header[263:265], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		output.Write(header)
		output.Write(entry.data)
		output.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	output.Write(make([]byte, 1024))
	return output.Bytes()
}
