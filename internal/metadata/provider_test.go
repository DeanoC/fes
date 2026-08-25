package metadata

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testIGDBPlatformsJSON() string {
	return `[{"id":1,"name":"Sega Mega Drive/Genesis","slug":"genesis-slash-megadrive","checksum":"platform-checksum","updated_at":1},{"id":2,"name":"Super Nintendo Entertainment System","slug":"snes","checksum":"snes-checksum","updated_at":1},{"id":3,"name":"Nintendo Entertainment System","slug":"nes","checksum":"nes-checksum","updated_at":1},{"id":4,"name":"Sega Master System/Mark III","slug":"sms","checksum":"sms-checksum","updated_at":1},{"id":5,"name":"Game Boy","slug":"gb","checksum":"gb-checksum","updated_at":1},{"id":6,"name":"Game Boy Advance","slug":"gba","checksum":"gba-checksum","updated_at":1},{"id":7,"name":"TurboGrafx-16/PC Engine","slug":"turbografx16--1","checksum":"pce-checksum","updated_at":1},{"id":8,"name":"Sega Game Gear","slug":"game-gear","checksum":"gg-checksum","updated_at":1},{"id":9,"name":"Game Boy Color","slug":"gbc","checksum":"gbc-checksum","updated_at":1},{"id":10,"name":"Atari 2600","slug":"atari2600","checksum":"a2600-checksum","updated_at":1},{"id":11,"name":"ColecoVision","slug":"colecovision","checksum":"coleco-checksum","updated_at":1},{"id":12,"name":"Atari Lynx","slug":"lynx","checksum":"lynx-checksum","updated_at":1},{"id":13,"name":"WonderSwan","slug":"wonderswan","checksum":"ws-checksum","updated_at":1},{"id":14,"name":"WonderSwan Color","slug":"wonderswan-color","checksum":"wsc-checksum","updated_at":1},{"id":15,"name":"Atari 7800","slug":"atari7800","checksum":"a7800-checksum","updated_at":1},{"id":16,"name":"Intellivision","slug":"intellivision","checksum":"intv-checksum","updated_at":1}]`
}

func TestIGDBProviderUsesOfficialTokenAndAPIWireContract(t *testing.T) {
	var requests []*http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Clone(request.Context()))
		body := "{}"
		contentType := "application/json"
		switch request.URL.Host {
		case "id.twitch.tv":
			body = `{"access_token":"fixture-token","expires_in":3600,"token_type":"bearer"}`
		case "api.igdb.com":
			if request.URL.Path == "/v4/platforms" {
				body = testIGDBPlatformsJSON()
			} else {
				body = `[{"id":99,"name":"Sonic the Hedgehog","platforms":[1],"summary":"A summary","first_release_date":662688000,"genres":[{"name":"Platformer"}],"involved_companies":[{"developer":true,"company":{"name":"SEGA"}}],"cover":{"image_id":"cover-id"},"artworks":[{"image_id":"backdrop-id"}],"checksum":"game-checksum","updated_at":2}]`
			}
		default:
			t.Fatalf("unexpected host %q", request.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "client-id", ClientSecret: "client-secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Lookup(context.Background(), ProviderQuery{NormalizedTitle: "sonic", System: "megadrive", Region: ""})
	if err != nil {
		t.Logf("requests=%d", len(requests))
		for index, request := range requests {
			t.Logf("request %d method=%s url=%s", index, request.Method, request.URL.Redacted())
		}
		if op, ok := err.(*OpError); ok {
			t.Fatalf("err=%v cause=%v", err, op.cause)
		}
		t.Fatal(err)
	}
	if result.PlatformID != "1" || len(result.Candidates) != 1 || result.Candidates[0].ProviderID != "99" {
		t.Fatalf("result = %#v", result)
	}
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want token/platform/games", len(requests))
	}
	token := requests[0]
	tokenBody, _ := io.ReadAll(token.Body)
	form, formErr := url.ParseQuery(string(tokenBody))
	if token.Method != http.MethodPost || token.URL.Scheme != "https" || token.URL.Host != "id.twitch.tv" || token.URL.Path != "/oauth2/token" || token.URL.RawQuery != "" || token.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || formErr != nil || form.Get("grant_type") != "client_credentials" || form.Get("client_id") != "client-id" || form.Get("client_secret") != "client-secret" {
		t.Fatalf("token request = %s %s", token.Method, token.URL.Redacted())
	}
	for _, request := range requests[1:] {
		if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.Host != "api.igdb.com" || request.Header.Get("Client-ID") != "client-id" || request.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatalf("API request = %s %s headers=%v", request.Method, request.URL.Redacted(), request.Header)
		}
		if request.URL.RawQuery != "" {
			t.Fatalf("API request unexpectedly has query: %s", request.URL.Redacted())
		}
	}
	platformBody, _ := io.ReadAll(requests[1].Body)
	if !strings.Contains(string(platformBody), `fields id,name,slug,checksum,updated_at`) || !strings.Contains(string(platformBody), `limit 16`) {
		t.Fatalf("platform body = %q", platformBody)
	}
	gameBody, _ := io.ReadAll(requests[2].Body)
	if !strings.Contains(string(gameBody), `limit 20`) || !strings.Contains(string(gameBody), `where platforms = 1`) || strings.Contains(string(gameBody), `fields *`) {
		t.Fatalf("game body = %q", gameBody)
	}
	_ = provider.Close()
}

func TestIGDBProviderMapsSafeHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   ErrorCode
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrPolicyBlocked},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusBadGateway, ErrUpstreamUnavailable},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := `{"error":"fixture secret body"}`
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{"3"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})}
			provider, err := NewIGDBProvider(IGDBConfig{ClientID: "id", ClientSecret: "secret", HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Lookup(context.Background(), ProviderQuery{NormalizedTitle: "sonic", System: "megadrive"})
			if opCode(err) != tc.code || strings.Contains(err.Error(), "fixture") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err = %v code=%s", err, opCode(err))
			}
		})
	}
}

func TestIGDBResolvedPlatformCardinalityFollowsExpectedSlugs(t *testing.T) {
	expected := map[string]string{"one": "One", "two": "Two", "three": "Three"}
	resolved := map[string]resolvedPlatform{
		"one": {ID: "1"}, "two": {ID: "2"}, "three": {ID: "3"},
	}
	if !hasExactPlatformSlugs(resolved, expected) {
		t.Fatal("three table-derived cover slugs were rejected")
	}
	delete(resolved, "three")
	resolved["other"] = resolvedPlatform{ID: "3"}
	if hasExactPlatformSlugs(resolved, expected) {
		t.Fatal("wrong cover slug set was accepted by cardinality alone")
	}
}

func (code ErrorCode) String() string { return string(code) }

func TestIGDBProviderHonorsCanceledContext(t *testing.T) {
	provider, err := NewIGDBProvider(IGDBConfig{ClientID: "id", ClientSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = provider.Lookup(ctx, ProviderQuery{NormalizedTitle: "sonic", System: "megadrive"})
	if opCode(err) != ErrDeadline && opCode(err) != ErrCanceled {
		t.Fatalf("err = %v code=%s", err, opCode(err))
	}
}
