// Package tenfoot is the native SDL3 10-foot FogCast launcher.
//
// It talks to the public host API over HTTP. Catalog, content transfer, and
// the MiSTer command path stay in the existing host and target processes.
package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	defaultPageLimit          = 200
	defaultMaxGames           = 10000
	maxAPIResponse            = 16 << 20
	defaultAttractLimit       = 24
	defaultAttractIdleSeconds = 60
)

// KitLeaseStatus is GET /v1/kit/lease on the selected target (status-only).
type KitLeaseStatus struct {
	HTTPStatus   int
	State        string
	Owner        string
	Purpose      string
	Generation   string
	ExpiresAt    string
	ExpiresInMS  int64
	Reason       string
	ErrorCode    string
	ErrorMessage string
	Unavailable  bool
}

// Client calls the FogCast public host API.
type Client struct {
	*hostclient.Client
}

// NewClient builds the sofa adapter around a host API client. baseURL defaults
// to hostclient.DefaultAPIBase.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{Client: hostclient.NewClient(baseURL, httpClient)}
}

// withAPIHost returns a client that sends Host: host on every request.
// The connection URL is unchanged. Empty host is a no-op.
func (c *Client) withAPIHost(host string) *Client {
	if c == nil || c.Client == nil || strings.TrimSpace(host) == "" {
		return c
	}
	return &Client{Client: c.Client.WithAPIHost(host)}
}

func apiURLIsLoopback(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackAPIHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	port := "8787"
	if err == nil {
		if p := u.Port(); p != "" {
			port = p
		} else if u.Scheme == "https" {
			port = "443"
		} else if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// smokeAPIHost is the Host header for -smoke against a non-loopback API URL.
// The host API allowlist is loopback; Docker/host-gateway URLs still connect
// but send Host: host.docker.internal and get 403 HOST_NOT_ALLOWED.
func smokeAPIHost(baseURL, explicit string) string {
	if host := strings.TrimSpace(explicit); host != "" {
		return host
	}
	if apiURLIsLoopback(baseURL) {
		return ""
	}
	return loopbackAPIHost(baseURL)
}

// Launch and Stop retain tenfoot's stamped API while delegating all HTTP and
// response decoding to the UI-independent hostclient.
func hostStamp(stamp ClientStamp) hostclient.ClientStamp {
	return hostclient.ClientStamp{TsUTC: stamp.TsUTC, MonoMS: stamp.MonoMS, FlightID: stamp.FlightID}
}

func (c *Client) Launch(ctx context.Context, gameID string) (hostclient.SessionResult, error) {
	return c.LaunchStamped(ctx, gameID, ClientStampNow())
}

func (c *Client) LaunchStamped(ctx context.Context, gameID string, stamp ClientStamp) (hostclient.SessionResult, error) {
	return c.Client.LaunchStamped(ctx, gameID, hostStamp(stamp))
}

func (c *Client) Stop(ctx context.Context) (hostclient.SessionResult, error) {
	return c.StopStamped(ctx, ClientStampNow())
}

// StopStamped is the rooms Soft-stop: now-playing B, Esc, Backspace, or s
// returns to the same room and keeps the kit lease while the shell stays up.
func (c *Client) StopStamped(ctx context.Context, stamp ClientStamp) (hostclient.SessionResult, error) {
	return c.Client.StopRetainLease(ctx, hostStamp(stamp))
}

// ReleaseIdleLease is shell exit after that Soft-stop when the service is
// idle. An empty body asks idle cleanup to release the retained kit lease.
// B/Back stays on StopStamped.
func (c *Client) ReleaseIdleLease(ctx context.Context, stamp ClientStamp) (hostclient.SessionResult, error) {
	if c == nil || c.Client == nil {
		return hostclient.SessionResult{}, fmt.Errorf("tenfoot client is nil")
	}
	return c.Client.StopStamped(ctx, hostStamp(stamp))
}

// ReleaseIdleGrants is shell exit when a play survived Soft-stop. It drops
// idle grants and does not stop that play.
func (c *Client) ReleaseIdleGrants(ctx context.Context, stamp ClientStamp) (hostclient.SessionResult, error) {
	if c == nil || c.Client == nil {
		return hostclient.SessionResult{}, fmt.Errorf("tenfoot client is nil")
	}
	return c.Client.ReleaseIdleGrants(ctx, hostStamp(stamp))
}

var errSessionsUnsupported = errors.New("play sessions are unavailable")

// survivingPlayCount reads GET /api/v1/sessions. A 404 means this host has
// no play list; callers then trust GET /api/v1/session alone.
func (c *Client) survivingPlayCount(ctx context.Context) (int, error) {
	if c == nil || c.Client == nil {
		return 0, fmt.Errorf("tenfoot client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+"/api/v1/sessions", http.NoBody)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, errSessionsUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return 0, hostclient.APIStatusError(resp.StatusCode, body)
	}
	var wire struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return 0, err
	}
	return len(wire.Sessions), nil
}

// SessionEvents loads GET /api/v1/session/events?after=N (JSON poll, not SSE).
func (c *Client) SessionEvents(ctx context.Context, after uint64) ([]hostclient.SessionEvent, error) {
	path := "/api/v1/session/events?after=" + strconv.FormatUint(after, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, hostclient.APIStatusError(resp.StatusCode, body)
	}
	var wire struct {
		Events []struct {
			Sequence     uint64                      `json:"sequence"`
			FlightID     string                      `json:"flight_id"`
			TSUTC        string                      `json:"ts_utc"`
			MonoMS       int64                       `json:"mono_ms"`
			ClientTSUTC  string                      `json:"client_ts_utc"`
			ClientMonoMS *int64                      `json:"client_mono_ms"`
			Event        string                      `json:"event"`
			State        string                      `json:"state"`
			GameID       *string                     `json:"game_id"`
			System       *string                     `json:"system"`
			Media        string                      `json:"media"`
			Progress     *hostclient.SessionProgress `json:"progress"`
			Input        *hostclient.SessionInput    `json:"input"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("session events: %w", err)
	}
	out := make([]hostclient.SessionEvent, 0, len(wire.Events))
	for _, row := range wire.Events {
		ev := hostclient.SessionEvent{
			Sequence:     row.Sequence,
			FlightID:     strings.TrimSpace(row.FlightID),
			TSUTC:        strings.TrimSpace(row.TSUTC),
			MonoMS:       row.MonoMS,
			ClientTSUTC:  strings.TrimSpace(row.ClientTSUTC),
			ClientMonoMS: row.ClientMonoMS,
			Event:        strings.TrimSpace(row.Event),
			State:        strings.TrimSpace(row.State),
			Media:        strings.TrimSpace(row.Media),
			Progress:     row.Progress,
			Input:        row.Input,
		}
		if row.GameID != nil {
			ev.GameID = strings.TrimSpace(*row.GameID)
		}
		if row.System != nil {
			ev.System = strings.TrimSpace(*row.System)
		}
		out = append(out, ev)
	}
	return out, nil
}

// KitLease loads GET /v1/kit/lease on the selected target agent.
// This is a status-only read of the target kit API, not a host proxy.
func (c *Client) KitLease(ctx context.Context, targetBase string) (KitLeaseStatus, error) {
	targetBase = strings.TrimRight(strings.TrimSpace(targetBase), "/")
	if targetBase == "" {
		return KitLeaseStatus{}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetBase+"/v1/kit/lease", http.NoBody)
	if err != nil {
		return KitLeaseStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		if isHostTransportError(err) {
			return KitLeaseStatus{Unavailable: true, ErrorMessage: "kit unreachable"}, nil
		}
		return KitLeaseStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return KitLeaseStatus{}, err
	}
	result := decodeKitLeaseBody(resp.StatusCode, body)
	if resp.StatusCode == http.StatusOK {
		return result, nil
	}
	if result.Unavailable || result.ErrorCode != "" {
		return result, nil
	}
	return result, hostclient.APIStatusError(resp.StatusCode, body)
}

func decodeKitLeaseBody(status int, body []byte) KitLeaseStatus {
	result := KitLeaseStatus{HTTPStatus: status}
	var wire struct {
		State       string `json:"state"`
		Owner       string `json:"owner"`
		Purpose     string `json:"purpose"`
		Generation  string `json:"generation"`
		ExpiresAt   string `json:"expires_at"`
		ExpiresInMS int64  `json:"expires_in_ms"`
		Reason      string `json:"reason"`
		Error       *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Status *struct {
			State       string `json:"state"`
			Owner       string `json:"owner"`
			Purpose     string `json:"purpose"`
			Generation  string `json:"generation"`
			ExpiresAt   string `json:"expires_at"`
			ExpiresInMS int64  `json:"expires_in_ms"`
			Reason      string `json:"reason"`
		} `json:"status"`
	}
	_ = json.Unmarshal(body, &wire)
	if wire.Status != nil {
		if wire.State == "" {
			wire.State = wire.Status.State
		}
		if wire.Owner == "" {
			wire.Owner = wire.Status.Owner
		}
		if wire.Purpose == "" {
			wire.Purpose = wire.Status.Purpose
		}
		if wire.Generation == "" {
			wire.Generation = wire.Status.Generation
		}
		if wire.ExpiresAt == "" {
			wire.ExpiresAt = wire.Status.ExpiresAt
		}
		if wire.ExpiresInMS == 0 {
			wire.ExpiresInMS = wire.Status.ExpiresInMS
		}
		if wire.Reason == "" {
			wire.Reason = wire.Status.Reason
		}
	}
	result.State = strings.TrimSpace(wire.State)
	result.Owner = strings.TrimSpace(wire.Owner)
	result.Purpose = strings.TrimSpace(wire.Purpose)
	result.Generation = strings.TrimSpace(wire.Generation)
	result.ExpiresAt = strings.TrimSpace(wire.ExpiresAt)
	result.ExpiresInMS = wire.ExpiresInMS
	result.Reason = strings.TrimSpace(wire.Reason)
	if wire.Error != nil {
		result.ErrorCode = strings.TrimSpace(wire.Error.Code)
		result.ErrorMessage = strings.TrimSpace(wire.Error.Message)
	}
	switch {
	case status == http.StatusServiceUnavailable && (result.ErrorCode == "TARGET_UNAVAILABLE" || result.ErrorCode == "KIT_LEASE_BLOCKED" || result.ErrorCode == "MISTER_UNAVAILABLE"):
		result.Unavailable = result.ErrorCode != "KIT_LEASE_BLOCKED"
		if result.ErrorCode == "KIT_LEASE_BLOCKED" && result.State == "" {
			result.State = "blocked"
		}
		if result.Reason == "" {
			result.Reason = result.ErrorMessage
		}
	case status == 0 || status >= 500:
		result.Unavailable = true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if result.ErrorCode == "" {
			result.ErrorCode = "UNAUTHORIZED"
		}
		if result.ErrorMessage == "" {
			result.ErrorMessage = "kit lease unavailable"
		}
	}
	return result
}

// OpenSessionPreview starts GET /api/v1/session/preview and returns an MJPEG
// reader. 404 (no decoder route), 503 inactive, and transport failures are
// PreviewUnavailable. The caller must Close the stream.
func (c *Client) OpenSessionPreview(ctx context.Context) (*MJPEGStream, error) {
	if c == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	httpClient := c.PreviewHTTPClient()
	if httpClient == nil {
		httpClient = c.HTTPClient()
	}
	if httpClient == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+"/api/v1/session/preview", http.NoBody)
	if err != nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	req.Header.Set("Accept", previewContentType)
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = "session preview is unavailable"
		}
		return nil, PreviewUnavailable{Status: resp.StatusCode, Message: msg}
	}
	stream, err := NewMJPEGStream(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		_ = resp.Body.Close()
		if IsPreviewUnavailable(err) {
			return nil, err
		}
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	return stream, nil
}

// PostUIEvent posts one sofa/tenfoot action to POST /api/v1/debug/ui-events.
// Failures are returned to the caller; the sofa treats them as best-effort.
func (c *Client) PostUIEvent(ctx context.Context, event UIEvent) error {
	if c == nil {
		return fmt.Errorf("host client is nil")
	}
	event.Kind = strings.TrimSpace(event.Kind)
	if event.Kind == "" {
		return fmt.Errorf("ui event kind is empty")
	}
	if strings.TrimSpace(event.Layer) == "" {
		event.Layer = "ui"
	}
	if strings.TrimSpace(event.Severity) == "" {
		event.Severity = "ok"
	}
	if strings.TrimSpace(event.TSUTC) == "" {
		event.TSUTC = ClientStampNow().TsUTC
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL()+"/api/v1/debug/ui-events", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ui event: HTTP %d", resp.StatusCode)
	}
	return nil
}

// SendCoreKey posts one keyboard event to POST /api/v1/session/input/event.
func (c *Client) SendCoreKey(ctx context.Context, event remoteinput.Event) error {
	payload, err := json.Marshal(map[string]any{"event": event})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL()+"/api/v1/session/input/event", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("input event: HTTP %d", resp.StatusCode)
	}
	return nil
}

func isHostTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
