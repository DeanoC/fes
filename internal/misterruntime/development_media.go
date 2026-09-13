package misterruntime

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) LoadMedia(ctx context.Context, path string) (Protocol2Response, error) {
	if !validRuntimePath(path) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}{2, "load_media", path})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
}

type protocol2MediaControl interface {
	protocol2StatusControl
	LoadMedia(context.Context, string) (Protocol2Response, error)
}

func mediaResponseMatches(response Protocol2Response, b protocol.DevelopmentMediaBinding) bool {
	if !response.OK || response.Error != nil || response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.Generation == nil {
		return false
	}
	p := response.ActivePackage
	activation := activationFromProtocol2(p.PackageID, p.Descriptor, response)
	return b.Matches(protocol.Status{State: protocol.StateActive, Development: true, CorePackage: corePackageStatus(activation)})
}

// LoadDevelopmentMedia runs under the target coordinator's existing lifecycle
// admission. Only this adapter supplies a local path; callers supply raw bytes.
func (r *Runtime) LoadDevelopmentMedia(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (apiErr *protocol.APIError) {
	if !b.Valid() {
		return protocol.DevelopmentMediaRequestError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(size, body)
	if apiErr != nil {
		return apiErr
	}
	if ctx.Err() != nil {
		return unavailableError()
	}
	control, ok := r.control.(protocol2MediaControl)
	if !ok {
		return unsupportedOperationError()
	}
	// Keyboard posters bypass coordinator admission. Keep them off the busy
	// runtime until transfer and restoration of held keys have completed.
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	before, err := control.Protocol2Status(ctx)
	if err != nil {
		return unavailableError()
	}
	if !mediaResponseMatches(before, b) {
		return protocol.DevelopmentMediaIdentityError()
	}
	root, err := filepath.Abs(os.TempDir())
	if err != nil {
		return unavailableError()
	}
	dir, err := os.MkdirTemp(root, "fogcast-development-media-")
	if err != nil {
		return unavailableError()
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "development media staging cleanup failed", Phase: "recovery"}
		}
	}()
	path := filepath.Join(dir, "media.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return unavailableError()
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || ctx.Err() != nil {
		return unavailableError()
	}
	// Recheck after staging, immediately before the one local mutation.
	before, err = control.Protocol2Status(ctx)
	if err != nil {
		return unavailableError()
	}
	if !mediaResponseMatches(before, b) {
		return protocol.DevelopmentMediaIdentityError()
	}
	response, err := control.LoadMedia(ctx, path)
	r.noteDispatch("load_media", err == nil)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media delivery is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return mapProtocol2Error(response.Error)
	}
	if !mediaResponseMatches(response, b) {
		return unavailableError()
	}
	for _, contract := range response.Capabilities.ActiveInterfaces {
		if contract.ID == "fes.keyboard" && contract.Major == 1 && contract.Minor == 0 {
			matrix := uint64(0xffffffffff)
			if r.keyboardPackageID == b.PackageID && r.keyboardGeneration == b.Generation {
				matrix = r.keyboardMatrix
			}
			if err := r.setKeyboardLocked(ctx, matrix); err != nil {
				return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media loaded but keyboard restoration failed", Phase: "input"}
			}
			break
		}
	}
	return nil
}
