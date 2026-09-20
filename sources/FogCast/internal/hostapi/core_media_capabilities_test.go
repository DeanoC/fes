package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaCapabilitiesAPIService struct {
	coreMediaAPIService
	result  protocol.CoreMediaCapabilities
	queries int
	id      string
}

func (s *mediaCapabilitiesAPIService) CoreMediaCapabilities(_ context.Context, id string) (protocol.CoreMediaCapabilities, error) {
	s.queries++
	s.id = id
	return s.result, s.err
}
func TestCoreMediaCapabilitiesAPIShapeAndAdmission(t *testing.T) {
	id := strings.Repeat("a", 64)
	path := "/api/v1/core-packages/" + id + "/media-capabilities"
	for _, media := range [][]protocol.CoreMediaCapability{
		{},
		{{Role: "blob", Format: "raw", MinBytes: 1, MaxBytes: 16384, Interface: protocol.RuntimeContract{ID: "fes.media.blob", Major: 1}, Transport: "fes-simple-computer-mailbox-v1"}},
	} {
		s := &mediaCapabilitiesAPIService{result: protocol.CoreMediaCapabilities{PackageID: id, Source: "declared-contract", Compatibility: "unknown", ImportMaxBytes: catalog.MaxCoreMediaBytes, Media: media}}
		out := mediaRequest(hostapi.New(s), "GET", path, "", "")
		var got protocol.CoreMediaCapabilities
		err := json.Unmarshal(out.Body.Bytes(), &got)
		if err != nil || out.Code != 200 || s.queries != 1 || s.id != id || !reflect.DeepEqual(got, s.result) || s.calls != 0 || s.imports != 0 {
			t.Fatalf("status=%d got=%+v calls=%d queries=%d err=%v", out.Code, got, s.calls, s.queries, err)
		}
		if len(media) == 0 && !strings.Contains(out.Body.String(), `"media":[]`) {
			t.Fatal(out.Body)
		}
	}
	for _, tc := range []struct {
		path, body, host string
		want             int
	}{
		{path, "{}", "127.0.0.1", 400},
		{"/api/v1/core-packages/bad/media-capabilities", "", "127.0.0.1", 400},
		{path, "", "remote.example", 403},
	} {
		s := &mediaCapabilitiesAPIService{}
		req := httptest.NewRequest("GET", tc.path, strings.NewReader(tc.body))
		req.Host = tc.host
		out := httptest.NewRecorder()
		hostapi.New(s).ServeHTTP(out, req)
		if out.Code != tc.want || s.queries != 0 || s.calls != 0 {
			t.Fatalf("status=%d calls=%d queries=%d", out.Code, s.calls, s.queries)
		}
	}
	out := mediaRequest(hostapi.New(&coreLibraryAPIService{}), "GET", path, "", "")
	if out.Code != 501 {
		t.Fatalf("optional service=%d %s", out.Code, out.Body)
	}
	for _, tc := range []struct {
		code   protocol.ErrorCode
		status int
	}{
		{protocol.CodeROMNotFound, 404}, {protocol.CodeInvalidArchive, 400}, {protocol.CodeInternal, 500},
	} {
		s := &mediaCapabilitiesAPIService{}
		s.err = &protocol.APIError{Code: tc.code}
		out := mediaRequest(hostapi.New(s), "GET", path, "", "")
		if out.Code != tc.status || s.queries != 1 || s.calls != 0 {
			t.Fatalf("status=%d queries=%d calls=%d", out.Code, s.queries, s.calls)
		}
	}
}
func TestCoreMediaCapabilitiesPairedListenerExcluded(t *testing.T) {
	s := &mediaCapabilitiesAPIService{}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(s), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{launcherToken, launcherID, 404}, {"", launcherID, 401}, {launcherToken, "wrong", 403},
	} {
		req := launcherRequest(http.MethodGet, "http://192.0.2.1:8789/api/v1/core-packages/"+strings.Repeat("a", 64)+"/media-capabilities", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-FogCast-Target-ID", tc.target)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.want || s.queries != 0 || s.calls != 0 {
			t.Fatalf("status=%d queries=%d calls=%d", out.Code, s.queries, s.calls)
		}
	}
}
