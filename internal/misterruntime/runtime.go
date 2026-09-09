package misterruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/protocol"
)

const unavailableMessage = "target runtime is unavailable"
const unsupportedOperationMessage = "requested operation is unsupported"
const unsupportedSystemMessage = "system is unsupported by target runtime"

type Runtime struct {
	saveRoot           string
	control            Control
	bootIDFile         string
	pollInterval       time.Duration
	healthTimeout      time.Duration
	developmentRBFPath string
	corePackageRoot    string
	rebootCommand      string
	packageMu          sync.Mutex
	activePackage      *corepackage.Staged
	retiredPackages    []corepackage.Staged
	coreBarrier        CoreReplacementBarrier
	events             flightdiag.Sink
	eventsPath         string
	eventsMu           sync.Mutex
	imported           int
}

type RuntimeOption func(*Runtime)

func WithDevelopmentRBFPath(path string) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.developmentRBFPath = path
	}
}

func WithCorePackageRoot(path string) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.corePackageRoot = path
	}
}

func WithRebootCommand(path string) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.rebootCommand = path
	}
}

type CoreReplacementBarrier interface {
	BeginCoreReplacement(context.Context) (func(context.Context, bool) error, error)
}

func WithCoreReplacementBarrier(barrier CoreReplacementBarrier) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.coreBarrier = barrier
	}
}

// ConfigureCoreReplacementBarrier wires the target-owned input lifecycle
// before the runtime is published to the coordinator.
func (r *Runtime) ConfigureCoreReplacementBarrier(barrier CoreReplacementBarrier) {
	r.coreBarrier = barrier
}

func NewRuntime(control Control, bootIDFile string, pollInterval, healthTimeout time.Duration, options ...RuntimeOption) *Runtime {
	runtime := &Runtime{control: control, bootIDFile: bootIDFile, pollInterval: pollInterval, healthTimeout: healthTimeout, eventsPath: DefaultEventsPath}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

type CoreActivation struct {
	PackageID        string
	Descriptor       corepackage.Descriptor
	Generation       uint64
	ObservedCore     string
	ActiveInterfaces []Protocol2Interface
	Gamepad          bool
}

type protocol2CoreControl interface {
	LoadCore(context.Context, string, string) (Protocol2Response, error)
}

type protocol2InspectionControl interface {
	InspectCore(context.Context, string, string) (Protocol2Response, error)
}

type protocol2StatusControl interface {
	Protocol2Status(context.Context) (Protocol2Response, error)
}

// InspectCore stages one bounded package for the existing runtime authority,
// verifies its exact identity and descriptor, and removes the private staging
// publication without changing the active hardware state.
func (r *Runtime) InspectCore(ctx context.Context, size int64, content io.Reader) (
	inspection protocol.CoreInspection, apiErr *protocol.APIError) {
	if r.corePackageRoot == "" {
		return protocol.CoreInspection{}, unsupportedOperationError()
	}
	if _, ok := r.control.(protocol2InspectionControl); !ok {
		return protocol.CoreInspection{}, unsupportedOperationError()
	}
	staged, err := corepackage.Stage(ctx, r.corePackageRoot, size, content)
	if err != nil {
		return protocol.CoreInspection{}, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	defer func() {
		if err := staged.Cleanup(); err != nil {
			r.retainRetired(staged)
			inspection = protocol.CoreInspection{}
			apiErr = &protocol.APIError{Code: protocol.CodeInternal,
				Message: "staged core package could not be cleaned up", Phase: "recovery"}
		}
	}()
	remote, apiErr := r.inspectStagedCore(ctx, staged)
	if apiErr != nil {
		return protocol.CoreInspection{}, apiErr
	}
	result := protocol.CoreInspection{PackageID: staged.PackageID,
		Descriptor: staged.Descriptor, Compatible: remote.Compatible}
	if remote.CompatibilityError != nil {
		result.CompatibilityError = mapProtocol2Error(remote.CompatibilityError)
	}
	return result, nil
}

func (r *Runtime) inspectStagedCore(ctx context.Context, staged corepackage.Staged) (*Protocol2Inspection, *protocol.APIError) {
	inspector, ok := r.control.(protocol2InspectionControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	response, err := inspector.InspectCore(ctx, staged.Directory, staged.PackageID)
	if err != nil {
		if errors.Is(err, errProtocol2Unsupported) {
			return nil, unsupportedOperationError()
		}
		return nil, unavailableError()
	}
	if !response.OK || response.Error != nil {
		return nil, mapProtocol2Error(response.Error)
	}
	if response.InspectedPackage == nil ||
		response.InspectedPackage.PackageID != staged.PackageID ||
		!reflect.DeepEqual(response.InspectedPackage.Descriptor, staged.Descriptor) ||
		response.InspectedPackage.Compatible == (response.InspectedPackage.CompatibilityError != nil) ||
		(!response.InspectedPackage.Compatible && response.InspectedPackage.CompatibilityError.Phase != "compatibility") {
		return nil, unavailableError()
	}
	return response.InspectedPackage, nil
}

func (r *Runtime) LoadCoreOwned(admission, observation, operationOwner context.Context,
	size int64, content io.Reader) (activation CoreActivation, attempted bool, apiErr *protocol.APIError) {
	if r.corePackageRoot == "" {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	control, ok := r.control.(protocol2CoreControl)
	if !ok {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	staged, err := corepackage.Stage(admission, r.corePackageRoot, size, content)
	if err != nil {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	cleanupStaged := true
	defer func() {
		if cleanupStaged {
			if err := staged.Cleanup(); err != nil {
				r.retainRetired(staged)
			}
		}
	}()
	if admission.Err() != nil || observation.Err() != nil || operationOwner.Err() != nil {
		return CoreActivation{}, false, unavailableError()
	}
	inspection, inspectErr := r.inspectStagedCore(admission, staged)
	if inspectErr != nil {
		return CoreActivation{}, false, inspectErr
	}
	if !inspection.Compatible {
		return CoreActivation{}, false,
			mapProtocol2Error(inspection.CompatibilityError)
	}
	var before *Protocol2Response
	statusControl, hasStatus := r.control.(protocol2StatusControl)
	if hasStatus {
		status, statusErr := statusControl.Protocol2Status(operationOwner)
		if statusErr != nil {
			if errors.Is(statusErr, errProtocol2Unsupported) {
				return CoreActivation{}, false, unsupportedOperationError()
			}
			return CoreActivation{}, false, unavailableError()
		}
		before = &status
	}
	finishBarrier := func(context.Context, bool) error { return nil }
	if r.coreBarrier != nil {
		finish, barrierErr := r.coreBarrier.BeginCoreReplacement(operationOwner)
		if barrierErr != nil {
			return CoreActivation{}, true, replacementBarrierError()
		}
		finishBarrier = finish
	}
	preserveInput := false
	defer func() {
		if err := finishBarrier(operationOwner, preserveInput); err != nil {
			activation = CoreActivation{}
			attempted = true
			apiErr = replacementBarrierError()
		}
	}()

	response, callErr := control.LoadCore(operationOwner,
		staged.Directory, staged.PackageID)
	r.noteDispatch("load_core", callErr == nil)
	attempted = callErr == nil || protocol2MutationAttempted(callErr)
	if callErr != nil {
		if errors.Is(callErr, errProtocol2Unsupported) {
			preserveInput = true
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if attempted {
			if hasStatus && before != nil {
				observed, disposition := r.reconcileLostCoreLoad(observation, operationOwner,
					statusControl, staged, *before)
				switch disposition {
				case coreLoadConfirmed:
					r.replaceActivePackage(staged)
					cleanupStaged = false
					return observed, true, nil
				case coreLoadPreserved:
					preserveInput = true
					return CoreActivation{}, false, unavailableError()
				case coreLoadRecovery:
					apiErr := unavailableError()
					apiErr.Phase = "recovery"
					return CoreActivation{}, true, apiErr
				}
			}
			// The result remains ambiguous. Keep the bytes for later reconciliation
			// or a proven-safe Stop boundary; never replay the load.
			r.retainRetired(staged)
			cleanupStaged = false
		} else {
			preserveInput = true
		}
		return CoreActivation{}, attempted, unavailableError()
	}
	if !response.OK || response.Error != nil {
		apiErr := mapProtocol2Error(response.Error)
		attempted = !protocol2PreMutationFailure(response.Error)
		preserveInput = !attempted
		return CoreActivation{}, attempted, apiErr
	}
	if response.State != "running_development" || response.Execution != "development" ||
		response.ActivePackage == nil || response.ActivePackage.PackageID != staged.PackageID ||
		response.Generation == nil || *response.Generation == 0 {
		return CoreActivation{}, true, unavailableError()
	}
	activation = activationFromProtocol2(staged.PackageID, staged.Descriptor, response)
	r.replaceActivePackage(staged)
	cleanupStaged = false
	return activation, true, nil
}

func replacementBarrierError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal,
		Message: "remote input could not be safely transitioned", Phase: "recovery"}
}

func activationFromProtocol2(packageID string, descriptor corepackage.Descriptor, response Protocol2Response) CoreActivation {
	activation := CoreActivation{PackageID: packageID, Descriptor: descriptor,
		ActiveInterfaces: append([]Protocol2Interface(nil), response.Capabilities.ActiveInterfaces...)}
	if response.Generation != nil {
		activation.Generation = *response.Generation
	}
	if response.Core != nil {
		activation.ObservedCore = *response.Core
	}
	for _, contract := range activation.ActiveInterfaces {
		if contract.ID == "fes.gamepad" && contract.Major == 1 && contract.Minor == 0 {
			activation.Gamepad = true
		}
	}
	return activation
}

type coreLoadDisposition uint8

const (
	coreLoadUnresolved coreLoadDisposition = iota
	coreLoadConfirmed
	coreLoadPreserved
	coreLoadRecovery
)

func (r *Runtime) reconcileLostCoreLoad(observation, operationOwner context.Context,
	control protocol2StatusControl, staged corepackage.Staged, before Protocol2Response) (CoreActivation, coreLoadDisposition) {
	if activation, result := r.observeLostCoreLoad(observation, control, staged, before); result != coreLoadUnresolved {
		return activation, result
	}
	if operationOwner == nil || operationOwner.Err() != nil {
		return CoreActivation{}, coreLoadUnresolved
	}
	fallback, cancel := context.WithTimeout(operationOwner, r.healthTimeout)
	defer cancel()
	return r.observeLostCoreLoad(fallback, control, staged, before)
}

func (r *Runtime) observeLostCoreLoad(ctx context.Context, control protocol2StatusControl,
	staged corepackage.Staged, before Protocol2Response) (CoreActivation, coreLoadDisposition) {
	for ctx != nil && ctx.Err() == nil {
		response, err := control.Protocol2Status(ctx)
		if err != nil {
			return CoreActivation{}, coreLoadUnresolved
		}
		if response.State == "starting" {
			if !waitForPoll(ctx, r.pollInterval) {
				return CoreActivation{}, coreLoadUnresolved
			}
			continue
		}
		if sameProtocol2RuntimeState(before, response) {
			return CoreActivation{}, coreLoadPreserved
		}
		if response.State == "running_development" && response.ActivePackage != nil &&
			response.ActivePackage.PackageID == staged.PackageID && response.Generation != nil &&
			!sameGeneration(before.Generation, response.Generation) {
			return activationFromProtocol2(staged.PackageID, staged.Descriptor, response), coreLoadConfirmed
		}
		return CoreActivation{}, coreLoadRecovery
	}
	return CoreActivation{}, coreLoadUnresolved
}

func sameProtocol2RuntimeState(left, right Protocol2Response) bool {
	if left.State != right.State || left.Execution != right.Execution ||
		!equalOptionalString(left.System, right.System) || !equalOptionalString(left.Core, right.Core) ||
		!sameGeneration(left.Generation, right.Generation) ||
		!reflect.DeepEqual(left.Capabilities.ActiveInterfaces, right.Capabilities.ActiveInterfaces) {
		return false
	}
	if left.ActivePackage == nil || right.ActivePackage == nil {
		return left.ActivePackage == nil && right.ActivePackage == nil
	}
	return left.ActivePackage.PackageID == right.ActivePackage.PackageID &&
		reflect.DeepEqual(left.ActivePackage.Descriptor, right.ActivePackage.Descriptor) &&
		reflect.DeepEqual(left.ActivePackage.Observed, right.ActivePackage.Observed)
}

func equalOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func sameGeneration(left, right *uint64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func protocol2PreMutationFailure(remote *Protocol2Error) bool {
	if remote == nil {
		return false
	}
	switch remote.Code {
	case "busy", "save_failed":
		return true
	}
	switch remote.Phase {
	case "request", "admission", "compatibility", "save":
		return true
	default:
		return false
	}
}

func (r *Runtime) retainRetired(staged corepackage.Staged) {
	r.packageMu.Lock()
	r.retiredPackages = append(r.retiredPackages, staged)
	r.packageMu.Unlock()
}

func (r *Runtime) replaceActivePackage(staged corepackage.Staged) {
	r.packageMu.Lock()
	previous := r.activePackage
	copy := staged
	r.activePackage = &copy
	r.packageMu.Unlock()
	if previous != nil {
		if err := previous.Cleanup(); err != nil {
			r.retainRetired(*previous)
		}
	}
}

func (r *Runtime) cleanupCorePackages() *protocol.APIError {
	r.packageMu.Lock()
	packages := append([]corepackage.Staged(nil), r.retiredPackages...)
	if r.activePackage != nil {
		packages = append(packages, *r.activePackage)
	}
	r.retiredPackages = nil
	r.activePackage = nil
	r.packageMu.Unlock()
	var failed []corepackage.Staged
	for _, staged := range packages {
		if err := staged.Cleanup(); err != nil {
			failed = append(failed, staged)
		}
	}
	if len(failed) != 0 {
		r.packageMu.Lock()
		r.retiredPackages = append(r.retiredPackages, failed...)
		r.packageMu.Unlock()
		return &protocol.APIError{Code: protocol.CodeInternal,
			Message: "staged core package could not be cleaned up", Phase: "recovery"}
	}
	return nil
}

func (r *Runtime) Health(version string) protocol.Health {
	health := protocol.Health{APIVersion: "v1", AgentVersion: version}
	if bootID, err := os.ReadFile(r.bootIDFile); err == nil {
		health.BootID = strings.TrimSpace(string(bootID))
	}
	response, err := r.boundedStatus(context.Background())
	health.Ready = err == nil && validIdle(response)
	r.drainEvents()
	return health
}

// ConfirmIdle observes physical idle for failed-launch reconciliation only.
// Unlike legacy process health, native idle proves no game remains active.
func (r *Runtime) ConfirmIdle(ctx context.Context) bool {
	response, err := r.boundedStatus(ctx)
	return err == nil && ctx.Err() == nil && validIdle(response) &&
		(response.Error == nil || response.Error.Code != "save_failed")
}

func (r *Runtime) StopReady() bool {
	response, err := r.boundedStatus(context.Background())
	return err == nil && (validIdle(response) || validNativeRunning(response) || validDevelopmentRunning(response) || retryableSaveFailure(response))
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	if control, ok := r.control.(protocol2StatusControl); ok {
		status, fallback := r.reconcileProtocol2(ctx, control)
		if !fallback {
			return status
		}
	}
	for {
		response, err := r.control.Status(ctx)
		if err == nil {
			if validIdle(response) {
				status := protocol.Status{State: protocol.StateIdle}
				if response.Error != nil {
					status.LastError = mapRemoteError(response.Error)
				}
				return status
			}
			if validDevelopmentRunning(response) {
				status := protocol.Status{State: protocol.StateActive, Development: true}
				if response.Core != nil {
					observed := *response.Core
					status.ObservedCore = &observed
				}
				return status
			}
			if response.State == "starting" && response.Execution == "development" && !validDevelopmentStarting(response) {
				return unavailableStatus()
			}
			if !response.OK || response.State != "starting" {
				return unavailableStatus()
			}
		} else if !errors.Is(err, errRuntimeConnection) {
			return unavailableStatus()
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return unavailableStatus()
		}
	}
}

func (r *Runtime) reconcileProtocol2(ctx context.Context, control protocol2StatusControl) (protocol.Status, bool) {
	for {
		response, err := control.Protocol2Status(ctx)
		if errors.Is(err, errProtocol2Unsupported) {
			return protocol.Status{}, true
		}
		if err != nil {
			if !waitForPoll(ctx, r.pollInterval) {
				return unavailableStatus(), false
			}
			continue
		}
		switch response.State {
		case "starting":
			if !waitForPoll(ctx, r.pollInterval) {
				return unavailableStatus(), false
			}
			continue
		case "idle":
			status := protocol.Status{State: protocol.StateIdle}
			status.LastError = mapOptionalProtocol2Error(response.Error)
			return status, false
		case "reboot_required":
			apiErr := mapProtocol2Error(response.Error)
			return protocol.Status{State: protocol.StateFailed, Recovery: protocol.RecoveryRebootRequired, LastError: apiErr}, false
		case "running_game":
			if response.System == nil || response.Core == nil {
				return unavailableStatus(), false
			}
			spec, ok := core.DefaultRegistry().Lookup(protocol.System(*response.System))
			if !ok || spec.ExpectedCore != *response.Core {
				return unavailableStatus(), false
			}
			system, expected, observed := spec.System, spec.ExpectedCore, *response.Core
			return protocol.Status{State: protocol.StateActive, System: &system,
				ExpectedCore: &expected, ObservedCore: &observed,
				LastError: mapOptionalProtocol2Error(response.Error)}, false
		case "running_development":
			status := protocol.Status{State: protocol.StateActive, Development: true,
				LastError: mapOptionalProtocol2Error(response.Error)}
			if response.Core != nil {
				observed := *response.Core
				status.ObservedCore = &observed
			}
			if response.ActivePackage == nil {
				return status, false
			}
			if r.corePackageRoot == "" || response.Generation == nil {
				return unavailableStatus(), false
			}
			adopted, err := corepackage.Adopt(r.corePackageRoot)
			activeIndex := matchingAdoptedPackage(adopted, *response.ActivePackage)
			if err != nil || activeIndex < 0 {
				return unavailableStatus(), false
			}
			r.adoptActivePackage(adopted, activeIndex)
			activation := activationFromProtocol2(adopted[activeIndex].PackageID, adopted[activeIndex].Descriptor, response)
			status.CorePackage = corePackageStatus(activation)
			return status, false
		default:
			return unavailableStatus(), false
		}
	}
}

func mapOptionalProtocol2Error(remote *Protocol2Error) *protocol.APIError {
	if remote == nil {
		return nil
	}
	return mapProtocol2Error(remote)
}

func matchingAdoptedPackage(adopted []corepackage.Staged, active Protocol2ActivePackage) int {
	for index := range adopted {
		if adopted[index].PackageID == active.PackageID && reflect.DeepEqual(adopted[index].Descriptor, active.Descriptor) {
			return index
		}
	}
	return -1
}

func (r *Runtime) adoptActivePackage(adopted []corepackage.Staged, activeIndex int) {
	r.packageMu.Lock()
	defer r.packageMu.Unlock()
	active := adopted[activeIndex]
	r.activePackage = &active
	for index := range adopted {
		if index != activeIndex {
			r.retiredPackages = append(r.retiredPackages, adopted[index])
		}
	}
}

func corePackageStatus(activation CoreActivation) *protocol.CorePackageStatus {
	interfaces := make([]protocol.RuntimeInterface, len(activation.ActiveInterfaces))
	for index, value := range activation.ActiveInterfaces {
		interfaces[index] = protocol.RuntimeInterface{ID: value.ID, Major: value.Major, Minor: value.Minor}
	}
	return &protocol.CorePackageStatus{PackageID: activation.PackageID, Generation: activation.Generation,
		ABI:     protocol.RuntimeContract{ID: activation.Descriptor.ABI.ID, Major: uint16(activation.Descriptor.ABI.Major), Minor: uint16(activation.Descriptor.ABI.Minor)},
		BuildID: activation.Descriptor.Build.ID, ActiveInterfaces: interfaces, Gamepad: activation.Gamepad}
}

func (r *Runtime) Prepare(spec core.Spec, candidate string) (mister.PreparedLaunch, *protocol.APIError) {
	if !validNativeSpec(spec) {
		return mister.PreparedLaunch{}, unsupportedSystemError()
	}
	rom, apiErr := validateNativeMedia(spec, candidate)
	if apiErr != nil {
		return mister.PreparedLaunch{}, apiErr
	}
	return mister.PreparedLaunch{Spec: spec, AbsoluteROM: rom}, nil
}

func (r *Runtime) Launch(ctx context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return r.launch(ctx, ctx, ctx, prepared, false)
}

// LaunchOwned keeps admission caller-bound, then transfers the sole dispatched
// mutation to the agent process owner. The daemon owns its phase deadlines;
// the separately bounded observation context limits only lost-response Status
// observation. Process shutdown still cancels both mutation and observation.
func (r *Runtime) LaunchOwned(admission, observation, operationOwner context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return r.launch(admission, observation, operationOwner, prepared, true)
}

func (r *Runtime) launch(admission, observation, operationOwner context.Context, prepared mister.PreparedLaunch, owned bool) (string, bool, *protocol.APIError) {
	if !validNativeSpec(prepared.Spec) {
		return "", false, unsupportedSystemError()
	}
	if prepared.RelativeROM != "" || len(prepared.MGL) != 0 {
		return "", false, invalidROMPathError()
	}
	rom, apiErr := validateNativeMedia(prepared.Spec, prepared.AbsoluteROM)
	if apiErr != nil || rom != prepared.AbsoluteROM {
		if apiErr != nil {
			return "", false, apiErr
		}
		return "", false, invalidROMPathError()
	}
	savePath, apiErr := r.prepareSavePath(admission, prepared)
	if apiErr != nil {
		return "", false, apiErr
	}
	response, err := r.boundedStatus(admission)
	if err != nil || !validIdle(response) {
		return "", false, unavailableError()
	}
	if admission.Err() != nil || observation.Err() != nil || (owned && operationOwner.Err() != nil) {
		return "", false, unavailableError()
	}
	request := LaunchRequest{
		SavePath: savePath,
		System:   string(prepared.Spec.System),
		RBF:      nativeRBFPath(prepared.Spec.System),
		Media:    map[string]string{},
		Settings: map[string]string{},
	}
	if prepared.Spec.System != protocol.SystemPong {
		request.Media["cartridge"] = rom
	}
	launchContext := observation
	if owned {
		launchContext = operationOwner
	}
	response, err = r.control.Launch(launchContext, request)
	r.noteDispatch("launch", err == nil)
	if err != nil {
		var reconciled bool
		if owned {
			reconciled = r.reconcileOwnedLostLaunch(observation, operationOwner, prepared.Spec)
		} else {
			reconciled = r.reconcileLostLaunch(admission, prepared.Spec)
		}
		if reconciled {
			return prepared.Spec.ExpectedCore, true, nil
		}
		return "", true, unavailableError()
	}
	if response.Error != nil || !response.OK {
		return "", true, mapRemoteError(response.Error)
	}
	if !validProfileState(response, prepared.Spec, "running_game") {
		return "", true, unavailableError()
	}
	return prepared.Spec.ExpectedCore, true, nil
}

func (r *Runtime) boundedStatus(parent context.Context) (Response, error) {
	ctx, cancel := context.WithTimeout(parent, r.healthTimeout)
	defer cancel()
	return r.control.Status(ctx)
}

func (r *Runtime) reconcileLostLaunch(parent context.Context, spec core.Spec) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), r.healthTimeout)
	defer cancel()
	reconciled, _ := r.reconcileLostLaunchWithin(ctx, spec)
	return reconciled
}

func (r *Runtime) reconcileOwnedLostLaunch(observation, operationOwner context.Context, spec core.Spec) bool {
	if operationOwner.Err() != nil {
		return false
	}
	if !contextExhausted(observation) {
		if reconciled, exhausted := r.reconcileLostLaunchWithin(observation, spec); reconciled {
			return true
		} else if !exhausted {
			return false
		}
	}
	if operationOwner.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(operationOwner, r.healthTimeout)
	defer cancel()
	reconciled, _ := r.reconcileLostLaunchWithin(ctx, spec)
	return reconciled
}

func (r *Runtime) reconcileLostLaunchWithin(ctx context.Context, spec core.Spec) (reconciled, exhausted bool) {
	for {
		response, err := r.control.Status(ctx)
		if err != nil {
			return false, contextExhausted(ctx)
		}
		if validProfileState(response, spec, "running_game") {
			return true, false
		}
		if !validIdle(response) && !validProfileState(response, spec, "starting") {
			return false, false
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return false, true
		}
	}
}

func contextExhausted(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	return ok && !time.Now().Before(deadline)
}

func (r *Runtime) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	observed, _, attempted, apiErr := r.loadDevelopmentRBF(ctx, ctx, ctx, size, content, false)
	return observed, attempted, apiErr
}

// LoadDevelopmentRBFOwned keeps staging and admission caller-bound, then
// transfers the sole runtime mutation to the agent process owner. Observation
// remains separately bounded and never replays the mutation.
func (r *Runtime) LoadDevelopmentRBFOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	observed, _, attempted, apiErr := r.LoadDevelopmentRBFOwnedWithRecovery(admission, observation, operationOwner, size, content)
	return observed, attempted, apiErr
}

// LoadDevelopmentRBFOwnedWithRecovery preserves an explicit native recovery
// marker while retaining the ordinary load API's unavailable error.
func (r *Runtime) LoadDevelopmentRBFOwnedWithRecovery(admission, observation, operationOwner context.Context, size int64, content io.Reader) (string, string, bool, *protocol.APIError) {
	return r.loadDevelopmentRBF(admission, observation, operationOwner, size, content, true)
}

func (r *Runtime) loadDevelopmentRBF(admission, observation, operationOwner context.Context, size int64, content io.Reader, owned bool) (string, string, bool, *protocol.APIError) {
	if r.developmentRBFPath == "" {
		return "", "", false, unsupportedOperationError()
	}
	if !validDevelopmentRBFPath(r.developmentRBFPath) || size < 1 || size > protocol.MaxDevelopmentRBFBytes || content == nil {
		return "", "", false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "development RBF input is invalid"}
	}
	if admission.Err() != nil {
		return "", "", false, unavailableError()
	}
	if err := mister.WriteAtomicDevelopmentRBF(r.developmentRBFPath, size, &contextReader{ctx: admission, reader: content}); err != nil {
		if admission.Err() != nil {
			return "", "", false, unavailableError()
		}
		return "", "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "development RBF could not be installed"}
	}
	response, err := r.boundedStatus(admission)
	if err != nil || !validIdle(response) {
		return "", "", false, unavailableError()
	}
	// Observation only bounds post-dispatch reconciliation; staging may consume it.
	if admission.Err() != nil || (owned && operationOwner.Err() != nil) {
		return "", "", false, unavailableError()
	}
	dispatchContext := observation
	if owned {
		dispatchContext = operationOwner
	}
	response, err = r.control.LoadDevelopmentRBF(dispatchContext, r.developmentRBFPath)
	r.noteDispatch("development_rbf", err == nil)
	if err != nil {
		if owned {
			observed, attempted, apiErr := r.reconcileOwnedLostDevelopment(observation, operationOwner)
			return observed, "", attempted, apiErr
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(admission), r.healthTimeout)
		defer cancel()
		observed, attempted, apiErr := r.reconcileLostDevelopmentWithin(ctx)
		return observed, "", attempted, apiErr
	}
	if validDevelopmentRunning(response) {
		return developmentObservation(response), "", true, nil
	}
	if rebootRequiredResponse(response) {
		return "", protocol.RecoveryRebootRequired, true, mapRemoteError(response.Error)
	}
	if response.Error != nil || !response.OK {
		return "", "", true, mapRemoteError(response.Error)
	}
	if validDevelopmentStarting(response) || validCleanIdle(response) {
		if owned {
			observed, attempted, apiErr := r.reconcileOwnedLostDevelopment(observation, operationOwner)
			return observed, "", attempted, apiErr
		}
		observed, attempted, apiErr := r.reconcileLostDevelopmentWithin(observation)
		return observed, "", attempted, apiErr
	}
	return "", "", true, unavailableError()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func (r *Runtime) reconcileOwnedLostDevelopment(observation, operationOwner context.Context) (string, bool, *protocol.APIError) {
	if operationOwner.Err() != nil {
		return "", true, unavailableError()
	}
	if !contextExhausted(observation) {
		observed, attempted, apiErr, exhausted := r.observeLostDevelopment(observation)
		if !exhausted {
			return observed, attempted, apiErr
		}
	}
	if operationOwner.Err() != nil {
		return "", true, unavailableError()
	}
	ctx, cancel := context.WithTimeout(operationOwner, r.healthTimeout)
	defer cancel()
	return r.reconcileLostDevelopmentWithin(ctx)
}

func (r *Runtime) reconcileLostDevelopmentWithin(ctx context.Context) (string, bool, *protocol.APIError) {
	observed, attempted, apiErr, _ := r.observeLostDevelopment(ctx)
	return observed, attempted, apiErr
}

func (r *Runtime) observeLostDevelopment(ctx context.Context) (observed string, attempted bool, apiErr *protocol.APIError, exhausted bool) {
	for {
		response, err := r.control.Status(ctx)
		if err != nil {
			return "", true, unavailableError(), contextExhausted(ctx)
		}
		if validDevelopmentRunning(response) {
			return developmentObservation(response), true, nil, false
		}
		if validIdle(response) && response.Error != nil {
			return "", true, mapRemoteError(response.Error), false
		}
		if !(validDevelopmentStarting(response) || validCleanIdle(response)) {
			return "", true, unavailableError(), false
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return "", true, unavailableError(), true
		}
	}
}

func (r *Runtime) RecoverDevelopment(ctx context.Context) (string, *protocol.APIError) {
	if err := ctx.Err(); err != nil {
		return "", unavailableError()
	}
	info, err := os.Stat(r.rebootCommand)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", unavailableError()
	}
	command := exec.Command(r.rebootCommand)
	if err := command.Start(); err != nil {
		return "", unavailableError()
	}
	go func() { _ = command.Wait() }()
	return "", nil
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	return r.stop(ctx, ctx, false)
}

// StopOwned keeps cancellation caller-bound until dispatch, then gives the
// sole admitted mutation to the separately bounded agent operation context.
func (r *Runtime) StopOwned(admission, operation context.Context) (string, *protocol.APIError) {
	return r.stop(admission, operation, true)
}

// StopOwnedWithRecovery preserves the native runtime's explicit reboot
// requirement without making the coordinator infer it from an error string.
func (r *Runtime) StopOwnedWithRecovery(admission, operation context.Context) (string, string, *protocol.APIError) {
	return r.stopWithRecovery(admission, operation, true)
}

func (r *Runtime) stop(admission, operation context.Context, owned bool) (string, *protocol.APIError) {
	observed, recovery, apiErr := r.stopWithRecovery(admission, operation, owned)
	if apiErr == nil && recovery != "" {
		return observed, unavailableError()
	}
	return observed, apiErr
}

func (r *Runtime) stopWithRecovery(admission, operation context.Context, owned bool) (string, string, *protocol.APIError) {
	ctx := admission
	if owned {
		if admission.Err() != nil || operation.Err() != nil {
			return "", "", unavailableError()
		}
		ctx = operation
	}
	response, err := r.control.Stop(ctx)
	r.noteDispatch("stop", err == nil)
	if err != nil {
		// The mutation may have completed before its reply was lost. Observe
		// once under the remaining operation budget; never replay Stop.
		if ctx.Err() != nil {
			return "", "", unavailableError()
		}
		response, err = r.boundedStatus(ctx)
		if err != nil || ctx.Err() != nil {
			return "", "", unavailableError()
		}
		if !validCleanIdle(response) && !rebootRequiredResponse(response) && !retryableSaveFailure(response) {
			return "", "", unavailableError()
		}
	}
	if err == nil && rebootRequiredResponse(response) {
		return "", protocol.RecoveryRebootRequired, nil
	}
	if err == nil && response.Error != nil {
		return "", "", mapRemoteError(response.Error)
	}
	if err != nil || !validCleanIdle(response) {
		return "", "", unavailableError()
	}
	if apiErr := r.cleanupCorePackages(); apiErr != nil {
		return "", "", apiErr
	}
	return "", "", nil
}

func rebootRequiredResponse(response Response) bool {
	return response.Protocol == 1 && !response.OK && response.State == "reboot_required" &&
		response.Execution == "none" && response.System == nil && response.Core == nil &&
		response.Error != nil && response.Error.Code == "idle_failed"
}

func waitForPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func unavailableStatus() protocol.Status {
	return protocol.Status{State: protocol.StateFailed, LastError: unavailableError()}
}

func unavailableError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: unavailableMessage}
}

func unsupportedOperationError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: unsupportedOperationMessage}
}

func unsupportedSystemError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: unsupportedSystemMessage}
}

func invalidROMPathError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "ROM path is invalid for native cartridge launch"}
}

func validNativeSpec(spec core.Spec) bool {
	if nativeRBFPath(spec.System) == "" {
		return false
	}
	registered, ok := core.DefaultRegistry().Lookup(spec.System)
	return ok && spec.System == registered.System && spec.ExpectedCore == registered.ExpectedCore
}

func validateNativeROM(system protocol.System, candidate string) (string, *protocol.APIError) {
	if strings.IndexByte(candidate, 0) >= 0 || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return "", invalidROMPathError()
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "ROM does not exist on the target"}
		}
		return "", invalidROMPathError()
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "ROM does not identify a non-empty regular file"}
	}
	extension := strings.ToLower(filepath.Ext(resolved))
	switch system {
	case protocol.SystemMegaDrive:
		if extension == ".md" || extension == ".gen" || extension == ".bin" {
			return resolved, nil
		}
	case protocol.SystemSNES:
		if extension == ".sfc" || extension == ".smc" || extension == ".bin" {
			return resolved, nil
		}
	case protocol.SystemNES:
		if extension == ".nes" {
			return resolved, nil
		}
	}
	return "", invalidROMPathError()
}

func validProfileState(response Response, spec core.Spec, state string) bool {
	return response.Protocol == 1 && response.OK && response.Error == nil &&
		response.State == state && response.Execution == "game" &&
		response.System != nil && *response.System == string(spec.System) &&
		response.Core != nil && *response.Core == spec.ExpectedCore
}

func validNativeRunning(response Response) bool {
	if response.System == nil {
		return false
	}
	spec, ok := core.DefaultRegistry().Lookup(protocol.System(*response.System))
	return ok && validNativeSpec(spec) && validProfileState(response, spec, "running_game")
}

func validateNativeMedia(spec core.Spec, candidate string) (string, *protocol.APIError) {
	if spec.System == protocol.SystemPong {
		if candidate != "" {
			return "", &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "Pong does not accept media"}
		}
		return "", nil
	}
	return validateNativeROM(spec.System, candidate)
}

func validIdle(response Response) bool {
	return response.Protocol == 1 && response.OK &&
		(response.Error == nil || validErrorCode(response.Error.Code)) &&
		response.State == "idle" && response.Execution == "none" &&
		response.System == nil && response.Core == nil
}

func validDevelopmentStarting(response Response) bool {
	return response.Protocol == 1 && response.OK && response.Error == nil &&
		response.State == "starting" && response.Execution == "development" &&
		response.System == nil && response.Core == nil
}

func validDevelopmentRunning(response Response) bool {
	return response.Protocol == 1 && response.OK && response.Error == nil &&
		response.State == "running_development" && response.Execution == "development" &&
		response.System == nil && (response.Core == nil || *response.Core != "")
}

func validCleanIdle(response Response) bool {
	return validIdle(response) && response.Error == nil
}

func developmentObservation(response Response) string {
	if response.Core == nil {
		return ""
	}
	return *response.Core
}

func validDevelopmentRBFPath(path string) bool {
	return path != "" && len(path) <= 4095 && strings.IndexByte(path, 0) < 0 &&
		filepath.IsAbs(path) && filepath.Clean(path) == path
}

func mapRemoteError(remote *RemoteError) *protocol.APIError {
	if remote == nil {
		return unavailableError()
	}
	switch remote.Code {
	case "invalid_request":
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "target runtime rejected the launch request"}
	case "unknown_system":
		return unsupportedSystemError()
	case "missing_media":
		return &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "target runtime could not open the cartridge"}
	case "busy":
		return &protocol.APIError{Code: protocol.CodeBusy, Message: "target runtime is busy"}
	case "save_failed":
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "SNES save could not be written; retry Stop before leaving the game"}
	case "core_mismatch":
		return &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "target runtime observed an unexpected core"}
	case "unsupported_protocol", "program_failed", "io_failed", "idle_failed":
		return unavailableError()
	default:
		return unavailableError()
	}
}

func mapProtocol2Error(remote *Protocol2Error) *protocol.APIError {
	if remote == nil {
		return unavailableError()
	}
	result := &protocol.APIError{Message: remote.Message, Phase: remote.Phase}
	if remote.Expected != nil {
		result.Expected = *remote.Expected
	}
	if remote.Observed != nil {
		result.Observed = *remote.Observed
	}
	switch remote.Code {
	case "invalid_request":
		result.Code = protocol.CodeBadRequest
	case "invalid_package":
		result.Code = protocol.CodeInvalidArchive
	case "unsupported_target", "unsupported_programming_profile", "unsupported_abi", "unsupported_interface", "unsupported_protocol":
		result.Code = protocol.CodeUnsupportedOperation
	case "busy":
		result.Code = protocol.CodeBusy
	case "core_mismatch":
		result.Code = protocol.CodeUnrecognizedCore
	default:
		result.Code = protocol.CodeMiSTerUnavailable
	}
	return result
}
