package hostapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/internal/metadata"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type presentationMetadata struct {
	result  metadata.Result
	err     error
	artwork metadata.Artwork
}

type recordingPresentationMetadata struct {
	presentationMetadata
	input metadata.LookupInput
}

func (m *recordingPresentationMetadata) Lookup(_ context.Context, input metadata.LookupInput) (metadata.Result, error) {
	m.input = input
	return m.result, m.err
}

type fallbackPresentationMetadata struct {
	presentationMetadata
	inputs []metadata.LookupInput
}

func (m *fallbackPresentationMetadata) Lookup(_ context.Context, input metadata.LookupInput) (metadata.Result, error) {
	m.inputs = append(m.inputs, input)
	if len(m.inputs) == 1 {
		return metadata.Result{Outcome: metadata.OutcomeNoMatch}, nil
	}
	return metadata.Result{
		Outcome: metadata.OutcomeExact,
		Presentation: metadata.Presentation{
			CoverArtworkID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		Attribution: metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"},
	}, nil
}

type ambiguousPresentationMetadata struct {
	presentationMetadata
	inputs []metadata.LookupInput
}

func (m *ambiguousPresentationMetadata) Lookup(_ context.Context, input metadata.LookupInput) (metadata.Result, error) {
	m.inputs = append(m.inputs, input)
	if len(m.inputs) == 1 {
		return metadata.Result{Outcome: metadata.OutcomeAmbiguous}, nil
	}
	return metadata.Result{
		Outcome: metadata.OutcomeExact,
		Presentation: metadata.Presentation{
			Genre: "Puzzle",
			Year:  "1989",
		},
		Attribution: metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"},
	}, nil
}

func (m presentationMetadata) Lookup(context.Context, metadata.LookupInput) (metadata.Result, error) {
	return m.result, m.err
}
func (m presentationMetadata) OpenArtwork(context.Context, string) (metadata.Artwork, error) {
	return m.artwork, m.err
}
func (m presentationMetadata) Close() error { return nil }

func TestPresentationRoutesExposeBoundedMetadataAndSafeAttribution(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemMegaDrive}}
	runtime := presentationMetadata{result: metadata.Result{Outcome: metadata.OutcomeExact, Presentation: metadata.Presentation{Summary: "summary", Year: "1991", Genre: "Platform", Studio: "SEGA", Players: "1", CoverArtworkID: "cover-handle", BackdropArtworkID: "backdrop-handle"}, Attribution: metadata.Attribution{Provider: metadata.ProviderIGDB, Label: "Data from IGDB.com"}}}
	handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	for _, forbidden := range []string{"client_secret", "image_id", "https://", "api.igdb.com"} {
		if bytes.Contains(response.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, response.Body.String())
		}
	}
	for _, expected := range []string{"\"game_id\":\"sonic\"", "\"state\":\"ready\"", "\"summary\":\"summary\"", "\"provider\":\"igdb\"", "\"label\":\"Data from IGDB.com\""} {
		if !bytes.Contains(response.Body.Bytes(), []byte(expected)) {
			t.Fatalf("response missing %s: %s", expected, response.Body.String())
		}
	}
}

func TestPresentationLookupUsesCanonicalCatalogTitle(t *testing.T) {
	service := &fakeService{game: catalog.Game{
		ID: "sonic", Title: "Sonic the Hedgehog (USA) [Rev A]", CanonicalTitle: "Sonic the Hedgehog",
		System: protocol.SystemMegaDrive,
	}}
	runtime := &recordingPresentationMetadata{presentationMetadata: presentationMetadata{result: metadata.Result{Outcome: metadata.OutcomeNoMatch}}}
	handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if runtime.input.Title != "Sonic the Hedgehog" || runtime.input.System != protocol.SystemMegaDrive {
		t.Fatalf("lookup input = %+v", runtime.input)
	}
}

func TestPresentationLookupPreservesMeaningfulParentheticalTitleSuffix(t *testing.T) {
	service := &fakeService{game: catalog.Game{
		ID: "tetris", Title: "Tetris (Nintendo) (USA)", CanonicalTitle: "Tetris",
		System: protocol.SystemGameBoy,
	}}
	runtime := &recordingPresentationMetadata{presentationMetadata: presentationMetadata{result: metadata.Result{Outcome: metadata.OutcomeExact}}}
	handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/tetris", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if runtime.input.Title != "Tetris (Nintendo) (USA)" || runtime.input.System != protocol.SystemGameBoy {
		t.Fatalf("lookup input = %+v", runtime.input)
	}
}

func TestPresentationLookupFallsBackToCanonicalCatalogTitleForDumpTags(t *testing.T) {
	for _, test := range []struct {
		name      string
		title     string
		canonical string
	}{
		{name: "quality flag", title: "Sonic the Hedgehog (USA) [!]", canonical: "Sonic the Hedgehog"},
		{name: "hack flag", title: "Sonic the Hedgehog [Hack]", canonical: "Sonic the Hedgehog"},
		{name: "prerelease flag", title: "Sonic the Hedgehog (Beta)", canonical: "Sonic the Hedgehog"},
		{name: "combined region", title: "Sonic the Hedgehog (USA, Europe)", canonical: "Sonic the Hedgehog"},
		{name: "multi digit revision", title: "Sonic the Hedgehog (Rev 10)", canonical: "Sonic the Hedgehog"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{game: catalog.Game{
				ID: "sonic", Title: test.title, CanonicalTitle: test.canonical,
				System: protocol.SystemMegaDrive,
			}}
			runtime := &fallbackPresentationMetadata{}
			handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"state":"ready"`) ||
				!strings.Contains(response.Body.String(), `"cover_artwork_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`) {
				t.Fatalf("canonical fallback did not produce ready presentation: %s", response.Body.String())
			}
			if len(runtime.inputs) != 2 || runtime.inputs[0].Title != test.title || runtime.inputs[1].Title != test.canonical {
				t.Fatalf("lookup inputs = %+v", runtime.inputs)
			}
			for _, input := range runtime.inputs {
				if input.System != protocol.SystemMegaDrive {
					t.Fatalf("lookup input = %+v", input)
				}
			}
		})
	}
}

func TestPresentationLookupPreservesMeaningfulSuffixAmbiguity(t *testing.T) {
	service := &fakeService{game: catalog.Game{
		ID: "tetris", Title: "Tetris (Nintendo) (USA)", CanonicalTitle: "Tetris",
		System: protocol.SystemGameBoy,
	}}
	runtime := &ambiguousPresentationMetadata{}
	handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/tetris", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"ambiguous"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if len(runtime.inputs) != 1 || runtime.inputs[0].Title != "Tetris (Nintendo) (USA)" || runtime.inputs[0].System != protocol.SystemGameBoy {
		t.Fatalf("lookup inputs = %+v", runtime.inputs)
	}
}

func TestPresentationRoutesMapDisabledUnconfiguredAndOutcomes(t *testing.T) {
	service := &fakeService{game: catalog.Game{ID: "sonic", Title: "Sonic", System: protocol.SystemSNES}}
	for _, test := range []struct {
		name    string
		state   metadata.ConfigState
		outcome metadata.Outcome
		status  int
		code    string
	}{
		{name: "unconfigured", state: metadata.StateUnconfigured, status: http.StatusOK, code: `"state":"unconfigured"`},
		{name: "disabled", state: metadata.StateDisabled, status: http.StatusOK, code: `"state":"disabled"`},
		{name: "no match", state: metadata.StateReady, outcome: metadata.OutcomeNoMatch, status: http.StatusOK, code: `"state":"no_match"`},
		{name: "ambiguous", state: metadata.StateReady, outcome: metadata.OutcomeAmbiguous, status: http.StatusOK, code: `"state":"ambiguous"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := hostapi.New(service, hostapi.WithMetadata(presentationMetadata{result: metadata.Result{Outcome: test.outcome}}, test.state))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/games/sonic", nil))
			if response.Code != test.status || !bytes.Contains(response.Body.Bytes(), []byte(test.code)) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestArtworkRouteServesOnlyOpaqueHandlesWithSecurityHeaders(t *testing.T) {
	service := &fakeService{}
	content := []byte("sanitized-image")
	runtime := presentationMetadata{artwork: metadata.Artwork{MIME: "image/png", Size: int64(len(content)), Reader: io.NopCloser(bytes.NewReader(content))}}
	handler := hostapi.New(service, hostapi.WithMetadata(runtime, metadata.StateReady))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/presentation/artwork/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("artwork response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.Bytes())
	}
	for _, rawURL := range []string{"/api/v1/presentation/artwork/../secret", "/api/v1/presentation/artwork/ABC?x=1"} {
		bad := httptest.NewRecorder()
		handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+rawURL, nil))
		if strings.Contains(rawURL, "ABC") {
			if bad.Code != http.StatusNotFound || !strings.Contains(bad.Body.String(), "ARTWORK_UNAVAILABLE") {
				t.Fatalf("%s status = %d body=%s", rawURL, bad.Code, bad.Body.String())
			}
		} else if bad.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body=%s", rawURL, bad.Code, bad.Body.String())
		}
	}
}

var _ = fogcast.Config{}
