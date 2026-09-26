package agent

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/flightdiag"

	"github.com/DeanoC/FogCast/internal/misterruntime"

	"github.com/DeanoC/FogCast/protocol"
)

type Runtime interface {
	Health(string) protocol.Health
	Reconcile(context.Context) protocol.Status
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

// idleRecoveryRuntime programs idle again without rebooting the board.
type idleRecoveryRuntime interface {
	RecoverIdle(context.Context) (idle bool, apiErr *protocol.APIError)
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

func WithEventSink(sink flightdiag.Sink) CoordinatorOption {
	return func(coordinator *Coordinator) { coordinator.events = sink }
}

// WithCoreLoadTimeout allows package admission, including target-side ROM
// composition, to have a longer observation budget than raw-RBF diagnostics.
func WithCoreLoadTimeout(timeout time.Duration) CoordinatorOption {
	return func(coordinator *Coordinator) {
		if timeout > 0 {
			coordinator.coreLoadTimeout = timeout
		}
	}
}

// WithArtifacts attaches the sealed target identity reported on Health.
func WithArtifacts(artifacts *protocol.Artifacts) CoordinatorOption {
	return func(coordinator *Coordinator) {
		coordinator.artifacts = cloneArtifacts(artifacts)
	}
}

type Coordinator struct {
	runtime          Runtime
	operationContext context.Context
	launchTimeout    time.Duration
	coreLoadTimeout  time.Duration
	stopTimeout      time.Duration
	transition       chan struct{}
	mu               sync.RWMutex
	status           protocol.Status
	updateBlocked    bool
	events           flightdiag.Sink
	artifacts        *protocol.Artifacts
}

func (c *Coordinator) record(kind, severity string, detail map[string]any) {
	if c.events == nil {
		return
	}
	c.events.Append(flightdiag.Event{
		Layer:    flightdiag.LayerRuntime,
		Kind:     kind,
		Severity: severity,
		Detail:   detail,
	})
}

func New(runtime Runtime, launchTimeout, stopTimeout time.Duration, options ...CoordinatorOption) *Coordinator {
	coordinator := &Coordinator{
		runtime:          runtime,
		operationContext: context.Background(),
		launchTimeout:    launchTimeout,
		coreLoadTimeout:  launchTimeout,
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
	artifacts := cloneArtifacts(c.artifacts)
	c.mu.RUnlock()
	if artifacts != nil {
		health.Artifacts = artifacts
	}
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

func cloneArtifacts(artifacts *protocol.Artifacts) *protocol.Artifacts {
	if artifacts == nil {
		return nil
	}
	copy := *artifacts
	if artifacts.Cores != nil {
		copy.Cores = make(map[string]string, len(artifacts.Cores))
		for system, digest := range artifacts.Cores {
			copy.Cores[system] = digest
		}
	}
	return &copy
}

func (c *Coordinator) Initialize(ctx context.Context) {
	c.set(c.runtime.Reconcile(ctx))
}

func (c *Coordinator) LoadDevelopmentRBF(parent context.Context, size int64, content io.Reader) (protocol.Status, *protocol.APIError) {
	c.record(flightdiag.KindFenceProgram, "ok", map[string]any{"operation": "development_rbf", "size": size})
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
	return c.loadCore(parent, size, content, "")
}
func (c *Coordinator) loadCore(parent context.Context, size int64, content io.Reader, libraryID string, composition ...bool) (protocol.Status, *protocol.APIError) {
	c.record(flightdiag.KindFenceProgram, "ok", map[string]any{"operation": "core_package", "size": size})
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
	observation, cancel := context.WithTimeout(c.operationContext, c.coreLoadTimeout)
	defer cancel()
	var activation misterruntime.CoreActivation
	var attempted bool
	var apiErr *protocol.APIError
	if len(composition) == 1 && composition[0] {
		composed, ok := c.runtime.(composedCoreRuntime)
		if !ok {
			return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
		}
		activation, attempted, apiErr = composed.LoadComposedCoreOwned(parent, observation, c.operationContext, size, content, libraryID)
	} else if libraryID != "" {
		library, ok := c.runtime.(libraryCoreRuntime)
		if !ok {
			return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
		}
		activation, attempted, apiErr = library.LoadLibraryCoreOwned(parent, observation, c.operationContext, size, content, libraryID)
	} else {
		activation, attempted, apiErr = runtime.LoadCoreOwned(parent, observation, c.operationContext, size, content)
	}
	if apiErr != nil {
		c.record(flightdiag.KindFenceABI, "error", map[string]any{"operation": "core_package", "ok": false})
		if !attempted {
			c.set(previous)
			return c.Status(), apiErr
		}
		if previous.CorePackage != nil && apiErr.Phase == "recovery" {
			check, cancel := context.WithTimeout(c.operationContext, c.launchTimeout)
			retained := c.runtime.Reconcile(check)
			cancel()
			if sameCoreGeneration(previous, retained) && retained.State == protocol.StateFailed && retained.Recovery != "" {
				c.set(retained)
				return c.Status(), apiErr
			}
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
	c.record(flightdiag.KindFenceABI, "ok", map[string]any{
		"operation": "core_package", "ok": true, "abi_id": activation.Descriptor.ABI.ID,
		"abi_major": activation.Descriptor.ABI.Major, "abi_minor": activation.Descriptor.ABI.Minor,
	})
	active := protocol.Status{State: protocol.StateActive, Development: true}
	if activation.ObservedCore != "" {
		active.ObservedCore = &activation.ObservedCore
	}
	interfaces := make([]protocol.RuntimeInterface, len(activation.ActiveInterfaces))
	for index, value := range activation.ActiveInterfaces {
		interfaces[index] = protocol.RuntimeInterface{ID: value.ID, Major: value.Major, Minor: value.Minor}
	}
	active.CorePackage = &protocol.CorePackageStatus{
		ROMLink:     activation.ROMLink,
		ROMLinks:    activation.ROMLinks,
		Composition: activation.Composition,
		MediaStream: activation.MediaStream,
		PackageID:   activation.PackageID, Generation: activation.Generation,
		ABI: protocol.RuntimeContract{ID: activation.Descriptor.ABI.ID,
			Major: uint16(activation.Descriptor.ABI.Major), Minor: uint16(activation.Descriptor.ABI.Minor)},
		BuildID: activation.Descriptor.Build.ID, ActiveInterfaces: interfaces,
		Gamepad: activation.Gamepad, PersistenceMode: activation.PersistenceMode,
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
	c.record(flightdiag.KindFenceHandoff, "ok", map[string]any{"operation": "stop"})
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	return c.stopLocked(parent)
}

func (c *Coordinator) stopLocked(parent context.Context) (protocol.Status, *protocol.APIError) {
	current := c.Status()
	if current.State == protocol.StateIdle && current.LastError == nil && current.Recovery == "" {
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
	if apiErr != nil && (apiErr.Code == protocol.CodeSaveFailed || apiErr.Phase == "recovery") && current.CorePackage != nil {
		checkCtx, cancel := context.WithTimeout(c.operationContext, c.stopTimeout)
		observed := c.runtime.Reconcile(checkCtx)
		cancel()
		if observed.State == protocol.StateActive && observed.Development && observed.Recovery == "" && observed.CorePackage != nil &&
			observed.CorePackage.PackageID == current.CorePackage.PackageID && observed.CorePackage.Generation == current.CorePackage.Generation &&
			(observed.LastError == nil || (observed.LastError.Code == protocol.CodeSaveFailed && observed.LastError.Phase == "save")) {
			current.LastError = cloneAPIError(apiErr)
			c.set(current)
			return c.Status(), apiErr
		}
		if sameCoreGeneration(current, observed) && observed.State == protocol.StateFailed && observed.Recovery != "" {
			c.set(observed)
			return c.Status(), apiErr
		}
	}
	if apiErr != nil {
		failed := cloneStatus(stopping)
		failed.Recovery = recovery
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
	c.set(protocol.Status{State: protocol.StateIdle})
	return c.Status(), nil
}

func idleProgramFailed(apiErr *protocol.APIError) bool {
	return apiErr != nil && apiErr.Code == protocol.CodeMiSTerUnavailable && apiErr.Phase == "recovery"
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
	c.record(flightdiag.KindFenceRecovery, "warn", map[string]any{"operation": "development_reboot"})
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	current := c.Status()
	if current.State != protocol.StateStopping || !current.Development || current.Recovery != protocol.RecoveryRebootRequired {
		return current, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "development reboot was not requested"}
	}
	ctx, cancel := context.WithTimeout(parent, c.stopTimeout)
	defer cancel()
	if idleRuntime, ok := c.runtime.(idleRecoveryRuntime); ok {
		idle, apiErr := idleRuntime.RecoverIdle(ctx)
		if apiErr == nil && idle {
			c.set(protocol.Status{State: protocol.StateIdle})
			return c.Status(), nil
		}
		if apiErr == nil || (apiErr.Code != protocol.CodeUnsupportedOperation && !idleProgramFailed(apiErr)) {
			if apiErr == nil {
				apiErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
			}
			failed := cloneStatus(current)
			failed.State = protocol.StateFailed
			failed.LastError = cloneAPIError(apiErr)
			c.set(failed)
			return c.Status(), apiErr
		}
	}
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
		if status.CorePackage.Composition != nil {
			compositionCopy := *status.CorePackage.Composition
			packageCopy.Composition = &compositionCopy
		}
		packageCopy.ActiveInterfaces = append([]protocol.RuntimeInterface(nil), status.CorePackage.ActiveInterfaces...)
		if status.CorePackage.MediaStream != nil {
			streamCopy := *status.CorePackage.MediaStream
			packageCopy.MediaStream = &streamCopy
		}
		if status.CorePackage.ROMLink != nil {
			romCopy := *status.CorePackage.ROMLink
			packageCopy.ROMLink = &romCopy
		}
		if status.CorePackage.ROMLinks != nil {
			linksCopy := *status.CorePackage.ROMLinks
			linksCopy.Sources = append([]corepackage.ROMSourceIdentity(nil), linksCopy.Sources...)
			packageCopy.ROMLinks = &linksCopy
		}
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

func sameCoreGeneration(left, right protocol.Status) bool {
	return left.CorePackage != nil && right.CorePackage != nil && left.CorePackage.PackageID == right.CorePackage.PackageID && left.CorePackage.Generation == right.CorePackage.Generation
}
