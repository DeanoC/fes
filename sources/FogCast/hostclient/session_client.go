package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// Session loads GET /api/v1/session using the normal polling client.
func (c *Client) Session(ctx context.Context) (SessionResult, error) {
	return GetSession(ctx, c.httpClient, c.baseURL, maxResponseBytes)
}

// Launch posts {game_id} to POST /api/v1/session/launch with client clocks.
func (c *Client) Launch(ctx context.Context, gameID string) (SessionResult, error) {
	return c.LaunchStamped(ctx, gameID, ClientStampNow())
}

// LaunchStamped is Launch with an explicit client stamp.
func (c *Client) LaunchStamped(ctx context.Context, gameID string, stamp ClientStamp) (SessionResult, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return SessionResult{}, fmt.Errorf("game id is empty")
	}
	payload, err := json.Marshal(struct {
		GameID string `json:"game_id"`
	}{GameID: gameID})
	if err != nil {
		return SessionResult{}, err
	}
	return c.mutateSession(ctx, http.MethodPost, "/api/v1/session/launch", bytes.NewReader(payload), c.launchHTTP, stamp, "launch", "active")
}

// LoadDevelopmentRBF posts a bounded application/octet-stream body to
// POST /api/v1/session/development-rbf. Content-Length is required.
func (c *Client) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (SessionResult, error) {
	return c.LoadDevelopmentRBFStamped(ctx, size, content, ClientStampNow())
}

// LoadDevelopmentRBFStamped is LoadDevelopmentRBF with an explicit client stamp.
func (c *Client) LoadDevelopmentRBFStamped(ctx context.Context, size int64, content io.Reader, stamp ClientStamp) (SessionResult, error) {
	if size < 1 {
		return SessionResult{}, fmt.Errorf("development RBF is empty")
	}
	if size > protocol.MaxDevelopmentRBFBytes {
		return SessionResult{}, fmt.Errorf("development RBF exceeds %d bytes", protocol.MaxDevelopmentRBFBytes)
	}
	if content == nil {
		return SessionResult{}, fmt.Errorf("development RBF input is invalid")
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/v1/session/development-rbf", content)
	if err != nil {
		return SessionResult{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	applyClientStamp(req, stamp)
	return c.doSessionMutation(req, c.launchHTTP, "development RBF", "active")
}

// Stop posts an empty body to POST /api/v1/session/stop with client clocks.
// An empty body is the explicit user Stop: idle cleanup releases the kit lease.
func (c *Client) Stop(ctx context.Context) (SessionResult, error) {
	return c.StopStamped(ctx, ClientStampNow())
}

// StopStamped is Stop with an explicit client stamp.
func (c *Client) StopStamped(ctx context.Context, stamp ClientStamp) (SessionResult, error) {
	return c.mutateSession(ctx, http.MethodPost, "/api/v1/session/stop", http.NoBody, c.stopHTTP, stamp, "stop", "idle")
}

// StopRetainLease is the sofa Soft-stop. retain_lease asks idle cleanup to
// keep the kit lease. Stamps stay on the existing client-clock headers.
func (c *Client) StopRetainLease(ctx context.Context, stamp ClientStamp) (SessionResult, error) {
	return c.mutateSession(ctx, http.MethodPost, "/api/v1/session/stop", bytes.NewReader([]byte(`{"retain_lease":true}`)), c.stopHTTP, stamp, "stop", "idle")
}

func (c *Client) mutateSession(ctx context.Context, method, path string, body io.Reader, httpClient *http.Client, stamp ClientStamp, label, expectedState string) (SessionResult, error) {
	req, err := c.NewRequest(ctx, method, path, body)
	if err != nil {
		return SessionResult{}, err
	}
	if body != http.NoBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	applyClientStamp(req, stamp)
	return c.doSessionMutation(req, httpClient, label, expectedState)
}

func (c *Client) doSessionMutation(req *http.Request, httpClient *http.Client, label, expectedState string) (SessionResult, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return SessionResult{HTTPStatus: resp.StatusCode}, err
	}
	result, err := DecodeSession(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("%s response: %w", label, err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if result.State != expectedState {
		return result, fmt.Errorf("%s response: expected %s session, got %q", label, expectedState, result.State)
	}
	return result, nil
}

// Health loads GET /api/v1/health. HTTP 200 while the host process is up.
func (c *Client) Health(ctx context.Context) (HealthResult, error) {
	var wire struct {
		Ready  bool `json:"ready"`
		Target struct {
			Reachable  bool             `json:"reachable"`
			Ready      bool             `json:"ready"`
			Connection TargetConnection `json:"connection"`
		} `json:"target"`
	}
	if err := c.getJSON(ctx, "/api/v1/health", &wire); err != nil {
		return HealthResult{}, err
	}
	return HealthResult{
		Ready:           wire.Ready,
		TargetReachable: wire.Target.Reachable,
		TargetReady:     wire.Target.Ready,
		Connection:      wire.Target.Connection,
	}, nil
}

// Status loads GET /api/v1/status. HTTP 503 TARGET_UNAVAILABLE is kit-down,
// not a host-process failure.
func (c *Client) Status(ctx context.Context) (TargetStatus, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/status", http.NoBody)
	if err != nil {
		return TargetStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TargetStatus{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return TargetStatus{HTTPStatus: resp.StatusCode}, err
	}
	result := TargetStatus{HTTPStatus: resp.StatusCode}
	var wire struct {
		State  string  `json:"state"`
		GameID *string `json:"game_id"`
		System *string `json:"system"`
		Core   *string `json:"core"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Connection TargetConnection `json:"connection"`
	}
	_ = json.Unmarshal(body, &wire)
	result.State = strings.TrimSpace(wire.State)
	result.Connection = wire.Connection
	if wire.GameID != nil {
		result.GameID = strings.TrimSpace(*wire.GameID)
	}
	if wire.System != nil {
		result.System = strings.TrimSpace(*wire.System)
	}
	if wire.Core != nil {
		result.Core = strings.TrimSpace(*wire.Core)
	}
	if wire.Error != nil {
		result.ErrorCode = strings.TrimSpace(wire.Error.Code)
		result.ErrorMessage = strings.TrimSpace(wire.Error.Message)
	}
	if resp.StatusCode == http.StatusServiceUnavailable && result.ErrorCode == "TARGET_UNAVAILABLE" {
		result.Unavailable = true
		return result, nil
	}
	if resp.StatusCode != http.StatusOK {
		if result.ErrorCode != "" {
			return result, fmt.Errorf("host API %d %s: %s", resp.StatusCode, result.ErrorCode, result.ErrorMessage)
		}
		return result, APIStatusError(resp.StatusCode, body)
	}
	return result, nil
}

// SessionInput loads GET /api/v1/session/input.
func (c *Client) SessionInput(ctx context.Context) (SessionInput, error) {
	var result SessionInput
	if err := c.getJSON(ctx, "/api/v1/session/input", &result); err != nil {
		return SessionInput{}, err
	}
	return result, nil
}

// AttachInput posts an empty body to POST /api/v1/session/input/attach.
func (c *Client) AttachInput(ctx context.Context) (SessionResult, error) {
	return c.postSessionInput(ctx, "/api/v1/session/input/attach")
}

// DetachInput posts an empty body to POST /api/v1/session/input/detach.
func (c *Client) DetachInput(ctx context.Context) (SessionResult, error) {
	return c.postSessionInput(ctx, "/api/v1/session/input/detach")
}

func (c *Client) postSessionInput(ctx context.Context, path string) (SessionResult, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, path, http.NoBody)
	if err != nil {
		return SessionResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return SessionResult{HTTPStatus: resp.StatusCode}, err
	}
	result, err := DecodeSession(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("input response: %w", err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if resp.StatusCode != http.StatusOK {
		return result, APIStatusError(resp.StatusCode, body)
	}
	if !validSessionState(result.State) {
		return result, fmt.Errorf("input response: invalid session state %q", result.State)
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, dest)
}

func (c *Client) mutateJSON(ctx context.Context, method, path string, dest any) error {
	return c.doJSON(ctx, method, path, nil, dest)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, dest any) error {
	var bodyReader io.Reader = http.NoBody
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := c.NewRequest(ctx, method, path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return APIStatusError(resp.StatusCode, body)
	}
	if dest == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func imageContentType(value string) bool {
	return strings.HasPrefix(ContentTypeMain(value), "image/")
}

func videoContentType(value string) bool {
	switch ContentTypeMain(value) {
	case "video/mp4", "video/webm", "video/x-m4v", "video/quicktime":
		return true
	default:
		return false
	}
}

// ContentTypeMain returns the lowercase media type without parameters.
func ContentTypeMain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.Index(value, ";"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

func sniffVideoMIME(header []byte) string {
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		return "video/mp4"
	}
	if len(header) >= 4 && header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3 {
		return "video/webm"
	}
	return ""
}
