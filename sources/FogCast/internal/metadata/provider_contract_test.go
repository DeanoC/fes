package metadata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type deadlineRoundTripFunc func(*http.Request) (*http.Response, error)

func (f deadlineRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProviderRetriesEveryTransient5xxOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	payload, status, opErr := provider.doJSON(context.Background(), http.MethodGet, server.URL, nil, nil, 1024)
	if opErr != nil || status != http.StatusOK || string(payload) != `{}` || calls.Load() != 2 {
		t.Fatalf("payload=%q status=%d err=%v calls=%d", payload, status, opErr, calls.Load())
	}
}

func TestProviderRequestTimeoutMapsToDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(150 * time.Millisecond)
	}))
	defer server.Close()
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, opErr := provider.doJSON(ctx, http.MethodGet, server.URL, nil, nil, 1024)
	if opErr == nil || opErr.Code != ErrDeadline {
		t.Fatalf("expected deadline, got %#v", opErr)
	}
}

func TestProviderTransportDeadlineMapsToDeadline(t *testing.T) {
	provider, err := NewIGDBProvider(IGDBConfig{
		ClientID: "client", ClientSecret: "secret",
		HTTPClient: &http.Client{Transport: deadlineRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, opErr := provider.doJSON(context.Background(), http.MethodGet, "https://api.igdb.com/v4/test", nil, nil, 1024)
	if opErr == nil || opErr.Code != ErrDeadline {
		t.Fatalf("expected transport deadline, got %#v", opErr)
	}
}

func TestCandidateLeavesPlayersUnavailableWhenTheGameSchemaOmitsUnsupportedField(t *testing.T) {
	candidate, err := candidateFromWire(igdbGame{ID: 1, Name: "Game"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Players != "" {
		t.Fatalf("unsupported player count was fabricated: %q", candidate.Players)
	}
}

func TestRetryAfterAcceptsBoundedHTTPDate(t *testing.T) {
	now := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if got := retryAfter(now); got < time.Second || got > 3*time.Second {
		t.Fatalf("HTTP-date retry-after = %s", got)
	}
	if got := retryAfter("999999"); got != 15*time.Minute {
		t.Fatalf("clamped retry-after = %s", got)
	}
}

func TestLimiterCancellationDoesNotBlockAfterTimerFires(t *testing.T) {
	limiter := newRequestLimiter()
	if err := limiter.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer limiter.release()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := limiter.acquire(ctx); err == nil {
		t.Fatal("expected limiter cancellation")
	}
}

func TestIGDBGamesQueryUsesOnlyDocumentedGameFields(t *testing.T) {
	allowedFields := map[string]struct{}{
		"id": {}, "name": {}, "alternative_names.name": {}, "platforms": {},
		"summary": {}, "first_release_date": {}, "genres.name": {},
		"involved_companies.company.name": {}, "involved_companies.developer": {},
		"involved_companies.publisher": {}, "cover.image_id": {}, "artworks.image_id": {},
		"checksum": {}, "updated_at": {},
	}
	var rejectedField string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := "{}"
		if request.URL.Host == "id.twitch.tv" {
			body = `{"access_token":"fixture-token","expires_in":3600}`
		} else if request.URL.Path == "/v4/platforms" {
			body = testIGDBPlatformsJSON()
		} else if request.URL.Path == "/v4/games" {
			fields := strings.TrimPrefix(strings.SplitN(readRequestBody(request), ";", 2)[0], "fields ")
			for _, field := range strings.Split(fields, ",") {
				if _, ok := allowedFields[field]; !ok {
					rejectedField = field
					return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unknown field")), Request: request}, nil
				}
			}
			body = `[{"id":99,"name":"Sonic the Hedgehog","platforms":[1],"summary":"A summary","first_release_date":662688000,"genres":[{"name":"Platformer"}],"involved_companies":[{"developer":true,"company":{"name":"SEGA"}}],"checksum":"game-checksum","updated_at":2}]`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client-id", ClientSecret: "client-secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Lookup(context.Background(), ProviderQuery{NormalizedTitle: "sonic", System: "megadrive"}); err != nil {
		t.Fatalf("strict documented Game field contract rejected production query: %v (field %q)", err, rejectedField)
	}
}

func readRequestBody(request *http.Request) string {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return ""
	}
	return string(body)
}

func TestContractPlatformMappingRefreshesAfterPositiveTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	var platformCalls atomic.Int32
	client := &http.Client{Transport: deadlineRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"access_token":"fixture-token","expires_in":315360000}`
		if request.URL.Path == "/v4/platforms" {
			platformCalls.Add(1)
			body = testIGDBPlatformsJSON()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	cache, err := OpenCache(context.Background(), CacheConfig{Root: filepath.Join(t.TempDir(), "metadata"), CredentialScope: "client", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	provider.AttachCache(cache)
	if _, err := provider.ResolvePlatform(context.Background(), protocol.SystemSNES); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * 24 * time.Hour)
	if _, err := provider.ResolvePlatform(context.Background(), protocol.SystemSNES); err != nil {
		t.Fatal(err)
	}
	if got := platformCalls.Load(); got != 2 {
		t.Fatalf("expired platform mapping was not refreshed: platform calls=%d", got)
	}
}

func TestContractPlatformResolutionWaiterCancellationDoesNotWaitOnLeader(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: deadlineRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/oauth2/token" {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-token","expires_in":315360000}`)), Request: request}, nil
		}
		if request.URL.Path == "/v4/platforms" {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-release:
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(testIGDBPlatformsJSON())), Request: request}, nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		return nil, errors.New("unexpected provider request")
	})}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := provider.ResolvePlatform(context.Background(), protocol.SystemSNES)
		firstDone <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := provider.ResolvePlatform(ctx, protocol.SystemSNES)
		secondDone <- err
	}()
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled platform waiter = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-secondDone
		t.Fatal("canceled platform waiter remained blocked behind platform leader")
	}
	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("platform leader = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("platform leader did not finish")
	}
}

func TestContractInvalidDurablePlatformMappingIsNotServed(t *testing.T) {
	now := time.Unix(1000, 0)
	var platformCalls atomic.Int32
	client := &http.Client{Transport: deadlineRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v4/platforms" {
			platformCalls.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(testIGDBPlatformsJSON())), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-token","expires_in":315360000}`)), Request: request}, nil
	})}
	cache, err := OpenCache(context.Background(), CacheConfig{Root: filepath.Join(t.TempDir(), "metadata"), CredentialScope: "client", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if err := cache.SavePlatformMapping(map[string]resolvedPlatform{
		"genesis-slash-megadrive": {ID: "not-a-number", Checksum: "md-a", Updated: 1},
		"snes":                    {ID: "2", Checksum: "snes-a", Updated: 1},
	}, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	provider.AttachCache(cache)
	id, err := provider.ResolvePlatform(context.Background(), protocol.SystemSNES)
	if err != nil || id != "2" {
		t.Fatalf("resolved platform = %q err=%v", id, err)
	}
	if got := platformCalls.Load(); got != 1 {
		t.Fatalf("invalid durable mapping was served without refresh: platform calls=%d", got)
	}
}

func TestContractPlatformLeaderCancellationDoesNotPoisonSharedFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var platformCalls atomic.Int32
	client := &http.Client{Transport: deadlineRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/oauth2/token" {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-token","expires_in":315360000}`)), Request: request}, nil
		}
		if request.URL.Path == "/v4/platforms" {
			platformCalls.Add(1)
			close(started)
			<-release
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(testIGDBPlatformsJSON())), Request: request}, nil
		}
		return nil, errors.New("unexpected provider request")
	})}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := provider.ResolvePlatform(firstCtx, protocol.SystemSNES)
		firstDone <- err
	}()
	<-started
	cancel()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled platform leader = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(release)
		t.Fatal("canceled platform leader remained blocked")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := provider.ResolvePlatform(context.Background(), protocol.SystemSNES)
		secondDone <- err
	}()
	close(release)
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("shared platform flight after leader cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shared platform flight did not finish")
	}
	if got := platformCalls.Load(); got != 1 {
		t.Fatalf("leader cancellation restarted platform request: calls=%d", got)
	}
}

func TestContractProviderCloseUnblocksActiveLookup(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Transport: deadlineRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/oauth2/token":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-token","expires_in":315360000}`)), Request: request}, nil
		case "/v4/platforms":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(testIGDBPlatformsJSON())), Request: request}, nil
		case "/v4/games":
			close(started)
			<-request.Context().Done()
			return nil, request.Context().Err()
		default:
			return nil, errors.New("unexpected provider request")
		}
	})}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client", ClientSecret: "secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := provider.Lookup(context.Background(), ProviderQuery{NormalizedTitle: "sonic", System: protocol.SystemSNES})
		done <- err
	}()
	<-started
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lookup after provider close = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider Close did not unblock active lookup")
	}
}
