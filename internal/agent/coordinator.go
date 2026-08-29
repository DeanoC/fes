package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"github.com/DeanoC/FogCast-POC/internal/mister"
	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/protocol"
)

const durableActiveReconcileTimeout = 40 * time.Second

var errNormalAvailabilityObservation = errors.New("normal admission live observation is unavailable")

type Runtime interface {
	Health(string) protocol.Health
	Reconcile(context.Context) protocol.Status
	Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError)
	Launch(context.Context, mister.PreparedLaunch) (observed string, dispatchAttempted bool, apiErr *protocol.APIError)
	Stop(context.Context) (string, *protocol.APIError)
}

type Coordinator struct {
	runtime       Runtime
	registry      core.Registry
	content       ContentStore
	launchTimeout time.Duration
	stopTimeout   time.Duration
	normalGate    hardwareowner.NormalGate
	transition    chan struct{}
	mu            sync.RWMutex
	status        protocol.Status
}

func New(runtime Runtime, registry core.Registry, launchTimeout, stopTimeout time.Duration, normalGates ...hardwareowner.NormalGate) *Coordinator {
	var normalGate hardwareowner.NormalGate
	if len(normalGates) != 0 {
		normalGate = normalGates[0]
	}
	return &Coordinator{
		runtime:       runtime,
		registry:      registry,
		launchTimeout: launchTimeout,
		stopTimeout:   stopTimeout,
		normalGate:    normalGate,
		transition:    make(chan struct{}, 1),
		status:        protocol.Status{State: protocol.StateIdle},
	}
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
	status := c.cachedStatus()
	if err := c.observeNormalAvailability(); err != nil {
		return unavailableStatus(status)
	}
	return status
}

func (c *Coordinator) Health(version string) protocol.Health {
	health := c.runtime.Health(version)
	status := c.Status()
	if status.State == protocol.StateFailed && status.LastError != nil && status.LastError.Code == protocol.CodeMiSTerUnavailable {
		health.Ready = false
	}
	return health
}

func (c *Coordinator) cachedStatus() protocol.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneStatus(c.status)
}

func (c *Coordinator) observeNormalAvailability() error {
	if c.normalGate == nil {
		return nil
	}
	observer, ok := c.normalGate.(hardwareowner.MaintenanceObserver)
	if !ok {
		return errNormalAvailabilityObservation
	}
	return observer.Observe()
}

func unavailableStatus(status protocol.Status) protocol.Status {
	status.State = protocol.StateFailed
	status.LastError = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer is unavailable"}
	return status
}

// persistUnavailableStatus records a failure that belongs to an actual
// transition, such as rejected admission or an ambiguous unlock. Public live
// observations use unavailableStatus directly and must never overwrite this
// authoritative transition state.
func (c *Coordinator) persistUnavailableStatus(status protocol.Status) protocol.Status {
	status = unavailableStatus(status)
	c.set(status)
	return cloneStatus(status)
}

func (c *Coordinator) Initialize(ctx context.Context) {
	status := c.runtime.Reconcile(ctx)
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

func (c *Coordinator) Launch(parent context.Context, request protocol.LaunchRequest) (status protocol.Status, resultErr *protocol.APIError) {
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
	normalUnlock, apiErr := c.enterNormal(parent)
	if apiErr != nil {
		return c.Status(), apiErr
	}
	defer func() {
		status, resultErr = c.finalizeNormalTransition(status, resultErr, normalUnlock)
	}()
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

// launchWithIntent performs the core transition while the caller holds
// normal admission. Keeping this separate lets cached launches acquire once
// before resolving and pinning content, then retain the same lease through
// their durable commit or abort handling.
func (c *Coordinator) launchWithIntent(parent context.Context, gameID string, spec core.Spec, romPath string, recordIntent func() *protocol.APIError) (protocol.Status, bool, *protocol.APIError) {
	if !c.runtime.Health("").Ready {
		return c.Status(), false, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Main_MiSTer or command pipe is unavailable"}
	}
	prepared, apiErr := c.runtime.Prepare(spec, romPath)
	if apiErr != nil {
		return c.Status(), false, apiErr
	}
	if recordIntent != nil {
		if apiErr := recordIntent(); apiErr != nil {
			return c.Status(), false, apiErr
		}
	}
	system, expected := spec.System, spec.ExpectedCore
	c.set(protocol.Status{State: protocol.StateLaunching, GameID: &gameID, System: &system, ExpectedCore: &expected})
	ctx, cancel := context.WithTimeout(parent, c.launchTimeout)
	defer cancel()
	observed, dispatchAttempted, apiErr := c.runtime.Launch(ctx, prepared)
	if apiErr != nil {
		failed := protocol.Status{State: protocol.StateFailed, GameID: &gameID, System: &system, ExpectedCore: &expected, LastError: cloneAPIError(apiErr)}
		if observed != "" {
			failed.ObservedCore = &observed
		}
		c.set(failed)
		return c.Status(), dispatchAttempted, apiErr
	}
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
	c.set(active)
	return c.Status(), dispatchAttempted, nil
}

func (c *Coordinator) Stop(parent context.Context) (status protocol.Status, resultErr *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	normalUnlock, apiErr := c.enterNormal(parent)
	if apiErr != nil {
		return c.Status(), apiErr
	}
	defer func() {
		status, resultErr = c.finalizeNormalTransition(status, resultErr, normalUnlock)
	}()
	// Admission has already performed the authoritative check. Use the cached
	// transition state for the idle decision so a transient public observation
	// cannot turn an idle no-op into a runtime Stop.
	status = c.cachedStatus()
	if status.State == protocol.StateIdle {
		return status, nil
	}
	if !c.runtime.Health("").Ready {
		return status, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Main_MiSTer or command pipe is unavailable"}
	}
	stopping := cloneStatus(status)
	stopping.State = protocol.StateStopping
	stopping.LastError = nil
	c.set(stopping)
	ctx, cancel := context.WithTimeout(parent, c.stopTimeout)
	defer cancel()
	observed, apiErr := c.runtime.Stop(ctx)
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

func (c *Coordinator) enterNormal(parent context.Context) (hardwareowner.Unlock, *protocol.APIError) {
	if c.normalGate == nil {
		return nil, nil
	}
	if parent == nil {
		parent = context.Background()
	}
	unlock, err := c.normalGate.Enter(parent)
	if err != nil {
		if unlock != nil {
			_ = unlock()
		}
		return nil, c.projectNormalAdmissionFailure()
	}
	if unlock == nil {
		return nil, c.projectNormalAdmissionFailure()
	}
	return unlock, nil
}

func (c *Coordinator) projectNormalAdmissionFailure() *protocol.APIError {
	apiErr := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer is unavailable"}
	c.persistUnavailableStatus(c.cachedStatus())
	return apiErr
}

// finalizeNormalTransition is the one release path for each admitted normal
// transition. A release error makes the result ambiguous: the public status
// is failed/unavailable, while an earlier transition error remains the
// deterministic API error. The release error itself is intentionally never
// exposed.
func (c *Coordinator) finalizeNormalTransition(status protocol.Status, primary *protocol.APIError, unlock hardwareowner.Unlock) (protocol.Status, *protocol.APIError) {
	if releaseErr := releaseNormal(unlock); releaseErr == nil {
		return status, primary
	}
	status = c.persistUnavailableStatus(status)
	if primary != nil {
		return status, primary
	}
	return status, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer is unavailable"}
}

func releaseNormal(unlock hardwareowner.Unlock) error {
	if unlock == nil {
		return nil
	}
	return unlock()
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
	return copy
}

func cloneAPIError(apiErr *protocol.APIError) *protocol.APIError {
	if apiErr == nil {
		return nil
	}
	copy := *apiErr
	return &copy
}
