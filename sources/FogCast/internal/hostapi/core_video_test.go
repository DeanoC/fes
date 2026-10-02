package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

type videoAPIService struct {
	coreLibraryAPIService
	profile string
	data    string
	reads   int
}

func (s *videoAPIService) ImportCoreVideoPart(_ context.Context, size int64, body io.Reader, profile string) (catalog.CoreVideoPart, error) {
	s.imports++
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		return catalog.CoreVideoPart{}, catalog.ErrInvalidCoreVideoPart
	}
	s.profile, s.data = profile, string(data)
	return catalog.CoreVideoPart{PartID: strings.Repeat("b", 64), PackageID: strings.Repeat("a", 64), Profile: profile}, nil
}

func (s *videoAPIService) CoreVideoParts(context.Context) ([]catalog.CoreVideoPart, error) {
	s.reads++
	return nil, nil
}

func (s *videoAPIService) CoreEntryVideo(_ context.Context, gameID string) (fogcast.CoreEntryVideo, error) {
	s.reads++
	var value fogcast.CoreEntryVideo
	err := json.Unmarshal([]byte(`{"game_id":"`+gameID+`","package_id":"`+strings.Repeat("a", 64)+`","preferred_profile":"scanlines","effective_profile":"direct","builtin":true,"fallback_reason":"No compatible build imported.","choices":[{"profile":"direct","label":"Direct","available":true},{"profile":"scanlines","label":"Scanlines","available":false,"reason":"No compatible build imported."}]}`), &value)
	return value, err
}

func TestVideoImportRequiresExplicitProfileAndBoundedBinary(t *testing.T) {
	service := &videoAPIService{}
	handler := hostapi.New(service)
	for _, tc := range []struct {
		name, profile, contentType string
		size                       int64
		chunked                    bool
		status                     int
	}{
		{"direct", "direct", "application/octet-stream", 4, false, 200},
		{"scanlines", "scanlines", "application/octet-stream", 4, false, 200},
		{"unknown profile", "crt", "application/octet-stream", 4, false, 400},
		{"parameterized type", "direct", "application/octet-stream; charset=utf-8", 4, false, 400},
		{"empty", "direct", "application/octet-stream", 0, false, 400},
		{"oversized", "direct", "application/octet-stream", catalog.MaxCoreMediaBytes + 1, false, 400},
		{"chunked", "direct", "application/octet-stream", 4, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := service.imports
			req := httptest.NewRequest(http.MethodPost, "/api/v1/library/video-parts/"+tc.profile, strings.NewReader("part"))
			req.Host = "127.0.0.1"
			req.ContentLength = tc.size
			req.Header.Set("Content-Type", tc.contentType)
			if tc.chunked {
				req.TransferEncoding = []string{"chunked"}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status == 200 {
				if service.imports != before+1 || service.profile != tc.profile || service.data != "part" {
					t.Fatalf("wrong import dispatch: %+v", service)
				}
			} else if service.imports != before {
				t.Fatal("invalid request reached importer")
			}
		})
	}
}

func TestVideoReadsExposeFallbackAndCannotChangeSelection(t *testing.T) {
	service := &videoAPIService{}
	handler := hostapi.New(service)
	list := serve(t, handler, http.MethodGet, "/api/v1/library/video-parts")
	if list.Code != 200 || strings.TrimSpace(list.Body.String()) != "[]" {
		t.Fatalf("empty inventory=%d %s", list.Code, list.Body.String())
	}
	resolved := serve(t, handler, http.MethodGet, "/api/v1/library/core-entries/fpga-example/video")
	if resolved.Code != 200 || !strings.Contains(resolved.Body.String(), `"preferred_profile":"scanlines"`) ||
		!strings.Contains(resolved.Body.String(), `"effective_profile":"direct"`) || !strings.Contains(resolved.Body.String(), `"fallback_reason":`) {
		t.Fatalf("resolved output=%d %s", resolved.Code, resolved.Body.String())
	}
	before := service.reads
	for _, path := range []string{"/api/v1/library/video-parts", "/api/v1/library/core-entries/fpga-example/video"} {
		response := serveBody(t, handler, http.MethodGet, path, "body")
		if response.Code != 400 {
			t.Fatalf("body accepted: %d %s", response.Code, response.Body.String())
		}
	}
	if service.reads != before || service.imports != 0 {
		t.Fatal("invalid read dispatched or inspection imported a part")
	}
	mutation := serveBody(t, handler, http.MethodPut, "/api/v1/library/core-entries/fpga-example/video", `{}`)
	if mutation.Code != http.StatusMethodNotAllowed {
		t.Fatalf("per-title video mutation exists: %d %s", mutation.Code, mutation.Body.String())
	}
}

func TestVideoPreferencePatchPreservesOtherSettingsAndLegacyWrites(t *testing.T) {
	service := &settingsFake{settings: fogcast.LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}}
	handler := hostapi.New(service)
	for _, body := range []string{`{"video_profile":"scanlines"}`, `{"attract_idle_seconds":12}`} {
		response := serveBody(t, handler, http.MethodPatch, "/api/v1/library/settings", body)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"video_profile":"scanlines"`) {
			t.Fatalf("patch=%s response=%d %s", body, response.Code, response.Body.String())
		}
	}
	for _, body := range []string{`{"video_profile":""}`, `{"video_profile":"crt"}`, `{"video_profile":1}`} {
		response := serveBody(t, handler, http.MethodPatch, "/api/v1/library/settings", body)
		if response.Code != 400 || service.LibrarySettings().VideoProfile != "scanlines" {
			t.Fatalf("invalid preference saved: %d %s", response.Code, response.Body.String())
		}
	}
	response := serveBody(t, handler, http.MethodPut, "/api/v1/library/settings", `{"attract_idle_seconds":15,"preferred_regions":["japan"]}`)
	if response.Code != 200 || service.LibrarySettings().VideoProfile != "scanlines" {
		t.Fatalf("legacy write changed video preference: %d %s", response.Code, response.Body.String())
	}
}
