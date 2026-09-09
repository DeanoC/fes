package misterruntime

import (
	"context"
	"encoding/json"
	"io"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

const CoreDataRoot = "/media/fat/fogcast/core-data"

func validateCoreDataShape(raw json.RawMessage) error {
	fields, err := exactRawObject(raw, []string{"package_id", "core_id", "layout", "mode", "revision", "paddle_speed", "best_rally"}, nil)
	if err != nil {
		return err
	}
	if err = requireRawKinds(fields, map[string]rawKind{"package_id": rawString, "core_id": rawString, "layout": rawNullableObject, "mode": rawString, "revision": rawString, "paddle_speed": rawUnsigned, "best_rally": rawUnsigned}); err != nil {
		return err
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
	return r.coreDataOperation(ctx, size, body, id, nil)
}
func (r *Runtime) UpdateCoreSettings(ctx context.Context, size int64, body io.Reader, update protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	if !update.Valid() {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core settings request is invalid", Phase: "request"}
	}
	return r.coreDataOperation(ctx, size, body, update.ExpectedPackageID, &update)
}
func (r *Runtime) coreDataOperation(ctx context.Context, size int64, body io.Reader, id string, update *protocol.CoreSettingsUpdate) (result protocol.CoreDataInspection, apiErr *protocol.APIError) {
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
	var response Protocol2Response
	if update == nil {
		response, err = control.InspectCoreData(ctx, staged.Directory, id, CoreDataRoot)
	} else {
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
	if result.PackageID != id || !result.Valid() || (update != nil && (result.Mode != "persistent" || result.Revision == "absent" || result.PaddleSpeed != update.PaddleSpeed)) {
		return protocol.CoreDataInspection{}, unavailableError()
	}
	return result, nil
}
func (r *Runtime) LoadLibraryCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package identity is invalid", Phase: "request"}
	}
	return r.loadCoreOwned(admission, observation, owner, size, body, id)
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
	if err != nil {
		// A lost Stop is observed once; it is never dispatched again.
		observer, ok := r.control.(protocol2StatusControl)
		if !ok || ctx.Err() != nil {
			return "", "", unavailableError()
		}
		response, err = observer.Protocol2Status(ctx)
		if err != nil {
			return "", "", unavailableError()
		}
	}
	if response.State == "reboot_required" && response.ActivePackage != nil {
		return optionalProtocol2Core(response), protocol.RecoveryRebootRequired, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core data recovery is required", Phase: "recovery"}
	}
	if response.Error != nil || !response.OK {
		return optionalProtocol2Core(response), "", mapProtocol2Error(response.Error)
	}
	if response.State != "idle" || response.ActivePackage != nil || response.Generation != nil {
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
