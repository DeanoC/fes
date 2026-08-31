package metadata

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ProviderLaunchBox is the provider name admitted by the LaunchBox runtime.
// It remains distinct from the existing provider name for config compatibility.
const ProviderLaunchBox ProviderName = "launchbox"

// ActivatableRuntime is an internal composition seam. Runtime consumers only
// need the narrower Runtime view; composition owns the one explicit activation
// transition after all dependencies have been built.
type ActivatableRuntime interface {
	Runtime
	Activate() error
}

type launchBoxWorker interface {
	Run(context.Context) error
	Close() error
}

type launchBoxWorkerFactory func(context.Context) (launchBoxWorker, error)

type launchBoxWorkerPreflighter interface {
	Preflight(context.Context) error
}

type launchBoxWorkerRequestGateAttacher interface {
	AttachLaunchBoxRequestGate(*launchBoxRequestGate)
}

type launchBoxRuntimeState uint8

const (
	launchBoxRuntimeInert launchBoxRuntimeState = iota
	launchBoxRuntimeActivating
	launchBoxRuntimeActive
	launchBoxRuntimeFailed
	launchBoxRuntimeClosed
)

const launchBoxCloseWait = 2 * time.Second

type launchBoxReaper struct {
	done chan struct{}
	err  error
}

type launchBoxOwnedResource struct {
	resource launchBoxResource
	once     sync.Once
	err      error
}

type launchBoxResource interface {
	Close() error
}

func (r *launchBoxOwnedResource) Close() error {
	if r == nil || r.resource == nil {
		return nil
	}
	r.once.Do(func() { r.err = r.resource.Close() })
	return r.err
}

type launchBoxOwnedWorker struct {
	worker launchBoxWorker
	once   sync.Once
	err    error
}

func (w *launchBoxOwnedWorker) Run(ctx context.Context) error {
	if w == nil || w.worker == nil {
		return context.Canceled
	}
	return w.worker.Run(ctx)
}

func (w *launchBoxOwnedWorker) Close() error {
	if w == nil || w.worker == nil {
		return nil
	}
	w.once.Do(func() { w.err = w.worker.Close() })
	return w.err
}

type launchBoxRuntime struct {
	policy        *launchBoxTransportPolicy
	workerFactory launchBoxWorkerFactory
	resources     []launchBoxResource
	requestGate   *launchBoxRequestGate

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu                   sync.Mutex
	state                launchBoxRuntimeState
	activationDone       chan struct{}
	activationErr        error
	activationCtx        context.Context
	activationStop       context.CancelFunc
	worker               *launchBoxOwnedWorker
	gateRegistrationDone chan struct{}
	workerCtx            context.Context
	workerStop           context.CancelFunc
	workerDone           chan struct{}
	cleanupDone          chan struct{}
	cleanupErr           error
	closeOnce            sync.Once
	closeErr             error
	reaper               *launchBoxReaper
}

// newLaunchBoxRuntime constructs an inert production runtime. It intentionally
// gives the worker factory no transport policy or request-capable object.
func newLaunchBoxRuntime() *launchBoxRuntime {
	policy := newProductionLaunchBoxTransportPolicy()
	return newLaunchBoxRuntimeWithDependencies(policy, newProductionLaunchBoxWorker, policy)
}

// newLaunchBoxRuntimeWithDependencies is package-private so lifecycle tests can
// provide in-memory workers and owned resources without exposing a production
// replacement seam. Production construction uses newLaunchBoxRuntime.
func newLaunchBoxRuntimeWithDependencies(policy *launchBoxTransportPolicy, factory launchBoxWorkerFactory, resources ...launchBoxResource) *launchBoxRuntime {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	owned := make([]launchBoxResource, 0, len(resources))
	for _, resource := range resources {
		if resource != nil {
			owned = append(owned, &launchBoxOwnedResource{resource: resource})
		}
	}
	return &launchBoxRuntime{
		policy:        policy,
		workerFactory: factory,
		resources:     owned,
		rootCtx:       rootCtx,
		rootCancel:    rootCancel,
		state:         launchBoxRuntimeInert,
	}
}

func newProductionLaunchBoxWorker(context.Context) (launchBoxWorker, error) {
	// The ingestion worker is supplied by the next slice. Keeping this factory
	// inert makes production Open safe before that composition is admitted.
	return &launchBoxIdleWorker{}, nil
}

type launchBoxIdleWorker struct{}

func (*launchBoxIdleWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (*launchBoxIdleWorker) Close() error { return nil }

func (r *launchBoxRuntime) Activate() error {
	if r == nil {
		return newOpError(ErrCanceled, context.Canceled)
	}

	r.mu.Lock()
	switch r.state {
	case launchBoxRuntimeClosed:
		r.mu.Unlock()
		return newOpError(ErrCanceled, context.Canceled)
	case launchBoxRuntimeActive:
		r.mu.Unlock()
		return nil
	case launchBoxRuntimeFailed:
		err := r.activationErr
		r.mu.Unlock()
		return err
	case launchBoxRuntimeActivating:
		done := r.activationDone
		r.mu.Unlock()
		<-done
		return r.activationResult()
	default:
		r.state = launchBoxRuntimeActivating
		r.activationDone = make(chan struct{})
		ctx, cancel := context.WithCancel(r.rootCtx)
		r.activationCtx = ctx
		r.activationStop = cancel
		r.mu.Unlock()

		return r.activateLeader(ctx, cancel, r.workerFactory)
	}
}

func (r *launchBoxRuntime) activateLeader(ctx context.Context, cancel context.CancelFunc, factory launchBoxWorkerFactory) error {
	var worker launchBoxWorker
	var err error
	if ctx.Err() != nil {
		err = context.Canceled
	} else if factory == nil {
		err = errors.New("LaunchBox worker factory is unavailable")
	} else {
		// The factory receives only a context. It cannot reach the sealed
		// transport policy or issue a request before activation commits.
		worker, err = factory(ctx)
		if err == nil && worker == nil {
			err = errors.New("LaunchBox worker is unavailable")
		}
	}

	if worker != nil {
		r.mu.Lock()
		r.worker = &launchBoxOwnedWorker{worker: worker}
		r.mu.Unlock()
	}

	if err == nil && worker != nil {
		if preflighter, ok := worker.(launchBoxWorkerPreflighter); ok {
			err = preflighter.Preflight(ctx)
		}
	}

	var requestGate *launchBoxRequestGate
	if err == nil && worker != nil && ctx.Err() == nil {
		// Registration is a pre-commit operation. The gate is deliberately
		// revoked until the registration returns and activation commits, so a
		// worker cannot issue a request while its registration is in flight.
		requestGate = newLaunchBoxRequestGate()
		registrationDone := make(chan struct{})
		r.mu.Lock()
		r.requestGate = requestGate
		r.gateRegistrationDone = registrationDone
		r.mu.Unlock()

		if attacher, ok := worker.(launchBoxWorkerRequestGateAttacher); ok {
			go func() {
				attacher.AttachLaunchBoxRequestGate(requestGate)
				close(registrationDone)
			}()
			select {
			case <-registrationDone:
			case <-ctx.Done():
				// The cleanup reaper waits for registrationDone before releasing
				// worker-owned resources. This keeps a blocked registration from
				// extending Close while retaining ownership until it returns.
			}
		} else {
			close(registrationDone)
		}
	}

	r.mu.Lock()
	closed := r.state == launchBoxRuntimeClosed
	activationCanceled := ctx.Err() != nil
	if err != nil || closed || activationCanceled {
		if closed || activationCanceled {
			r.activationErr = newOpError(ErrCanceled, context.Canceled)
		} else {
			r.activationErr = newOpError(ErrStorage, nil)
			r.state = launchBoxRuntimeFailed
		}
		activationErr := r.activationErr
		done := r.activationDone
		r.mu.Unlock()
		cancel()
		r.mu.Lock()
		requestGate := r.requestGate
		r.mu.Unlock()
		if requestGate != nil {
			requestGate.revoke()
		}
		if r.policy != nil {
			_ = r.policy.Close()
		}
		close(done)
		if closed || activationCanceled {
			return activationErr
		}
		// The activation failure owns any worker/resource state returned by the
		// factory. Cleanup is ordered and retained by the shared reaper.
		_ = r.cleanupWithin(launchBoxCloseWait)
		return activationErr
	}

	workerCtx, workerCancel := context.WithCancel(r.rootCtx)
	r.workerCtx = workerCtx
	r.workerStop = workerCancel
	r.workerDone = make(chan struct{})
	workerDone := r.workerDone
	// Commit active ownership and attach the private request capability only
	// after registration has returned and this final state check has passed.
	r.state = launchBoxRuntimeActive
	requestGate.attach(r.policy)
	done := r.activationDone
	r.mu.Unlock()
	cancel()

	go func() {
		_ = worker.Run(workerCtx)
		close(workerDone)
	}()
	close(done)
	return nil
}

func (r *launchBoxRuntime) activationResult() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.state {
	case launchBoxRuntimeActive:
		return nil
	case launchBoxRuntimeClosed:
		return newOpError(ErrCanceled, context.Canceled)
	case launchBoxRuntimeFailed:
		return r.activationErr
	default:
		return newOpError(ErrCanceled, context.Canceled)
	}
}

func (r *launchBoxRuntime) Lookup(ctx context.Context, _ LookupInput) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, mapContextError(err)
	}
	r.mu.Lock()
	state := r.state
	r.mu.Unlock()
	switch state {
	case launchBoxRuntimeClosed:
		return Result{}, newOpError(ErrCanceled, context.Canceled)
	case launchBoxRuntimeInert, launchBoxRuntimeActivating:
		return Result{}, newOpError(ErrUpstreamUnavailable, nil)
	case launchBoxRuntimeFailed:
		return Result{}, newOpError(ErrStorage, nil)
	default:
		// The bounded SQLite/index lookup is admitted by the next slice. Until
		// then, an active but empty runtime remains an honest offline result.
		return Result{}, newOpError(ErrUpstreamUnavailable, nil)
	}
}

func (r *launchBoxRuntime) OpenArtwork(ctx context.Context, _ string) (Artwork, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Artwork{}, mapContextError(err)
	}
	r.mu.Lock()
	closed := r.state == launchBoxRuntimeClosed
	r.mu.Unlock()
	if closed {
		return Artwork{}, newOpError(ErrCanceled, context.Canceled)
	}
	return Artwork{}, newOpError(ErrStorage, nil)
}

func (r *launchBoxRuntime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.state = launchBoxRuntimeClosed
		activationStop := r.activationStop
		workerStop := r.workerStop
		requestGate := r.requestGate
		policy := r.policy
		r.rootCancel()
		r.mu.Unlock()

		if requestGate != nil {
			requestGate.revoke()
		}
		// Revoke the gate and close the transport before requesting worker stop;
		// canceled worker contexts and closed idle connections unblock I/O before
		// the join begins.
		if activationStop != nil {
			activationStop()
		}
		if policy != nil {
			_ = policy.Close()
		}
		if workerStop != nil {
			workerStop()
		}
		r.closeErr = r.cleanupWithin(launchBoxCloseWait)
		if r.closeErr != nil && opCode(r.closeErr) != ErrStorage {
			r.closeErr = newOpError(ErrStorage, r.closeErr)
		}
	})
	return r.closeErr
}

func (r *launchBoxRuntime) cleanupWithin(timeout time.Duration) error {
	r.mu.Lock()
	if r.cleanupDone == nil {
		r.cleanupDone = make(chan struct{})
		reaper := &launchBoxReaper{done: r.cleanupDone}
		r.reaper = reaper
		go r.runCleanup(reaper)
	}
	done := r.cleanupDone
	r.mu.Unlock()
	if timeout <= 0 {
		timeout = launchBoxCloseWait
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		r.mu.Lock()
		err := r.cleanupErr
		r.mu.Unlock()
		return err
	case <-timer.C:
		return newOpError(ErrStorage, errors.New("LaunchBox shutdown retained by reaper"))
	}
}

func (r *launchBoxRuntime) runCleanup(reaper *launchBoxReaper) {
	var errs []error

	r.mu.Lock()
	activationDone := r.activationDone
	worker := r.worker
	workerDone := r.workerDone
	gateRegistrationDone := r.gateRegistrationDone
	resources := append([]launchBoxResource(nil), r.resources...)
	workerStop := r.workerStop
	requestGate := r.requestGate
	policy := r.policy
	r.mu.Unlock()

	if activationDone != nil {
		<-activationDone
		r.mu.Lock()
		worker = r.worker
		workerDone = r.workerDone
		gateRegistrationDone = r.gateRegistrationDone
		resources = append([]launchBoxResource(nil), r.resources...)
		workerStop = r.workerStop
		requestGate = r.requestGate
		policy = r.policy
		r.mu.Unlock()
	}
	if gateRegistrationDone != nil {
		<-gateRegistrationDone
	}

	if requestGate != nil {
		requestGate.revoke()
	}
	if policy != nil {
		if err := policy.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if workerStop != nil {
		workerStop()
	}

	// Worker.Close is a stop request. It runs before the join, while all root
	// and generation resources remain owned by this cleanup/reaper.
	if worker != nil {
		if err := worker.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if workerDone != nil {
		<-workerDone
	}

	// Only after worker exit may the reaper release root/generation/storage
	// descriptors. A timeout above leaves this goroutine as their owner.
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if err := resource.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	r.mu.Lock()
	r.cleanupErr = errors.Join(errs...)
	reaper.err = r.cleanupErr
	r.mu.Unlock()
	close(reaper.done)
}

// Open's legacy Runtime return type is retained for callers in the current
// composition. A configured LaunchBox runtime is still an ActivatableRuntime;
// later composition can assert that internal interface without widening the
// public host dependency.
func openLaunchBoxRuntime(config RuntimeConfig) (Runtime, error) {
	if !config.Configured || !config.Enabled {
		return nil, nil
	}
	return newLaunchBoxRuntime(), nil
}
