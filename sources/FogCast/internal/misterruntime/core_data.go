package misterruntime

import (
	"context"
	"encoding/json"
	"io"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

const CoreDataRoot = "/media/fat/fogcast/core-data"

func validateCoreDataShape(raw json.RawMessage) error {
	common := []string{"package_id", "core_id", "layout", "mode", "revision"}
	fields, err := exactRawObject(raw, common, []string{"paddle_speed", "best_rally", "game_id", "machine"})
	if err != nil {
		return err
	}
	if err = requireRawKinds(fields, map[string]rawKind{"package_id": rawString, "core_id": rawString, "layout": rawNullableObject, "mode": rawString, "revision": rawString}); err != nil {
		return err
	}
	if fields["game_id"] != nil || fields["machine"] != nil {
		required := append(append([]string{}, common...), "game_id", "machine")
		fields, err = exactRawObject(raw, required, nil)
		if err != nil {
			return err
		}
		if err = requireRawKinds(fields, map[string]rawKind{"game_id": rawString, "machine": rawObject}); err != nil {
			return err
		}
		machine, machineErr := exactRawObject(fields["machine"], []string{"preset", "frequency", "code_profile", "turbo", "sb_base", "sb_irq", "sb_dma"}, nil)
		if machineErr != nil {
			return machineErr
		}
		if machineErr = requireRawKinds(machine, map[string]rawKind{
			"preset": rawUnsigned, "frequency": rawUnsigned, "code_profile": rawUnsigned, "turbo": rawUnsigned,
			"sb_base": rawUnsigned, "sb_irq": rawUnsigned, "sb_dma": rawUnsigned,
		}); machineErr != nil {
			return machineErr
		}
	} else {
		required := append(append([]string{}, common...), "paddle_speed", "best_rally")
		fields, err = exactRawObject(raw, required, nil)
		if err != nil {
			return err
		}
		if err = requireRawKinds(fields, map[string]rawKind{"paddle_speed": rawUnsigned, "best_rally": rawUnsigned}); err != nil {
			return err
		}
	}
	if !isNull(fields["layout"]) {
		return validateSupportedInterfaceShape(fields["layout"])
	}
	return nil
}

type coreDataRequest struct {
	Protocol         int                   `json:"protocol"`
	Operation        string                `json:"operation"`
	PackagePath      string                `json:"package_path"`
	PackageID        string                `json:"package_id"`
	DataRoot         string                `json:"data_root"`
	ExpectedRevision string                `json:"expected_revision,omitempty"`
	PaddleSpeed      *protocol.PaddleSpeed `json:"paddle_speed,omitempty"`
	// A pc386 record is keyed by its library game; its update carries the
	// machine settings in place of paddle_speed.
	GameID  string                    `json:"game_id,omitempty"`
	Machine *protocol.MachineSettings `json:"machine,omitempty"`
}

func (client *Client) InspectCoreData(ctx context.Context, path, id, root string) (Protocol2Response, error) {
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "inspect_core_data", PackagePath: path, PackageID: id, DataRoot: root})
}
func (client *Client) UpdateCoreSettings(ctx context.Context, path, id, root, revision string, speed protocol.PaddleSpeed) (Protocol2Response, error) {
	if !protocol.ValidCoreDataRevision(revision) || !speed.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "update_core_settings", PackagePath: path, PackageID: id, DataRoot: root, ExpectedRevision: revision, PaddleSpeed: &speed})
}
func (client *Client) coreData(ctx context.Context, req coreDataRequest) (Protocol2Response, error) {
	if !validRuntimePath(req.PackagePath) || !validRuntimePath(req.DataRoot) || !protocol2Hex64.MatchString(req.PackageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := client.callRawTracked(ctx, req) // Never replay a possibly applied settings mutation.
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.OK && (response.CoreData == nil || response.CoreData.PackageID != req.PackageID) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}
func (client *Client) LoadLibraryCore(ctx context.Context, path, id, root string) (Protocol2Response, error) {
	return client.loadCoreOperation(ctx, path, id, root)
}

type protocol2DataControl interface {
	InspectCoreData(context.Context, string, string, string) (Protocol2Response, error)
	UpdateCoreSettings(context.Context, string, string, string, string, protocol.PaddleSpeed) (Protocol2Response, error)
}
type protocol2LibraryControl interface {
	LoadLibraryCore(context.Context, string, string, string) (Protocol2Response, error)
}

func (r *Runtime) InspectCoreData(ctx context.Context, size int64, body io.Reader, id string) (protocol.CoreDataInspection, *protocol.APIError) {
	return r.coreDataOperation(ctx, size, body, id, "", nil)
}

// InspectGameCoreData reads a pc386 library game's machine record.
func (r *Runtime) InspectGameCoreData(ctx context.Context, size int64, body io.Reader, id, gameID string) (protocol.CoreDataInspection, *protocol.APIError) {
	if !protocol.ValidMediaGameID(gameID) {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "library game is invalid", Phase: "request"}
	}
	return r.coreDataOperation(ctx, size, body, id, gameID, nil)
}
func (r *Runtime) UpdateCoreSettings(ctx context.Context, size int64, body io.Reader, update protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	if !update.Valid() {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core settings request is invalid", Phase: "request"}
	}
	return r.coreDataOperation(ctx, size, body, update.ExpectedPackageID, update.GameID, &update)
}
func (r *Runtime) coreDataOperation(ctx context.Context, size int64, body io.Reader, id, gameID string, update *protocol.CoreSettingsUpdate) (result protocol.CoreDataInspection, apiErr *protocol.APIError) {
	control, ok := r.control.(protocol2DataControl)
	if !ok || r.corePackageRoot == "" {
		return result, unsupportedOperationError()
	}
	if !protocol2Hex64.MatchString(id) {
		return result, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package identity is invalid", Phase: "request"}
	}
	staged, err := corepackage.Stage(ctx, r.corePackageRoot, size, body)
	if err != nil {
		return result, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	defer func() {
		if err := staged.Cleanup(); err != nil {
			r.retainRetired(staged)
			result = protocol.CoreDataInspection{}
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "staged core package could not be cleaned up", Phase: "recovery"}
		}
	}()
	if staged.PackageID != id {
		return result, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "package identity differs from request", Phase: "admission"}
	}
	// A pc386 record is per library game; Pong's is per core.
	machine := protocol.DeclaresPc386Machine(staged.Descriptor)
	if machine != (gameID != "") || (update != nil && machine != (update.Machine != nil)) {
		return result, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core data request does not match the package's record", Phase: "request"}
	}
	var response Protocol2Response
	switch {
	case machine:
		games, ok := r.control.(protocol2MachineDataControl)
		if !ok {
			return result, unsupportedOperationError()
		}
		if update == nil {
			response, err = games.InspectMachineCoreData(ctx, staged.Directory, id, CoreDataRoot, gameID)
		} else {
			response, err = games.UpdateMachineSettings(ctx, staged.Directory, id, CoreDataRoot, gameID, update.ExpectedRevision, *update.Machine)
		}
	case update == nil:
		response, err = control.InspectCoreData(ctx, staged.Directory, id, CoreDataRoot)
	default:
		response, err = control.UpdateCoreSettings(ctx, staged.Directory, id, CoreDataRoot, update.ExpectedRevision, update.PaddleSpeed)
	}
	if err != nil {
		return result, unavailableError()
	}
	if !response.OK || response.Error != nil {
		return result, mapCoreDataError(response.Error)
	}
	if response.CoreData == nil {
		return result, unavailableError()
	}
	result = protocol.CoreDataInspection{CoreData: *response.CoreData, Descriptor: staged.Descriptor}
	if result.PackageID != id || !result.Valid() || result.GameID != gameID || (update != nil && !update.Applied(result.CoreData)) {
		return protocol.CoreDataInspection{}, unavailableError()
	}
	return result, nil
}
func (r *Runtime) LoadLibraryCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package identity is invalid", Phase: "request"}
	}
	return r.loadCoreOwned(admission, observation, owner, size, body, id, nil)
}

// LoadLibraryMachineCoreOwned launches a pc386 library package with its game
// and its staged disk, if bound.
func (r *Runtime) LoadLibraryMachineCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string, machine protocol.LibraryMachineBinding) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) || !protocol.ValidMediaGameID(machine.GameID) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "library machine launch is invalid", Phase: "request"}
	}
	return r.loadCoreOwned(admission, observation, owner, size, body, id, &machine)
}

func mapCoreDataError(remote *Protocol2Error) *protocol.APIError {
	result := mapProtocol2Error(remote)
	result.Expected = ""
	result.Observed = ""
	switch result.Code {
	case protocol.CodeMiSTerUnavailable:
		result.Message = unavailableMessage
	case protocol.CodeBadRequest:
		result.Message = "core data request is invalid"
	case protocol.CodeInvalidArchive:
		result.Message = "core package is invalid"
	case protocol.CodeBusy:
		result.Message = "target core data is busy"
	case protocol.CodeUnsupportedOperation:
		result.Message = unsupportedOperationMessage
	}
	return result
}

// Protocol2RecoverIdle asks the runtime to program idle again after
// reboot_required. It does not reboot the board.
func (client *Client) Protocol2RecoverIdle(ctx context.Context) (Protocol2Response, error) {
	line, _, err := client.callRawTracked(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{2, "recover_idle"})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		if rejection, ok := legacyRecoverIdleRejection(line); ok {
			return rejection, nil
		}
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || response.CoreData != nil {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

// Protocol2Stop preserves described-package identity on persistence recovery.
func (client *Client) Protocol2Stop(ctx context.Context) (Protocol2Response, error) {
	line, _, err := client.callRawTracked(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{2, "stop"})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || response.CoreData != nil {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

type protocol2StopControl interface {
	Protocol2Stop(context.Context) (Protocol2Response, error)
}

func (r *Runtime) stopCorePackage(ctx context.Context, control protocol2StopControl) (string, string, *protocol.APIError) {
	response, err := control.Protocol2Stop(ctx)
	r.noteDispatch("stop", err == nil)
	if err != nil {
		// A lost Stop is observed once; it is never dispatched again.
		if ctx.Err() != nil {
			return "", "", unavailableError()
		}
		response, err = r.boundedStatus(ctx)
		if err != nil {
			return "", "", unavailableError()
		}
	}
	if response.State == "reboot_required" {
		if response.ActivePackage == nil {
			return optionalProtocol2Core(response), protocol.RecoveryRebootRequired, nil
		}
		return optionalProtocol2Core(response), protocol.RecoveryRebootRequired, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core data recovery is required", Phase: "recovery"}
	}
	if response.Error != nil || !response.OK {
		return optionalProtocol2Core(response), "", mapProtocol2Error(response.Error)
	}
	if response.State != "idle" || response.Execution != "none" || response.Core != nil || response.System != nil || response.ActivePackage != nil || response.Generation != nil {
		return "", "", unavailableError()
	}
	if apiErr := r.cleanupCorePackages(); apiErr != nil {
		return "", "", apiErr
	}
	return "", "", nil
}
func optionalProtocol2Core(response Protocol2Response) string {
	if response.Core != nil {
		return *response.Core
	}
	return ""
}
