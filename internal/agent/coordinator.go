package agent

import (
	"context"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/mister"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type Runtime interface {
	Health(string) protocol.Health
	Reconcile(context.Context) protocol.Status
	Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError)
	Launch(context.Context, mister.PreparedLaunch) (string, *protocol.APIError)
	Stop(context.Context) (string, *protocol.APIError)
}

type Coordinator struct {
	runtime       Runtime
	registry      core.Registry
	content       ContentStore
	launchTimeout time.Duration
	stopTimeout   time.Duration
	transition    chan struct{}
	mu            sync.RWMutex
	status        protocol.Status
}

func New(runtime Runtime, registry core.Registry, launchTimeout, stopTimeout time.Duration) *Coordinator {
	return &Coordinator{
		runtime:       runtime,
		registry:      registry,
		launchTimeout: launchTimeout,
		stopTimeout:   stopTimeout,
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneStatus(c.status)
}

func (c *Coordinator) Health(version string) protocol.Health {
	health := c.runtime.Health(version)
	status := c.Status()
	if status.State == protocol.StateFailed && status.LastError != nil && status.LastError.Code == protocol.CodeMiSTerUnavailable {
		health.Ready = false
	}
	return health
}

func (c *Coordinator) Initialize(ctx context.Context) {
	status := c.runtime.Reconcile(ctx)
	c.set(status)
	if c.content != nil {
		c.content.ReconcileActive(status)
	}
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
	return c.launch(parent, request.GameID, spec, request.ROMPath)
}

func (c *Coordinator) launch(parent context.Context, gameID string, spec core.Spec, romPath string) (protocol.Status, *protocol.APIError) {
	if !c.runtime.Health("").Ready {
		return c.Status(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Main_MiSTer or command pipe is unavailable"}
	}
	prepared, apiErr := c.runtime.Prepare(spec, romPath)
	if apiErr != nil {
		return c.Status(), apiErr
	}
	system, expected := spec.System, spec.ExpectedCore
	c.set(protocol.Status{State: protocol.StateLaunching, GameID: &gameID, System: &system, ExpectedCore: &expected})
	ctx, cancel := context.WithTimeout(parent, c.launchTimeout)
	defer cancel()
	observed, apiErr := c.runtime.Launch(ctx, prepared)
	if apiErr != nil {
		failed := protocol.Status{State: protocol.StateFailed, GameID: &gameID, System: &system, ExpectedCore: &expected, LastError: cloneAPIError(apiErr)}
		if observed != "" {
			failed.ObservedCore = &observed
		}
		c.set(failed)
		return c.Status(), apiErr
	}
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
	c.set(active)
	return c.Status(), nil
}

func (c *Coordinator) Stop(parent context.Context) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.end()
	current := c.Status()
	if current.State == protocol.StateIdle {
		return current, nil
	}
	if !c.runtime.Health("").Ready {
		return current, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Main_MiSTer or command pipe is unavailable"}
	}
	stopping := cloneStatus(current)
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
