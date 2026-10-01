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

type launcherAuthTransport struct {
	base     http.RoundTripper
	token    string
	targetID string
}

func (t launcherAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	if t.targetID != "" {
		clone.Header.Set("X-FogCast-Target-ID", t.targetID)
	}
	return t.base.RoundTrip(clone)
}

func launcherHTTPClient(token, targetID string) *http.Client {
	if token == "" && targetID == "" {
		return nil
	}
	return &http.Client{Transport: launcherAuthTransport{base: http.DefaultTransport, token: token, targetID: targetID}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

const (
	defaultPageLimit          = 200
	defaultMaxGames           = 10000
	maxAPIResponse            = 16 << 20
	defaultAttractLimit       = 24
	defaultAttractIdleSeconds = 60
)

// KitLeaseStatus is the status-only lease projection for a paired target.
type KitLeaseStatus struct {
	HTTPStatus   int    `json:"-"`
	State        string `json:"state"`
	Owner        string `json:"owner"`
	Purpose      string `json:"purpose"`
	Generation   string `json:"generation"`
	ExpiresAt    string `json:"expires_at"`
	ExpiresInMS  int64  `json:"expires_in_ms"`
	Reason       string `json:"reason"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Unavailable  bool   `json:"unavailable,omitempty"`
}

// Client calls the FogCast public host API.
type Client struct {
	*hostclient.Client
	paired bool
}

// NewClient builds the sofa adapter around a host API client. baseURL defaults
// to hostclient.DefaultAPIBase.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	paired := false
	if httpClient != nil {
		if auth, ok := httpClient.Transport.(launcherAuthTransport); ok {
			paired = auth.targetID != ""
		}
	}
	return &Client{Client: hostclient.NewClient(baseURL, httpClient), paired: paired}
}

func (c *Client) PairedKitLease(ctx context.Context) (KitLeaseStatus, error) {
	if c == nil || c.Client == nil {
		return KitLeaseStatus{}, fmt.Errorf("tenfoot client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+"/api/v1/launcher/kit-lease", http.NoBody)
	if err != nil {
		return KitLeaseStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return KitLeaseStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return KitLeaseStatus{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return KitLeaseStatus{}, hostclient.APIStatusError(resp.StatusCode, body)
	}
	var status KitLeaseStatus
	err = json.Unmarshal(body, &status)
	return status, err
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

func (c *Client) StopExpectedStamped(ctx context.Context, expected hostclient.SessionResult, stamp ClientStamp) (hostclient.SessionResult, error) {
	if expected.ID == "" {
		return c.StopStamped(ctx, stamp)
	}
	return c.Client.StopRetainLeaseExpected(ctx, expected, hostStamp(stamp))
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
