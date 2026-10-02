package misterruntime

import (
	"context"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) SetSessionDisplay(ctx context.Context, packageID string, generation uint64, visible bool) (Protocol2Response, error) {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		Visible            bool   `json:"visible"`
	}{2, "session_display", packageID, generation, visible})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && (response.InspectedPackage != nil || response.CoreData != nil) {
		err = errInvalidRuntimeResponse
	}
	return response, err
}

type protocol2SessionDisplayControl interface {
	protocol2StatusControl
	SetSessionDisplay(context.Context, string, uint64, bool) (Protocol2Response, error)
}

// SessionDisplayFocus fences input delivery until the physical result is known.
// Its completion retains focus when a failed operation cannot prove a close.
type SessionDisplayFocus interface {
	BeginSessionDisplay(context.Context) (func(bool), error)
}

func (r *Runtime) ConfigureSessionDisplayFocus(focus SessionDisplayFocus) {
	r.displayFocus = focus
}

func sessionDisplayResponseMatches(response Protocol2Response, b protocol.DevelopmentMediaBinding) bool {
	if !liveMediaResponseMatches(response, b) {
		return false
	}
	p := response.ActivePackage
	activation := activationFromProtocol2(p.PackageID, p.Descriptor, response)
	return protocol.SessionDisplayCapable(corePackageStatus(activation))
}

func (r *Runtime) SetSessionDisplay(ctx context.Context, visible bool, b protocol.DevelopmentMediaBinding) *protocol.APIError {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.SessionDisplayRequestError()
	}
	control, ok := r.control.(protocol2SessionDisplayControl)
	if !ok {
		return unsupportedOperationError()
	}
	before, err := control.Protocol2Status(ctx)
	if err != nil {
		return unavailableError()
	}
	if !sessionDisplayResponseMatches(before, b) {
		return protocol.SessionDisplayIdentityError()
	}
	// Acquire the input fence before computerMu: completing an in-flight key
	// post may need computerMu. The runtime itself posts the neutral matrix.
	focused := true
	if r.displayFocus != nil {
		finish, err := r.displayFocus.BeginSessionDisplay(ctx)
		if err != nil {
			return &protocol.APIError{Code: protocol.CodeBusy, Message: "launcher input focus is unavailable", Phase: "input", Cause: err}
		}
		defer func() { finish(focused) }()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	response, err := control.SetSessionDisplay(ctx, b.PackageID, b.Generation, visible)
	r.noteDispatch("session_display", err == nil)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher display change is unconfirmed; retry close or inspect the session", Phase: "display"}
	}
	if !response.OK || response.Error != nil {
		return mapProtocol2Error(response.Error)
	}
	if !sessionDisplayResponseMatches(response, b) || response.MenuDisplay == nil ||
		!response.MenuDisplay.Session || response.MenuDisplay.CoreGeneration != b.Generation ||
		response.MenuDisplay.PackageID == nil || *response.MenuDisplay.PackageID != b.PackageID ||
		response.MenuDisplay.Available != visible || visible && response.MenuDisplay.Generation == 0 {
		return unavailableError()
	}
	focused = visible
	r.keyboardMatrix = 0xffffffffff
	r.keyboardPackageID, r.keyboardGeneration = b.PackageID, b.Generation
	return nil
}
