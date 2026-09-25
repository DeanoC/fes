package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/targetclient"
)

const (
	defaultBridgeReadyWait = 5 * time.Second
	maxBridgeRequestBytes  = 8 << 10
)

// HTTPBridgeStarter requests a target-owned input bridge through the
// authenticated MiSTer agent API. The target agent owns the uinput device and
// bridge process; the host only owns the session lease and data connection.
type HTTPBridgeStarterConfig struct {
	KitLease     *targetclient.KitLease
	BaseURL      *url.URL
	Token        string
	HTTPClient   *http.Client
	ReadyTimeout time.Duration
	DialTimeout  time.Duration
}

type HTTPBridgeStarter struct {
	mu             sync.Mutex
	kitLease       *targetclient.KitLease
	kitLeaseSource func() *targetclient.KitLease
	baseURL        url.URL
	token          string
	httpClient     *http.Client
	readyTimeout   time.Duration
	dialTimeout    time.Duration
}

func NewHTTPBridgeStarter(config HTTPBridgeStarterConfig) (*HTTPBridgeStarter, error) {
	if config.BaseURL == nil || config.BaseURL.Scheme != "http" || config.BaseURL.Host == "" || config.BaseURL.User != nil || config.BaseURL.Path != "" || config.BaseURL.RawQuery != "" || config.BaseURL.Fragment != "" {
		return nil, ErrRemoteInputInvalid
	}
	if strings.TrimSpace(config.Token) == "" {
		return nil, ErrRemoteInputInvalid
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.ReadyTimeout <= 0 {
		config.ReadyTimeout = defaultBridgeReadyWait
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultDialTimeout
	}
	baseURL := *config.BaseURL
	return &HTTPBridgeStarter{
		baseURL:      baseURL,
		kitLease:     config.KitLease,
		token:        config.Token,
		httpClient:   config.HTTPClient,
		readyTimeout: config.ReadyTimeout,
		dialTimeout:  config.DialTimeout,
	}, nil
}

func (s *HTTPBridgeStarter) Start(ctx context.Context, spec BridgeSpec) (BridgeHandle, error) {
	if s == nil || ctx == nil || spec.Session == 0 || len(spec.Token) < 16 || validateBridgeCore(spec.Core) != nil {
		return nil, ErrRemoteInputInvalid
	}
	requestBody := struct {
		Session uint64 `json:"session"`
		Token   string `json:"token"`
		Core    string `json:"core"`
	}{Session: spec.Session, Token: hex.EncodeToString(spec.Token), Core: spec.Core}
	var response struct {
		Ready bool `json:"ready"`
	}
	var sentKitToken string
	if err := s.doJSONRequest(ctx, http.MethodPost, "/v1/input/attach", requestBody, &response, "", &sentKitToken); err != nil || !response.Ready {
		return nil, ErrRemoteInputInvalid
	}
	handle := &httpBridgeHandle{
		starter:  s,
		kitToken: sentKitToken,
		session:  spec.Session,
		ready:    make(chan struct{}),
	}
	close(handle.ready)
	return handle, nil
}

func (s *HTTPBridgeStarter) doJSON(ctx context.Context, method, path string, body any, result any) error {
	return s.doJSONWithToken(ctx, method, path, body, result, "")
}
func (s *HTTPBridgeStarter) doJSONWithToken(ctx context.Context, method, path string, body any, result any, kitToken string) error {
	return s.doJSONRequest(ctx, method, path, body, result, kitToken, nil)
}
func (s *HTTPBridgeStarter) doJSONRequest(ctx context.Context, method, path string, body any, result any, kitToken string, sentKitToken *string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if len(encoded) > maxBridgeRequestBytes {
		return ErrRemoteInputInvalid
	}
	request, err := http.NewRequestWithContext(ctx, method, s.endpoint(path).String(), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	_, token, lease := s.currentOrigin()
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if kitToken != "" {
		if err := lease.AuthorizeExisting(request, kitToken); err != nil {
			return err
		}
	} else if err := lease.Authorize(request, path == "/v1/input/attach"); err != nil {
		return err
	}
	// Bind an input handle to the exact grant used for dispatch, even when
	// another session replaces ownership while the response is in flight.
	if sentKitToken != nil {
		*sentKitToken = request.Header.Get(targetclient.KitLeaseHeader)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBridgeRequestBytes+1))
	if err != nil || len(data) > maxBridgeRequestBytes {
		return ErrRemoteInputInvalid
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ErrRemoteInputInvalid
	}
	if err := json.Unmarshal(data, result); err != nil {
		return err
	}
	return nil
}

func (s *HTTPBridgeStarter) currentOrigin() (url.URL, string, *targetclient.KitLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease := s.kitLease
	if s.kitLeaseSource != nil {
		if current := s.kitLeaseSource(); current != nil {
			lease = current
		}
	}
	return s.baseURL, s.token, lease
}

func (s *HTTPBridgeStarter) endpoint(path string) *url.URL {
	baseURL, _, lease := s.currentOrigin()
	endpoint := baseURL
	if lease != nil {
		endpoint = *lease.Endpoint()
	}
	endpoint.Path = path
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return &endpoint
}

type httpBridgeHandle struct {
	kitToken string
	starter  *HTTPBridgeStarter
	session  uint64
	ready    chan struct{}
	mu       sync.Mutex
	stopped  bool
}

func (h *httpBridgeHandle) Ready() <-chan struct{} { return h.ready }
func (h *httpBridgeHandle) Endpoint() string       { return h.starter.endpoint("").Host }

// Dial opens an authenticated CONNECT stream through the already-managed
// agent tunnel. The target API proxies the stream to its target-owned bridge.
func (h *httpBridgeHandle) Dial(ctx context.Context) (net.Conn, error) {
	if h == nil || h.starter == nil || ctx == nil {
		return nil, ErrRemoteInputInvalid
	}
	h.mu.Lock()
	stopped := h.stopped
	h.mu.Unlock()
	if stopped {
		return nil, ErrRemoteInputClosed
	}
	dialer := net.Dialer{Timeout: h.starter.dialTimeout}
	dialCtx, cancel := context.WithTimeout(ctx, h.starter.dialTimeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", h.starter.endpoint("").Host)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, ErrRemoteInputNoStream
	}
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Path: "/v1/input/stream"},
		Host:   h.starter.endpoint("").Host,
		Header: make(http.Header),
	}
	_, token, lease := h.starter.currentOrigin()
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-FogCast-Input-Session", strconv.FormatUint(h.session, 10))
	if err := lease.AuthorizeExisting(request, h.kitToken); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := request.Write(conn); err != nil {
		_ = conn.Close()
		return nil, ErrRemoteInputInvalid
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil || response.StatusCode != http.StatusOK {
		_ = conn.Close()
		if err == nil {
			return nil, ErrRemoteInputNoStream
		}
		return nil, ErrRemoteInputInvalid
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	return conn, nil
}

func (h *httpBridgeHandle) Stop(ctx context.Context) error {
	if h == nil || h.starter == nil {
		return nil
	}
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil
	}
	h.stopped = true
	h.mu.Unlock()
	_, _, lease := h.starter.currentOrigin()
	if lease != nil && lease.CurrentToken() != h.kitToken {
		return targetclient.ErrKitLeaseLost
	}
	requestBody := struct {
		Session uint64 `json:"session"`
		Reason  string `json:"reason"`
	}{Session: h.session, Reason: "detach"}
	if err := h.starter.doJSONWithToken(ctxOrBackground(ctx), http.MethodPost, "/v1/input/detach", requestBody, &struct {
		Ready bool `json:"ready"`
	}{}, h.kitToken); err != nil {
		return ErrRemoteInputInvalid
	}
	return nil
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// BridgeDialer is implemented by target-backed bridge handles. Local loopback
// test handles may omit it and use Endpoint instead.
type BridgeDialer interface {
	Dial(context.Context) (net.Conn, error)
}

var _ BridgeStarter = (*HTTPBridgeStarter)(nil)
var _ BridgeHandle = (*httpBridgeHandle)(nil)
var _ BridgeDialer = (*httpBridgeHandle)(nil)

// WithKitLease must be called at composition time before starting input.
func (s *HTTPBridgeStarter) WithKitLease(lease *targetclient.KitLease) {
	s.mu.Lock()
	s.kitLease = lease
	s.mu.Unlock()
}

// WithKitLeaseSource resolves the current session target's lease at attach time.
func (s *HTTPBridgeStarter) WithKitLeaseSource(source func() *targetclient.KitLease) {
	s.mu.Lock()
	s.kitLeaseSource = source
	s.mu.Unlock()
}

// SetOrigin updates the fallback agent origin used when no kit lease is bound.
func (s *HTTPBridgeStarter) SetOrigin(baseURL *url.URL, token string) error {
	if baseURL == nil || baseURL.Scheme != "http" || baseURL.Host == "" || baseURL.User != nil || baseURL.Path != "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return ErrRemoteInputInvalid
	}
	if strings.TrimSpace(token) == "" {
		return ErrRemoteInputInvalid
	}
	copied := *baseURL
	s.mu.Lock()
	s.baseURL = copied
	s.token = token
	s.mu.Unlock()
	return nil
}
