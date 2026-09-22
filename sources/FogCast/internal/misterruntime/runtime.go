package misterruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/corepackage"

	"github.com/DeanoC/FogCast/internal/flightdiag"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

const unavailableMessage = "target runtime is unavailable"
const unsupportedOperationMessage = "requested operation is unsupported"

type Runtime struct {
	computerMu         sync.Mutex
	keyboardMatrix     uint64
	keyboardPackageID  string
	keyboardGeneration uint64
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
	runtime := &Runtime{control: control, bootIDFile: bootIDFile, pollInterval: pollInterval, healthTimeout: healthTimeout, eventsPath: DefaultEventsPath, keyboardMatrix: 0xffffffffff}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

type CoreActivation struct {
	Composition      *expansion.Composition
	MediaStream      *protocol.MediaStreamCapability
	PersistenceMode  string
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

type protocol2RomInitControl interface {
	LoadInitializedCore(context.Context, string, string, string, string) (Protocol2Response, error)
	LoadInitializedLibraryCore(context.Context, string, string, string, string, string) (Protocol2Response, error)
	LoadInitializedComposedCore(context.Context, string, string, string, string, expansion.Composition, string, string) (Protocol2Response, error)
}

type protocol2InspectionControl interface {
	InspectCore(context.Context, string, string) (Protocol2Response, error)
}

type protocol2StatusControl interface {
	Protocol2Status(context.Context) (Protocol2Response, error)
}

type protocol2KeyboardControl interface {
	SetKeyboard(context.Context, uint64) (Protocol2Response, error)
}

func (r *Runtime) SetKeyboard(ctx context.Context, matrix uint64) error {
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	return r.setKeyboardLocked(ctx, matrix)
}

func (r *Runtime) setKeyboardLocked(ctx context.Context, matrix uint64) error {
	control, ok := r.control.(protocol2KeyboardControl)
	if !ok {
		return unsupportedOperationError()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	response, err := control.SetKeyboard(ctx, matrix)
	if err != nil {
		return err
	}
	if !response.OK {
		return mapProtocol2Error(response.Error)
	}
	r.keyboardMatrix = matrix
	r.keyboardPackageID, r.keyboardGeneration = "", 0
	if response.ActivePackage != nil && response.Generation != nil {
		r.keyboardPackageID = response.ActivePackage.PackageID
		r.keyboardGeneration = *response.Generation
	}
	return nil
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
		Descriptor: staged.Descriptor, Compatible: remote.Compatible, PersistenceLayout: remote.PersistenceLayout}
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
	return r.loadCoreOwned(admission, observation, operationOwner, size, content, "")
}
func (r *Runtime) LoadComposedCoreOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string) (CoreActivation, bool, *protocol.APIError) {
	return r.loadCoreOwnedMode(admission, observation, operationOwner, size, content, libraryID, true)
}
func (r *Runtime) loadCoreOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string) (activation CoreActivation, attempted bool, apiErr *protocol.APIError) {
	return r.loadCoreOwnedMode(admission, observation, operationOwner, size, content, libraryID, false)
}
func (r *Runtime) loadCoreOwnedMode(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string, composed bool) (activation CoreActivation, attempted bool, apiErr *protocol.APIError) {
	if r.corePackageRoot == "" {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	control, ok := r.control.(protocol2CoreControl)
	if !ok {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	var staged corepackage.Staged
	var err error
	var romInit *corepackage.RomInit
	body, err := io.ReadAll(io.LimitReader(content, size+1))
	if err != nil || int64(len(body)) != size {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	if corepackage.IsRomInit(body) {
		decoded, decodeErr := corepackage.ReadRomInit(body)
		if decodeErr != nil {
			return CoreActivation{}, false, &protocol.APIError{
				Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
		}
		romInit = &decoded
		if len(decoded.Composition) > 0 {
			body = decoded.Composition
			composed = true
		} else {
			body = decoded.Package
			composed = false
		}
		size = int64(len(body))
	}
	content = bytes.NewReader(body)
	if composed {
		if _, ok := r.control.(protocol2CompositionControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		staged, err = corepackage.StageComposition(admission, r.corePackageRoot, size, content)
	} else {
		staged, err = corepackage.Stage(admission, r.corePackageRoot, size, content)
	}
	if err != nil {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	if libraryID != "" && staged.PackageID != libraryID {
		if err := staged.Cleanup(); err != nil {
			r.retainRetired(staged)
		}
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "package identity differs from request", Phase: "admission"}
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
	expectedMode := "volatile"
	if libraryID != "" && !composed {
		inspector, ok := r.control.(protocol2DataControl)
		if !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if _, ok := r.control.(protocol2LibraryControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		data, err := inspector.InspectCoreData(admission, staged.Directory, staged.PackageID, CoreDataRoot)
		if err != nil {
			return CoreActivation{}, false, unavailableError()
		}
		if !data.OK || data.Error != nil {
			failure := mapCoreDataError(data.Error)
			failure.Phase = "admission"
			return CoreActivation{}, false, failure
		}
		if data.CoreData == nil || data.CoreData.PackageID != staged.PackageID || !(protocol.CoreDataInspection{CoreData: *data.CoreData, Descriptor: staged.Descriptor}).Valid() || !reflect.DeepEqual(data.CoreData.Layout, inspection.PersistenceLayout) {
			return CoreActivation{}, false, unavailableError()
		}
		expectedMode = data.CoreData.Mode
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
	var programmedPath string
	if romInit != nil {
		programmedPath, err = staged.RetainProgrammedBitstream(romInit.Programmed)
		if err != nil {
			return CoreActivation{}, false, &protocol.APIError{
				Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
		}
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

	var response Protocol2Response
	var callErr error
	if romInit != nil {
		initControl, ok := r.control.(protocol2RomInitControl)
		if !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		sum := sha256.Sum256(romInit.Programmed)
		programmedSHA := hex.EncodeToString(sum[:])
		if composed {
			response, callErr = initControl.LoadInitializedComposedCore(operationOwner, staged.Directory, staged.PackageID, staged.ExpansionDirectory, staged.PayloadPath, *staged.Composition, programmedPath, programmedSHA)
			r.noteDispatch("load_initialized_composed_core", callErr == nil)
		} else if libraryID != "" {
			response, callErr = initControl.LoadInitializedLibraryCore(operationOwner, staged.Directory, staged.PackageID, CoreDataRoot, programmedPath, programmedSHA)
			r.noteDispatch("load_initialized_library_core", callErr == nil)
		} else {
			response, callErr = initControl.LoadInitializedCore(operationOwner, staged.Directory, staged.PackageID, programmedPath, programmedSHA)
			r.noteDispatch("load_initialized_core", callErr == nil)
		}
	} else if composed {
		response, callErr = r.control.(protocol2CompositionControl).LoadComposedCore(operationOwner, staged.Directory, staged.PackageID, staged.ExpansionDirectory, staged.PayloadPath, *staged.Composition)
		r.noteDispatch("load_composed_core", callErr == nil)
	} else if libraryID != "" {
		response, callErr = r.control.(protocol2LibraryControl).LoadLibraryCore(operationOwner, staged.Directory, staged.PackageID, CoreDataRoot)
		r.noteDispatch("load_library_core", callErr == nil)
	} else {
		response, callErr = control.LoadCore(operationOwner, staged.Directory, staged.PackageID)
		r.noteDispatch("load_core", callErr == nil)
	}
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
					if observed.PersistenceMode != expectedMode {
						r.retainRetired(staged)
						cleanupStaged = false
						failure := unavailableError()
						failure.Phase = "recovery"
						return CoreActivation{}, true, failure
					}
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
		if response.Error != nil && response.Error.Phase == "core_data" && (response.Error.Code == "corrupt_data" || response.Error.Code == "incompatible_data" || response.Error.Code == "save_failed") && before != nil && sameProtocol2RuntimeState(*before, response) {
			attempted = false
			preserveInput = true
			apiErr.Phase = "admission"
		}
		if response.Error != nil && response.Error.Code == "save_failed" {
			preserveInput = before != nil && sameProtocol2RuntimeState(*before, response) && (response.State == "running_development" || response.State == "idle")
			attempted = !preserveInput
			if attempted {
				apiErr.Phase = "recovery"
			}
		}
		return CoreActivation{}, attempted, apiErr
	}
	if response.State != "running_development" || response.Execution != "development" ||
		response.ActivePackage == nil || response.ActivePackage.PackageID != staged.PackageID ||
		!reflect.DeepEqual(response.ActivePackage.Composition, staged.Composition) ||
		response.Generation == nil || *response.Generation == 0 {
		r.retainRetired(staged)
		cleanupStaged = false
		return CoreActivation{}, true, unavailableError()
	}
	activation = activationFromProtocol2(staged.PackageID, staged.Descriptor, response)
	if activation.PersistenceMode != expectedMode {
		r.retainRetired(staged)
		cleanupStaged = false
		failure := unavailableError()
		failure.Phase = "recovery"
		return CoreActivation{}, true, failure
	}
	r.replaceActivePackage(staged)
	cleanupStaged = false
	return activation, true, nil
}

func replacementBarrierError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal,
		Message: "remote input could not be safely transitioned", Phase: "recovery"}
}

func activationFromProtocol2(packageID string, descriptor corepackage.Descriptor, response Protocol2Response) CoreActivation {
	activation := CoreActivation{PackageID: packageID, Descriptor: descriptor, PersistenceMode: "volatile",
		MediaStream:      response.Capabilities.MediaStream,
		ActiveInterfaces: append([]Protocol2Interface(nil), response.Capabilities.ActiveInterfaces...)}
	if response.ActivePackage != nil {
		activation.Composition = cloneComposition(response.ActivePackage.Composition)
	}
	if response.ActivePackage != nil && response.ActivePackage.PersistenceMode != "" {
		activation.PersistenceMode = response.ActivePackage.PersistenceMode
	}
	if response.Generation != nil {
		activation.Generation = *response.Generation
	}
	if response.Core != nil {
		activation.ObservedCore = *response.Core
	}
	for _, contract := range activation.ActiveInterfaces {
		if (contract.ID == "fes.gamepad" || contract.ID == "fes.gamepad.ports") && contract.Major == 1 && contract.Minor == 0 {
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
			response.ActivePackage.PackageID == staged.PackageID && reflect.DeepEqual(response.ActivePackage.Composition, staged.Composition) && response.Generation != nil &&
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
		reflect.DeepEqual(left.ActivePackage.Observed, right.ActivePackage.Observed) &&
		reflect.DeepEqual(left.ActivePackage.Composition, right.ActivePackage.Composition)
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
// Native idle proves no package or diagnostic remains active.
func (r *Runtime) ConfirmIdle(ctx context.Context) bool {
	response, err := r.boundedStatus(ctx)
	return err == nil && ctx.Err() == nil && validIdle(response) &&
		(response.Error == nil || response.Error.Code != "save_failed")
}

func (r *Runtime) StopReady() bool {
	response, err := r.boundedStatus(context.Background())
	return err == nil && (validIdle(response) || validDevelopmentRunning(response) || (response.ActivePackage != nil && describedPackageRuntimeState(response)))
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	return r.reconcileProtocol2(ctx, r.control)
}

func (r *Runtime) reconcileProtocol2(ctx context.Context, control protocol2StatusControl) protocol.Status {
	for {
		response, err := r.boundedStatus(ctx)
		if errors.Is(err, errProtocol2Unsupported) {
			return unavailableStatus()
		}
		if err != nil {
			if !waitForPoll(ctx, r.pollInterval) {
				return unavailableStatus()
			}
			continue
		}
		if !validProtocol2Response(response) || !response.OK && response.State != "reboot_required" && !protocol2ResumedSaveFailure(response) {
			return unavailableStatus()
		}
		switch response.State {
		case "starting":
			if response.Error != nil {
				return unavailableStatus()
			}
			if !waitForPoll(ctx, r.pollInterval) {
				return unavailableStatus()
			}
			continue
		case "idle":
			status := protocol.Status{State: protocol.StateIdle}
			status.LastError = mapOptionalProtocol2Error(response.Error)
			return status
		case "reboot_required":
			if response.ActivePackage != nil {
				return r.reconcileActiveCore(response, true)
			}
			apiErr := mapProtocol2Error(response.Error)
			return protocol.Status{State: protocol.StateFailed, Recovery: protocol.RecoveryRebootRequired, LastError: apiErr}
		case "running_development":
			return r.reconcileActiveCore(response, false)
		default:
			return unavailableStatus()
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
		if adopted[index].PackageID == active.PackageID && reflect.DeepEqual(adopted[index].Descriptor, active.Descriptor) && reflect.DeepEqual(adopted[index].Composition, active.Composition) {
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
	return &protocol.CorePackageStatus{Composition: cloneComposition(activation.Composition), PackageID: activation.PackageID, Generation: activation.Generation, PersistenceMode: activation.PersistenceMode,
		MediaStream: activation.MediaStream,
		ABI:         protocol.RuntimeContract{ID: activation.Descriptor.ABI.ID, Major: uint16(activation.Descriptor.ABI.Major), Minor: uint16(activation.Descriptor.ABI.Minor)},
		BuildID:     activation.Descriptor.Build.ID, ActiveInterfaces: interfaces, Gamepad: activation.Gamepad}
}

func (r *Runtime) boundedStatus(parent context.Context) (Protocol2Response, error) {
	ctx, cancel := context.WithTimeout(parent, r.healthTimeout)
	defer cancel()
	return r.control.Protocol2Status(ctx)
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
	if err := writeAtomicDevelopmentRBF(r.developmentRBFPath, size, &contextReader{ctx: admission, reader: content}); err != nil {
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
	response, err = r.control.Protocol2LoadDevelopmentRBF(dispatchContext, r.developmentRBFPath)
	r.noteDispatch("development_rbf", err == nil)
	if err != nil {
		var mutation protocol2MutationError
		if errors.As(err, &mutation) && !mutation.attempted {
			return "", "", false, unavailableError()
		}
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
		return "", protocol.RecoveryRebootRequired, true, mapProtocol2Error(response.Error)
	}
	if response.Error != nil || !response.OK {
		return "", "", true, mapProtocol2Error(response.Error)
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
		response, err := r.control.Protocol2Status(ctx)
		if err != nil {
			return "", true, unavailableError(), contextExhausted(ctx)
		}
		if validDevelopmentRunning(response) {
			return developmentObservation(response), true, nil, false
		}
		if validIdle(response) && response.Error != nil {
			return "", true, mapProtocol2Error(response.Error), false
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

// RecoverIdle restores protocol-2 idle through LoadIdle. It does not execute
// rebootCommand. Call it when the runtime is reboot_required or already idle
// after a service restart. A running session must use Stop.
func (r *Runtime) RecoverIdle(ctx context.Context) (string, *protocol.APIError) {
	if err := ctx.Err(); err != nil {
		return "", unavailableError()
	}
	control, ok := r.control.(protocol2RecoverIdleControl)
	if !ok {
		return "", unsupportedOperationError()
	}
	response, err := control.Protocol2RecoverIdle(ctx)
	r.noteDispatch("recover_idle", err == nil)
	if err != nil {
		return "", unavailableError()
	}
	if rebootRequiredResponse(response) {
		return optionalProtocol2Core(response), mapProtocol2Error(response.Error)
	}
	if response.Error != nil || !response.OK {
		return optionalProtocol2Core(response), mapProtocol2Error(response.Error)
	}
	if !validIdle(response) {
		return "", unavailableError()
	}
	if apiErr := r.cleanupCorePackages(); apiErr != nil {
		return "", apiErr
	}
	return "", nil
}

// RuntimeSocketUnreachable reports that the local runtime socket could not be
// contacted. A protocol error from a live daemon is still reachable.
func (r *Runtime) RuntimeSocketUnreachable(ctx context.Context) bool {
	if r == nil || r.control == nil {
		return true
	}
	_, err := r.control.Protocol2Status(ctx)
	if err == nil {
		return false
	}
	return errors.Is(err, errRuntimeConnection) ||
		errors.Is(err, errRuntimeDeadline) ||
		errors.Is(err, errRuntimeRequestWrite) ||
		errors.Is(err, errRuntimeResponseRead) ||
		errors.Is(err, errRuntimeResponseTooLong) ||
		errors.Is(err, errRuntimeMissingNewline)
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
	if admission.Err() != nil || (owned && operation.Err() != nil) {
		return "", "", unavailableError()
	}
	ctx := admission
	if owned {
		ctx = operation
	}
	return r.stopCorePackage(ctx, r.control)
}

func rebootRequiredResponse(response Protocol2Response) bool {
	return response.Protocol == 2 && !response.OK && response.State == "reboot_required" &&
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

func validIdle(response Protocol2Response) bool {
	return response.Protocol == 2 && response.OK &&
		(response.Error == nil || validProtocol2Error(*response.Error)) &&
		response.State == "idle" && response.Execution == "none" &&
		response.System == nil && response.Core == nil && response.ActivePackage == nil && response.Generation == nil
}

func validDevelopmentStarting(response Protocol2Response) bool {
	return response.Protocol == 2 && response.OK && response.Error == nil &&
		response.State == "starting" && response.Execution == "development" &&
		response.System == nil && response.Core == nil
}

func validDevelopmentRunning(response Protocol2Response) bool {
	return response.Protocol == 2 && response.OK && response.Error == nil &&
		response.State == "running_development" && response.Execution == "development" &&
		response.System == nil && response.Core == nil && response.ActivePackage == nil &&
		response.Generation != nil && *response.Generation > 0 && len(response.Capabilities.ActiveInterfaces) == 0
}

func validCleanIdle(response Protocol2Response) bool {
	return validIdle(response) && response.Error == nil
}

func developmentObservation(response Protocol2Response) string {
	if response.Core == nil {
		return ""
	}
	return *response.Core
}

func validDevelopmentRBFPath(path string) bool {
	return path != "" && len(path) <= 4095 && strings.IndexByte(path, 0) < 0 &&
		filepath.IsAbs(path) && filepath.Clean(path) == path
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
	case "corrupt_data":
		result.Code = protocol.CodeCorruptData
		result.Message = "stored core data is corrupt"
		result.Expected = ""
		result.Observed = ""
	case "incompatible_data":
		result.Code = protocol.CodeIncompatibleData
		result.Message = "stored or active core data is incompatible"
		result.Expected = ""
		result.Observed = ""
	case "stale_revision":
		result.Code = protocol.CodeStaleRevision
		result.Message = "core data revision changed; refresh before retrying"
		result.Expected = ""
		result.Observed = ""
	case "save_failed":
		result.Code = protocol.CodeSaveFailed
		result.Message = "core data could not be durably written; inspect status before retrying"
		result.Expected = ""
		result.Observed = ""
	case "idle_failed":
		result.Code = protocol.CodeMiSTerUnavailable
		result.Message = "target runtime recovery is required"
		result.Expected = ""
		result.Observed = ""
	case "core_mismatch":
		result.Code = protocol.CodeUnrecognizedCore
	default:
		result.Code = protocol.CodeMiSTerUnavailable
		result.Message = "target runtime is unavailable"
		result.Expected, result.Observed = "", ""
	}
	return result
}

func (r *Runtime) reconcileActiveCore(response Protocol2Response, recovery bool) protocol.Status {
	status := protocol.Status{State: protocol.StateActive, Development: true,
		LastError: mapOptionalProtocol2Error(response.Error)}
	if recovery {
		status.State = protocol.StateFailed
		status.Recovery = protocol.RecoveryRebootRequired
	}
	if response.Core != nil {
		observed := *response.Core
		status.ObservedCore = &observed
	}
	if response.ActivePackage == nil {
		return status
	}
	if r.corePackageRoot == "" || response.Generation == nil {
		return unavailableStatus()
	}
	adopted, err := corepackage.Adopt(r.corePackageRoot)
	activeIndex := matchingAdoptedPackage(adopted, *response.ActivePackage)
	if err != nil || activeIndex < 0 {
		return unavailableStatus()
	}
	r.adoptActivePackage(adopted, activeIndex)
	activation := activationFromProtocol2(adopted[activeIndex].PackageID, adopted[activeIndex].Descriptor, response)
	status.CorePackage = corePackageStatus(activation)
	return status
}

func cloneComposition(value *expansion.Composition) *expansion.Composition {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

type protocol2CompositionControl interface {
	LoadComposedCore(context.Context, string, string, string, string, expansion.Composition) (Protocol2Response, error)
}
