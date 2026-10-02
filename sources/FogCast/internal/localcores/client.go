package localcores

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	clientDialTimeout = time.Second
	clientTimeout     = 3 * time.Second
	// launchPostTimeout covers the agent's 60s load plus a short margin so a
	// normal program returns on this POST. List and Stop stay on clientTimeout.
	launchPostTimeout = 65 * time.Second
	// launchReconcileTimeout is how long Launch keeps reading status after
	// that POST deadline. A lost response is not "did not launch".
	launchReconcileTimeout = 8 * time.Second
	launchReconcilePoll    = 200 * time.Millisecond
	maxClientBody          = 1 << 20
)

// Typed failures from the local-control socket. Error text is the JSON
// code the agent writes (in_use, blocked, not_found, unavailable).
var (
	ErrInUse       = errors.New("in_use")
	ErrBlocked     = errors.New("blocked")
	ErrNotFound    = errors.New("not_found")
	ErrUnavailable = errors.New("unavailable")
)

// Client calls the kit-local control socket over HTTP. A nil or empty
// client returns ErrUnavailable and does not panic. List and Stop give up
// after 3s. Launch waits long enough for the agent's load, then reads
// status before deciding the program failed. The agent keeps an in-flight
// program if this process gives up.
type Client struct {
	path string
	http *http.Client
	// launchPost, reconcileFor and reconcileEvery override the launch
	// deadlines. Zero uses the production values. Tests set them short.
	launchPost     time.Duration
	reconcileFor   time.Duration
	reconcileEvery time.Duration
}

// NewClient dials socketPath for each request. An empty path does not listen.
// The shared HTTP client has no overall timeout: each call applies its own
// deadline so Launch can outlive List and Stop.
func NewClient(socketPath string) *Client {
	path := strings.TrimSpace(socketPath)
	dialer := &net.Dialer{Timeout: clientDialTimeout}
	return &Client{
		path: path,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					if path == "" {
						return nil, errors.New("local control socket path is empty")
					}
					return dialer.DialContext(ctx, "unix", path)
				},
				IdleConnTimeout: clientTimeout,
			},
		},
	}
}

// List returns the installed cores. A missing socket is an error.
// An empty catalog is an empty slice, not nil.
func (c *Client) List(ctx context.Context) ([]Core, error) {
	var cores []Core
	if err := c.do(ctx, http.MethodGet, "/v1/local/cores", &cores); err != nil {
		return nil, err
	}
	if cores == nil {
		cores = []Core{}
	}
	return cores, nil
}

// Launch posts one package id. Anything that is not a lowercase SHA-256
// hex id is not_found and is not sent. The POST waits about 65s. If that
// deadline fires and the caller did not cancel, Launch polls
// GET /v1/local/status and returns nil when that package is already
// running. It does not post launch a second time. A completed in_use,
// blocked, not_found, or unavailable response is returned as-is.
func (c *Client) Launch(ctx context.Context, packageID string) error {
	if !sha256Hex(packageID) {
		return ErrNotFound
	}
	if c == nil || c.http == nil || c.path == "" {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	post := c.launchPost
	if post <= 0 {
		post = launchPostTimeout
	}
	postCtx, cancel := context.WithTimeout(ctx, post)
	err := c.doRequest(postCtx, http.MethodPost, "/v1/local/cores/"+packageID+"/launch", nil, nil)
	cancel()
	if err == nil || !timedOut(err) || ctx.Err() != nil {
		return err
	}
	return c.reconcileRunning(ctx, packageID)
}

// LaunchROM posts the same launch route with a local cartridge path.
// The agent reads the file. An empty or relative path is not sent.
// This does not call the host session.
func (c *Client) LaunchROM(ctx context.Context, packageID, romPath string) error {
	if !sha256Hex(packageID) {
		return ErrNotFound
	}
	romPath = strings.TrimSpace(romPath)
	if romPath == "" || !filepath.IsAbs(romPath) || strings.ContainsAny(romPath, "\r\n") {
		return ErrUnavailable
	}
	if c == nil || c.http == nil || c.path == "" {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(struct {
		ROMPath string `json:"rom_path"`
	}{ROMPath: romPath})
	if err != nil {
		return ErrUnavailable
	}
	post := c.launchPost
	if post <= 0 {
		post = launchPostTimeout
	}
	postCtx, cancel := context.WithTimeout(ctx, post)
	err = c.doRequest(postCtx, http.MethodPost, "/v1/local/cores/"+packageID+"/launch", body, nil)
	cancel()
	if err == nil || !timedOut(err) || ctx.Err() != nil {
		return err
	}
	return c.reconcileRunning(ctx, packageID)
}

// Status reads the agent's launch phase. Running is true only after load
// has published the kit-local lease.
func (c *Client) Status(ctx context.Context) (RunStatus, error) {
	var status RunStatus
	if err := c.do(ctx, http.MethodGet, "/v1/local/status", &status); err != nil {
		return RunStatus{}, err
	}
	return status, nil
}

// Stop asks the agent to return the kit to the menu when this process
// holds the kit-local lease.
func (c *Client) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/local/stop", nil)
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	return c.doRequest(ctx, method, path, nil, out)
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte, out any) error {
	if c == nil || c.http == nil || c.path == "" {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, clientTimeout)
		defer cancel()
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://local-control"+path, reader)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if dialUnavailable(err) {
			return ErrUnavailable
		}
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxClientBody))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyStatus(resp.StatusCode, payload)
	}
	if out == nil || len(strings.TrimSpace(string(payload))) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return err
	}
	return nil
}

func classifyStatus(status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	switch payload.Error {
	case ErrInUse.Error():
		return ErrInUse
	case ErrBlocked.Error():
		return ErrBlocked
	case ErrNotFound.Error():
		return ErrNotFound
	case ErrUnavailable.Error():
		return ErrUnavailable
	}
	switch status {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	default:
		if payload.Error != "" {
			return errors.New(payload.Error)
		}
		return fmt.Errorf("local control: status %d", status)
	}
}

func (c *Client) reconcileRunning(ctx context.Context, packageID string) error {
	budget := c.reconcileFor
	if budget <= 0 {
		budget = launchReconcileTimeout
	}
	every := c.reconcileEvery
	if every <= 0 {
		every = launchReconcilePoll
	}
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(budget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	timer := time.NewTimer(0)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return ErrUnavailable
		}
		poll, cancel := context.WithTimeout(ctx, clientTimeout)
		status, err := c.Status(poll)
		cancel()
		if err == nil {
			if status.Running && status.PackageID == packageID {
				return nil
			}
			if status.Phase == phaseIdle || status.Phase == "" {
				return ErrUnavailable
			}
		} else if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		wait := every
		if remain := time.Until(deadline); remain < wait {
			wait = remain
		}
		if wait <= 0 {
			return ErrUnavailable
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func timedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// dialUnavailable is a missing socket or a refused connect. A timeout is
// not this: Launch still has to ask whether the program kept running.
func dialUnavailable(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}
