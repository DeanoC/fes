package agent

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

const durableActiveReconcileTimeout = 40 * time.Second

type Runtime interface {
	Health(string) protocol.Health
	Reconcile(context.Context) protocol.Status
	Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError)
	Launch(context.Context, mister.PreparedLaunch) (observed string, dispatchAttempted bool, apiErr *protocol.APIError)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (observed string, dispatchAttempted bool, apiErr *protocol.APIError)
	RecoverDevelopment(context.Context) (observed string, apiErr *protocol.APIError)
	Stop(context.Context) (string, *protocol.APIError)
}

type idleConfirmingRuntime interface {
	ConfirmIdle(context.Context) bool
}

type stopReadyRuntime interface {
	StopReady() bool
}

type ownedLaunchRuntime interface {
	LaunchOwned(context.Context, context.Context, context.Context, mister.PreparedLaunch) (observed string, dispatchAttempted bool, apiErr *protocol.APIError)
}

type ownedDevelopmentRuntime interface {
	LoadDevelopmentRBFOwned(context.Context, context.Context, context.Context, int64, io.Reader) (observed string, dispatchAttempted bool, apiErr *protocol.APIError)
}

type ownedDevelopmentRecoveryRuntime interface {
	LoadDevelopmentRBFOwnedWithRecovery(context.Context, context.Context, context.Context, int64, io.Reader) (observed, recovery string, dispatchAttempted bool, apiErr *protocol.APIError)
}

type ownedCoreRuntime interface {
	LoadCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader) (misterruntime.CoreActivation, bool, *protocol.APIError)
}

type coreInspectingRuntime interface {
	InspectCore(context.Context, int64, io.Reader) (protocol.CoreInspection, *protocol.APIError)
}

type ownedStopRuntime interface {
	StopOwned(context.Context, context.Context) (observed string, apiErr *protocol.APIError)
}

type ownedStopRecoveryRuntime interface {
	StopOwnedWithRecovery(context.Context, context.Context) (observed, recovery string, apiErr *protocol.APIError)
}

type CoordinatorOption func(*Coordinator)

// WithOperationContext roots already-admitted runtime mutations in the agent
// process lifetime instead of the lifetime of one HTTP connection.
func WithOperationContext(ctx context.Context) CoordinatorOption {
	return func(coordinator *Coordinator) {
		if ctx != nil {
			coordinator.operationContext = ctx
		}
	}
}

type Coordinator struct {
	runtime          Runtime
	registry         core.Registry
	content          ContentStore
	operationContext context.Context
	launchTimeout    time.Duration
	stopTimeout      time.Duration
	transition       chan struct{}
	mu               sync.RWMutex
	status           protocol.Status
	updateBlocked    bool
}

func New(runtime Runtime, registry core.Registry, launchTimeout, stopTimeout time.Duration, options ...CoordinatorOption) *Coordinator {
	coordinator := &Coordinator{
		runtime:          runtime,
		registry:         registry,
		operationContext: context.Background(),
		launchTimeout:    launchTimeout,
		stopTimeout:      stopTimeout,
		transition:       make(chan struct{}, 1),
		status:           protocol.Status{State: protocol.StateIdle},
	}
	for _, option := range options {
		if option != nil {
			option(coordinator)
		}
	}
	return coordinator
}

func (c *Coordinator) begin() bool {
	select {
	case c.transition <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *Coordinator) end() {
	<-c.transition
}

func (c *Coordinator) set(status protocol.Status) {
	c.mu.Lock()
	c.status = cloneStatus(status)
	c.mu.Unlock()
}

func (c *Coordinator) Status() protocol.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneStatus(c.status)
}

func (c *Coordinator) Health(version string) protocol.Health {
	health := c.runtime.Health(version)
	c.mu.RLock()
	blocked := c.updateBlocked
	c.mu.RUnlock()
	if blocked {
		health.Ready = false
	}
	status := c.Status()
	_, nativeIdle := c.runtime.(idleConfirmingRuntime)
	if status.State == protocol.StateFailed && (nativeIdle || (status.LastError != nil && status.LastError.Code == protocol.CodeMiSTerUnavailable)) {
		health.Ready = false
	}
	return health
}

func (c *Coordinator) Initialize(ctx context.Context) {
	status := c.runtime.Reconcile(ctx)
	if validActiveDevelopment(status) {
		c.set(status)
		return
	}
	verification, cancel := context.WithTimeout(context.WithoutCancel(ctx), durableActiveReconcileTimeout)
	defer cancel()
	status, selected, conclusive := c.reconcileDurableActive(verification, status)
	if c.content != nil && conclusive {
		if apiErr := c.content.ReconcileActive(verification, status, selected); apiErr != nil {
			status.State = protocol.StateFailed
			status.System = nil
			status.ExpectedCore = nil
			status.LastError = cloneAPIError(apiErr)
		}
	}
	c.set(status)
}

func (c *Coordinator) reconcileDurableActive(ctx context.Context, status protocol.Status) (protocol.Status, *targetcache.ActiveRecordEntry, bool) {
	if c.content == nil {
		return status, nil, true
	}
	records, ok, apiErr := c.content.ActiveRecordSystems(ctx)
	if apiErr != nil {
		status.State = protocol.StateFailed
		status.System = nil
		status.ExpectedCore = nil
		status.LastError = cloneAPIError(apiErr)
		return status, nil, false
	}
	if !ok {
		return status, nil, true
	}
	if status.State == protocol.StateIdle {
		return status, nil, true
	}
	if status.State != protocol.StateActive && status.State != protocol.StateFailed {
		if records.Interrupted {
			return interruptedLaunchStatus(status), nil, false
		}
		return status, &records.Candidate, true
	}
	canResolve := status.ObservedCore != nil && (status.State == protocol.StateActive ||
		(status.LastError != nil && status.LastError.Code == protocol.CodeUnrecognizedCore))
	if !canResolve {
		if records.Interrupted {
			if status.State == protocol.StateActive {
				return interruptedLaunchStatus(status), nil, false
			}
			return status, nil, false
		}
		return status, nil, false
	}
	selected := records.Candidate
	if records.Interrupted {
		candidateMatches := observedCoreMatchesSystem(c.registry, *status.ObservedCore, records.Candidate.System)
		previousMatches := records.Previous != nil && observedCoreMatchesSystem(c.registry, *status.ObservedCore, records.Previous.System)
		if candidateMatches == previousMatches {
			if candidateMatches && records.Previous != nil && records.Candidate == *records.Previous {
				selected = records.Candidate
			} else {
				return interruptedLaunchStatus(status), nil, false
			}
		} else if previousMatches {
			selected = *records.Previous
		}
	}
	system := selected.System
	spec, ok := c.registry.LookupObservedForSystem(*status.ObservedCore, system)
	if !ok {
		if records.Interrupted {
			return interruptedLaunchStatus(status), nil, false
		}
		return status, &selected, true
	}
	expected := spec.ExpectedCore
	status.State = protocol.StateActive
	status.System = &system
	status.ExpectedCore = &expected
	status.LastError = nil
	return status, &selected, true
}

func observedCoreMatchesSystem(registry core.Registry, observed string, system protocol.System) bool {
	_, ok := registry.LookupObservedForSystem(observed, system)
	return ok
}

func interruptedLaunchStatus(status protocol.Status) protocol.Status {
	status.State = protocol.StateFailed
	status.System = nil
	status.ExpectedCore = nil
	status.LastError = &protocol.APIError{Code: protocol.CodeInternal, Message: "a launch was interrupted and requires reconciliation"}
	return status
}

func (c *Coordinator) Launch(parent context.Context, request protocol.LaunchRequest) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	if err := protocol.ValidateGameID(request.GameID); err != nil {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBadRequest, Message: err.Error()}
	}
	spec, ok := c.registry.Lookup(request.System)
	if !ok {
		return c.Status(), unsupportedSystemError()
	}
	var intent targetcache.LaunchIntent
	intentRecorded := false
	var recordIntent func() *protocol.APIError
	if c.content != nil {
		recordIntent = func() *protocol.APIError {
			var apiErr *protocol.APIError
			intent, apiErr = c.content.RecordDirectLaunchIntent(request.System)
			intentRecorded = apiErr == nil
			return apiErr
		}
	}
	status, dispatchAttempted, apiErr := c.launchWithIntent(parent, request.GameID, spec, request.ROMPath, recordIntent)
	if apiErr != nil {
		if intentRecorded && !dispatchAttempted {
			_ = c.content.AbortDirectLaunch(request.System, intent)
		}
		return status, apiErr
	}
	if c.content != nil {
		if apiErr := c.content.CommitDirectLaunch(request.System, intent); apiErr != nil {
			return status, &protocol.APIError{Code: protocol.CodeInternal, Message: "direct active record cannot be committed"}
		}
	}
	return status, nil
}

func (c *Coordinator) launchWithIntent(parent context.Context, gameID string, spec core.Spec, romPath string, recordIntent func() *protocol.APIError) (protocol.Status, bool, *protocol.APIError) {
	if !c.Health("").Ready {
		return c.Status(), false, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
	prepared, apiErr := c.runtime.Prepare(spec, romPath)
	if apiErr != nil {
		return c.Status(), false, apiErr
	}
	prepared.GameID = gameID
	if recordIntent != nil {
		if apiErr := recordIntent(); apiErr != nil {
			return c.Status(), false, apiErr
		}
	}
	system, expected := spec.System, spec.ExpectedCore
	c.set(protocol.Status{State: protocol.StateLaunching, GameID: &gameID, System: &system, ExpectedCore: &expected})
	var observed string
	var dispatchAttempted bool
	if runtime, ok := c.runtime.(ownedLaunchRuntime); ok {
		observation, cancel := context.WithTimeout(c.operationContext, c.launchTimeout)
		defer cancel()
		observed, dispatchAttempted, apiErr = runtime.LaunchOwned(parent, observation, c.operationContext, prepared)
	} else {
		ctx, cancel := context.WithTimeout(parent, c.launchTimeout)
		defer cancel()
		observed, dispatchAttempted, apiErr = c.runtime.Launch(ctx, prepared)
	}
	if apiErr != nil {
		failed := protocol.Status{State: protocol.StateFailed, GameID: &gameID, System: &system, ExpectedCore: &expected, LastError: cloneAPIError(apiErr)}
		if observed != "" {
			failed.ObservedCore = &observed
		}
		// The runtime may already have recovered from this failed attempt.
		// Keep the launch error, but publish idle only after physical and
		// durable-content cleanup are both confirmed under this transition.
		if runtime, ok := c.runtime.(idleConfirmingRuntime); ok && runtime.ConfirmIdle(c.operationContext) {
			if c.content == nil || c.content.ClearActive() == nil {
				c.set(protocol.Status{State: protocol.StateIdle, LastError: cloneAPIError(apiErr)})
				return c.Status(), dispatchAttempted, apiErr
			}
		}
		c.set(failed)
		return c.Status(), dispatchAttempted, apiErr
	}
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
	c.set(active)
	return c.Status(), dispatchAttempted, nil
}

func (c *Coordinator) LoadDevelopmentRBF(parent context.Context, size int64, content io.Reader) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	if !c.Health("").Ready {
		return c.Status(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
	previous := c.Status()
	c.set(protocol.Status{State: protocol.StateLaunching, Development: true})
	var observed string
	var dispatchAttempted bool
	var recovery string
	var apiErr *protocol.APIError
	if runtime, ok := c.runtime.(ownedDevelopmentRuntime); ok {
		observation, cancel := context.WithTimeout(c.operationContext, c.launchTimeout)
		defer cancel()
		if recoveryRuntime, ok := c.runtime.(ownedDevelopmentRecoveryRuntime); ok {
			observed, recovery, dispatchAttempted, apiErr = recoveryRuntime.LoadDevelopmentRBFOwnedWithRecovery(parent, observation, c.operationContext, size, content)
		} else {
			observed, dispatchAttempted, apiErr = runtime.LoadDevelopmentRBFOwned(parent, observation, c.operationContext, size, content)
		}
	} else {
		ctx, cancel := context.WithTimeout(parent, c.launchTimeout)
		defer cancel()
		observed, dispatchAttempted, apiErr = c.runtime.LoadDevelopmentRBF(ctx, size, content)
	}
	if recovery == protocol.RecoveryRebootRequired {
		pending := protocol.Status{State: protocol.StateStopping, Development: true, Recovery: recovery}
		if observed != "" {
			pending.ObservedCore = &observed
		}
		c.set(pending)
		if apiErr == nil {
			apiErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
		}
		return c.Status(), apiErr
	}
	if apiErr != nil {
		if !dispatchAttempted && apiErr.Code == protocol.CodeUnsupportedOperation {
			c.set(previous)
			return c.Status(), apiErr
		}
		failed := protocol.Status{State: protocol.StateFailed, Development: true, LastError: cloneAPIError(apiErr)}
		if observed != "" {
			failed.ObservedCore = &observed
		}
		c.set(failed)
		return c.Status(), apiErr
	}
	active := protocol.Status{State: protocol.StateActive, Development: true}
	if observed != "" && observed != "MENU" {
		active.ObservedCore = &observed
	}
	c.set(active)
	return c.Status(), nil
}

func (c *Coordinator) LoadCore(parent context.Context, size int64, content io.Reader) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(ownedCoreRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation,
			Message: "requested operation is unsupported"}
	}
	previous := c.Status()
	observation, cancel := context.WithTimeout(c.operationContext, c.launchTimeout)
	defer cancel()
	activation, attempted, apiErr := runtime.LoadCoreOwned(parent, observation,
		c.operationContext, size, content)
	if apiErr != nil {
		if !attempted {
			c.set(previous)
			return c.Status(), apiErr
		}
		if runtime, ok := c.runtime.(idleConfirmingRuntime); ok && runtime.ConfirmIdle(c.operationContext) {
			c.set(protocol.Status{State: protocol.StateIdle, LastError: cloneAPIError(apiErr)})
			return c.Status(), apiErr
		}
		failed := protocol.Status{State: protocol.StateFailed, Development: true,
			LastError: cloneAPIError(apiErr)}
		if activation.ObservedCore != "" {
			failed.ObservedCore = &activation.ObservedCore
		}
		c.set(failed)
		return c.Status(), apiErr
	}
	active := protocol.Status{State: protocol.StateActive, Development: true}
	if activation.ObservedCore != "" {
		active.ObservedCore = &activation.ObservedCore
	}
	interfaces := make([]protocol.RuntimeInterface, len(activation.ActiveInterfaces))
	for index, value := range activation.ActiveInterfaces {
		interfaces[index] = protocol.RuntimeInterface{ID: value.ID, Major: value.Major, Minor: value.Minor}
	}
	active.CorePackage = &protocol.CorePackageStatus{
		PackageID: activation.PackageID, Generation: activation.Generation,
		ABI: protocol.RuntimeContract{ID: activation.Descriptor.ABI.ID,
			Major: uint16(activation.Descriptor.ABI.Major), Minor: uint16(activation.Descriptor.ABI.Minor)},
		BuildID: activation.Descriptor.Build.ID, ActiveInterfaces: interfaces,
		Gamepad: activation.Gamepad,
	}
	c.set(active)
	return c.Status(), nil
}

// InspectCore serializes the read-only runtime inspection with target
// transitions while leaving the published game/session state unchanged.
func (c *Coordinator) InspectCore(parent context.Context, size int64, content io.Reader) (protocol.CoreInspection, *protocol.APIError) {
	if !c.begin() {
		return protocol.CoreInspection{}, &protocol.APIError{Code: protocol.CodeBusy,
			Message: "another launch or stop transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(coreInspectingRuntime)
	if !ok {
		return protocol.CoreInspection{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation,
			Message: "requested operation is unsupported"}
	}
	return runtime.InspectCore(parent, size, content)
}

func (c *Coordinator) Stop(parent context.Context) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	return c.stopLocked(parent)
}

func (c *Coordinator) stopLocked(parent context.Context) (protocol.Status, *protocol.APIError) {
	current := c.Status()
	if current.State == protocol.StateIdle {
		return current, nil
	}
	if current.State == protocol.StateStopping && current.Development && current.Recovery == protocol.RecoveryRebootRequired {
		return current, nil
	}
	if current.Development {
		if _, native := c.runtime.(ownedDevelopmentRuntime); !native {
			stopping := cloneStatus(current)
			stopping.State = protocol.StateStopping
			stopping.LastError = nil
			stopping.Recovery = protocol.RecoveryRebootRequired
			c.set(stopping)
			return c.Status(), nil
		}
	}
	if !runtimeStopReady(c.runtime) {
		return current, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
	stopping := cloneStatus(current)
	stopping.State = protocol.StateStopping
	stopping.LastError = nil
	c.set(stopping)
	var observed string
	var recovery string
	var apiErr *protocol.APIError
	if runtime, ok := c.runtime.(ownedStopRuntime); ok {
		operation, cancel := context.WithTimeout(c.operationContext, c.stopTimeout)
		defer cancel()
		if recoveryRuntime, ok := c.runtime.(ownedStopRecoveryRuntime); ok {
			observed, recovery, apiErr = recoveryRuntime.StopOwnedWithRecovery(parent, operation)
		} else {
			observed, apiErr = runtime.StopOwned(parent, operation)
		}
	} else {
		ctx, cancel := context.WithTimeout(parent, c.stopTimeout)
		defer cancel()
		observed, apiErr = c.runtime.Stop(ctx)
	}
	if apiErr == nil && current.Development && recovery == protocol.RecoveryRebootRequired {
		stopping.Recovery = recovery
		if observed != "" {
			stopping.ObservedCore = &observed
		}
		c.set(stopping)
		return c.Status(), nil
	}
	if apiErr == nil && recovery != "" {
		apiErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
	if apiErr != nil {
		failed := cloneStatus(stopping)
		failed.State = protocol.StateFailed
		failed.LastError = cloneAPIError(apiErr)
		if observed == "" {
			failed.ObservedCore = nil
		} else {
			failed.ObservedCore = &observed
		}
		c.set(failed)
		return c.Status(), apiErr
	}
	if c.content != nil {
		if apiErr := c.content.ClearActive(); apiErr != nil {
			failed := cloneStatus(stopping)
			failed.State = protocol.StateFailed
			failed.LastError = cloneAPIError(apiErr)
			if observed == "" {
				failed.ObservedCore = nil
			} else {
				failed.ObservedCore = &observed
			}
			c.set(failed)
			return c.Status(), apiErr
		}
	}
	c.set(protocol.Status{State: protocol.StateIdle})
	return c.Status(), nil
}

func runtimeStopReady(runtime Runtime) bool {
	if operationReady, ok := runtime.(stopReadyRuntime); ok {
		return operationReady.StopReady()
	}
	return runtime.Health("").Ready
}

func validActiveDevelopment(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development &&
		status.GameID == nil && status.System == nil && status.ExpectedCore == nil &&
		(status.ObservedCore == nil || *status.ObservedCore != "") &&
		status.LastError == nil && status.Recovery == ""
}

func (c *Coordinator) RebootDevelopment(parent context.Context) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	current := c.Status()
	if current.State != protocol.StateStopping || !current.Development || current.Recovery != protocol.RecoveryRebootRequired {
		return current, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "development reboot was not requested"}
	}
	if c.content != nil {
		if apiErr := c.content.ClearActive(); apiErr != nil {
			failed := cloneStatus(current)
			failed.State = protocol.StateFailed
			failed.LastError = cloneAPIError(apiErr)
			c.set(failed)
			return c.Status(), apiErr
		}
	}
	ctx, cancel := context.WithTimeout(parent, c.stopTimeout)
	defer cancel()
	observed, apiErr := c.runtime.RecoverDevelopment(ctx)
	if apiErr != nil {
		failed := cloneStatus(current)
		failed.State = protocol.StateFailed
		failed.LastError = cloneAPIError(apiErr)
		if observed != "" {
			failed.ObservedCore = &observed
		}
		c.set(failed)
		return c.Status(), apiErr
	}
	return c.Status(), nil
}

func cloneStatus(status protocol.Status) protocol.Status {
	copy := status
	if status.GameID != nil {
		value := *status.GameID
		copy.GameID = &value
	}
	if status.System != nil {
		value := *status.System
		copy.System = &value
	}
	if status.ExpectedCore != nil {
		value := *status.ExpectedCore
		copy.ExpectedCore = &value
	}
	if status.ObservedCore != nil {
		value := *status.ObservedCore
		copy.ObservedCore = &value
	}
	copy.LastError = cloneAPIError(status.LastError)
	if status.CorePackage != nil {
		packageCopy := *status.CorePackage
		packageCopy.ActiveInterfaces = append([]protocol.RuntimeInterface(nil), status.CorePackage.ActiveInterfaces...)
		copy.CorePackage = &packageCopy
	}
	return copy
}

func cloneAPIError(apiErr *protocol.APIError) *protocol.APIError {
	if apiErr == nil {
		return nil
	}
	copy := *apiErr
	return &copy
}
