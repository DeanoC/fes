package remotemedia

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

type managedSenderFakeSource struct {
	mu     sync.Mutex
	closed int
}

func (s *managedSenderFakeSource) Start() error { return nil }
func (s *managedSenderFakeSource) Next(ctx context.Context) (EncodedSample, error) {
	<-ctx.Done()
	return EncodedSample{}, ctx.Err()
}
func (s *managedSenderFakeSource) Stats() CaptureStats { return CaptureStats{} }
func (s *managedSenderFakeSource) Close() error {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
	return nil
}
func (s *managedSenderFakeSource) closeCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }

type managedSenderFake struct {
	runStarted chan struct{}
	runDone    chan struct{}
	closeOnce  sync.Once
	closed     chan struct{}
}

func newManagedSenderFake() *managedSenderFake {
	return &managedSenderFake{runStarted: make(chan struct{}), runDone: make(chan struct{}), closed: make(chan struct{})}
}

type managedSenderReadyFake struct {
	*managedSenderFake
	readyErr error
}

type managedSenderRuntimeErrorFake struct {
	release chan struct{}
	closed  chan struct{}
}

func (s *managedSenderRuntimeErrorFake) Run(context.Context) error {
	<-s.release
	return errors.New("token=hostile-secret /Users/private/capture.sock")
}

func (s *managedSenderRuntimeErrorFake) RunReady(ctx context.Context, ready func(error)) error {
	ready(nil)
	return s.Run(ctx)
}

func (s *managedSenderRuntimeErrorFake) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

type managedSenderRunOnly struct{ *managedSenderRuntimeErrorFake }

func (s managedSenderRunOnly) Run(ctx context.Context) error {
	return s.managedSenderRuntimeErrorFake.Run(ctx)
}

type managedSenderFlakyClose struct {
	mu         sync.Mutex
	closeCalls int
}

func (*managedSenderFlakyClose) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (s *managedSenderFlakyClose) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	if s.closeCalls == 1 {
		return errors.New("first close failed")
	}
	return nil
}

func (s *managedSenderReadyFake) RunReady(ctx context.Context, ready func(error)) error {
	ready(s.readyErr)
	return s.Run(ctx)
}

func (s *managedSenderFake) Run(ctx context.Context) error {
	close(s.runStarted)
	<-ctx.Done()
	close(s.runDone)
	return ctx.Err()
}
func (s *managedSenderFake) Close() error { s.closeOnce.Do(func() { close(s.closed) }); return nil }

func managedSenderConfig() ManagedSenderConfig {
	return ManagedSenderConfig{Session: "session", Generation: 7, Token: "secret-token", RTPAddress: "127.0.0.1:5004", SSRC: 1}
}

func newManagedSenderForTest(t *testing.T, fake *managedSenderFake, source *managedSenderFakeSource) *ManagedSender {
	t.Helper()
	component, err := NewManagedSender(managedSenderConfig(), source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) { return fake, nil }),
		WithManagedSenderStopTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	return component
}

func TestManagedSenderStartReturnsAfterRunOwnershipIsEstablished(t *testing.T) {
	fake, source := newManagedSenderFake(), &managedSenderFakeSource{}
	component := newManagedSenderForTest(t, fake, source)
	started := time.Now()
	handle, err := component.Start(context.Background(), "game")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Start blocked for %v", elapsed)
	}
	select {
	case <-fake.runStarted:
	case <-time.After(time.Second):
		t.Fatal("Run was not started")
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestManagedSenderRuntimeOutlivesSuccessfulStartContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake, source := newManagedSenderFake(), &managedSenderFakeSource{}
	component := newManagedSenderForTest(t, fake, source)
	handle, err := component.Start(ctx, "game")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	select {
	case <-fake.runDone:
		t.Fatal("successful startup context cancellation stopped owned runtime")
	case <-time.After(50 * time.Millisecond):
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestManagedSenderRuntimeLogsUseSanitizedStableLabel(t *testing.T) {
	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	for _, readyRunner := range []bool{false, true} {
		logs.Reset()
		fake := &managedSenderRuntimeErrorFake{release: make(chan struct{}), closed: make(chan struct{})}
		component, err := NewManagedSender(managedSenderConfig(), &managedSenderFakeSource{},
			WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
				if readyRunner {
					return fake, nil
				}
				return managedSenderRunOnly{fake}, nil
			}),
			WithManagedSenderStopTimeout(time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := component.Start(context.Background(), "game")
		if err != nil {
			t.Fatalf("Start(ready=%v): %v", readyRunner, err)
		}
		close(fake.release)
		select {
		case <-handle.(*managedSenderHandle).Done():
		case <-time.After(time.Second):
			t.Fatalf("runtime did not stop (ready=%v)", readyRunner)
		}
		got := logs.String()
		if !strings.Contains(got, "managed media sender stopped unexpectedly") {
			t.Fatalf("log missing stable label (ready=%v): %q", readyRunner, got)
		}
		for _, hostile := range []string{"hostile-secret", "/Users/private", "capture.sock"} {
			if strings.Contains(got, hostile) {
				t.Fatalf("log leaked %q (ready=%v): %q", hostile, readyRunner, got)
			}
		}
	}
}

func TestManagedSenderStopCancelsRunAndClosesResourcesIdempotently(t *testing.T) {
	fake, source := newManagedSenderFake(), &managedSenderFakeSource{}
	component := newManagedSenderForTest(t, fake, source)
	handle, err := component.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.runDone:
	case <-time.After(time.Second):
		t.Fatal("Run was not cancelled")
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.closed:
	default:
		t.Fatal("sender was not closed")
	}
	if got := source.closeCount(); got != 0 {
		t.Fatalf("source close count = %d, want 0", got)
	}
}

func TestManagedSenderStartFailureIsSanitizedAndCleansSource(t *testing.T) {
	secret := "token=top-secret /Users/private/capture"
	source := &managedSenderFakeSource{}
	component, err := NewManagedSender(managedSenderConfig(), source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) { return nil, errors.New(secret) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = component.Start(context.Background(), "game")
	if err == nil || !errors.Is(err, ErrManagedSenderStart) || strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "/Users/private") {
		t.Fatalf("unsafe startup error: %v", err)
	}
	if got := source.closeCount(); got != 0 {
		t.Fatalf("source close count = %d, want 0", got)
	}
}

func TestManagedSenderStartHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake, source := newManagedSenderFake(), &managedSenderFakeSource{}
	component := newManagedSenderForTest(t, fake, source)
	_, err := component.Start(ctx, "game")
	if err == nil || !errors.Is(err, ErrManagedSenderStart) {
		t.Fatalf("Start error = %v", err)
	}
	select {
	case <-fake.runStarted:
		t.Fatal("Run started after cancellation")
	default:
	}
}

func TestManagedSenderStartWaitsForRunnerReadinessAndSanitizesFailure(t *testing.T) {
	fake := &managedSenderReadyFake{managedSenderFake: newManagedSenderFake(), readyErr: errors.New("token=secret /private/network")}
	source := &managedSenderFakeSource{}
	component, err := NewManagedSender(managedSenderConfig(), source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) { return fake, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Start(context.Background(), "game"); err == nil || !errors.Is(err, ErrManagedSenderStart) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Start error = %v", err)
	}
	select {
	case <-fake.closed:
	case <-time.After(time.Second):
		t.Fatal("runner was not closed")
	}
	if got := source.closeCount(); got != 0 {
		t.Fatalf("source close count = %d, want 0", got)
	}
}

func TestManagedSenderStartClosesPartialRunnerButNotSourceOnFactoryError(t *testing.T) {
	fake := newManagedSenderFake()
	source := &managedSenderFakeSource{}
	component, err := NewManagedSender(managedSenderConfig(), source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
			return fake, errors.New("construction failed")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Start(context.Background(), "game"); err == nil || !errors.Is(err, ErrManagedSenderStart) {
		t.Fatalf("Start error = %v", err)
	}
	select {
	case <-fake.closed:
	case <-time.After(time.Second):
		t.Fatal("partial runner was not closed")
	}
	if got := source.closeCount(); got != 0 {
		t.Fatalf("source close count = %d, want 0", got)
	}
}

func TestManagedSenderStartPreservesCancellationAfterSynchronousFactory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := newManagedSenderFake()
	source := &managedSenderFakeSource{}
	component, err := NewManagedSender(managedSenderConfig(), source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
			cancel()
			return fake, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Start(ctx, "game"); err == nil || !errors.Is(err, ErrManagedSenderStart) {
		t.Fatalf("Start error = %v", err)
	}
	select {
	case <-fake.closed:
	case <-time.After(time.Second):
		t.Fatal("runner was not closed after cancellation")
	}
}

type managedSenderStartErrorSource struct {
	managedSenderFakeSource
	err error
}

func (s *managedSenderStartErrorSource) Start() error { return s.err }

func TestManagedSenderDefaultRunnerSurfacesCaptureStartupFailure(t *testing.T) {
	source := &managedSenderStartErrorSource{err: errors.New("capture /Users/private/device")}
	component, err := NewManagedSender(managedSenderConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Start(context.Background(), "game"); err == nil || !errors.Is(err, ErrManagedSenderStart) || strings.Contains(err.Error(), "private") {
		t.Fatalf("Start error = %v", err)
	}
	if got := source.closeCount(); got != 1 {
		t.Fatalf("source close count = %d, want 1", got)
	}
}

func TestManagedSenderDefaultRunnerSurfacesNetworkSetupFailure(t *testing.T) {
	cfg := managedSenderConfig()
	cfg.RTPAddress = "%%%invalid-address%%%"
	source := &managedSenderFakeSource{}
	component, err := NewManagedSender(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Start(context.Background(), "game"); err == nil || !errors.Is(err, ErrManagedSenderStart) || strings.Contains(err.Error(), "invalid-address") {
		t.Fatalf("Start error = %v", err)
	}
	if got := source.closeCount(); got != 1 {
		t.Fatalf("source close count = %d, want 1", got)
	}
}

func TestManagedSenderStartValidatesConfigurationWithoutLeakingSecretsOrPaths(t *testing.T) {
	cfg := managedSenderConfig()
	cfg.Session = ""
	cfg.Token = "token=secret"
	cfg.RTPAddress = "/Users/private/rtp"
	component, err := NewManagedSender(cfg, &managedSenderFakeSource{}, WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) { return newManagedSenderFake(), nil }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = component.Start(context.Background(), "game")
	if err == nil || !errors.Is(err, ErrManagedSenderStart) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/Users/private") {
		t.Fatalf("unsafe validation error: %v", err)
	}
}

func TestManagedSenderStopRetriesOnlyFailedCleanup(t *testing.T) {
	runner := &managedSenderFlakyClose{}
	runDone := make(chan struct{})
	close(runDone)
	_, cancel := context.WithCancel(context.Background())
	handle := &managedSenderHandle{
		runner:  runner,
		cancel:  cancel,
		runDone: runDone,
		timeout: 100 * time.Millisecond,
		done:    make(chan struct{}),
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, ErrManagedSenderStop) {
		t.Fatalf("first Stop = %v, want cleanup failure", err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry Stop = %v", err)
	}
	runner.mu.Lock()
	closeCalls := runner.closeCalls
	runner.mu.Unlock()
	if closeCalls != 2 {
		t.Fatalf("runner Close calls = %d, want failed attempt plus retry", closeCalls)
	}
}

func TestManagedSenderStartReturnsPartialHandleWhenRunnerCleanupFails(t *testing.T) {
	runner := &managedSenderFlakyClose{}
	component, err := NewManagedSender(
		managedSenderConfig(),
		&managedSenderFakeSource{},
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
			return runner, errors.New("factory returned partial runner")
		}),
		WithManagedSenderStopTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	handle, startErr := component.Start(context.Background(), "game")
	if !errors.Is(startErr, ErrManagedSenderStart) || handle == nil {
		t.Fatalf("Start = handle %v err %v, want retryable partial runner", handle, startErr)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("partial runner cleanup retry = %v", err)
	}
	runner.mu.Lock()
	closeCalls := runner.closeCalls
	runner.mu.Unlock()
	if closeCalls != 2 {
		t.Fatalf("runner Close calls = %d, want failed startup cleanup plus retry", closeCalls)
	}
}
