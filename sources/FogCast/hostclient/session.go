// Package hostclient contains UI-independent contracts for the FogCast host API.
package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	ErrResponseTooLarge  = errors.New("host response exceeds maximum size")
	ErrResponseTruncated = errors.New("host response is truncated")
)

// SessionProgress is the optional progress object returned by a session
// operation.
type SessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// SessionInput is the read-only remote-input state. SessionID binds a kit
// stream to the persistent host session that owns it.
type SessionInput struct {
	State     string `json:"state"`
	Ready     bool   `json:"ready"`
	SessionID string `json:"session_id,omitempty"`
}

// SessionCoreInterface is one versioned interface advertised by a package.
type SessionCoreInterface struct {
	ID    string `json:"id"`
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

// IsKeyboard reports the exact keyboard interface supported by session clients.
// Other versions are not assumed to be compatible with fes.keyboard 1.0.
func (i SessionCoreInterface) IsKeyboard() bool {
	return i.ID == "fes.keyboard" && i.Major == 1 && i.Minor == 0
}

// SessionCorePackage is the package capability projection attached to a
// session. Generation is deliberately uint64 so the wire value is lossless.
type SessionCorePackage struct {
	Generation       uint64                 `json:"generation"`
	Gamepad          bool                   `json:"gamepad"`
	ActiveInterfaces []SessionCoreInterface `json:"active_interfaces"`
}

// SessionResult is the common host response for session reads and mutations.
type SessionResult struct {
	HTTPStatus              int
	State                   string
	GameID                  string
	System                  string
	Execution               string
	Media                   string
	Progress                *SessionProgress
	Input                   *SessionInput
	Development             bool
	DevelopmentSessionState string
	CorePackage             *SessionCorePackage
	CoreKeyboard            bool
	// HPSFramebuffer is set when the session JSON includes hps_framebuffer.
	// Nil means the host did not say whether this idle enables SPI 0x002f.
	HPSFramebuffer *bool
	ErrorCode      string
	ErrorMessage   string
	FlightID       string
}

// ReadResponseBody reads one bounded HTTP response body. It rejects a body
// larger than maxBytes and a body shorter than its declared Content-Length.
// It does not close response.Body; the caller retains ownership of the HTTP
// response lifecycle.
func ReadResponseBody(response *http.Response, maxBytes int64) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("%w: body unavailable", ErrResponseTruncated)
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid response limit %d", maxBytes)
	}
	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: content length %d exceeds %d", ErrResponseTooLarge, response.ContentLength, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: %v", ErrResponseTruncated, err)
		}
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: read more than %d bytes", ErrResponseTooLarge, maxBytes)
	}
	if response.ContentLength >= 0 && int64(len(body)) < response.ContentLength {
		return nil, fmt.Errorf("%w: read %d of %d bytes", ErrResponseTruncated, len(body), response.ContentLength)
	}
	return body, nil
}

// GetSession performs the host session read using the caller-provided HTTP
// client and response bound. The client's transport, redirect policy, and
// deadline are preserved exactly.
func GetSession(ctx context.Context, client *http.Client, baseURL string, maxBytes int64) (SessionResult, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/session", http.NoBody)
	if err != nil {
		return SessionResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return SessionResult{}, err
	}
	defer response.Body.Close()
	result := SessionResult{HTTPStatus: response.StatusCode}
	body, err := ReadResponseBody(response, maxBytes)
	if err != nil {
		return result, fmt.Errorf("session response: %w", err)
	}
	result, err = DecodeSession(response.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("session response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		if result.ErrorCode != "" {
			return result, fmt.Errorf("host API %d %s: %s", response.StatusCode, result.ErrorCode, result.ErrorMessage)
		}
		return result, APIStatusError(response.StatusCode, body)
	}
	return result, nil
}

// DecodeSession decodes a session response without applying a caller-specific
// response-size bound or transport policy.
func DecodeSession(status int, body []byte) (SessionResult, error) {
	result := SessionResult{HTTPStatus: status}
	if len(bytes.TrimSpace(body)) == 0 {
		return result, fmt.Errorf("empty body")
	}
	var wire struct {
		State                   string              `json:"state"`
		GameID                  *string             `json:"game_id"`
		System                  *string             `json:"system"`
		Execution               string              `json:"execution"`
		Media                   string              `json:"media"`
		Progress                *SessionProgress    `json:"progress"`
		Input                   *SessionInput       `json:"input"`
		FlightID                string              `json:"flight_id"`
		Development             *bool               `json:"development"`
		DevelopmentActive       *bool               `json:"development_active"`
		DevelopmentSessionState string              `json:"development_session_state"`
		CorePackage             *SessionCorePackage `json:"core_package"`
		HPSFramebuffer          *bool               `json:"hps_framebuffer"`
		Error                   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return result, err
	}
	result.State = wire.State
	if wire.GameID != nil {
		result.GameID = strings.TrimSpace(*wire.GameID)
	}
	if wire.System != nil {
		result.System = strings.TrimSpace(*wire.System)
	}
	result.Execution = strings.TrimSpace(wire.Execution)
	result.Media = strings.TrimSpace(wire.Media)
	result.FlightID = strings.TrimSpace(wire.FlightID)
	result.Progress = wire.Progress
	result.Input = wire.Input
	result.DevelopmentSessionState = strings.TrimSpace(wire.DevelopmentSessionState)
	result.CorePackage = wire.CorePackage
	result.HPSFramebuffer = wire.HPSFramebuffer
	switch {
	case wire.DevelopmentActive != nil:
		result.Development = *wire.DevelopmentActive
	case wire.Development != nil:
		result.Development = *wire.Development
	default:
		result.Development = result.Execution == "fpga_development"
	}
	if wire.CorePackage != nil {
		for _, contract := range wire.CorePackage.ActiveInterfaces {
			if contract.IsKeyboard() {
				result.CoreKeyboard = true
				break
			}
		}
	}
	if result.Development && result.Execution == "" {
		switch result.State {
		case "active", "launching":
			result.Execution = "fpga_development"
		}
	}
	if wire.Error != nil {
		result.ErrorCode = wire.Error.Code
		result.ErrorMessage = wire.Error.Message
		return result, nil
	}
	if !validSessionState(wire.State) {
		return result, fmt.Errorf("invalid session state %q", wire.State)
	}
	return result, nil
}

func validSessionState(state string) bool {
	switch state {
	case "idle", "launching", "active", "stopping", "failed":
		return true
	default:
		return false
	}
}

// APIStatusError formats a structured or plain host API error response.
func APIStatusError(status int, body []byte) error {
	var wire struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error.Code != "" {
		return fmt.Errorf("host API %d %s: %s", status, wire.Error.Code, wire.Error.Message)
	}
	return fmt.Errorf("host API status %d", status)
}
