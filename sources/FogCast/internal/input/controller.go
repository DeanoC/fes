// Package input owns target-side remote-input leases and streams.
package input

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
)

const streamDialTimeout = 2 * time.Second

type Spec struct {
	Session uint64
	Token   []byte
	Core    string
	// ExecutionGeneration is internal target authority, never supplied in the
	// attach body. The guarded request context selects it.
	ExecutionGeneration string
}

type Controller interface {
	Attach(context.Context, Spec) error
	Detach(context.Context, uint64) error
	OpenStream(context.Context, uint64) (net.Conn, error)
	Close() error
}

type TargetController struct {
	lifecycle         sync.Mutex
	transitionPending atomic.Int32
	transitionEpoch   atomic.Uint64
	mu                sync.Mutex
	active            *lease
	listenAddress     string
	listen            func(network, address string) (net.Listener, error)
	uinputPath        string
	persistent        bridge.Sink
	keyboard          *KeyboardSink
	ports             *controllerPortsSink
	portsBinding      func(context.Context) (*ControllerBinding, error)
	observe           func(context.Context) (CoreObservation, error)
	mediaUncertainty  func(context.Context) (localhid.MatrixMediaWitness, bool, error)
	mediaRetirer      func(context.Context, localhid.MatrixMediaRetirement) error
	mediaNeutral      func(context.Context, localhid.MatrixMediaNeutral) error
	matrixAdmission   func(context.Context, string, string, uint64, func(context.Context) error) error
	needsRelease      bool
	closed            bool
	localMu           sync.Mutex
	localLn           net.Listener
	localStarting     bool
	localConn         net.Conn
	localPath         string
	localClosed       bool
	localDrops        atomic.Uint64
	// Once v2 is attempted, raw keyboard stays fenced across connections
	// until an acknowledged physical-state handoff is implemented.
	localV2Guard atomic.Bool
	// Disabled until the native sender and startup recovery path are ready.
	localRecoveryEnabled atomic.Bool
	// Protected by localMu. Once serving starts the selected mode cannot change.
	localRecoveryFrozen bool
	localReported       uint64
	localWarning        time.Time
	localLeases         *kitlease.Manager
	localKeyboardGrant  *localKeyboardGrant
	pending             *lease
	closeOnce           sync.Once
	closeErr            error
	localInstanceOnce   sync.Once
	localInstance       string
	localInstanceErr    error
	localEntropy        io.Reader
	localRecoveryMu     sync.Mutex
	localRecoverySeen   bool
	localRecoveryReady  bool
	localRecoveryStart  MatrixBinding
	localTargetID       string
	localBootID         string
	localQueueLease     string
	localQueueEpochs    map[string]uint64
}

// EnableLocalInputRecovery selects the acknowledged local input path before
// the listener starts. The sender, target identity and physical-state handoff
// must be configured separately; missing prerequisites remain unavailable. No
// production caller enables this while that integration is incomplete.
func (c *TargetController) EnableLocalInputRecovery() error {
	if c == nil {
		return errors.New("input controller is closed")
	}
	c.localMu.Lock()
	defer c.localMu.Unlock()
	if c.localClosed || c.localStarting || c.localLn != nil || c.localRecoveryFrozen {
		return errors.New("local input listener has started or closed")
	}
	c.localRecoveryEnabled.Store(true)
	return nil
}

// reserveLocalQueue tombstones each accepted publisher/epoch within the
// validated execution lease. A failed BEGIN consumes its epoch as well.
func (c *TargetController) reserveLocalQueue(leaseGeneration, publisher string, epoch uint64) bool {
	if !localIdentity.MatchString(leaseGeneration) || !validObservedMatrixInstance(publisher) || epoch == 0 || epoch == ^uint64(0) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.localQueueLease != leaseGeneration {
		c.localQueueLease = leaseGeneration
		c.localQueueEpochs = make(map[string]uint64)
	}
	old, exists := c.localQueueEpochs[publisher]
	if (exists && epoch <= old) || (!exists && len(c.localQueueEpochs) >= 1024) {
		return false
	}
	c.localQueueEpochs[publisher] = epoch
	return true
}

// SetLocalTargetIdentity installs the immutable configured target and boot
// identity before local recovery is exposed. Missing or changed identity fails
// closed for v2 recovery without affecting menu or Stop.
func (c *TargetController) SetLocalTargetIdentity(targetID, bootID string) error {
	if c == nil || !localhid.ValidRecoveryIdentity(targetID) || !localhid.ValidRecoveryIdentity(bootID) {
		return errLocalRecoveryPending
	}
	c.localRecoveryMu.Lock()
	defer c.localRecoveryMu.Unlock()
	if c.localTargetID != "" && (c.localTargetID != targetID || c.localBootID != bootID) {
		return errLocalRecoveryPending
	}
	c.localTargetID, c.localBootID = targetID, bootID
	return nil
}

// LocalAgentInstance names this input authority, not the boot or runtime. A
// new controller gets a fresh value; entropy failure never invents an identity.
func (c *TargetController) LocalAgentInstance() (string, error) {
	if c == nil {
		return "", errors.New("input controller is closed")
	}
	c.localInstanceOnce.Do(func() {
		source := c.localEntropy
		if source == nil {
			source = rand.Reader
		}
		var value [16]byte
		if _, err := io.ReadFull(source, value[:]); err != nil {
			c.localInstanceErr = err
			return
		}
		if value == ([16]byte{}) {
			c.localInstanceErr = errors.New("invalid input authority identity")
			return
		}
		c.localInstance = hex.EncodeToString(value[:])
	})
	return c.localInstance, c.localInstanceErr
}

// SetLocalLeaseManager supplies the authority for generation-bound physical
// input received from the kit's root-only local socket. Configure it before
// starting the listener.
func (c *TargetController) SetLocalLeaseManager(leases *kitlease.Manager) {
	if c != nil {
		c.localLeases = leases
	}
}

type lease struct {
	session             uint64
	token               []byte
	core                string
	server              *bridge.Server
	cancel              context.CancelFunc
	executionGeneration string
	keyboardBinding     *KeyboardHIDBinding
	// lifetime is the admitting execution lease's lifetime. The bridge is
	// cancelled when that lease is revoked, independently of the request.
	lifetime context.Context
}

type localKeyboardGrant struct {
	executionGeneration string
	binding             KeyboardHIDBinding
}

func NewTargetController() *TargetController {
	return NewTargetControllerWithConfig("127.0.0.1:18183", "/dev/uinput")
}

func NewTargetControllerWithConfig(listenAddress, uinputPath string) *TargetController {
	if listenAddress == "" {
		listenAddress = "127.0.0.1:18183"
	}
	if uinputPath == "" {
		uinputPath = "/dev/uinput"
	}
	return &TargetController{listenAddress: listenAddress, uinputPath: uinputPath}
}

func NewNativeTargetControllerWithConfig(listenAddress, uinputPath string) (*TargetController, error) {
	pads, err := bridge.CreateUInputGamepad(uinputPath)
	if err != nil {
		return nil, err
	}
	keys := NewKeyboardSink()
	merged := newPadMerge(pads)
	ports := &controllerPortsSink{fallback: muxSink{keys: keys, pads: merged}, keys: keys, hid: &keyboardHIDSink{}, mouse: &mouseSink{}, pads: merged}
	controller := newTargetControllerWithSink(listenAddress, ports)
	controller.ports = ports
	controller.keyboard = keys
	return controller, nil
}

func (c *TargetController) ConfigureControllerPorts(binding func(context.Context) (*ControllerBinding, error), poster ControllerPoster) {
	if c == nil || c.ports == nil {
		return
	}
	c.portsBinding = binding
	c.ports.poster = poster
}

// ObserveCore stores the runtime read used by Attach and the kit-local feed.
// It does not require a host input lease.
func (c *TargetController) ObserveCore(observe func(context.Context) (CoreObservation, error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.observe = observe
	c.mu.Unlock()
}

// SetKeyboardHIDPoster wires set_keyboard_hid for fes.keyboard.hid sessions.
func (c *TargetController) SetKeyboardHIDPoster(poster KeyboardHIDPoster) {
	if c == nil || c.ports == nil || c.ports.hid == nil {
		return
	}
	c.ports.hid.setPoster(poster)
}

func (c *TargetController) SetMousePoster(poster MousePoster) {
	if c == nil || c.ports == nil || c.ports.mouse == nil {
		return
	}
	c.ports.mouse.mu.Lock()
	defer c.ports.mouse.mu.Unlock()
	c.ports.mouse.poster = poster
}

func (c *TargetController) SetKeyboardPoster(poster MatrixPoster) {
	if c == nil || c.keyboard == nil {
		return
	}
	c.keyboard.SetPoster(poster)
}

func (c *TargetController) SetKeyboardRetirer(retirer MatrixRetirer) {
	if c == nil || c.keyboard == nil {
		return
	}
	c.keyboard.SetRetirer(retirer)
}

func (c *TargetController) SetKeyboardMatrixAdmission(admit func(context.Context, string, string, uint64, func(context.Context) error) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.matrixAdmission = admit
}

func (c *TargetController) SetKeyboardMediaRetirement(query func(context.Context) (localhid.MatrixMediaWitness, bool, error), retire func(context.Context, localhid.MatrixMediaRetirement) error, neutral func(context.Context, localhid.MatrixMediaNeutral) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mediaUncertainty, c.mediaRetirer, c.mediaNeutral = query, retire, neutral
}

// retireMediaObserved is called under lifecycle. The witness is captured
// before a fresh observation and matched again by the adapter under its lock.
func retireMediaObserved(ctx context.Context, observe func(context.Context) (CoreObservation, error), query func(context.Context) (localhid.MatrixMediaWitness, bool, error), retire func(context.Context, localhid.MatrixMediaRetirement) error, validate func(context.Context) error) error {
	if query == nil || retire == nil {
		return nil
	}
	witness, pending, err := query(ctx)
	if err != nil || !pending {
		return err
	}
	if observe == nil {
		return errMatrixUnconfirmed
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return err
	}
	if !matrixObservationRetires(obs, witness.RuntimeInstance, witness.PackageID, witness.Generation) {
		return errMatrixUnconfirmed
	}
	return retire(ctx, localhid.MatrixMediaRetirement{RetireOf: witness,
		ObservedRuntimeInstance: obs.RuntimeInstance, ObservedPackageID: obs.PackageID,
		ObservedGeneration: obs.Generation, Idle: obs.IdleConfirmed, Validate: validate})
}

func newTargetControllerWithSink(listenAddress string, sink bridge.Sink) *TargetController {
	if listenAddress == "" {
		listenAddress = "127.0.0.1:18183"
	}
	return &TargetController{listenAddress: listenAddress, persistent: sink}
}

// beginLifecycleTransition announces a queued lifecycle mutation before it
// waits for the lock. A timed tap may wait for a local input write, but must
// not overtake a queued Stop, replacement, attach or display handoff.
func (c *TargetController) beginLifecycleTransition() func() {
	c.transitionPending.Add(1)
	c.transitionEpoch.Add(1)
	var once sync.Once
	return func() { once.Do(func() { c.transitionPending.Add(-1) }) }
}

func (c *TargetController) Attach(ctx context.Context, spec Spec) error {
	if c == nil || ctx == nil || spec.Session == 0 || len(spec.Token) < 16 || !validCore(spec.Core) {
		return errors.New("invalid input lease")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lifetime := kitlease.LeaseLifetime(ctx)
	if lifetime == nil {
		if kitlease.Admitted(ctx) {
			return errors.New("admitted input lease lifetime is unavailable")
		}
		// An unguarded controller has no kit lease to revoke; its existing
		// attachment lifecycle continues to be controlled by Detach or Close.
		lifetime = context.Background()
	}
	done := c.beginLifecycleTransition()
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := lockInputMutex(ctx, &c.lifecycle); err != nil {
		return err
	}
	defer c.lifecycle.Unlock()
	spec.ExecutionGeneration = kitlease.Generation(ctx)
	return c.attachLocked(ctx, spec, lifetime)
}

// releasePersistentLocked performs only confirmed source-neutral recovery while
// the caller owns lifecycle. Each retirement observation follows the failure.
func (c *TargetController) releasePersistentLocked(ctx context.Context, persistent bridge.Sink) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	release := func(ctx context.Context) error {
		if ports, ok := persistent.(*controllerPortsSink); ok {
			return ports.releaseAllContext(ctx)
		}
		return persistent.ReleaseAll()
	}
	releaseErr := release(ctx)
	if releaseErr == nil {
		return nil
	}
	c.mu.Lock()
	observe, query, retire, neutral := c.observe, c.mediaUncertainty, c.mediaRetirer, c.mediaNeutral
	keyboard := c.keyboard
	ports := c.ports
	c.mu.Unlock()
	var hid *keyboardHIDSink
	if persistentPorts, ok := persistent.(*controllerPortsSink); ok {
		hid = persistentPorts.hid
	} else if ports != nil {
		hid = ports.hid
	}
	epoch := c.transitionEpoch.Load()
	validate := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.transitionEpoch.Load() != epoch {
			return errNoCore
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return errNoCore
		}
		return nil
	}
	if keyboard != nil {
		if _, err := keyboard.neutralMedia(ctx, query, neutral, validate); err == nil {
			releaseErr = release(ctx)
		}
	}
	if releaseErr != nil {
		if err := retireMediaObserved(ctx, observe, query, retire, validate); err != nil {
			return errors.Join(releaseErr, err)
		}
		releaseErr = release(ctx)
	}
	if releaseErr != nil && keyboard != nil {
		if err := keyboard.retireObserved(ctx, observe, validate); err != nil {
			return errors.Join(releaseErr, err)
		}
		releaseErr = release(ctx)
	}
	if releaseErr != nil && hid != nil {
		retired, err := hid.retireLocalV2Observed(ctx, observe, validate)
		if err != nil {
			return errors.Join(releaseErr, err)
		}
		if retired {
			releaseErr = release(ctx)
		}
	}
	if releaseErr != nil && keyboard != nil {
		if err := keyboard.retireUnboundIdle(ctx, observe, validate); err != nil {
			return errors.Join(releaseErr, err)
		}
		releaseErr = release(context.WithValue(ctx, matrixIdleCleanupKey{}, validate))
	}
	return releaseErr
}

func (c *TargetController) attachLocked(ctx context.Context, spec Spec, lifetime context.Context) error {
	if lifetime == nil || lifetime.Err() != nil {
		return errors.New("input lease lifetime is unavailable")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("input controller is closed")
	}
	if c.needsRelease {
		persistent := c.persistent
		c.mu.Unlock()
		if persistent == nil {
			return errors.New("input state could not be released")
		}
		if err := c.releasePersistentLocked(ctx, persistent); err != nil {
			return err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return errors.New("input controller is closed")
		}
		c.needsRelease = false
	}
	current := c.active
	if current != nil && current.session == spec.Session && current.core == spec.Core &&
		current.executionGeneration == spec.ExecutionGeneration &&
		current.lifetime != nil && current.lifetime.Done() == lifetime.Done() &&
		subtle.ConstantTimeCompare(current.token, spec.Token) == 1 {
		c.mu.Unlock()
		return nil
	}
	c.active = nil
	c.mu.Unlock()
	if err := stopLease(current); err != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
		return err
	}

	var sink bridge.Sink
	var remote *remoteSourceSink
	var keyboardBinding *KeyboardHIDBinding
	var matrixBinding *MatrixBinding
	c.mu.Lock()
	observe := c.observe
	bindingFn := c.portsBinding
	c.mu.Unlock()
	if c.ports != nil && (observe != nil || bindingFn != nil) {
		if observe != nil {
			obs, err := observe(ctx)
			if err != nil {
				return err
			}
			if err := c.ports.setObservationContext(ctx, obs); err != nil {
				return err
			}
			if err := c.ports.bind(obs.Binding); err != nil {
				return err
			}
			if obs.Active && obs.KeyboardHID != nil {
				copy := *obs.KeyboardHID
				keyboardBinding = &copy
			}
			if obs.Active && obs.Keyboard && obs.RuntimeInstance != "" && obs.Generation != 0 {
				matrixBinding = &MatrixBinding{RuntimeInstance: obs.RuntimeInstance, PackageID: obs.PackageID, Generation: obs.Generation}
			}
		} else {
			binding, err := bindingFn(ctx)
			if err != nil {
				return err
			}
			if err := c.ports.bind(binding); err != nil {
				return err
			}
		}
	}
	if c.ports != nil {
		// The host bridge releases only its own source. The retained sink
		// still receives lease and core-replacement neutralization.
		remote = &remoteSourceSink{ports: c.ports, matrix: matrixBinding, hid: keyboardBinding, controller: c}
		retained := retainedSink{Sink: remote}
		if c.ports.hid != nil {
			sink = retainedTapSink{retainedSink: retained, controller: c, hid: c.ports.hid, binding: keyboardBinding}
		} else {
			sink = retained
		}
	} else if c.persistent != nil {
		sink = retainedSink{Sink: c.persistent}
	} else {
		var err error
		sink, err = bridge.OpenUInput(c.uinputPath)
		if err != nil {
			return err
		}
	}
	server, err := bridge.New(bridge.Config{
		Addr:    c.listenAddress,
		Token:   append([]byte(nil), spec.Token...),
		Session: spec.Session,
		Core:    spec.Core,
		Listen:  c.listen,
	}, sink)
	if err != nil {
		_ = sink.Close()
		return err
	}
	if remote != nil {
		remote.server = server
	}
	leaseCtx, cancel := context.WithCancel(lifetime)
	candidate := &lease{
		session:             spec.Session,
		token:               append([]byte(nil), spec.Token...),
		core:                spec.Core,
		server:              server,
		cancel:              cancel,
		executionGeneration: spec.ExecutionGeneration,
		keyboardBinding:     keyboardBinding,
		lifetime:            lifetime,
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.Join(errors.New("input controller is closed"), stopLease(candidate))
	}
	c.pending = candidate
	c.mu.Unlock()
	go func() { _ = server.ListenAndServe(leaseCtx) }()
	var startupErr error
	select {
	case <-ctx.Done():
		startupErr = ctx.Err()
	case <-lifetime.Done():
		startupErr = lifetime.Err()
	case startupErr = <-server.Startup():
	}
	if startupErr == nil {
		startupErr = errors.Join(ctx.Err(), lifetime.Err())
	}
	if startupErr != nil {
		stopErr := stopLease(candidate)
		c.mu.Lock()
		if c.pending == candidate {
			c.pending = nil
		}
		if stopErr != nil {
			c.needsRelease = c.persistent != nil
		}
		c.mu.Unlock()
		return errors.Join(startupErr, stopErr)
	}

	c.mu.Lock()
	if c.pending == candidate {
		c.pending = nil
	}
	if c.closed {
		c.mu.Unlock()
		return errors.Join(errors.New("input controller is closed"), stopLease(candidate))
	}
	if c.active != nil {
		previous := c.active
		c.active = candidate
		c.mu.Unlock()
		stopLease(previous)
		return nil
	}
	c.active = candidate
	c.mu.Unlock()
	return nil
}

// GrantLocalKeyboard is an explicit request by the current execution owner.
// It belongs to the execution lease and HID generation observed now. A remote
// stream detach alone does not revoke a kit-local hold.
func (c *TargetController) LocalKeyboardBinding() (KeyboardHIDBinding, bool) {
	if c == nil {
		return KeyboardHIDBinding{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil || c.active.executionGeneration == "" || c.active.keyboardBinding == nil {
		return KeyboardHIDBinding{}, false
	}
	return *c.active.keyboardBinding, true
}

func (c *TargetController) GrantLocalKeyboard(ctx context.Context, session uint64, expected KeyboardHIDBinding) error {
	if c == nil || ctx == nil || !kitlease.Admitted(ctx) || kitlease.Generation(ctx) == "" || session == 0 {
		return errors.New("local keyboard admission requires the execution owner")
	}
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	active, observe := c.active, c.observe
	c.mu.Unlock()
	if active == nil || active.session != session || active.executionGeneration != kitlease.Generation(ctx) ||
		active.keyboardBinding == nil || *active.keyboardBinding != expected || observe == nil {
		return errors.New("local keyboard input session is unavailable")
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	defer cancel()
	obs, err := observe(probe)
	if err != nil || !obs.Active || obs.KeyboardHID == nil || *obs.KeyboardHID != expected || expected.PackageID == "" || expected.Generation == 0 {
		return errors.New("local HID keyboard core is unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != active || ctx.Err() != nil {
		return errors.New("local keyboard input session changed")
	}
	c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: kitlease.Generation(ctx), binding: *obs.KeyboardHID}
	return nil
}

func (c *TargetController) localKeyboardAdmitted(binding localDelegation) bool {
	if c.localLeases.LocalHostGeneration(binding.LeaseGeneration) {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.localKeyboardGrant != nil && c.localKeyboardGrant.executionGeneration == binding.LeaseGeneration &&
		c.localKeyboardGrant.binding.RuntimeInstance == binding.RuntimeInstance &&
		c.localKeyboardGrant.binding.PackageID == binding.PackageID && c.localKeyboardGrant.binding.Generation == binding.CoreGeneration
}

// BeginCoreReplacement closes the active producer, waits for its in-flight
// writes, and neutralizes the retained sink before the runtime may open a new
// input reader. The lifecycle lock stays held until the returned function
// runs. Kit-local delivery uses that same lock and drops a frame while it is
// held, so the frame cannot rebind the generation ReleaseAll just cleared.
// The returned function must be called exactly once. Passing preserve
// reconstructs the same logical lease after a proven pre-mutation failure;
// false permanently retires it.
func (c *TargetController) BeginCoreReplacement(ctx context.Context) (func(context.Context, bool) error, error) {
	if c == nil || ctx == nil {
		return nil, errors.New("invalid input replacement barrier")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := c.beginLifecycleTransition()
	transferred := false
	defer func() {
		if !transferred {
			done()
		}
	}()
	if err := lockInputMutex(ctx, &c.lifecycle); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.lifecycle.Unlock()
		return nil, errors.New("input controller is closed")
	}
	current := c.active
	grant := c.localKeyboardGrant
	c.active = nil
	c.localKeyboardGrant = nil
	c.mu.Unlock()
	stopped := false
	if err := stopLease(current); err != nil {
		closeErr, pureClose := err.(*bridge.CloseError)
		if c.ports != nil && pureClose && closeErr.OnlyRelease() {
			if recoveryErr := c.releasePersistentLocked(ctx, c.ports); recoveryErr == nil {
				stopped = true
			} else {
				err = errors.Join(err, recoveryErr)
			}
		}
		if !stopped {
			c.mu.Lock()
			c.needsRelease = c.persistent != nil
			c.mu.Unlock()
			c.lifecycle.Unlock()
			return nil, err
		}
	}
	if c.ports != nil && !stopped {
		if err := c.releasePersistentLocked(ctx, c.ports); err != nil {
			c.mu.Lock()
			c.needsRelease = true
			c.mu.Unlock()
			c.lifecycle.Unlock()
			return nil, err
		}
	}

	var once sync.Once
	var finishErr error
	finish := func(finishCtx context.Context, preserve bool) error {
		once.Do(func() {
			defer done()
			defer c.lifecycle.Unlock()
			if !preserve || current == nil {
				return
			}
			if finishCtx == nil {
				finishErr = errors.New("invalid input replacement completion")
				return
			}
			finishErr = c.attachLocked(finishCtx, Spec{
				Session:             current.session,
				Token:               append([]byte(nil), current.token...),
				Core:                current.core,
				ExecutionGeneration: current.executionGeneration,
			}, current.lifetime)
			if finishErr == nil {
				c.mu.Lock()
				c.localKeyboardGrant = grant
				c.mu.Unlock()
			}
		})
		return finishErr
	}
	transferred = true
	return finish, nil
}

func (c *TargetController) Detach(ctx context.Context, session uint64) error {
	if c == nil || session == 0 {
		return errors.New("invalid input session")
	}
	done := c.beginLifecycleTransition()
	defer done()
	c.mu.Lock()
	pending := c.pending
	if pending != nil && pending.session == session {
		c.pending = nil
	} else {
		pending = nil
	}
	c.mu.Unlock()
	pendingErr := stopLease(pending)
	if pendingErr != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
	}

	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	current := c.active
	if current == nil || current.session != session {
		c.mu.Unlock()
		if pending == nil {
			return nil
		}
		if ctx == nil {
			return pendingErr
		}
		return errors.Join(pendingErr, ctx.Err())
	}
	c.active = nil
	c.mu.Unlock()
	releaseErr := errors.Join(pendingErr, stopLease(current))
	if releaseErr != nil {
		c.mu.Lock()
		c.needsRelease = c.persistent != nil
		c.mu.Unlock()
	}
	if ctx == nil {
		return releaseErr
	}
	return errors.Join(releaseErr, ctx.Err())
}

func (c *TargetController) OpenStream(ctx context.Context, session uint64) (net.Conn, error) {
	if c == nil || ctx == nil || session == 0 {
		return nil, errors.New("invalid input session")
	}
	c.mu.Lock()
	current := c.active
	if current == nil || current.session != session || current.server == nil || current.server.Addr() == nil {
		c.mu.Unlock()
		return nil, errors.New("input session is unavailable")
	}
	endpoint := current.server.Addr().String()
	c.mu.Unlock()
	dialer := net.Dialer{Timeout: streamDialTimeout}
	return dialer.DialContext(ctx, "tcp", endpoint)
}

// ReleaseAll ends remote input without destroying the retained uinput device.
// The kit lease calls this only after all admitted HTTP operations finish.
func (c *TargetController) ReleaseAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := c.beginLifecycleTransition()
	defer done()
	c.mu.Lock()
	pending, current := c.pending, c.active
	c.mu.Unlock()
	var result error
	if pending != nil {
		result = errors.Join(result, c.Detach(ctx, pending.session))
	}
	if current != nil {
		result = errors.Join(result, c.Detach(ctx, current.session))
	}
	if err := lockInputMutex(ctx, &c.lifecycle); err != nil {
		return errors.Join(result, err)
	}
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	c.localKeyboardGrant = nil
	persistent := c.persistent
	c.mu.Unlock()
	if persistent != nil {
		releaseErr := c.releasePersistentLocked(ctx, persistent)
		result = errors.Join(result, releaseErr)
		c.mu.Lock()
		c.needsRelease = releaseErr != nil
		c.mu.Unlock()
	}
	return result
}

func (c *TargetController) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		done := c.beginLifecycleTransition()
		defer done()
		c.closeLocal()
		c.mu.Lock()
		c.closed = true
		pending := c.pending
		c.pending = nil
		c.mu.Unlock()
		pendingErr := stopLease(pending)

		c.lifecycle.Lock()
		defer c.lifecycle.Unlock()
		c.mu.Lock()
		current := c.active
		c.active = nil
		persistent := c.persistent
		c.persistent = nil
		needsRelease := c.needsRelease
		c.needsRelease = false
		c.mu.Unlock()
		stopErr := stopLease(current)
		var releaseErr error
		var persistentErr error
		if persistent != nil {
			if needsRelease {
				releaseErr = persistent.ReleaseAll()
			}
			persistentErr = persistent.Close()
		}
		c.closeErr = errors.Join(pendingErr, stopErr, releaseErr, persistentErr)
	})
	return c.closeErr
}

type retainedSink struct {
	bridge.Sink
}

func (retainedSink) Close() error { return nil }
func (s retainedSink) ApplyContext(ctx context.Context, frame protocol.InputFrame) error {
	if sink, ok := s.Sink.(bridge.ContextSink); ok {
		return sink.ApplyContext(ctx, frame)
	}
	return s.Sink.Apply(frame)
}
func (s retainedSink) ReleaseAllContext(ctx context.Context) error {
	if sink, ok := s.Sink.(bridge.ContextReleaseSink); ok {
		return sink.ReleaseAllContext(ctx)
	}
	return s.Sink.ReleaseAll()
}

// Only the native HID sink offers target-timed taps. Other bridges continue
// their existing input behavior and decline the optional tap probe.
type retainedTapSink struct {
	retainedSink
	controller *TargetController
	hid        *keyboardHIDSink
	binding    *KeyboardHIDBinding
}

func (s retainedTapSink) TapChord(ctx context.Context, f protocol.InputFrame) error {
	return s.Tap(ctx, f)
}

func (s retainedTapSink) Tap(ctx context.Context, f protocol.InputFrame) error {
	// Match kit-local admission: no new press may race core replacement or
	// an unconfirmed active generation. Do not hold lifecycle over the tap
	// interval, because doing so would drop unrelated kit-local input.
	if ctx == nil {
		ctx = context.Background()
	}
	if s.hid == nil || s.hid.focusFence.Load() {
		return errTapUnavailable
	}
	focusEpoch := s.hid.fenceEpoch.Load()
	transitionEpoch := s.controller.transitionEpoch.Load()
	if s.controller.transitionPending.Load() != 0 {
		return errTapUnavailable
	}
	s.controller.mu.Lock()
	admittedLease, closed := s.controller.active, s.controller.closed
	s.controller.mu.Unlock()
	if closed {
		return errTapUnavailable
	}
	// A kit-local key may be in its bounded status/read and HID write while
	// this independent remote tap arrives. Wait for that short operation,
	// then observe the core again under the lifecycle fence. Cancellation or
	// a longer-running replacement still rejects without dispatching a tap.
	wait, cancelWait := context.WithTimeout(ctx, localCoreObserveTimeout+localCoreWriteTimeout)
	defer cancelWait()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := wait.Err(); err != nil {
			return errors.Join(errTapUnavailable, err)
		}
		if s.controller.lifecycle.TryLock() {
			if err := wait.Err(); err != nil {
				s.controller.lifecycle.Unlock()
				return errors.Join(errTapUnavailable, err)
			}
			break
		}
		select {
		case <-wait.Done():
			return errors.Join(errTapUnavailable, wait.Err())
		case <-ticker.C:
		}
	}
	locked := true
	defer func() {
		if locked {
			s.controller.lifecycle.Unlock()
		}
	}()
	// A waiter admitted before a focus handoff, detach or replacement must
	// never dispatch into the resulting session, even if its core generation
	// later happens to match again.
	s.controller.mu.Lock()
	sameLease := s.controller.active == admittedLease && !s.controller.closed
	s.controller.mu.Unlock()
	if !sameLease || s.controller.transitionPending.Load() != 0 || s.controller.transitionEpoch.Load() != transitionEpoch ||
		s.hid.focusFence.Load() || s.hid.fenceEpoch.Load() != focusEpoch {
		return errTapUnavailable
	}
	observeCtx, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	s.controller.mu.Lock()
	observe := s.controller.observe
	s.controller.mu.Unlock()
	if observe == nil {
		cancel()
		return errTapUnavailable
	}
	obs, err := observe(observeCtx)
	if err == nil && observeCtx.Err() != nil {
		err = observeCtx.Err()
	}
	if err != nil || !obs.Active || s.binding == nil || obs.KeyboardHID == nil || *obs.KeyboardHID != *s.binding || obs.KeyboardHID.Generation != f.ServerMonoNS {
		cancel()
		return errors.Join(errTapUnavailable, err)
	}
	err = s.controller.setLocalObservation(observeCtx, obs)
	cancel()
	if err != nil {
		return errors.Join(errTapUnavailable, err)
	}
	return s.hid.tap(ctx, f, func() {
		locked = false
		s.controller.lifecycle.Unlock()
	})
}

func stopLease(current *lease) error {
	if current == nil {
		return nil
	}
	if current.cancel != nil {
		current.cancel()
	}
	if current.server != nil {
		return current.server.Close()
	}
	return nil
}

func validCore(core string) bool {
	if core == "" || len(core) > 128 {
		return false
	}
	for _, char := range core {
		if char < 0x20 || char == 0x7f || char == '/' || char == '\\' {
			return false
		}
	}
	return true
}

var _ Controller = (*TargetController)(nil)
