package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/librarymedia"
	"github.com/DeanoC/FogCast/protocol"
)

type overlayMediaService struct {
	*fakeService
	media librarymedia.GameMedia
}

func (s overlayMediaService) CoverHandle(context.Context, string) string {
	return s.media.Cover
}

func (s overlayMediaService) GameMedia(context.Context, string) (librarymedia.GameMedia, error) {
	return s.media, nil
}

type presentationWire struct {
	GameID       string `json:"game_id"`
	State        string `json:"state"`
	Presentation *struct {
		Summary string `json:"summary"`
		Year    string `json:"year"`
		Genre   string `json:"genre"`
		Studio  string `json:"studio"`
		Players string `json:"players"`
	} `json:"presentation"`
	Attribution *struct {
		Provider string `json:"provider"`
		Label    string `json:"label"`
	} `json:"attribution"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestPresentationWireUsesSeparateReadyDTOAndSafeOutcomeStates(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	ready := presentationMetadata{result: metadata.Result{
		Outcome:      metadata.OutcomeExact,
		Presentation: metadata.Presentation{Summary: "summary", Year: "1991", Genre: "Platform", Studio: "SEGA", Players: "1"},
		Attribution:  metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"},
	}}
	handler := hostapi.New(service, hostapi.WithMetadata(ready, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d body=%s", response.Code, response.Body.String())
	}
	var wire presentationWire
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.GameID != "sonic" || wire.State != "ready" || wire.Presentation == nil || wire.Presentation.Summary != "summary" || wire.Attribution == nil || wire.Attribution.Provider != "igdb" || wire.Attribution.Label != "Data from IGDB.com" || wire.Error != nil {
		t.Fatalf("ready wire = %#v body=%s", wire, response.Body.String())
	}

	for _, outcome := range []struct {
		name  string
		state metadata.Outcome
		want  string
	}{
		{name: "no match", state: metadata.OutcomeNoMatch, want: "no_match"},
		{name: "ambiguous", state: metadata.OutcomeAmbiguous, want: "ambiguous"},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{result: metadata.Result{Outcome: outcome.state}}, metadata.StateReady))
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
			if result.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", result.Code, result.Body.String())
			}
			var safe presentationWire
			if err := json.Unmarshal(result.Body.Bytes(), &safe); err != nil {
				t.Fatal(err)
			}
			if safe.GameID != "sonic" || safe.State != outcome.want || safe.Presentation != nil || safe.Attribution != nil || safe.Error != nil {
				t.Fatalf("safe wire = %#v body=%s", safe, result.Body.String())
			}
		})
	}
}

func TestPresentationWirePreservesAllEmptyReadyFieldsWithoutFallback(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{result: metadata.Result{
		Outcome:      metadata.OutcomeExact,
		Presentation: metadata.Presentation{},
		Attribution:  metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"},
	}}, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("all-empty ready status = %d body=%s", response.Code, response.Body.String())
	}
	var wire presentationWire
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.State != "ready" || wire.Presentation == nil || wire.Presentation.Summary != "" || wire.Presentation.Year != "" || wire.Presentation.Genre != "" || wire.Presentation.Studio != "" || wire.Presentation.Players != "" || wire.Attribution == nil || wire.Attribution.Label != "Data from IGDB.com" {
		t.Fatalf("all-empty ready wire = %#v body=%s", wire, response.Body.String())
	}
}

func TestPresentationWirePreservesIndependentReadyFieldsWhenOptionalTextIsEmpty(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	for _, field := range []struct {
		name string
		set  func(*metadata.Presentation)
	}{
		{name: "summary", set: func(value *metadata.Presentation) { value.Summary = "" }},
		{name: "year", set: func(value *metadata.Presentation) { value.Year = "" }},
		{name: "genre", set: func(value *metadata.Presentation) { value.Genre = "" }},
		{name: "studio", set: func(value *metadata.Presentation) { value.Studio = "" }},
		{name: "players", set: func(value *metadata.Presentation) { value.Players = "" }},
	} {
		t.Run(field.name, func(t *testing.T) {
			presentation := metadata.Presentation{Summary: "summary", Year: "1991", Genre: "genre", Studio: "studio", Players: "players"}
			field.set(&presentation)
			handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{result: metadata.Result{
				Outcome: metadata.OutcomeExact, Presentation: presentation,
				Attribution: metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"},
			}}, metadata.StateReady))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var wire presentationWire
			if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if wire.State != "ready" || wire.Presentation == nil || wire.Presentation.Summary != presentation.Summary || wire.Presentation.Year != presentation.Year || wire.Presentation.Genre != presentation.Genre || wire.Presentation.Studio != presentation.Studio || wire.Presentation.Players != presentation.Players {
				t.Fatalf("partial ready wire = %#v body=%s", wire, response.Body.String())
			}
		})
	}
}

func TestPresentationWireMapsProviderFailureToOfflineWithoutErrorPayload(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{err: &metadata.OpError{Code: metadata.ErrUpstreamUnavailable}}, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var wire presentationWire
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.GameID != "sonic" || wire.State != "offline" || wire.Error != nil || wire.Presentation != nil || wire.Attribution != nil {
		t.Fatalf("offline wire = %#v body=%s", wire, response.Body.String())
	}
}

func TestPresentationOverlayRewritesOnlyOfflineWithLocalMedia(t *testing.T) {
	handle := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := overlayMediaService{
		fakeService: &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}},
		media:       librarymedia.GameMedia{Cover: handle},
	}
	type overlayWire struct {
		GameID       string `json:"game_id"`
		State        string `json:"state"`
		Presentation *struct {
			CoverArtworkID string `json:"cover_artwork_id"`
		} `json:"presentation"`
		Attribution *struct {
			Provider string `json:"provider"`
			Label    string `json:"label"`
		} `json:"attribution"`
	}
	get := func(t *testing.T, handler http.Handler) overlayWire {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
		var wire overlayWire
		if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		if wire.GameID != "sonic" || wire.Presentation == nil || wire.Presentation.CoverArtworkID != handle || wire.Attribution != nil {
			t.Fatalf("overlay wire = %#v body=%s", wire, response.Body.String())
		}
		return wire
	}

	t.Run("offline", func(t *testing.T) {
		handler := hostapi.New(base, hostapi.WithMetadata(presentationMetadata{err: &metadata.OpError{Code: metadata.ErrUpstreamUnavailable}}, metadata.StateReady))
		if wire := get(t, handler); wire.State != "ready" {
			t.Fatalf("offline overlay state = %q", wire.State)
		}
	})
	for _, tc := range []struct {
		name  string
		state metadata.ConfigState
		want  string
	}{
		{name: "disabled", state: metadata.StateDisabled, want: "disabled"},
		{name: "unconfigured", state: metadata.StateUnconfigured, want: "unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := hostapi.New(base, hostapi.WithMetadata(nil, tc.state))
			if wire := get(t, handler); wire.State != tc.want {
				t.Fatalf("%s overlay state = %q", tc.name, wire.State)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		outcome metadata.Outcome
		want    string
	}{
		{name: "no_match", outcome: metadata.OutcomeNoMatch, want: "no_match"},
		{name: "ambiguous", outcome: metadata.OutcomeAmbiguous, want: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := hostapi.New(base, hostapi.WithMetadata(presentationMetadata{result: metadata.Result{Outcome: tc.outcome}}, metadata.StateReady))
			if wire := get(t, handler); wire.State != tc.want {
				t.Fatalf("%s overlay state = %q", tc.name, wire.State)
			}
		})
	}
}

func TestPresentationWireDisabledAndUnconfiguredRemainSuccessfulFallbackStates(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	for _, state := range []struct {
		name string
		cfg  metadata.ConfigState
		want string
	}{
		{name: "disabled", cfg: metadata.StateDisabled, want: "disabled"},
		{name: "unconfigured", cfg: metadata.StateUnconfigured, want: "unconfigured"},
	} {
		t.Run(state.name, func(t *testing.T) {
			handler := hostapi.New(service, hostapi.WithMetadata(nil, state.cfg))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var wire presentationWire
			if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if wire.GameID != "sonic" || wire.State != state.want || wire.Presentation != nil || wire.Attribution != nil || wire.Error != nil {
				t.Fatalf("fallback wire = %#v body=%s", wire, response.Body.String())
			}
		})
	}
}

func TestPresentationRouteRejectsBodiesAndBoundsUntrustedRuntimeFields(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	result := metadata.Result{Outcome: metadata.OutcomeExact, Presentation: metadata.Presentation{
		Summary: string(bytes.Repeat([]byte{'x'}, 241)), Year: "1991", Genre: "genre", Studio: "studio", Players: "1",
	}, Attribution: metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"}}
	handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{result: result}, metadata.StateReady))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", bytes.NewBufferString("poison"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("body status = %d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), bytes.Repeat([]byte{'x'}, 241)) {
		t.Fatalf("bounded status/body = %d %s", response.Code, response.Body.String())
	}
}

func TestArtworkRouteInvalidOrMissingObjectIsGenericNotFound(t *testing.T) {
	handler := hostapi.New(&fakeService{}, hostapi.WithMetadata(presentationMetadata{err: fmt.Errorf("private artwork failure")}, metadata.StateReady))
	for _, path := range []string{
		"/api/v1/presentation/artwork/ABC",
		"/api/v1/presentation/artwork/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
		if response.Code != http.StatusNotFound || !bytes.Contains(response.Body.Bytes(), []byte("ARTWORK_UNAVAILABLE")) {
			t.Fatalf("%s response = %d %s", path, response.Code, response.Body.String())
		}
	}
	content := []byte("sanitized-image")
	runtime := presentationMetadata{artwork: metadata.Artwork{MIME: "image/png", Size: int64(len(content)), Reader: io.NopCloser(bytes.NewReader(content))}}
	response := httptest.NewRecorder()
	hostapi.New(&fakeService{}, hostapi.WithMetadata(runtime, metadata.StateReady)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/artwork/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("artwork headers/status = %d %v", response.Code, response.Header())
	}
}

var _ context.Context
