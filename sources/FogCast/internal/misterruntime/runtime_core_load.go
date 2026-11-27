package misterruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type CoreActivation struct {
	StaticComposition *expansion.StaticComposition
	PartsComposition  *expansion.PartsComposition
	ROMLink           *corepackage.ROMLinkIdentity
	ROMLinks          *corepackage.ROMLinksIdentity
	Composition       *expansion.Composition
	SlotComposition   *expansion.SlotComposition
	MediaStream       *protocol.MediaStreamCapability
	MediaUnits        []protocol.MediaUnitStatus
	PersistenceMode   string
	PackageID         string
	RuntimeInstance   string
	Descriptor        corepackage.Descriptor
	Generation        uint64
	ObservedCore      string
	ActiveInterfaces  []Protocol2Interface
	Gamepad           bool
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
	// OK is the result of this inspection. Error can still describe a
	// previous runtime fault retained in status after MENU recovery.
	if !response.OK {
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
	return r.loadCoreOwned(admission, observation, operationOwner, size, content, "", nil)
}

func (r *Runtime) LoadComposedCoreOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string) (CoreActivation, bool, *protocol.APIError) {
	return r.loadCoreOwnedMode(admission, observation, operationOwner, size, content, libraryID, nil, true)
}

func (r *Runtime) loadCoreOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string, machine *protocol.LibraryMachineBinding) (activation CoreActivation, attempted bool, apiErr *protocol.APIError) {
	return r.loadCoreOwnedMode(admission, observation, operationOwner, size, content, libraryID, machine, false)
}

func (r *Runtime) loadCoreOwnedMode(admission, observation, operationOwner context.Context, size int64, content io.Reader, libraryID string, machine *protocol.LibraryMachineBinding, composed bool, partsMode ...bool) (activation CoreActivation, attempted bool, apiErr *protocol.APIError) {
	if r.corePackageRoot == "" {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	control, ok := r.control.(protocol2CoreControl)
	if !ok {
		return CoreActivation{}, false, unsupportedOperationError()
	}
	parts := len(partsMode) == 1 && partsMode[0]
	maxInputSize := int64(max(corepackage.MaxROMInputSize, corepackage.MaxCompositionArchiveSize))
	if parts {
		maxInputSize = corepackage.MaxPartsArchiveSize
	}
	if size < 1 || size > maxInputSize {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid core input size", Phase: "admission"}
	}
	var staged corepackage.Staged
	var err error
	var romInit *corepackage.RomInit
	if admission.Err() != nil {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	// The body goes once to a private file in the staging root and is read
	// through a mapping, not onto the heap (#863). Nothing staged keeps its
	// bytes, so the mapping ends with this call.
	spool, err := corepackage.SpoolInput(admission, r.corePackageRoot, size, maxInputSize, content)
	if err != nil {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	defer spool.Close()
	body := spool.Bytes()
	romLinkedV2 := corepackage.IsROMInputV2(body)
	romLinked := romLinkedV2 || corepackage.IsROMInput(body)
	pcxtInitial := corepackage.IsPcxtInitialInput(body)
	if parts && (romLinked || corepackage.IsRomInit(body)) {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "developer parts require a parts archive", Phase: "admission"}
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
	}
	if parts {
		if _, ok := r.control.(protocol2PartsControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if libraryID != "" {
			if _, ok := r.control.(protocol2LibraryPartsControl); !ok {
				return CoreActivation{}, false, unsupportedOperationError()
			}
		}
		staged, err = corepackage.StagePartsBytes(admission, r.corePackageRoot, body)
	} else if pcxtInitial && !composed && libraryID != "" {
		staged, err = corepackage.StagePcxtInitialInputSpooled(admission, r.corePackageRoot, spool)
	} else if romLinked {
		if romLinkedV2 {
			if _, ok := r.control.(protocol2ROMLinksControl); !ok {
				return CoreActivation{}, false, unsupportedOperationError()
			}
		} else if _, ok := r.control.(protocol2ROMLinkControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		statusControl, ok := r.control.(protocol2StatusControl)
		if !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		status, statusErr := statusControl.Protocol2Status(admission)
		if statusErr != nil {
			return CoreActivation{}, false, unavailableError()
		}
		// A firmware-upload package needs no linker; which kind this input
		// is, is known only once it is staged.
		if status.Capabilities.ROMLinking != 1 && (romLinkedV2 || !firmwareUploadSupported(status.Capabilities)) {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if romLinkedV2 {
			staged, err = corepackage.StageROMInputV2Spooled(admission, r.corePackageRoot, spool)
		} else {
			staged, err = corepackage.StageROMInputSpooled(admission, r.corePackageRoot, spool)
			if err == nil && !romDeliverySupported(status.Capabilities, staged.Descriptor) {
				if cleanupErr := staged.Cleanup(); cleanupErr != nil {
					r.retainRetired(staged)
				}
				return CoreActivation{}, false, unsupportedOperationError()
			}
		}
		composed = staged.Composition != nil || staged.SlotComposition != nil || staged.PartsComposition != nil
	} else if composed && corepackage.IsStaticCompositionBundle(body) {
		if _, ok := r.control.(protocol2StaticCompositionControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		staged, err = corepackage.StageStaticCompositionBytes(admission, r.corePackageRoot, body)
	} else if composed {
		_, single := r.control.(protocol2CompositionControl)
		_, slots := r.control.(protocol2SlotCompositionControl)
		if (corepackage.IsSlotCompositionBundle(body) && !slots) || (!corepackage.IsSlotCompositionBundle(body) && !single) {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		staged, err = corepackage.StageCompositionBytes(admission, r.corePackageRoot, body)
	} else {
		staged, err = corepackage.StageBytes(admission, r.corePackageRoot, body)
	}
	if err != nil {
		return CoreActivation{}, false, &protocol.APIError{
			Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	// The published files own their bytes now. Release the spool before the
	// load so tmpfs holds one copy; a ROM input keeps it as input.tar.
	if romInit != nil {
		romInit.Programmed = bytes.Clone(romInit.Programmed)
	}
	if err := spool.Close(); err != nil {
		if cleanupErr := staged.Cleanup(); cleanupErr != nil {
			r.retainRetired(staged)
		}
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
	if staged.Descriptor.Format == 5 && (staged.StaticComposition == nil || romInit != nil || parts || machine != nil || staged.InitialMedia != nil || staged.ROMLink != nil || staged.ROMLinks != nil) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "static package requires its cold composition without ROM inputs", Phase: "admission"}
	}
	if staged.InitialMedia != nil {
		if libraryID == "" || (staged.ROMLink == nil && !pcxtInitial) {
			return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "initial disk requires an explicit library ROM launch", Phase: "admission"}
		}
		if pcxtInitial {
			if _, ok := r.control.(protocol2InitialCoreControl); !ok {
				return CoreActivation{}, false, unsupportedOperationError()
			}
		} else if !r.supportsInitialROMCore(staged) {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
	if staged.PartsComposition != nil && staged.ROMLink != nil {
		if _, ok := r.control.(protocol2ROMPartsCompositionControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if _, ok := r.control.(protocol2PartsControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
	if staged.PartsComposition != nil && staged.ROMLinks != nil {
		if _, ok := r.control.(protocol2ROMLinksPartsCompositionControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if _, ok := r.control.(protocol2PartsControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
	if staged.SlotComposition != nil && staged.ROMLink != nil {
		if _, ok := r.control.(protocol2ROMSlotControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
	if staged.FirmwarePath != "" {
		if _, ok := r.control.(protocol2FirmwareControl); !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
	if (staged.Descriptor.ROM != nil && staged.ROMLink == nil) || (staged.Descriptor.Format == 4 && staged.ROMLinks == nil) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "ROM-bearing package requires selected ROM input", Phase: "admission"}
	}
	// Only a fes.computer package with a per-game record takes a game.
	pc386 := protocol.DeclaresPc386Machine(staged.Descriptor)
	pcxt := protocol.DeclaresPcxtMachine(staged.Descriptor)
	perGame := pc386 || pcxt
	if machine != nil && (libraryID == "" || composed || parts || romInit != nil || staged.ROMLinks != nil || !perGame ||
		(staged.ROMLink != nil && (!pcxt || staged.FirmwarePath == ""))) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "a library game binds only a machine library package", Phase: "admission"}
	}
	if machine == nil && libraryID != "" && perGame {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "a machine library launch requires its library game", Phase: "admission"}
	}
	if machine != nil && machine.Disk != nil && !protocol.DeclaresAtaDisk(staged.Descriptor) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "package declares no hard disk", Phase: "admission"}
	}
	if machine != nil && staged.InitialMedia != nil && (machine.Disk != nil || len(machine.Floppies) != 0 || staged.InitialMedia.GameID != machine.GameID) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "initial media does not match the library game binding", Phase: "admission"}
	}
	if machine != nil && len(machine.Floppies) != 0 && !protocol.DeclaresPcFloppy(staged.Descriptor) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "package declares no floppy drives", Phase: "admission"}
	}
	disk, diskErr := r.resolveStagedDisk(machine)
	if diskErr != nil {
		return CoreActivation{}, false, diskErr
	}
	floppies, floppyErr := r.resolveStagedFloppies(machine)
	if floppyErr != nil {
		return CoreActivation{}, false, floppyErr
	}
	if machine != nil {
		_, loads := r.control.(protocol2MachineLibraryControl)
		_, inspects := r.control.(protocol2MachineDataControl)
		if !loads || !inspects {
			return CoreActivation{}, false, unsupportedOperationError()
		}
	}
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
	if libraryID != "" && (!composed || parts) {
		inspector, ok := r.control.(protocol2DataControl)
		if !ok {
			return CoreActivation{}, false, unsupportedOperationError()
		}
		if !parts {
			if _, ok := r.control.(protocol2LibraryControl); !ok {
				return CoreActivation{}, false, unsupportedOperationError()
			}
		}
		var data Protocol2Response
		var err error
		if machine != nil {
			data, err = r.control.(protocol2MachineDataControl).InspectMachineCoreData(admission, staged.Directory, staged.PackageID, CoreDataRoot, machine.GameID)
		} else {
			data, err = inspector.InspectCoreData(admission, staged.Directory, staged.PackageID, CoreDataRoot)
		}
		if err != nil {
			return CoreActivation{}, false, unavailableError()
		}
		if machine != nil && (data.CoreData == nil || data.CoreData.GameID != machine.GameID) && data.OK {
			return CoreActivation{}, false, unavailableError()
		}
		if !data.OK {
			failure := mapCoreDataError(data.Error)
			failure.Phase = "admission"
			return CoreActivation{}, false, failure
		}
		if data.CoreData == nil || data.CoreData.PackageID != staged.PackageID || !(protocol.CoreDataInspection{CoreData: *data.CoreData, Descriptor: staged.Descriptor}).Valid() || !reflect.DeepEqual(data.CoreData.Layout, inspection.PersistenceLayout) {
			return CoreActivation{}, false, unavailableError()
		}
		expectedMode = data.CoreData.Mode
	}
	if staged.InitialMedia != nil || disk != nil {
		expectedMode = "persistent"
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
	if staged.StaticComposition != nil && (before == nil || before.State != "idle") {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the running core before selecting a static expansion", Phase: "admission"}
	}
	var programmedPath string
	if romInit != nil {
		programmedPath, err = staged.RetainProgrammedBitstream(romInit.Programmed)
		if err != nil {
			return CoreActivation{}, false, &protocol.APIError{
				Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
		}
	}
	// Admission remains cancellable until input ownership begins to change.
	if admission.Err() != nil || observation.Err() != nil || operationOwner.Err() != nil {
		return CoreActivation{}, false, unavailableError()
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
	if staged.StaticComposition != nil {
		response, callErr = r.control.(protocol2StaticCompositionControl).LoadStaticComposedCore(operationOwner, staged.Directory, staged.PackageID, staged.ExpansionDirectory, *staged.StaticComposition)
		r.noteDispatch("load_composed_core", callErr == nil)
	} else if staged.InitialMedia != nil {
		if pcxtInitial && machine != nil {
			m := staged.InitialMedia
			initial := &InitialMediaRequest{Path: m.Path, Size: uint32(m.Size), Unit: m.Unit, DataRoot: MediaDataRoot, GameID: m.GameID, BaseMediaID: m.BaseMediaID}
			response, callErr = r.control.(protocol2MachineLibraryControl).LoadLibraryMachineCore(operationOwner, staged.Directory, staged.PackageID, CoreDataRoot, machine.GameID, nil, initial, nil)
			r.noteDispatch("load_library_core", callErr == nil)
		} else if pcxtInitial {
			response, callErr = r.loadInitialCore(operationOwner, staged)
		} else {
			gameID := ""
			if machine != nil {
				gameID = machine.GameID
			}
			response, callErr = r.loadInitialROMCore(operationOwner, staged, gameID)
		}
	} else if staged.PartsComposition != nil && staged.ROMLink != nil {
		response, callErr = r.control.(protocol2ROMPartsCompositionControl).LoadROMPartsComposedCore(operationOwner, staged.Directory, staged.PackageID, partPaths(staged), staged.PayloadPath, *staged.PartsComposition, staged.ProgrammedPath, *staged.ROMLink)
		r.noteDispatch("load_rom_composed_core", callErr == nil)
	} else if staged.PartsComposition != nil && staged.ROMLinks != nil {
		response, callErr = r.control.(protocol2ROMLinksPartsCompositionControl).LoadROMLinksPartsComposedCore(operationOwner, staged.Directory, staged.PackageID, partPaths(staged), staged.PayloadPath, *staged.PartsComposition, staged.ProgrammedPath, *staged.ROMLinks)
		r.noteDispatch("load_rom_composed_core", callErr == nil)
	} else if staged.PartsComposition != nil {
		if libraryID != "" {
			response, callErr = r.control.(protocol2LibraryPartsControl).LoadLibraryPartsCore(operationOwner, staged.Directory, staged.PackageID, CoreDataRoot, staged.PayloadPath, partPaths(staged), *staged.PartsComposition)
			r.noteDispatch("load_parts_library_core", callErr == nil)
		} else {
			response, callErr = r.control.(protocol2PartsControl).LoadPartsCore(operationOwner, staged.Directory, staged.PackageID, staged.PayloadPath, partPaths(staged), *staged.PartsComposition)
			r.noteDispatch("load_parts_core", callErr == nil)
		}
	} else if staged.ROMLinks != nil {
		dataRoot := ""
		if libraryID != "" && !composed {
			dataRoot = CoreDataRoot
		}
		response, callErr = r.control.(protocol2ROMLinksControl).LoadROMLinksLinkedCore(operationOwner, staged.Directory, staged.PackageID, dataRoot, staged.ExpansionDirectory, staged.PayloadPath, staged.Composition, staged.ProgrammedPath, *staged.ROMLinks)
		r.noteDispatch("load_rom_core", callErr == nil)
	} else if staged.ROMLink != nil && staged.SlotComposition != nil {
		response, callErr = r.control.(protocol2ROMSlotControl).LoadROMSlotComposedCore(operationOwner, staged.Directory, staged.PackageID, slotExpansionPaths(staged), staged.PayloadPath, *staged.SlotComposition, staged.ProgrammedPath, *staged.ROMLink)
		r.noteDispatch("load_rom_composed_core", callErr == nil)
	} else if staged.FirmwarePath != "" {
		dataRoot, gameID := "", ""
		if libraryID != "" {
			dataRoot = CoreDataRoot
		}
		if machine != nil {
			gameID = machine.GameID
		}
		response, callErr = r.control.(protocol2FirmwareControl).LoadFirmwareCore(operationOwner, staged.Directory, staged.PackageID, dataRoot, staged.FirmwarePath, *staged.ROMLink, nil, gameID)
		r.noteDispatch("load_rom_core", callErr == nil)
	} else if staged.ROMLink != nil {
		dataRoot := ""
		if libraryID != "" && !composed {
			dataRoot = CoreDataRoot
		}
		response, callErr = r.control.(protocol2ROMLinkControl).LoadROMLinkedCore(operationOwner, staged.Directory, staged.PackageID, dataRoot, staged.ExpansionDirectory, staged.PayloadPath, staged.Composition, staged.ProgrammedPath, *staged.ROMLink)
		r.noteDispatch("load_rom_core", callErr == nil)
	} else if romInit != nil {
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
	} else if composed && staged.SlotComposition != nil {
		response, callErr = r.control.(protocol2SlotCompositionControl).LoadSlotComposedCore(operationOwner, staged.Directory, staged.PackageID, slotExpansionPaths(staged), staged.PayloadPath, *staged.SlotComposition)
		r.noteDispatch("load_composed_core", callErr == nil)
	} else if composed {
		response, callErr = r.control.(protocol2CompositionControl).LoadComposedCore(operationOwner, staged.Directory, staged.PackageID, staged.ExpansionDirectory, staged.PayloadPath, *staged.Composition)
		r.noteDispatch("load_composed_core", callErr == nil)
	} else if machine != nil {
		response, callErr = r.control.(protocol2MachineLibraryControl).LoadLibraryMachineCore(operationOwner, staged.Directory, staged.PackageID, CoreDataRoot, machine.GameID, disk, nil, floppies)
		r.noteDispatch("load_library_core", callErr == nil)
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
					r.retainRetired(staged)
					cleanupStaged = false
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
		if disk != nil && response.Error != nil && response.Error.Code == "corrupt_data" && response.Error.Phase == "identity" {
			// The ATA disk's base check: the staged image no longer matches
			// its digest. A CRC the core does not match is phase input; the
			// file can be right then, so it stays.
			r.removeCorruptStagedDisk(disk)
			apiErr.Message = protocol.StagedDiskCorruptMessage
		}
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
		if !response.OK && response.State == "reboot_required" && response.ActivePackage != nil && validProtocol2Response(response) {
			// A validated recovery reply keeps a physical owner. Retain this
			// attempted load's sources until confirmed Stop, including a mismatched
			// identity that cannot prove these bytes unused. Restart adoption still
			// requires the exact package, descriptor and ROM/composition identities.
			r.retainRetired(staged)
			cleanupStaged = false
			attempted = true
			preserveInput = false
			apiErr.Phase = "recovery"
		}
		return CoreActivation{}, attempted, apiErr
	}
	if response.State != "running_development" || response.Execution != "development" ||
		response.ActivePackage == nil || response.ActivePackage.PackageID != staged.PackageID ||
		!reflect.DeepEqual(response.ActivePackage.Descriptor, staged.Descriptor) ||
		!reflect.DeepEqual(response.ActivePackage.StaticComposition, staged.StaticComposition) ||
		!reflect.DeepEqual(response.ActivePackage.Composition, staged.Composition) ||
		!reflect.DeepEqual(response.ActivePackage.SlotComposition, staged.SlotComposition) ||
		!reflect.DeepEqual(response.ActivePackage.PartsComposition, staged.PartsComposition) ||
		!reflect.DeepEqual(response.ActivePackage.ROMLink, staged.ROMLink) ||
		!reflect.DeepEqual(response.ActivePackage.ROMLinks, staged.ROMLinks) ||
		response.Generation == nil || *response.Generation == 0 || (!initialMediaMatches(response, staged.InitialMedia) || !initialMediaGenerationMatches(response, before, staged.InitialMedia)) {
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
		MediaUnits:       cloneMediaUnits(response.Capabilities.MediaUnits),
		ActiveInterfaces: append([]Protocol2Interface(nil), response.Capabilities.ActiveInterfaces...)}
	if response.ActivePackage != nil {
		activation.StaticComposition = cloneStaticComposition(response.ActivePackage.StaticComposition)
		activation.PartsComposition = clonePartsComposition(response.ActivePackage.PartsComposition)
		activation.Composition = cloneComposition(response.ActivePackage.Composition)
		activation.SlotComposition = cloneSlotComposition(response.ActivePackage.SlotComposition)
		activation.ROMLink = cloneROMLink(response.ActivePackage.ROMLink)
		activation.ROMLinks = cloneROMLinks(response.ActivePackage.ROMLinks)
	}
	if response.ActivePackage != nil && response.ActivePackage.PersistenceMode != "" {
		activation.PersistenceMode = response.ActivePackage.PersistenceMode
	}
	if response.Generation != nil {
		activation.Generation = *response.Generation
	}
	if response.OK && response.Error == nil && response.State == "running_development" && response.Execution == "development" && response.ActivePackage != nil && response.ActivePackage.PackageID == packageID && response.RuntimeInstance != nil && response.Generation != nil {
		identity := protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: *response.RuntimeInstance, PackageID: packageID, Generation: *response.Generation}
		if identity.Validate() == nil {
			activation.RuntimeInstance = identity.RuntimeInstance
		}
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
			response.ActivePackage.PackageID == staged.PackageID && reflect.DeepEqual(response.ActivePackage.Descriptor, staged.Descriptor) && reflect.DeepEqual(response.ActivePackage.StaticComposition, staged.StaticComposition) && reflect.DeepEqual(response.ActivePackage.Composition, staged.Composition) && reflect.DeepEqual(response.ActivePackage.SlotComposition, staged.SlotComposition) && reflect.DeepEqual(response.ActivePackage.PartsComposition, staged.PartsComposition) && reflect.DeepEqual(response.ActivePackage.ROMLink, staged.ROMLink) && reflect.DeepEqual(response.ActivePackage.ROMLinks, staged.ROMLinks) && response.Generation != nil &&
			!sameGeneration(before.Generation, response.Generation) && initialMediaMatches(response, staged.InitialMedia) && initialMediaGenerationMatches(response, &before, staged.InitialMedia) {
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
		left.ActivePackage.PersistenceMode == right.ActivePackage.PersistenceMode &&
		reflect.DeepEqual(left.ActivePackage.Descriptor, right.ActivePackage.Descriptor) &&
		reflect.DeepEqual(left.ActivePackage.Observed, right.ActivePackage.Observed) &&
		reflect.DeepEqual(left.ActivePackage.StaticComposition, right.ActivePackage.StaticComposition) &&
		reflect.DeepEqual(left.ActivePackage.Composition, right.ActivePackage.Composition) &&
		reflect.DeepEqual(left.ActivePackage.SlotComposition, right.ActivePackage.SlotComposition) && reflect.DeepEqual(left.ActivePackage.PartsComposition, right.ActivePackage.PartsComposition) &&
		reflect.DeepEqual(left.ActivePackage.ROMLink, right.ActivePackage.ROMLink) &&
		reflect.DeepEqual(left.ActivePackage.ROMLinks, right.ActivePackage.ROMLinks)
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

func slotExpansionPaths(staged corepackage.Staged) []SlotExpansionPath {
	paths := make([]SlotExpansionPath, 0, len(staged.SlotDirectories))
	for _, directory := range staged.SlotDirectories {
		paths = append(paths, SlotExpansionPath{Slot: directory.Slot, Path: directory.Directory})
	}
	return paths
}

func cloneSlotComposition(value *expansion.SlotComposition) *expansion.SlotComposition {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Expansions = append([]expansion.SlotExpansion(nil), value.Expansions...)
	return &copy
}

func cloneMediaUnits(value []protocol.MediaUnitStatus) []protocol.MediaUnitStatus {
	if len(value) == 0 {
		return nil
	}
	return protocol.CloneMediaUnits(value)
}

func cloneComposition(value *expansion.Composition) *expansion.Composition {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneROMLink(value *corepackage.ROMLinkIdentity) *corepackage.ROMLinkIdentity {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneROMLinks(value *corepackage.ROMLinksIdentity) *corepackage.ROMLinksIdentity {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Sources = append([]corepackage.ROMSourceIdentity(nil), value.Sources...)
	return &copy
}
