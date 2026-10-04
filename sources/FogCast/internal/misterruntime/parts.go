package misterruntime

import (
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type PartPath struct {
	Role string `json:"role"`
	Path string `json:"path"`
}

func validPartsComposition(c expansion.PartsComposition, id string) bool {
	calculated, err := expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	if err != nil || calculated != c.ID || c.PackageID != id || !protocol2Hex64.MatchString(c.ShellSHA256) || c.PayloadSize < 40408 || c.PayloadSize > corepackage.MaxPayloadSize {
		return false
	}
	for i, p := range c.Parts {
		if i > 0 && c.Parts[i-1].Role >= p.Role {
			return false
		}
	}
	return true
}

func (client *Client) partsCore(ctx context.Context, operation, path, id, root, payload string, parts []PartPath, c expansion.PartsComposition) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(payload) || !validPartsComposition(c, id) || len(parts) != len(c.Parts) || (operation == "load_parts_library_core" && !validRuntimePath(root)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	for i, p := range parts {
		if p.Role != c.Parts[i].Role || !validRuntimePath(p.Path) {
			return Protocol2Response{}, errInvalidRuntimeRequest
		}
	}
	mutation := operation == "load_parts_core" || operation == "load_parts_library_core"
	if mutation {
		if _, err := client.Protocol2Status(ctx); err != nil {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
		}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol    int                        `json:"protocol"`
		Operation   string                     `json:"operation"`
		PackagePath string                     `json:"package_path"`
		PackageID   string                     `json:"package_id"`
		Parts       []PartPath                 `json:"parts"`
		PayloadPath string                     `json:"payload_path"`
		Composition expansion.PartsComposition `json:"composition"`
		DataRoot    string                     `json:"data_root,omitempty"`
	}{2, operation, path, id, parts, payload, c, root})
	if err != nil {
		if mutation {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
		}
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && response.OK {
		if mutation {
			if response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.ActivePackage.PackageID != id || !reflect.DeepEqual(response.ActivePackage.PartsComposition, &c) || response.InspectedPackage != nil {
				err = errInvalidRuntimeResponse
			}
		} else if response.InspectedPackage == nil || response.InspectedPackage.PackageID != id {
			err = errInvalidRuntimeResponse
		}
	}
	if err != nil && mutation {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
	}
	return response, err
}

func (client *Client) LoadPartsCore(ctx context.Context, path, id, payload string, parts []PartPath, c expansion.PartsComposition) (Protocol2Response, error) {
	return client.partsCore(ctx, "load_parts_core", path, id, "", payload, parts, c)
}
func (client *Client) LoadLibraryPartsCore(ctx context.Context, path, id, root, payload string, parts []PartPath, c expansion.PartsComposition) (Protocol2Response, error) {
	return client.partsCore(ctx, "load_parts_library_core", path, id, root, payload, parts, c)
}
func (client *Client) InspectPartsCore(ctx context.Context, path, id, payload string, parts []PartPath, c expansion.PartsComposition) (Protocol2Response, error) {
	return client.partsCore(ctx, "inspect_parts_core", path, id, "", payload, parts, c)
}

type protocol2PartsControl interface {
	LoadPartsCore(context.Context, string, string, string, []PartPath, expansion.PartsComposition) (Protocol2Response, error)
	InspectPartsCore(context.Context, string, string, string, []PartPath, expansion.PartsComposition) (Protocol2Response, error)
}
type protocol2LibraryPartsControl interface {
	LoadLibraryPartsCore(context.Context, string, string, string, string, []PartPath, expansion.PartsComposition) (Protocol2Response, error)
}

func partPaths(s corepackage.Staged) []PartPath {
	result := make([]PartPath, len(s.PartDirectories))
	for i, p := range s.PartDirectories {
		result[i] = PartPath{p.Role, p.Directory}
	}
	return result
}

func (r *Runtime) LoadPartsCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader) (CoreActivation, bool, *protocol.APIError) {
	return r.loadCoreOwnedMode(admission, observation, owner, size, body, "", true, true)
}
func (r *Runtime) LoadLibraryPartsCoreOwned(admission, observation, owner context.Context, size int64, body io.Reader, id string) (CoreActivation, bool, *protocol.APIError) {
	if !protocol2Hex64.MatchString(id) {
		return CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid package identity", Phase: "admission"}
	}
	return r.loadCoreOwnedMode(admission, observation, owner, size, body, id, true, true)
}

func (r *Runtime) InspectPartsCore(ctx context.Context, size int64, body io.Reader) (result protocol.PartsInspection, apiErr *protocol.APIError) {
	control, ok := r.control.(protocol2PartsControl)
	if !ok || r.corePackageRoot == "" {
		return result, unsupportedOperationError()
	}
	staged, err := corepackage.StageParts(ctx, r.corePackageRoot, size, body)
	if err != nil {
		return result, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "developer parts are invalid", Phase: "admission"}
	}
	defer func() {
		if err := staged.Cleanup(); err != nil {
			r.retainRetired(staged)
			result = protocol.PartsInspection{}
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "staged developer parts could not be cleaned up", Phase: "recovery"}
		}
	}()
	response, err := control.InspectPartsCore(ctx, staged.Directory, staged.PackageID, staged.PayloadPath, partPaths(staged), *staged.PartsComposition)
	if err != nil {
		if errors.Is(err, errProtocol2Unsupported) {
			return result, unsupportedOperationError()
		}
		return result, unavailableError()
	}
	if !response.OK || response.Error != nil {
		return result, mapProtocol2Error(response.Error)
	}
	inspection := response.InspectedPackage
	if inspection == nil || inspection.PackageID != staged.PackageID || !reflect.DeepEqual(inspection.Descriptor, staged.Descriptor) || !inspection.Compatible || inspection.CompatibilityError != nil {
		return result, unavailableError()
	}
	result = protocol.PartsInspection{Core: protocol.CoreInspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor, Compatible: true, PersistenceLayout: inspection.PersistenceLayout}, Composition: *clonePartsComposition(staged.PartsComposition), PersistenceMode: "volatile"}
	return result, nil
}

func clonePartsComposition(c *expansion.PartsComposition) *expansion.PartsComposition {
	if c == nil {
		return nil
	}
	copy := *c
	copy.Parts = append([]expansion.PartSelection(nil), c.Parts...)
	return &copy
}

// ROM parts reuse the existing composed-ROM operation and its separate source
// receipt; the parts tuple describes the retained pre-ROM overlay.
type protocol2ROMPartsCompositionControl interface {
	LoadROMPartsComposedCore(context.Context, string, string, []PartPath, string, expansion.PartsComposition, string, corepackage.ROMLinkIdentity) (Protocol2Response, error)
}

func (client *Client) LoadROMPartsComposedCore(ctx context.Context, path, id string, parts []PartPath, payload string, c expansion.PartsComposition, programmed string, link corepackage.ROMLinkIdentity) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(payload) || !validRuntimePath(programmed) || !validPartsComposition(c, id) || len(parts) != len(c.Parts) || c.Layout != expansion.AtariStVideoLayout || !protocol2Identifier.MatchString(link.ROMID) || !link.ValidFor(corepackage.Descriptor{Format: 3, ROM: &corepackage.ROM{ID: link.ROMID, SHA256: link.MapSHA256, SourceSize: link.SourceSize}}) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	for i, p := range parts {
		if p.Role != c.Parts[i].Role || !validRuntimePath(p.Path) {
			return Protocol2Response{}, errInvalidRuntimeRequest
		}
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	if before.Capabilities.ROMLinking != 1 {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol       int                         `json:"protocol"`
		Operation      string                      `json:"operation"`
		PackagePath    string                      `json:"package_path"`
		PackageID      string                      `json:"package_id"`
		Parts          []PartPath                  `json:"parts"`
		PayloadPath    string                      `json:"payload_path"`
		Composition    expansion.PartsComposition  `json:"composition"`
		ProgrammedPath string                      `json:"programmed_path"`
		ROMLink        corepackage.ROMLinkIdentity `json:"rom_link"`
	}{2, "load_rom_composed_core", path, id, parts, payload, c, programmed, link})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && response.OK {
		if response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.ActivePackage.PackageID != id || !reflect.DeepEqual(response.ActivePackage.PartsComposition, &c) || !reflect.DeepEqual(response.ActivePackage.ROMLink, &link) || response.ActivePackage.Composition != nil || response.ActivePackage.SlotComposition != nil || response.InspectedPackage != nil {
			err = errInvalidRuntimeResponse
		}
	}
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
	}
	return response, nil
}
