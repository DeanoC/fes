package misterruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) LoadFirmware(ctx context.Context, path string) (Protocol2Response, error) {
	if !validRuntimePath(path) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}{2, "load_firmware", path})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
}

type protocol2FirmwareControl interface {
	protocol2StatusControl
	LoadFirmware(context.Context, string) (Protocol2Response, error)
}

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

type protocol2MediaStreamControl interface {
	LoadMediaStream(context.Context, string, string, uint64, uint32) (Protocol2Response, error)
}

func (c *Client) LoadMediaStream(ctx context.Context, path, packageID string, generation uint64, size uint32) (Protocol2Response, error) {
	if !validRuntimePath(path) || protocol.ValidateDigest(packageID) != nil || generation == 0 || size < 1 || int64(size) > protocol.MaxDeclaredMediaStreamBytes {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		Path               string `json:"path"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		Size               uint32 `json:"size"`
	}{2, "load_media_stream", path, packageID, generation, size})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
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
	return r.LoadDevelopmentMediaOwned(ctx, ctx, size, body, b)
}

func (r *Runtime) LoadDevelopmentMediaOwned(ctx, owner context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (apiErr *protocol.APIError) {
	if !b.Valid() {
		return protocol.DevelopmentMediaRequestError()
	}
	if b.Role == protocol.FirmwareRole && size != protocol.FirmwareBytes {
		return protocol.DevelopmentFirmwareRequestError()
	}
	if b.Stream {
		if size < 1 || size > protocol.MaxDeclaredMediaStreamBytes || body == nil {
			return protocol.DevelopmentMediaRequestError()
		}
	} else {
		data, err := protocol.ReadDevelopmentMedia(size, body)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	if ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media upload cancelled", Phase: "admission", Cause: ctx.Err()}
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
	if b.Stream && (!before.Capabilities.MediaStream.Valid() || size < int64(before.Capabilities.MediaStream.MinBytes) || size > int64(before.Capabilities.MediaStream.MaxBytes)) {
		return protocol.DevelopmentMediaRequestError()
	}
	streamControl, streamOK := r.control.(protocol2MediaStreamControl)
	if b.Stream && !streamOK {
		return unsupportedOperationError()
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
			var cause error = err
			if apiErr != nil {
				cause = errors.Join(apiErr, err)
			}
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "development media staging cleanup failed", Phase: "recovery", Cause: cause}
		}
	}()
	path := filepath.Join(dir, "media.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return unavailableError()
	}
	writeErr := stageMediaStream(ctx, file, body, size)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media staging failed", Phase: "admission", Cause: errors.Join(writeErr, closeErr, ctx.Err())}
	}
	// Recheck after staging, immediately before the one local mutation.
	before, err = control.Protocol2Status(ctx)
	if err != nil {
		return unavailableError()
	}
	if !mediaResponseMatches(before, b) {
		return protocol.DevelopmentMediaIdentityError()
	}
	if b.Stream && (!before.Capabilities.MediaStream.Valid() || size < int64(before.Capabilities.MediaStream.MinBytes) || size > int64(before.Capabilities.MediaStream.MaxBytes)) {
		return protocol.DevelopmentMediaRequestError()
	}
	if ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media upload cancelled", Phase: "admission", Cause: ctx.Err()}
	}
	var response Protocol2Response
	operation := "load_media"
	if b.Role == protocol.FirmwareRole {
		firmware, ok := r.control.(protocol2FirmwareControl)
		if !ok {
			return unsupportedOperationError()
		}
		if size != protocol.FirmwareBytes {
			return protocol.DevelopmentFirmwareRequestError()
		}
		operation = "load_firmware"
		response, err = firmware.LoadFirmware(ctx, path)
	} else if b.Stream {
		var cancel context.CancelFunc
		// The daemon media worker owns up to 120s; leave time for its reply
		// and recovery before releasing this adapter's lifecycle admission.
		ctx, cancel = context.WithTimeout(owner, 135*time.Second)
		defer cancel()
		operation = "load_media_stream"
		response, err = streamControl.LoadMediaStream(ctx, path, b.PackageID, b.Generation, uint32(size))
	} else {
		response, err = control.LoadMedia(ctx, path)
	}
	r.noteDispatch(operation, err == nil)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media delivery is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return mapProtocol2Error(response.Error)
	}
	if !mediaResponseMatches(response, b) {
		return unavailableError()
	}
	if b.Role == protocol.FirmwareRole {
		return nil
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

func stageMediaStream(ctx context.Context, dst io.Writer, src io.Reader, size int64) error {
	buf := make([]byte, 32<<10)
	remaining := size
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := src.Read(buf[:min(int64(len(buf)), remaining)])
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			remaining -= int64(n)
		}
		if err != nil {
			if err == io.EOF && remaining == 0 {
				break
			}
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var extra [1]byte
	n, err := src.Read(extra[:])
	if err != nil && err != io.EOF {
		return err
	}
	if n != 0 || err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return ctx.Err()
}
