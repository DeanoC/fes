package misterruntime

import (
	"context"
	"encoding/json"
	"io"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

const CoreDataRoot = "/media/fat/fogcast/core-data"

type ataDiskLoadRequest struct {
	Path      string
	DataRoot  string
	Selection protocol.AtaDiskSelection
}

func (d *ataDiskLoadRequest) valid() bool {
	return d != nil && validRuntimePath(d.Path) && d.DataRoot == MediaDataRoot && d.Selection.Valid()
}

func validateCoreDataShape(raw json.RawMessage) error {
	fields, err := exactRawObject(raw, []string{"package_id", "core_id", "layout", "mode", "revision"}, []string{"paddle_speed", "best_rally", "game_id", "machine"})
	if err != nil {
		return err
	}
	if err = requireRawKinds(fields, map[string]rawKind{"package_id": rawString, "core_id": rawString, "layout": rawNullableObject, "mode": rawString, "revision": rawString}); err != nil {
		return err
	}
	_, hasGameID := fields["game_id"]
	_, hasMachine := fields["machine"]
	if hasGameID || hasMachine {
		if !hasGameID || !hasMachine || fields["paddle_speed"] != nil || fields["best_rally"] != nil {
			return errInvalidRuntimeResponse
		}
		if err = requireRawKinds(fields, map[string]rawKind{"game_id": rawString, "machine": rawObject}); err != nil {
			return err
		}
		machine, err := exactRawObject(fields["machine"], []string{"preset", "frequency", "code_profile", "turbo", "sb_base", "sb_irq", "sb_dma"}, nil)
		if err != nil || requireRawKinds(machine, map[string]rawKind{"preset": rawUnsigned, "frequency": rawUnsigned, "code_profile": rawUnsigned, "turbo": rawUnsigned, "sb_base": rawUnsigned, "sb_irq": rawUnsigned, "sb_dma": rawUnsigned}) != nil {
			return errInvalidRuntimeResponse
		}
	} else if err = requireRawKinds(fields, map[string]rawKind{"paddle_speed": rawUnsigned, "best_rally": rawUnsigned}); err != nil {
		return err
	}
	if !isNull(fields["layout"]) {
		return validateSupportedInterfaceShape(fields["layout"])
	}
	return nil
}

type coreDataRequest struct {
	Protocol         int                            `json:"protocol"`
	Operation        string                         `json:"operation"`
	PackagePath      string                         `json:"package_path"`
	PackageID        string                         `json:"package_id"`
	DataRoot         string                         `json:"data_root"`
	GameID           string                         `json:"game_id,omitempty"`
	ExpectedRevision string                         `json:"expected_revision,omitempty"`
	PaddleSpeed      *protocol.PaddleSpeed          `json:"paddle_speed,omitempty"`
	Machine          *protocol.PC386MachineSettings `json:"machine,omitempty"`
}

func (client *Client) InspectCoreData(ctx context.Context, path, id, root, gameID string) (Protocol2Response, error) {
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "inspect_core_data", PackagePath: path, PackageID: id, DataRoot: root, GameID: gameID})
}
func (client *Client) UpdateCoreSettings(ctx context.Context, path, id, root string, update protocol.CoreSettingsUpdate) (Protocol2Response, error) {
	if !update.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	req := coreDataRequest{Protocol: 2, Operation: "update_core_settings", PackagePath: path, PackageID: id, DataRoot: root, GameID: update.GameID, ExpectedRevision: update.ExpectedRevision, Machine: update.Machine}
	if update.Machine == nil {
		speed := update.PaddleSpeed
		req.PaddleSpeed = &speed
	}
	return client.coreData(ctx, req)
}
func (client *Client) coreData(ctx context.Context, req coreDataRequest) (Protocol2Response, error) {
	if !validRuntimePath(req.PackagePath) || !validRuntimePath(req.DataRoot) || !protocol2Hex64.MatchString(req.PackageID) ||
		(req.GameID != "" && protocol.ValidateGameID(req.GameID) != nil) ||
		(req.Operation == "update_core_settings" && ((req.Machine == nil) == (req.PaddleSpeed == nil))) {
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
func (client *Client) LoadLibraryCore(ctx context.Context, path, id, root, gameID string) (Protocol2Response, error) {
	return client.loadCoreOperation(ctx, path, id, root, gameID)
}
func (client *Client) LoadLibraryCoreWithAtaDisk(ctx context.Context, path, id, root, gameID string,
	disk ataDiskLoadRequest) (Protocol2Response, error) {
	if !disk.valid() || protocol.ValidateGameID(gameID) != nil {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.loadCoreOperationWithAtaDisk(ctx, path, id, root, gameID, disk)
}

type protocol2DataControl interface {
	InspectCoreData(context.Context, string, string, string, string) (Protocol2Response, error)
	UpdateCoreSettings(context.Context, string, string, string, protocol.CoreSettingsUpdate) (Protocol2Response, error)
}
type protocol2LibraryControl interface {
	LoadLibraryCore(context.Context, string, string, string, string) (Protocol2Response, error)
}
type protocol2LibraryAtaControl interface {
	LoadLibraryCoreWithAtaDisk(context.Context, string, string, string, string, ataDiskLoadRequest) (Protocol2Response, error)
}

func (r *Runtime) InspectCoreData(ctx context.Context, size int64, body io.Reader, id, gameID string) (protocol.CoreDataInspection, *protocol.APIError) {
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
	dataGameID := ""
	if pc386MachineRecordPackage(staged.Descriptor) {
		if protocol.ValidateGameID(gameID) != nil || (update != nil && update.Machine == nil) {
			return result, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core data game identity or machine settings are invalid", Phase: "request"}
		}
		dataGameID = gameID
	} else if update != nil && update.Machine != nil {
		return result, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "machine settings do not match the package layout", Phase: "request"}
	}
	var response Protocol2Response
	if update == nil {
		response, err = control.InspectCoreData(ctx, staged.Directory, id, CoreDataRoot, dataGameID)
	} else {
		runtimeUpdate := *update
		runtimeUpdate.GameID = dataGameID
		response, err = control.UpdateCoreSettings(ctx, staged.Directory, id, CoreDataRoot, runtimeUpdate)
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
	if result.PackageID != id || !result.Valid() || result.GameID != dataGameID || (update != nil && (result.Mode != "persistent" || result.Revision == "absent" ||
		(update.Machine != nil && (result.Machine == nil || *result.Machine != *update.Machine)) ||
		(update.Machine == nil && result.PaddleSpeed != update.PaddleSpeed))) {
		return protocol.CoreDataInspection{}, unavailableError()
	}
	return result, nil
}
func pc386MachineRecordPackage(descriptor corepackage.Descriptor) bool {
	if !protocol.ComputerMediaABI(descriptor.ABI.ID, descriptor.ABI.Major, descriptor.ABI.Minor) {
		return false
	}
	for _, contract := range descriptor.Interfaces {
		if contract.ID == "fes.pc386.machine" && contract.Major == 1 && contract.Minor == 0 {
			return true
		}
	}
	return false
}

func (r *Runtime) LoadLibraryCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id, gameID string) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package identity is invalid", Phase: "request"}
	}
	return r.loadCoreOwned(admission, observation, owner, size, body, id, gameID)
}

func (r *Runtime) LoadLibraryCoreOwnedWithAtaDisk(admission, observation, owner context.Context,
	size int64, body io.Reader, id, gameID, path string, selection protocol.AtaDiskSelection) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) || protocol.ValidateGameID(gameID) != nil ||
		!validRuntimePath(path) || !selection.Valid() {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest,
			Message: "library ATA disk request is invalid", Phase: "request"}
	}
	disk := &ataDiskLoadRequest{Path: path, DataRoot: MediaDataRoot, Selection: selection}
	return r.loadCoreOwnedMode(admission, observation, owner, size, body, id, false, gameID, disk)
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
