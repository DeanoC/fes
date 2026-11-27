package misterruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

const unavailableMessage = "target runtime is unavailable"

const unsupportedOperationMessage = "requested operation is unsupported"

type Runtime struct {
	computerMu             sync.Mutex
	keyboardMatrix         uint64
	keyboardInstance       string
	keyboardPackageID      string
	keyboardGeneration     uint64
	keyboardUncertain      *localhid.MatrixWitness
	keyboardMediaUncertain *localhid.MatrixMediaWitness
	control                Control
	bootIDFile             string
	pollInterval           time.Duration
	healthTimeout          time.Duration
	developmentRBFPath     string
	corePackageRoot        string
	diskImageRoot          string
	recoveryReboot         func(context.Context) error
	packageMu              sync.Mutex
	activePackage          *corepackage.Staged
	retiredPackages        []corepackage.Staged
	coreBarrier            CoreReplacementBarrier
	displayFocus           SessionDisplayFocus
	events                 flightdiag.Sink
	eventsPath             string
	eventsMu               sync.Mutex
	imported               int
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

// WithRecoveryReboot supplies the agent's guarded command starter. The starter
// returns after dispatch and owns command retirement and cancellation afterward.
func WithRecoveryReboot(start func(context.Context) error) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.recoveryReboot = start
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
	runtime := &Runtime{control: control, bootIDFile: bootIDFile, pollInterval: pollInterval, healthTimeout: healthTimeout, eventsPath: DefaultEventsPath, keyboardMatrix: 0xffffffffff, diskImageRoot: DiskImageRoot}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

type protocol2CoreControl interface {
	LoadCore(context.Context, string, string) (Protocol2Response, error)
}

type protocol2ROMLinkControl interface {
	LoadROMLinkedCore(context.Context, string, string, string, string, string, *expansion.Composition, string, corepackage.ROMLinkIdentity) (Protocol2Response, error)
}

type protocol2FirmwareControl interface {
	LoadFirmwareCore(context.Context, string, string, string, string, corepackage.ROMLinkIdentity, *InitialMediaRequest, string) (Protocol2Response, error)
}

type protocol2ROMLinksControl interface {
	LoadROMLinksLinkedCore(context.Context, string, string, string, string, string, *expansion.Composition, string, corepackage.ROMLinksIdentity) (Protocol2Response, error)
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
	SetKeyboard(context.Context, string, string, uint64, uint64) (Protocol2Response, error)
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

// ObserveRuntimeInput obtains the binding from one validated runtime status.
// Callers must compare it with their current session before publishing it.
func (r *Runtime) ObserveRuntimeInput(ctx context.Context) (*protocol.RuntimeInputIdentity, error) {
	response, err := r.ControllerStatus(ctx)
	if err != nil {
		return nil, err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !validProtocol2Response(response) {
		return nil, errInvalidRuntimeResponse
	}
	identity := runtimeInputIdentity(response)
	if identity == nil {
		return nil, errInvalidRuntimeResponse
	}
	return identity, nil
}

func runtimeInputIdentity(response Protocol2Response) *protocol.RuntimeInputIdentity {
	if !response.OK || response.Error != nil || response.State != "running_development" || response.Execution != "development" || response.RuntimeInstance == nil || response.Generation == nil {
		return nil
	}
	candidate := protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: *response.RuntimeInstance, Generation: *response.Generation}
	if response.ActivePackage != nil {
		candidate.Kind = protocol.RuntimeInputDescribed
		candidate.PackageID = response.ActivePackage.PackageID
	}
	if candidate.Validate() != nil {
		return nil
	}
	return &candidate
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
		if adopted[index].PackageID == active.PackageID && reflect.DeepEqual(adopted[index].Descriptor, active.Descriptor) && reflect.DeepEqual(adopted[index].StaticComposition, active.StaticComposition) && reflect.DeepEqual(adopted[index].Composition, active.Composition) && reflect.DeepEqual(adopted[index].SlotComposition, active.SlotComposition) && reflect.DeepEqual(adopted[index].PartsComposition, active.PartsComposition) && reflect.DeepEqual(adopted[index].ROMLink, active.ROMLink) && reflect.DeepEqual(adopted[index].ROMLinks, active.ROMLinks) {
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

// ExpansionSocketReceipt is the running receipt for a package's rear socket.
// A load that succeeded with no composition had none staged and none echoed
// by the runtime (the load is refused otherwise), so the socket is reported
// empty. For a ZX81 RomInit load this rests on the host staging no
// composition, the same trust a composed RomInit receipt already has: the
// kit checks the programmed image against its receipt, not the package
// payload. A composed, slotted or parts load is not reported here.
func ExpansionSocketReceipt(activation CoreActivation) string {
	if activation.StaticComposition != nil || activation.Composition != nil || activation.SlotComposition != nil || activation.PartsComposition != nil {
		return ""
	}
	if !corepackage.RearExpansionSocket(activation.Descriptor) {
		return ""
	}
	return protocol.ExpansionSocketEmpty
}

func corePackageStatus(activation CoreActivation) *protocol.CorePackageStatus {
	interfaces := make([]protocol.RuntimeInterface, len(activation.ActiveInterfaces))
	for index, value := range activation.ActiveInterfaces {
		interfaces[index] = protocol.RuntimeInterface{ID: value.ID, Major: value.Major, Minor: value.Minor}
	}
	return &protocol.CorePackageStatus{StaticComposition: cloneStaticComposition(activation.StaticComposition), PartsComposition: clonePartsComposition(activation.PartsComposition), ROMLink: cloneROMLink(activation.ROMLink), ROMLinks: cloneROMLinks(activation.ROMLinks), Composition: cloneComposition(activation.Composition), PackageID: activation.PackageID, CoreID: activation.Descriptor.Core.ID, Generation: activation.Generation, PersistenceMode: activation.PersistenceMode,
		SlotComposition: cloneSlotComposition(activation.SlotComposition), ExpansionSocket: ExpansionSocketReceipt(activation), MediaUnits: cloneMediaUnits(activation.MediaUnits),
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
	var identity *protocol.RuntimeInputIdentity
	if !recovery {
		identity = runtimeInputIdentity(response)
	}
	if recovery {
		status.State = protocol.StateFailed
		status.Recovery = protocol.RecoveryRebootRequired
	}
	if response.Core != nil {
		observed := *response.Core
		status.ObservedCore = &observed
	}
	if response.ActivePackage == nil {
		status.RuntimeInput = identity
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
	status.RuntimeInput = identity
	return status
}

type protocol2CompositionControl interface {
	LoadComposedCore(context.Context, string, string, string, string, expansion.Composition) (Protocol2Response, error)
}
