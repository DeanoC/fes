package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

const contentDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestProbeContentRequestContractAndAbsentResponse(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.EscapedPath() != "/v2/cache/snes/"+contentDigest {
			t.Errorf("escaped path = %q", r.URL.EscapedPath())
		}
		if r.URL.RawQuery != "extension=sfc" {
			t.Errorf("raw query = %q, want extension=sfc", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		if r.Header.Get("Content-Type") != "" {
			t.Errorf("content type = %q, want empty", r.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(w, `{"present":false}`)
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL + "/discarded?private=1#discarded")
	if err != nil {
		t.Fatal(err)
	}
	response, err := host.NewClient(baseURL, "test-token", server.Client()).ProbeContent(context.Background(), protocol.SystemSNES, content)
	if err != nil {
		t.Fatal(err)
	}
	if response.Present || response.System != nil || response.Content != nil {
		t.Fatalf("response = %#v, want absent", response)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestProbeContentAcceptsExactPresentResponse(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	transport := &staticResponseTransport{body: `{"present":true,"system":"snes","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`}
	response, err := contentClientWithTransport(transport).ProbeContent(context.Background(), protocol.SystemSNES, content)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Present || response.System == nil || *response.System != protocol.SystemSNES || response.Content == nil || *response.Content != content {
		t.Fatalf("response = %#v", response)
	}
}

func TestUploadContentRequestContract(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "md"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/v2/cache/megadrive/"+contentDigest {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.URL.RawQuery != "extension=md" {
			t.Errorf("raw query = %q, want extension=md", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("content type = %q", got)
		}
		if r.ContentLength != content.Size {
			t.Errorf("content length = %d, want %d", r.ContentLength, content.Size)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != "rom" {
			t.Errorf("body = %q, want rom", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"created","system":"megadrive","content":{"sha256":"`+contentDigest+`","size":3,"extension":"md"}}`)
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	response, err := host.NewClient(baseURL, "test-token", server.Client()).UploadContent(context.Background(), protocol.SystemMegaDrive, content, strings.NewReader("rom"))
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != protocol.CacheUploadCreated || response.System != protocol.SystemMegaDrive || response.Content != content {
		t.Fatalf("response = %#v", response)
	}
}

func TestLaunchContentRequestContract(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	want := protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: content}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v2/launch" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		const wantJSON = `{"game_id":"snes-synthetic","system":"snes","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`
		if string(body) != wantJSON {
			t.Errorf("body = %s, want %s", body, wantJSON)
		}
		_, _ = io.WriteString(w, `{"status":{"state":"active","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"`+contentDigest+`","size":3,"extension":"sfc"}}`)
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	response, err := host.NewClient(baseURL, "test-token", server.Client()).LaunchContent(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status.State != protocol.StateActive || response.Content != content {
		t.Fatalf("response = %#v", response)
	}
}

func TestContentClientValidatesBeforeSending(t *testing.T) {
	transport := &countingResponseTransport{}
	client := contentClientWithTransport(transport)
	valid := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "probe system", call: func() error { _, err := client.ProbeContent(context.Background(), "../../private", valid); return err }},
		{name: "probe digest", call: func() error {
			invalid := valid
			invalid.SHA256 = "../private"
			_, err := client.ProbeContent(context.Background(), protocol.SystemSNES, invalid)
			return err
		}},
		{name: "upload extension", call: func() error {
			invalid := valid
			invalid.Extension = ".SFC?private=1"
			_, err := client.UploadContent(context.Background(), protocol.SystemSNES, invalid, strings.NewReader("rom"))
			return err
		}},
		{name: "upload size", call: func() error {
			invalid := valid
			invalid.Size = 0
			_, err := client.UploadContent(context.Background(), protocol.SystemSNES, invalid, strings.NewReader(""))
			return err
		}},
		{name: "launch game", call: func() error {
			_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "../private", System: protocol.SystemSNES, Content: valid})
			return err
		}},
		{name: "launch system", call: func() error {
			_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: "../../private", Content: valid})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("invalid request sent")
			}
		})
	}
	if transport.calls != 0 {
		t.Fatalf("transport calls = %d, want 0", transport.calls)
	}
}

func TestContentClientRejectsMismatchedResponses(t *testing.T) {
	valid := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	otherDigest := strings.Repeat("a", 64)
	tests := []struct {
		name     string
		response string
		call     func(*host.Client) error
	}{
		{
			name:     "probe system",
			response: `{"present":true,"system":"megadrive","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.ProbeContent(context.Background(), protocol.SystemSNES, valid)
				return err
			},
		},
		{
			name:     "probe content",
			response: `{"present":true,"system":"snes","content":{"sha256":"` + otherDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.ProbeContent(context.Background(), protocol.SystemSNES, valid)
				return err
			},
		},
		{
			name:     "absent probe with identity",
			response: `{"present":false,"system":"snes","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.ProbeContent(context.Background(), protocol.SystemSNES, valid)
				return err
			},
		},
		{
			name:     "upload system",
			response: `{"result":"created","system":"megadrive","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.UploadContent(context.Background(), protocol.SystemSNES, valid, strings.NewReader("rom"))
				return err
			},
		},
		{
			name:     "upload content",
			response: `{"result":"created","system":"snes","content":{"sha256":"` + otherDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.UploadContent(context.Background(), protocol.SystemSNES, valid, strings.NewReader("rom"))
				return err
			},
		},
		{
			name:     "upload missing result",
			response: `{"system":"snes","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.UploadContent(context.Background(), protocol.SystemSNES, valid, strings.NewReader("rom"))
				return err
			},
		},
		{
			name:     "upload unknown result",
			response: `{"result":"replaced","system":"snes","content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.UploadContent(context.Background(), protocol.SystemSNES, valid, strings.NewReader("rom"))
				return err
			},
		},
		{
			name:     "launch content",
			response: `{"status":{"state":"active","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + otherDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch status system",
			response: `{"status":{"state":"active","game_id":"snes-synthetic","system":"megadrive","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch missing state",
			response: `{"status":{"game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch unknown state",
			response: `{"status":{"state":"ready","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch non-active state",
			response: `{"status":{"state":"idle","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch missing game ID",
			response: `{"status":{"state":"active","game_id":null,"system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
		{
			name:     "launch mismatched game ID",
			response: `{"status":{"state":"active","game_id":"snes-other","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + contentDigest + `","size":3,"extension":"sfc"}}`,
			call: func(client *host.Client) error {
				_, err := client.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: valid})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &staticResponseTransport{body: tt.response}
			if err := tt.call(contentClientWithTransport(transport)); err == nil {
				t.Fatal("mismatched response accepted")
			}
		})
	}
}

func TestLaunchContentRejectsHostileCoreAndLastErrorWithoutReflection(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	request := protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: content}
	privateToken := "synthetic-private-token"
	privatePath := "/Volumes/private-library/game.sfc"
	tests := []struct {
		name   string
		mutate func(*protocol.CachedLaunchResponse)
	}{
		{name: "missing expected core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ExpectedCore = nil }},
		{name: "wrong expected core", mutate: func(response *protocol.CachedLaunchResponse) {
			value := "MegaDrive-" + privateToken + privatePath
			response.Status.ExpectedCore = &value
		}},
		{name: "oversized expected core", mutate: func(response *protocol.CachedLaunchResponse) {
			value := "SNES-" + privateToken + strings.Repeat("x", 64<<10)
			response.Status.ExpectedCore = &value
		}},
		{name: "missing observed core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ObservedCore = nil }},
		{name: "wrong observed core", mutate: func(response *protocol.CachedLaunchResponse) {
			value := "MegaDrive-" + privateToken + privatePath
			response.Status.ObservedCore = &value
		}},
		{name: "last error", mutate: func(response *protocol.CachedLaunchResponse) {
			response.Status.LastError = &protocol.APIError{
				Code: protocol.ErrorCode("PRIVATE_" + privateToken), Message: privatePath,
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gameID, system, coreName := request.GameID, request.System, "SNES"
			response := protocol.CachedLaunchResponse{
				Status: protocol.Status{
					State: protocol.StateActive, GameID: &gameID, System: &system,
					ExpectedCore: &coreName, ObservedCore: &coreName,
				},
				Content: content,
			}
			test.mutate(&response)
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			_, err = contentClientWithTransport(&staticResponseTransport{body: string(body)}).LaunchContent(context.Background(), request)
			if err == nil {
				t.Fatal("hostile launch success response accepted")
			}
			if strings.Contains(err.Error(), privateToken) || strings.Contains(err.Error(), privatePath) {
				t.Fatalf("error reflects hostile response fields: %v", err)
			}
		})
	}
}

func TestContentClientBoundsResponsesAndDecodesTypedAPIErrors(t *testing.T) {
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	t.Run("oversized", func(t *testing.T) {
		transport := &staticResponseTransport{body: strings.Repeat("x", (1<<20)+1)}
		if _, err := contentClientWithTransport(transport).ProbeContent(context.Background(), protocol.SystemSNES, content); err == nil {
			t.Fatal("oversized response accepted")
		}
	})
	t.Run("typed API error", func(t *testing.T) {
		transport := &staticResponseTransport{status: http.StatusInsufficientStorage, body: `{"error":{"code":"CACHE_FULL","message":"capacity unavailable"}}`}
		_, err := contentClientWithTransport(transport).UploadContent(context.Background(), protocol.SystemSNES, content, strings.NewReader("rom"))
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeCacheFull {
			t.Fatalf("error = %#v, want CACHE_FULL API error", err)
		}
	})
}

func TestUploadContentIsOneShotAndPropagatesBodyReadFailure(t *testing.T) {
	privatePath := "/Volumes/private-library/synthetic.sfc"
	readFailure := errors.New("read " + privatePath + ": interrupted")
	reader := &failingCountingReader{data: []byte("r"), err: readFailure}
	transport := &failingUploadTransport{}
	client := contentClientWithTransport(transport)
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}

	_, err := client.UploadContent(context.Background(), protocol.SystemSNES, content, reader)
	if err == nil {
		t.Fatal("upload succeeded")
	}
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeTransferFailed {
		t.Fatalf("error = %#v, want TRANSFER_FAILED", err)
	}
	if !errors.Is(err, readFailure) {
		t.Fatalf("error does not preserve body read failure: %v", err)
	}
	if strings.Contains(err.Error(), privatePath) {
		t.Fatalf("error exposes source path: %q", err)
	}
	if transport.calls != 1 {
		t.Fatalf("transport calls = %d, want 1", transport.calls)
	}
	if transport.sawGetBody {
		t.Fatal("upload request exposed GetBody and could be replayed")
	}
	if reader.reads != 2 || reader.bytesRead != 1 {
		t.Fatalf("reader = %d reads, %d bytes, want 2 reads and 1 byte", reader.reads, reader.bytesRead)
	}
}

func TestUploadContentRequestBodyCloseReachesOriginalSource(t *testing.T) {
	source := &closeTrackingReader{
		Reader: strings.NewReader("rom"),
		closed: make(chan struct{}),
	}
	transport := &closingFailureTransport{failure: errors.New("synthetic transport failure")}
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}

	_, err := contentClientWithTransport(transport).UploadContent(context.Background(), protocol.SystemSNES, content, source)
	if err == nil {
		t.Fatal("upload succeeded")
	}
	select {
	case <-source.closed:
	default:
		t.Fatal("closing the HTTP request body did not close the original upload source")
	}
}

func TestContentMethodsUseCallerContextsIndependently(t *testing.T) {
	type contextKey struct{}
	transport := &contextResponseTransport{t: t, values: map[string]string{
		http.MethodGet + " /v2/cache/snes/" + contentDigest: "request",
		http.MethodPut + " /v2/cache/snes/" + contentDigest: "upload",
		http.MethodPost + " /v2/launch":                     "request",
	}}
	client := contentClientWithTransport(transport)
	content := protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}
	requestContext, cancelRequest := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "request"), time.Second)
	defer cancelRequest()
	uploadContext, cancelUpload := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "upload"), time.Minute)
	defer cancelUpload()
	transport.contextValue = func(ctx context.Context) string {
		value, _ := ctx.Value(contextKey{}).(string)
		return value
	}

	if _, err := client.ProbeContent(requestContext, protocol.SystemSNES, content); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadContent(uploadContext, protocol.SystemSNES, content, strings.NewReader("rom")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LaunchContent(requestContext, protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: content}); err != nil {
		t.Fatal(err)
	}
}

func contentClientWithTransport(transport http.RoundTripper) *host.Client {
	baseURL, _ := url.Parse("http://fogcast.invalid/discarded?private=1#discarded")
	return host.NewClient(baseURL, "test-token", &http.Client{Transport: transport})
}

type countingResponseTransport struct {
	calls int
}

func (t *countingResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected request")
}

type staticResponseTransport struct {
	status int
	body   string
}

func (t *staticResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	status := t.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    request,
	}, nil
}

type failingCountingReader struct {
	data      []byte
	err       error
	reads     int
	bytesRead int
}

func (r *failingCountingReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		r.bytesRead += n
		return n, nil
	}
	return 0, r.err
}

type failingUploadTransport struct {
	calls      int
	sawGetBody bool
}

type closeTrackingReader struct {
	io.Reader
	once   sync.Once
	closed chan struct{}
}

func (r *closeTrackingReader) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

type closingFailureTransport struct {
	failure error
}

func (t *closingFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := request.Body.Close(); err != nil {
		return nil, err
	}
	return nil, t.failure
}

func (t *failingUploadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	t.sawGetBody = request.GetBody != nil
	_, err := io.Copy(io.Discard, request.Body)
	if err == nil {
		err = errors.New("transport interrupted")
	}
	return nil, err
}

type contextResponseTransport struct {
	t            *testing.T
	mu           sync.Mutex
	values       map[string]string
	contextValue func(context.Context) string
}

func (t *contextResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := request.Method + " " + request.URL.Path
	want, ok := t.values[key]
	if !ok {
		t.t.Errorf("unexpected request %s", key)
	}
	if got := t.contextValue(request.Context()); got != want {
		t.t.Errorf("%s context value = %q, want %q", key, got, want)
	}

	var response any
	switch request.Method {
	case http.MethodGet:
		response = protocol.CacheProbeResponse{Present: false}
	case http.MethodPut:
		response = protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: protocol.SystemSNES, Content: protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"}}
	case http.MethodPost:
		system := protocol.SystemSNES
		gameID, expectedCore, observedCore := "snes-synthetic", "SNES", "SNES"
		response = protocol.CachedLaunchResponse{
			Status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expectedCore, ObservedCore: &observedCore},
			Content: protocol.ContentIdentity{SHA256: contentDigest, Size: 3, Extension: "sfc"},
		}
	}
	body, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: request}, nil
}
