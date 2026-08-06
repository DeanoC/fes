package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBridgeReadyWait = 5 * time.Second
	maxBridgeRequestBytes  = 8 << 10
)

// HTTPBridgeStarter requests a target-owned input bridge through the
// authenticated MiSTer agent API. The target agent owns the uinput device and
// bridge process; the host only owns the session lease and data connection.
type HTTPBridgeStarterConfig struct {
	BaseURL      *url.URL
	Token        string
	HTTPClient   *http.Client
	ReadyTimeout time.Duration
	DialTimeout  time.Duration
}

type HTTPBridgeStarter struct {
	baseURL      url.URL
	token        string
	httpClient   *http.Client
	readyTimeout time.Duration
	dialTimeout  time.Duration
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
	if err := s.doJSON(ctx, http.MethodPost, "/v1/input/attach", requestBody, &response); err != nil || !response.Ready {
		return nil, ErrRemoteInputInvalid
	}
	handle := &httpBridgeHandle{
		starter: s,
		session: spec.Session,
		ready:   make(chan struct{}),
	}
	close(handle.ready)
	return handle, nil
}

func (s *HTTPBridgeStarter) doJSON(ctx context.Context, method, path string, body any, result any) error {
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
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("Content-Type", "application/json")
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

func (s *HTTPBridgeStarter) endpoint(path string) *url.URL {
	endpoint := s.baseURL
	endpoint.Path = path
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return &endpoint
}

type httpBridgeHandle struct {
	starter *HTTPBridgeStarter
	session uint64
	ready   chan struct{}
	mu      sync.Mutex
	stopped bool
}

func (h *httpBridgeHandle) Ready() <-chan struct{} { return h.ready }
func (h *httpBridgeHandle) Endpoint() string       { return h.starter.baseURL.Host }

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
	conn, err := dialer.DialContext(dialCtx, "tcp", h.starter.baseURL.Host)
	if err != nil {
		return nil, ErrRemoteInputInvalid
	}
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Path: "/v1/input/stream"},
		Host:   h.starter.baseURL.Host,
		Header: make(http.Header),
	}
	request.Header.Set("Authorization", "Bearer "+h.starter.token)
	request.Header.Set("X-FogCast-Input-Session", strconv.FormatUint(h.session, 10))
	if err := request.Write(conn); err != nil {
		_ = conn.Close()
		return nil, ErrRemoteInputInvalid
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil || response.StatusCode != http.StatusOK {
		_ = conn.Close()
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
	requestBody := struct {
		Session uint64 `json:"session"`
		Reason  string `json:"reason"`
	}{Session: h.session, Reason: "detach"}
	if err := h.starter.doJSON(ctxOrBackground(ctx), http.MethodPost, "/v1/input/detach", requestBody, &struct {
		Ready bool `json:"ready"`
	}{}); err != nil {
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
