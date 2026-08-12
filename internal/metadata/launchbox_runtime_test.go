package metadata

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type launchBoxTestWorker struct {
	run       func(context.Context) error
	closeFunc func() error
	started   chan struct{}
	startOnce sync.Once
	runs      atomic.Int32
	closes    atomic.Int32
}

type launchBoxGateWorker struct {
	gate      *launchBoxRequestGate
	preflight func(context.Context) error
	run       func(context.Context, *launchBoxRequestGate) error
	started   chan struct{}
	startOnce sync.Once
}

type launchBoxBlockingGateWorker struct {
	attachEntered chan struct{}
	releaseAttach chan struct{}
	attachOnce    sync.Once
	runCount      atomic.Int32
}

func (w *launchBoxBlockingGateWorker) AttachLaunchBoxRequestGate(*launchBoxRequestGate) {
	if w.attachEntered != nil {
		w.attachOnce.Do(func() { close(w.attachEntered) })
	}
	if w.releaseAttach != nil {
		<-w.releaseAttach
	}
}

func (w *launchBoxBlockingGateWorker) Run(context.Context) error {
	w.runCount.Add(1)
	return nil
}

func (*launchBoxBlockingGateWorker) Close() error { return nil }

type launchBoxRecordingRoundTripper struct {
	requests    atomic.Int32
	requestSeen chan struct{}
	seenOnce    sync.Once
	respond     bool
}

func (r *launchBoxRecordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests.Add(1)
	if r.requestSeen != nil {
		r.seenOnce.Do(func() { close(r.requestSeen) })
	}
	if !r.respond {
		return nil, errors.New("recording transport reached unexpectedly")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    request,
	}, nil
}

type launchBoxRoundTripFunc func(*http.Request) (*http.Response, error)

func (f launchBoxRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newLaunchBoxRecordingPolicy(respond bool) (*launchBoxTransportPolicy, *launchBoxRecordingRoundTripper) {
	policy := newProductionLaunchBoxTransportPolicy()
	recorder := &launchBoxRecordingRoundTripper{respond: respond}
	policy.client.Transport = recorder
	return policy, recorder
}

func (w *launchBoxGateWorker) Preflight(ctx context.Context) error {
	if w.preflight == nil {
		return nil
	}
	return w.preflight(ctx)
}

func (w *launchBoxGateWorker) AttachLaunchBoxRequestGate(gate *launchBoxRequestGate) {
	w.gate = gate
}

func (w *launchBoxGateWorker) Run(ctx context.Context) error {
	if w.started != nil {
		w.startOnce.Do(func() { close(w.started) })
	}
	if w.run != nil {
		return w.run(ctx, w.gate)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (*launchBoxGateWorker) Close() error { return nil }

func (w *launchBoxTestWorker) Run(ctx context.Context) error {
	w.runs.Add(1)
	if w.started != nil {
		w.startOnce.Do(func() { close(w.started) })
	}
	if w.run != nil {
		return w.run(ctx)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (w *launchBoxTestWorker) Close() error {
	w.closes.Add(1)
	if w.closeFunc != nil {
		return w.closeFunc()
	}
	return nil
}

type launchBoxTestResource struct {
	closeFunc func() error
	closes    atomic.Int32
}

func (r *launchBoxTestResource) Close() error {
	r.closes.Add(1)
	if r.closeFunc != nil {
		return r.closeFunc()
	}
	return nil
}

func TestLaunchBoxRuntimeOpenIsInertUntilActivation(t *testing.T) {
	var factoryCalls atomic.Int32
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		factoryCalls.Add(1)
		return &launchBoxTestWorker{}, nil
	})
	if runtime == nil {
		t.Fatal("runtime is nil")
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("worker factory called during Open: %d", got)
	}
	if runtime.activationDone != nil || runtime.worker != nil || runtime.reaper != nil {
		t.Fatalf("runtime started work during Open: %#v", runtime)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("worker factory called during Close-before-activation: %d", got)
	}
}

func TestLaunchBoxRuntimeFactoryCannotRequestBeforeActivationCommit(t *testing.T) {
	policy, recorder := newLaunchBoxRecordingPolicy(false)
	runtime := newLaunchBoxRuntimeWithDependencies(policy, func(ctx context.Context) (launchBoxWorker, error) {
		if _, err := policy.newArchiveRequest(ctx); err == nil {
			return nil, errors.New("factory unexpectedly created a request")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gamesdb.launchbox-app.com/Metadata.zip", nil)
		if err != nil {
			return nil, err
		}
		if _, err := newLaunchBoxRequestGate().Do(request); err == nil {
			return nil, errors.New("factory unexpectedly reached a request gate")
		}
		return nil, errors.New("factory failed")
	})
	if opCode(runtime.Activate()) != ErrStorage {
		t.Fatal("factory failure did not map to storage")
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("factory failure issued %d transport requests", got)
	}
}

func TestLaunchBoxRuntimePreflightCannotRequestBeforeActivationCommit(t *testing.T) {
	policy, recorder := newLaunchBoxRecordingPolicy(false)
	worker := &launchBoxGateWorker{preflight: func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gamesdb.launchbox-app.com/Metadata.zip", nil)
		if err != nil {
			return err
		}
		if _, err := newLaunchBoxRequestGate().Do(request); err == nil {
			return errors.New("preflight unexpectedly reached a request gate")
		}
		return errors.New("preflight failed")
	}}
	runtime := newLaunchBoxRuntimeWithDependencies(policy, func(context.Context) (launchBoxWorker, error) {
		return worker, nil
	})
	if opCode(runtime.Activate()) != ErrStorage {
		t.Fatal("preflight failure did not map to storage")
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("preflight failure issued %d transport requests", got)
	}
}

func TestLaunchBoxRuntimeAttachesRequestGateOnlyAfterActivationCommit(t *testing.T) {
	policy, recorder := newLaunchBoxRecordingPolicy(true)
	recorder.requestSeen = make(chan struct{})
	worker := &launchBoxGateWorker{run: func(ctx context.Context, gate *launchBoxRequestGate) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gamesdb.launchbox-app.com/Metadata.zip", nil)
		if err != nil {
			return err
		}
		response, err := gate.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	runtime := newLaunchBoxRuntimeWithDependencies(policy, func(context.Context) (launchBoxWorker, error) {
		return worker, nil
	})
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("request issued before activation: %d", got)
	}
	if err := runtime.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-recorder.requestSeen:
	case <-time.After(time.Second):
		t.Fatal("committed worker did not issue its gated request")
	}
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("committed request count = %d, want 1", got)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchBoxRequestGateRevocationDoesNotWaitForInFlightRequest(t *testing.T) {
	requestEntered := make(chan struct{})
	releaseRequest := make(chan struct{})
	policy := newProductionLaunchBoxTransportPolicy()
	policy.client.Transport = launchBoxRoundTripFunc(func(*http.Request) (*http.Response, error) {
		close(requestEntered)
		<-releaseRequest
		return nil, errors.New("request released")
	})
	gate := newLaunchBoxRequestGate()
	gate.attach(policy)
	request, err := policy.newArchiveRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	doDone := make(chan struct{})
	go func() {
		_, _ = gate.Do(request)
		close(doDone)
	}()
	<-requestEntered

	revokeDone := make(chan struct{})
	go func() {
		gate.revoke()
		close(revokeDone)
	}()
	select {
	case <-revokeDone:
	case <-time.After(100 * time.Millisecond):
		close(releaseRequest)
		<-doDone
		t.Fatal("request-gate revocation waited for in-flight I/O")
	}
	close(releaseRequest)
	select {
	case <-doDone:
	case <-time.After(time.Second):
		t.Fatal("in-flight request did not finish after release")
	}
}

func TestLaunchBoxRuntimeCloseRetainsRealRootDescriptorUntilWorkerExit(t *testing.T) {
	root, err := os.CreateTemp("", "launchbox-root-")
	if err != nil {
		t.Fatal(err)
	}
	rootName := root.Name()
	defer os.Remove(rootName)

	workerStarted := make(chan struct{})
	releaseWorkerClose := make(chan struct{})
	releaseWorkerRun := make(chan struct{})
	worker := &launchBoxTestWorker{
		started: workerStarted,
		run: func(context.Context) error {
			<-releaseWorkerRun
			return nil
		},
		closeFunc: func() error {
			<-releaseWorkerClose
			return nil
		},
	}
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		return worker, nil
	}, root)
	if err := runtime.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case err := <-closeDone:
		if opCode(err) != ErrStorage {
			t.Fatalf("Close result = %v, want storage timeout", err)
		}
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("Close did not return at its two-second bound")
	}
	if _, err := root.Stat(); err != nil {
		t.Fatalf("root descriptor closed while worker was still live: %v", err)
	}
	if runtime.reaper == nil {
		t.Fatal("timed out Close did not retain a reaper")
	}

	close(releaseWorkerClose)
	select {
	case <-runtime.reaper.done:
		t.Fatal("reaper finished before the worker joined")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := root.Stat(); err != nil {
		t.Fatalf("root descriptor closed before worker exit: %v", err)
	}
	close(releaseWorkerRun)
	select {
	case <-runtime.reaper.done:
	case <-time.After(time.Second):
		t.Fatal("retained reaper did not finish after worker exit")
	}
	if _, err := root.Stat(); err == nil {
		t.Fatal("root descriptor remained open after retained reaper completed")
	}
}

func TestOpenLaunchBoxRejectsInjectedProviderBeforeConstruction(t *testing.T) {
	provider := &fakeProvider{started: make(chan struct{})}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{
		Configured:   true,
		Enabled:      true,
		ProviderName: ProviderLaunchBox,
		Provider:     provider,
	})
	if runtimeValue != nil || opCode(err) != ErrPolicyBlocked {
		t.Fatalf("Open injected provider = runtime:%v err:%v, want policy_blocked and nil runtime", runtimeValue, err)
	}
	select {
	case <-provider.started:
		t.Fatal("Open LaunchBox invoked the replaceable legacy provider")
	default:
	}
}

func TestOpenLaunchBoxUsesInertSealedProductionConstruction(t *testing.T) {
	runtimeValue, err := Open(context.Background(), RuntimeConfig{
		Configured:   true,
		Enabled:      true,
		ProviderName: ProviderLaunchBox,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, ok := runtimeValue.(*launchBoxRuntime)
	if !ok || runtime == nil {
		t.Fatalf("Open LaunchBox runtime = %T, want *launchBoxRuntime", runtimeValue)
	}
	if runtime.policy == nil || runtime.policy.client == nil || runtime.policy.transport == nil {
		t.Fatal("Open LaunchBox did not construct the sealed production policy")
	}
	if runtime.activationDone != nil || runtime.worker != nil || runtime.reaper != nil {
		t.Fatalf("Open LaunchBox started work: %#v", runtime)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenLaunchBoxRejectsReplacementProviderAndHTTPClient(t *testing.T) {
	for name, config := range map[string]RuntimeConfig{
		"provider":    {Configured: true, Enabled: true, ProviderName: ProviderLaunchBox, Provider: &fakeProvider{}},
		"http client": {Configured: true, Enabled: true, ProviderName: ProviderLaunchBox, HTTPClient: &http.Client{}},
	} {
		t.Run(name, func(t *testing.T) {
			runtimeValue, err := Open(context.Background(), config)
			if runtimeValue != nil || opCode(err) != ErrPolicyBlocked {
				t.Fatalf("Open replacement config = runtime:%v err:%v, want policy_blocked and nil runtime", runtimeValue, err)
			}
		})
	}
}

func TestLaunchBoxRuntimeActivationIsIdempotentAndConcurrent(t *testing.T) {
	worker := &launchBoxTestWorker{started: make(chan struct{})}
	var factoryCalls atomic.Int32
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		factoryCalls.Add(1)
		return worker, nil
	})
	const callers = 16
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- runtime.Activate()
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Activate = %v", err)
		}
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("worker factory calls = %d, want 1", got)
	}
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("activated worker did not start")
	}
	if got := worker.runs.Load(); got != 1 {
		t.Fatalf("worker runs = %d, want 1", got)
	}
	if err := runtime.Activate(); err != nil {
		t.Fatalf("repeat Activate = %v", err)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("repeat Activate started another worker: %d", got)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if got := worker.closes.Load(); got != 1 {
		t.Fatalf("worker closes = %d, want 1", got)
	}
}

func TestLaunchBoxRuntimeActivationFailureRollsBackPartialWorker(t *testing.T) {
	worker := &launchBoxTestWorker{}
	resource := &launchBoxTestResource{}
	activationCanceled := make(chan struct{})
	wantErr := errors.New("activation fixture failure")
	var factoryCalls atomic.Int32
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(ctx context.Context) (launchBoxWorker, error) {
		factoryCalls.Add(1)
		go func() {
			<-ctx.Done()
			close(activationCanceled)
		}()
		return worker, wantErr
	}, resource)
	firstErr := runtime.Activate()
	if opCode(firstErr) != ErrStorage {
		t.Fatalf("activation error = %v, want storage", firstErr)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("worker factory calls = %d", got)
	}
	if got := worker.closes.Load(); got != 1 {
		t.Fatalf("partial worker closes = %d, want 1", got)
	}
	if got := resource.closes.Load(); got != 1 {
		t.Fatalf("activation resource closes = %d, want 1", got)
	}
	if secondErr := runtime.Activate(); opCode(secondErr) != ErrStorage {
		t.Fatalf("repeated activation error = %v, want same storage result", secondErr)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("repeated activation retried worker factory: %d", got)
	}
	if closeErr := runtime.Close(); closeErr != nil {
		t.Fatalf("Close after failed activation = %v", closeErr)
	}
	if got := worker.closes.Load(); got != 1 {
		t.Fatalf("failed worker closed more than once: %d", got)
	}
	select {
	case <-activationCanceled:
	case <-time.After(time.Second):
		t.Fatal("failed activation context was not canceled")
	}
	if got := resource.closes.Load(); got != 1 {
		t.Fatalf("failed activation resource closed more than once: %d", got)
	}
}

func TestLaunchBoxRuntimeFailedActivationClosesPartialWorkerExactlyOnceAcrossClose(t *testing.T) {
	worker := &launchBoxTestWorker{}
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		return worker, errors.New("activation fixture failure")
	})
	if opCode(runtime.Activate()) != ErrStorage {
		t.Fatal("activation did not fail with storage")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if got := worker.closes.Load(); got != 1 {
		t.Fatalf("partial worker closes = %d, want 1", got)
	}
}

func TestLaunchBoxRuntimeCloseBeforeActivationPreventsLaterWork(t *testing.T) {
	var factoryCalls atomic.Int32
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		factoryCalls.Add(1)
		return &launchBoxTestWorker{}, nil
	})
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Activate(); opCode(err) != ErrCanceled {
		t.Fatalf("Activate after Close = %v, want canceled", err)
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("worker factory ran after Close: %d", got)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal("repeated Close changed a successful close: ", err)
	}
}

func TestLaunchBoxRuntimeCloseDuringActivationRetainsOwnershipUntilFactoryReturns(t *testing.T) {
	factoryEntered := make(chan struct{})
	releaseFactory := make(chan struct{})
	worker := &launchBoxTestWorker{}
	var factoryCalls atomic.Int32
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(ctx context.Context) (launchBoxWorker, error) {
		factoryCalls.Add(1)
		close(factoryEntered)
		select {
		case <-releaseFactory:
			return worker, nil
		case <-ctx.Done():
			<-releaseFactory
			return worker, ctx.Err()
		}
	})
	activationDone := make(chan error, 1)
	go func() { activationDone <- runtime.Activate() }()
	<-factoryEntered

	started := time.Now()
	firstCloseErr := runtime.Close()
	if time.Since(started) > 2500*time.Millisecond {
		t.Fatalf("Close exceeded bound: %s", time.Since(started))
	}
	if opCode(firstCloseErr) != ErrStorage {
		t.Fatalf("Close while activation is blocked = %v, want storage timeout", firstCloseErr)
	}
	if secondCloseErr := runtime.Close(); opCode(secondCloseErr) != opCode(firstCloseErr) {
		t.Fatalf("repeated Close changed result: first=%v second=%v", firstCloseErr, secondCloseErr)
	}
	if runtime.reaper == nil {
		t.Fatal("timed out Close did not retain a reaper")
	}
	close(releaseFactory)
	select {
	case err := <-activationDone:
		if opCode(err) != ErrCanceled {
			t.Fatalf("activation after Close = %v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("activation did not finish after factory release")
	}
	select {
	case <-runtime.reaper.done:
	case <-time.After(2 * time.Second):
		t.Fatal("retained activation reaper did not finish")
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factory calls = %d", got)
	}
	if got := worker.runs.Load(); got != 0 {
		t.Fatalf("worker started after Close won activation race: %d", got)
	}
}

func TestLaunchBoxRuntimeCloseBoundDuringGateRegistration(t *testing.T) {
	policy, recorder := newLaunchBoxRecordingPolicy(false)
	worker := &launchBoxBlockingGateWorker{
		attachEntered: make(chan struct{}),
		releaseAttach: make(chan struct{}),
	}
	runtime := newLaunchBoxRuntimeWithDependencies(policy, func(context.Context) (launchBoxWorker, error) {
		return worker, nil
	})

	activationDone := make(chan error, 1)
	go func() { activationDone <- runtime.Activate() }()
	select {
	case <-worker.attachEntered:
	case <-time.After(time.Second):
		t.Fatal("request-gate registration did not start")
	}

	closeDone := make(chan error, 1)
	started := time.Now()
	go func() { closeDone <- runtime.Close() }()
	var firstCloseErr error
	select {
	case firstCloseErr = <-closeDone:
	case <-time.After(2500 * time.Millisecond):
		close(worker.releaseAttach)
		<-activationDone
		t.Fatal("Close exceeded its two-second caller bound during request-gate registration")
	}
	if elapsed := time.Since(started); elapsed > 2500*time.Millisecond {
		t.Fatalf("Close took %s, exceeding its two-second caller bound", elapsed)
	}
	if opCode(firstCloseErr) != ErrStorage {
		t.Fatalf("Close result = %v, want storage timeout", firstCloseErr)
	}
	if secondCloseErr := runtime.Close(); secondCloseErr != firstCloseErr {
		t.Fatalf("repeated Close changed the first result: first=%v second=%v", firstCloseErr, secondCloseErr)
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("blocked registration issued %d requests", got)
	}
	var reaper *launchBoxReaper
	runtime.mu.Lock()
	reaper = runtime.reaper
	runtime.mu.Unlock()
	if reaper == nil {
		t.Fatal("timed out Close did not retain a reaper")
	}
	select {
	case <-reaper.done:
		t.Fatal("retained reaper finished before request-gate registration released")
	default:
	}
	if got := worker.runCount.Load(); got != 0 {
		t.Fatalf("worker Run count = %d while request-gate registration was blocked, want zero", got)
	}

	close(worker.releaseAttach)
	select {
	case err := <-activationDone:
		if opCode(err) != ErrCanceled {
			t.Fatalf("activation after Close = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("activation did not finish after request-gate registration released")
	}
	runtime.mu.Lock()
	reaper = runtime.reaper
	runtime.mu.Unlock()
	if reaper == nil {
		t.Fatal("timed out Close did not retain a reaper")
	}
	select {
	case <-reaper.done:
	case <-time.After(time.Second):
		t.Fatal("retained reaper did not finish after request-gate registration released")
	}
	if got := worker.runCount.Load(); got != 0 {
		t.Fatalf("worker Run count = %d, want zero", got)
	}
}

func TestLaunchBoxRuntimeCloseRetainsBlockedResourcesAndEventuallyReleasesAll(t *testing.T) {
	workerCloseEntered := make(chan struct{})
	releaseWorkerClose := make(chan struct{})
	rootCloseEntered := make(chan struct{})
	releaseRootClose := make(chan struct{})
	worker := &launchBoxTestWorker{closeFunc: func() error {
		close(workerCloseEntered)
		<-releaseWorkerClose
		return nil
	}}
	root := &launchBoxTestResource{closeFunc: func() error {
		close(rootCloseEntered)
		<-releaseRootClose
		return nil
	}}
	runtime := newLaunchBoxRuntimeWithDependencies(nil, func(context.Context) (launchBoxWorker, error) {
		return worker, nil
	}, root)
	if err := runtime.Activate(); err != nil {
		t.Fatal(err)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case <-workerCloseEntered:
	case <-time.After(time.Second):
		t.Fatal("worker Close was not started")
	}
	select {
	case closeErr := <-closeDone:
		if opCode(closeErr) != ErrStorage {
			t.Fatalf("Close result = %v, want storage timeout", closeErr)
		}
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("Close did not return at its two-second bound")
	}
	if runtime.reaper == nil {
		t.Fatal("timed out Close did not retain a reaper")
	}
	select {
	case <-rootCloseEntered:
		t.Fatal("root Close ran before the blocked worker stop request returned")
	default:
	}
	close(releaseWorkerClose)
	select {
	case <-rootCloseEntered:
	case <-time.After(time.Second):
		t.Fatal("root Close did not start after worker stop and join")
	}
	close(releaseRootClose)
	select {
	case <-runtime.reaper.done:
	case <-time.After(2 * time.Second):
		t.Fatal("retained resource reaper did not finish")
	}
	if got := worker.closes.Load(); got != 1 {
		t.Fatalf("worker closes = %d, want 1", got)
	}
	if got := root.closes.Load(); got != 1 {
		t.Fatalf("root closes = %d, want 1", got)
	}
}

func TestLaunchBoxRuntimeProductionPolicyDoesNotUseAmbientClient(t *testing.T) {
	policy := newProductionLaunchBoxTransportPolicy()
	if policy == nil || policy.client == nil || policy.transport == nil {
		t.Fatal("production policy is incomplete")
	}
	if policy.client.Transport != policy.transport || policy.transport.Proxy != nil || policy.client.Jar != nil {
		t.Fatal("production policy is not sealed")
	}
	if policy.client.CheckRedirect == nil || policy.client.Timeout != 0 {
		t.Fatal("production policy has unsafe redirect/timeout defaults")
	}
	if policy.transport.TLSClientConfig == nil || policy.transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("production policy TLS minimum is too weak")
	}
	request, err := policy.newArchiveRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.String() != "https://gamesdb.launchbox-app.com/Metadata.zip" {
		t.Fatalf("archive URL = %s", request.URL)
	}
	if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		t.Fatal("production request carries credentials")
	}
}
