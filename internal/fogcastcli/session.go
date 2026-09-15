package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type hostSession struct {
	ID          string                      `json:"id"`
	Target      string                      `json:"target,omitempty"`
	TargetID    string                      `json:"target_id,omitempty"`
	State       protocol.State              `json:"state"`
	GameID      *string                     `json:"game_id,omitempty"`
	System      *protocol.System            `json:"system,omitempty"`
	Execution   string                      `json:"execution,omitempty"`
	Media       string                      `json:"media,omitempty"`
	Progress    *hostSessionProgress        `json:"progress,omitempty"`
	Input       *hostSessionInput           `json:"input,omitempty"`
	CorePackage *protocol.CorePackageStatus `json:"core_package,omitempty"`
	FlightID    string                      `json:"flight_id,omitempty"`
}

type hostSessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type hostSessionInput struct {
	State string `json:"state"`
	Ready bool   `json:"ready"`
}

func runHostSessionCommand(ctx context.Context, origin string, args []string) commandResult {
	method, path := http.MethodGet, "/api/v1/session"
	var body []byte
	switch args[0] {
	case "status":
	case "launch":
		method, path = http.MethodPost, "/api/v1/session/launch"
		encoded, err := json.Marshal(struct {
			GameID string `json:"game_id"`
		}{GameID: args[1]})
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		body = encoded
	case "stop":
		method, path = http.MethodPost, "/api/v1/session/stop"
	default:
		return commandResult{err: errors.New("invalid command"), exit: 2}
	}
	return callHostSession(ctx, origin, method, path, body)
}

func callHostSession(ctx context.Context, origin, method, path string, body []byte) commandResult {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "FogCast API request is invalid", Phase: "request"}, exit: 1}
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not accepted")
	}}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return commandResult{err: err, exit: 1}
		}
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable", Phase: "request"}, exit: 1}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FogCast host API response is unavailable", Phase: "request"}, exit: 1}
	}
	if response.StatusCode != http.StatusOK {
		var envelope protocol.ErrorEnvelope
		if json.Unmarshal(payload, &envelope) == nil && envelope.Error.Code != "" {
			return commandResult{err: &envelope.Error, exit: 1}
		}
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FogCast host session request failed", Phase: "request"}, exit: 1}
	}
	session, err := decodeHostSession(payload)
	if err != nil {
		return commandResult{err: err, exit: 1}
	}
	result := statusResult{State: session.State, GameID: session.GameID, System: session.System, CorePackage: session.CorePackage}
	return commandResult{jsonValue: session, human: func(output io.Writer) error { return writeHumanStatusResult(output, result) }}
}

func decodeHostSession(payload []byte) (hostSession, error) {
	var session hostSession
	if json.Unmarshal(payload, &session) != nil || !validHostSessionState(session.State) {
		return hostSession{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "FogCast host returned an invalid session", Phase: "recovery"}
	}
	if session.GameID != nil && (len(*session.GameID) > maxPublicGameIDBytes || protocol.ValidateGameID(*session.GameID) != nil) {
		session.GameID = nil
	}
	if session.System != nil && protocol.ValidateSystem(*session.System) != nil {
		session.System = nil
	}
	return session, nil
}

func validHostSessionState(state protocol.State) bool {
	switch state {
	case protocol.StateIdle, protocol.StateLaunching, protocol.StateActive, protocol.StateStopping, protocol.StateFailed:
		return true
	default:
		return false
	}
}
