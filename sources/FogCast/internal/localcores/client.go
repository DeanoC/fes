package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

const (
	clientDialTimeout = time.Second
	clientTimeout     = 3 * time.Second
	maxClientBody     = 1 << 20
)

// Typed failures from the local-control socket. Error text is the JSON
// code the agent writes (unavailable or warming).
var (
	ErrUnavailable = errors.New("unavailable")
	// ErrWarming means the agent is still filling the catalog caches. The
	// HTTP call itself already returned. Callers that poll, including
	// tenfoot, retry until a list succeeds. A successful empty list is not
	// this state.
	ErrWarming = errors.New("warming")
)

// Client calls the kit-local control socket over HTTP. A nil or empty
// client returns ErrUnavailable and does not panic. List gives up after 3s.
type Client struct {
	path string
	http *http.Client
}

// NewClient dials socketPath for each request. An empty path does not listen.
// The shared HTTP client applies a deadline to each list call.
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
// An empty catalog is an empty slice, not nil. An unreadable durable
// library is the same empty slice: the socket answers HTTP 200. Bearer
// REST uses HTTP 503 CORE_PACKAGE_STORE_UNAVAILABLE for that failure.
// While the agent is still warming the catalog caches, List returns
// ErrWarming as soon as that response arrives. It does not wait out the
// 3s client timeout.
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

func (c *Client) do(ctx context.Context, method, path string, out any) error {
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
	req, err := http.NewRequestWithContext(ctx, method, "http://local-control"+path, nil)
	if err != nil {
		return err
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
	case ErrUnavailable.Error():
		return ErrUnavailable
	case ErrWarming.Error():
		return ErrWarming
	}
	switch status {
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	default:
		if payload.Error != "" {
			return errors.New(payload.Error)
		}
		return fmt.Errorf("local control: status %d", status)
	}
}

func dialUnavailable(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}
