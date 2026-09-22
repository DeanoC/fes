package misterruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) ReplaceLiveMedia(ctx context.Context, path, packageID string, generation uint64) (Protocol2Response, error) {
	if !validRuntimePath(path) || protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		Path               string `json:"path"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
	}{2, "replace_live_media", path, packageID, generation})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
}

func (c *Client) ClearLiveMedia(ctx context.Context, packageID string, generation uint64) (Protocol2Response, error) {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
	}{2, "clear_media", packageID, generation})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
}

type protocol2LiveMediaControl interface {
	protocol2StatusControl
	ReplaceLiveMedia(context.Context, string, string, uint64) (Protocol2Response, error)
	ClearLiveMedia(context.Context, string, uint64) (Protocol2Response, error)
}

func liveMediaResponseMatches(response Protocol2Response, b protocol.DevelopmentMediaBinding) bool {
	if !response.OK || response.Error != nil || response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.Generation == nil {
		return false
	}
	p := response.ActivePackage
	activation := activationFromProtocol2(p.PackageID, p.Descriptor, response)
	return b.MatchesLive(protocol.Status{State: protocol.StateActive, Development: true, CorePackage: corePackageStatus(activation)})
}

// ReplaceLiveMedia stages caller bytes and fills the mailbox without hold-reset.
// It does not restore the keyboard matrix; mid-session begin leaves it alone.
func (r *Runtime) ReplaceLiveMedia(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (apiErr *protocol.APIError) {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.LiveMediaRequestError()
	}
	data, err := protocol.ReadDevelopmentMedia(size, body)
	if err != nil {
		return protocol.LiveMediaRequestError()
	}
	if ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "live media upload cancelled", Phase: "admission", Cause: ctx.Err()}
	}
	control, ok := r.control.(protocol2LiveMediaControl)
	if !ok {
		return unsupportedOperationError()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	before, statusErr := control.Protocol2Status(ctx)
	if statusErr != nil {
		return unavailableError()
	}
	if !liveMediaResponseMatches(before, b) {
		return protocol.LiveMediaIdentityError()
	}
	root, absErr := filepath.Abs(os.TempDir())
	if absErr != nil {
		return unavailableError()
	}
	dir, mkErr := os.MkdirTemp(root, "fogcast-live-media-")
	if mkErr != nil {
		return unavailableError()
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			var cause error = removeErr
			if apiErr != nil {
				cause = errors.Join(apiErr, removeErr)
			}
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "live media staging cleanup failed", Phase: "recovery", Cause: cause}
		}
	}()
	path := filepath.Join(dir, "media.bin")
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if openErr != nil {
		return unavailableError()
	}
	writeErr := stageMediaStream(ctx, file, bytes.NewReader(data), int64(len(data)))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "live media staging failed", Phase: "admission", Cause: errors.Join(writeErr, closeErr, ctx.Err())}
	}
	before, statusErr = control.Protocol2Status(ctx)
	if statusErr != nil {
		return unavailableError()
	}
	if !liveMediaResponseMatches(before, b) {
		return protocol.LiveMediaIdentityError()
	}
	if ctx.Err() != nil {
		return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "live media upload cancelled", Phase: "admission", Cause: ctx.Err()}
	}
	response, dispatchErr := control.ReplaceLiveMedia(ctx, path, b.PackageID, b.Generation)
	r.noteDispatch("replace_live_media", dispatchErr == nil)
	if dispatchErr != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media delivery is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return mapLiveMediaError(response.Error)
	}
	if !liveMediaResponseMatches(response, b) {
		return unavailableError()
	}
	return nil
}

func (r *Runtime) ClearLiveMedia(ctx context.Context, b protocol.DevelopmentMediaBinding) *protocol.APIError {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.LiveMediaRequestError()
	}
	control, ok := r.control.(protocol2LiveMediaControl)
	if !ok {
		return unsupportedOperationError()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	before, err := control.Protocol2Status(ctx)
	if err != nil {
		return unavailableError()
	}
	if !liveMediaResponseMatches(before, b) {
		return protocol.LiveMediaIdentityError()
	}
	response, err := control.ClearLiveMedia(ctx, b.PackageID, b.Generation)
	r.noteDispatch("clear_media", err == nil)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media clear is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return mapLiveMediaError(response.Error)
	}
	if !liveMediaResponseMatches(response, b) {
		return unavailableError()
	}
	return nil
}

func mapLiveMediaError(remote *Protocol2Error) *protocol.APIError {
	// Loader invalid-state (GP error 4) is already "busy", including when the
	// runtime still reports the raw Exchange rejection. A poisoned toggle,
	// deadline, or unstable ACK may still arrive as phase=input io_failed;
	// those transport glitches stay retryable. GP error 2 is invalid index.
	// The runtime retries the legacy eject before that string is returned, so
	// a leaked "response 2" is a hard mismatch, not loader contention. A hard
	// MMIO failure or any other invalid clear acknowledgement stays
	// unavailable, even when its phase is input.
	if remote != nil && (remote.Code == "busy" || inputTransportGlitch(remote) || loaderInvalidState(remote)) {
		return protocol.LiveMediaBusyError()
	}
	return mapProtocol2Error(remote)
}

func loaderInvalidState(remote *Protocol2Error) bool {
	if remote == nil || remote.Code != "io_failed" || remote.Phase != "input" {
		return false
	}
	return remote.Message == "FES GP command rejected with response 4"
}

func inputTransportGlitch(remote *Protocol2Error) bool {
	if remote == nil || remote.Code != "io_failed" || remote.Phase != "input" {
		return false
	}
	message := remote.Message
	return strings.Contains(message, "FES GP exchange state is ambiguous") ||
		strings.Contains(message, "FES GP exchange deadline exceeded") ||
		strings.Contains(message, "FES GP response stability deadline exceeded") ||
		strings.Contains(message, "unstable FES GP response") ||
		strings.Contains(message, "invalid FES GP response signature")
}
