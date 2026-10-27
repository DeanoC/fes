package misterruntime

import (
	"context"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) SetSessionDisplay(ctx context.Context, packageID string, generation uint64, display protocol.SessionDisplay) (Protocol2Response, error) {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 || !display.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol           int                   `json:"protocol"`
		Operation          string                `json:"operation"`
		ExpectedPackageID  string                `json:"expected_package_id"`
		ExpectedGeneration uint64                `json:"expected_generation"`
		Visible            bool                  `json:"visible"`
		Focus              protocol.SessionFocus `json:"focus"`
	}{2, "session_display", packageID, generation, display.Visible, display.Focus})
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
	SetSessionDisplay(context.Context, string, uint64, protocol.SessionDisplay) (Protocol2Response, error)
}

// SessionDisplayFocus fences input delivery until the physical result is known.
// Its completion retains focus when a failed operation cannot prove a close.
type SessionDisplayFocus interface {
	BeginSessionDisplay(context.Context, bool) (func(bool), error)
}

func (r *Runtime) ConfigureSessionDisplayFocus(focus SessionDisplayFocus) {
	r.displayFocus = focus
}

func sessionDisplayResponseMatches(response Protocol2Response, b protocol.DevelopmentMediaBinding) bool {
	if !response.OK || response.Error != nil || response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.Generation == nil {
		return false
	}
	p := response.ActivePackage
	activation := activationFromProtocol2(p.PackageID, p.Descriptor, response)
	return b.MatchesSessionDisplay(protocol.Status{State: protocol.StateActive, Development: true, CorePackage: corePackageStatus(activation)})
}

// sessionDisplayFocusReported is the input focus a reply reports for the
// bound session's plane. Any other reply proves nothing about focus.
func sessionDisplayFocusReported(response Protocol2Response, b protocol.DevelopmentMediaBinding) (protocol.SessionFocus, bool) {
	m := response.MenuDisplay
	if response.State != "running_development" || response.ActivePackage == nil || response.ActivePackage.PackageID != b.PackageID ||
		response.Generation == nil || *response.Generation != b.Generation || m == nil || !m.Session ||
		m.CoreGeneration != b.Generation || m.PackageID == nil || *m.PackageID != b.PackageID ||
		m.Focus != protocol.SessionFocusMachine && m.Focus != protocol.SessionFocusLauncher {
		return "", false
	}
	return m.Focus, true
}

func (r *Runtime) SetSessionDisplay(ctx context.Context, display protocol.SessionDisplay, b protocol.DevelopmentMediaBinding) *protocol.APIError {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole || !display.Valid() {
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
		finish, err := r.displayFocus.BeginSessionDisplay(ctx, display.Focus == protocol.SessionFocusLauncher)
		if err != nil {
			return &protocol.APIError{Code: protocol.CodeBusy, Message: "launcher input focus is unavailable", Phase: "input", Cause: err}
		}
		defer func() { finish(focused) }()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	response, err := control.SetSessionDisplay(ctx, b.PackageID, b.Generation, display)
	r.noteDispatch("session_display", err == nil)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher display change is unconfirmed; retry close or inspect the session", Phase: "display"}
	}
	if !response.OK || response.Error != nil {
		// A refusal from this session still reports where the runtime left
		// input, for example machine focus kept after a failed neutral post.
		if focus, ok := sessionDisplayFocusReported(response, b); ok {
			focused = focus == protocol.SessionFocusLauncher
		}
		return mapProtocol2Error(response.Error)
	}
	if !sessionDisplayResponseMatches(response, b) || response.MenuDisplay == nil ||
		!response.MenuDisplay.Session || response.MenuDisplay.CoreGeneration != b.Generation ||
		response.MenuDisplay.PackageID == nil || *response.MenuDisplay.PackageID != b.PackageID ||
		response.MenuDisplay.Available != display.Visible || display.Visible && response.MenuDisplay.Generation == 0 ||
		response.MenuDisplay.Focus != display.Focus {
		return unavailableError()
	}
	focused = display.Focus == protocol.SessionFocusLauncher
	// The runtime posts a neutral matrix only when the launcher takes focus.
	// Machine focus leaves held machine keys as they were.
	if focused {
		r.keyboardMatrix = 0xffffffffff
		r.keyboardPackageID, r.keyboardGeneration = b.PackageID, b.Generation
	}
	return nil
}
