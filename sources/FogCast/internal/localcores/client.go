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
	"time"
)

const (
	clientDialTimeout = time.Second
	clientTimeout     = 3 * time.Second
	maxClientBody     = 1 << 20
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
// client returns ErrUnavailable and does not panic. Timeouts are short:
// the agent keeps an in-flight program if this process gives up.
type Client struct {
	path string
	http *http.Client
}

// NewClient dials socketPath for each request. An empty path does not listen.
func NewClient(socketPath string) *Client {
	path := strings.TrimSpace(socketPath)
	dialer := &net.Dialer{Timeout: clientDialTimeout}
	return &Client{
		path: path,
		http: &http.Client{
			Timeout: clientTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					if path == "" {
						return nil, errors.New("local control socket path is empty")
					}
					return dialer.DialContext(ctx, "unix", path)
				},
				IdleConnTimeout:       clientTimeout,
				ResponseHeaderTimeout: clientTimeout,
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
// hex id is not_found and is not sent.
func (c *Client) Launch(ctx context.Context, packageID string) error {
	if !sha256Hex(packageID) {
		return ErrNotFound
	}
	return c.do(ctx, http.MethodPost, "/v1/local/cores/"+packageID+"/launch", nil)
}

// Stop asks the agent to return the kit to the menu when this process
// holds the kit-local lease.
func (c *Client) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/local/stop", nil)
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	if c == nil || c.http == nil || c.path == "" {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://local-control"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxClientBody))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyStatus(resp.StatusCode, body)
	}
	if out == nil || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
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
