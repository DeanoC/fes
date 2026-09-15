package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	maxResponseBytes         = 1 << 20
	defaultHTTPClientTimeout = 5 * time.Second
)

type Client struct {
	endpointMu sync.RWMutex
	kitLease   *KitLease
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

func NewClient(baseURL *url.URL, token string, httpClient *http.Client) *Client {
	baseCopy := *baseURL
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	boundedClient := *httpClient
	// An agent redirect is not an authenticated endpoint selection.
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: &baseCopy, token: token, httpClient: &boundedClient}
}

func (c *Client) Health(ctx context.Context) (protocol.Health, error) {
	var health protocol.Health
	err := c.doJSON(ctx, http.MethodGet, "/v1/health", nil, &health)
	return health, err
}

func (c *Client) Status(ctx context.Context) (protocol.Status, error) {
	var status protocol.Status
	err := c.doJSON(ctx, http.MethodGet, "/v1/status", nil, &status)
	return status, err
}

func (c *Client) Launch(ctx context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
	var status protocol.Status
	err := c.doJSON(ctx, http.MethodPost, "/v1/launch", request, &status)
	return status, err
}

func (c *Client) Stop(ctx context.Context) (protocol.Status, error) {
	var status protocol.Status
	err := c.doJSON(ctx, http.MethodPost, "/v1/stop", nil, &status)
	return status, err
}

type CastStatus struct {
	State      string                    `json:"state"`
	Session    string                    `json:"session,omitempty"`
	Generation uint64                    `json:"generation,omitempty"`
	Media      *protocol.CastStatusMedia `json:"media,omitempty"`
}

func (c *Client) CastStart(ctx context.Context, session, token string, generation uint64) (CastStatus, error) {
	var status CastStatus
	err := c.doJSON(ctx, http.MethodPost, "/v1/cast/start", struct {
		Session    string `json:"session"`
		Token      string `json:"token"`
		Generation uint64 `json:"generation"`
	}{Session: session, Token: token, Generation: generation}, &status)
	return status, err
}

// CastStartWithMedia uses the versioned media admission extension. Callers
// needing compatibility with legacy peers should continue to use CastStart.
func (c *Client) CastStartWithMedia(ctx context.Context, session, token string, generation uint64, media protocol.CastMediaSet) (CastStatus, error) {
	if err := protocol.ValidateCastMediaSet(media); err != nil {
		return CastStatus{}, err
	}
	var status CastStatus
	err := c.doJSON(ctx, http.MethodPost, "/v1/cast/start", struct {
		Session    string                `json:"session"`
		Token      string                `json:"token"`
		Generation uint64                `json:"generation"`
		Media      protocol.CastMediaSet `json:"media"`
	}{Session: session, Token: token, Generation: generation, Media: media}, &status)
	if err == nil {
		if acknowledgementErr := protocol.ValidateCastMediaAcknowledgement(media, status.Media); acknowledgementErr != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), defaultHTTPClientTimeout)
			_, stopErr := c.CastStop(stopCtx, session, generation)
			cancel()
			return CastStatus{}, errors.Join(acknowledgementErr, stopErr)
		}
	}
	return status, err
}

func (c *Client) CastStop(ctx context.Context, session string, generation uint64) (CastStatus, error) {
	var status CastStatus
	err := c.doJSON(ctx, http.MethodPost, "/v1/cast/stop", struct {
		Session    string `json:"session"`
		Generation uint64 `json:"generation"`
	}{Session: session, Generation: generation}, &status)
	return status, err
}

func (c *Client) CastStatus(ctx context.Context) (CastStatus, error) {
	var status CastStatus
	err := c.doJSON(ctx, http.MethodGet, "/v1/cast/status", nil, &status)
	return status, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	return c.doJSONQuery(ctx, method, path, nil, requestBody, responseBody)
}

func (c *Client) doJSONQuery(ctx context.Context, method, path string, query url.Values, requestBody any, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, query).String(), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	if err := c.authorizeMutation(request); err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	return decodeResponse(response, responseBody)
}

func (c *Client) endpoint(path string, query url.Values) *url.URL {
	c.endpointMu.RLock()
	endpoint := *c.baseURL
	c.endpointMu.RUnlock()
	endpoint.Path = path
	endpoint.RawPath = ""
	endpoint.RawQuery = query.Encode()
	endpoint.Fragment = ""
	return &endpoint
}

func decodeResponse(response *http.Response, responseBody any) error {
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var envelope protocol.ErrorEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("decode HTTP %d error: %w", response.StatusCode, err)
		}
		if envelope.Error.Code == "" {
			return fmt.Errorf("HTTP %d response has no symbolic error code", response.StatusCode)
		}
		return &envelope.Error
	}
	if err := json.Unmarshal(data, responseBody); err != nil {
		return fmt.Errorf("decode HTTP %d response: %w", response.StatusCode, err)
	}
	return nil
}
