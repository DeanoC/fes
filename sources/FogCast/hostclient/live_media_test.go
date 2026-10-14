package hostclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestLiveMediaCapableRequiresSimpleComputerBlob(t *testing.T) {
	t.Parallel()
	blob := []SessionCoreInterface{{ID: "fes.media.blob", Major: 1, Minor: 0}}
	ok := &SessionCorePackage{
		PackageID: strings.Repeat("a", 64), Generation: 3,
		ABI: SessionCoreABI{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: blob,
	}
	if !LiveMediaCapable(ok) || ComputerTapeCapable(ok) {
		t.Fatal("expected blob capable and not a computer tape unit")
	}
	if LiveMediaCapable(&SessionCorePackage{ABI: SessionCoreABI{ID: "fes.application", Major: 1}, ActiveInterfaces: blob}) {
		t.Fatal("application must not be live-media capable")
	}
	if LiveMediaCapable(&SessionCorePackage{ABI: SessionCoreABI{ID: "fes.simple-computer", Major: 1}}) {
		t.Fatal("missing blob must not be capable")
	}
}

func TestLiveMediaCapableProjectsComputerZx81Tape(t *testing.T) {
	t.Parallel()
	pkg := strings.Repeat("a", 64)
	body := fmt.Sprintf(`{"id":"host-1","target":"dev","target_id":"dev-id","state":"active","core_package":{"package_id":%q,"generation":9,"abi":{"id":"fes.computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.zx81-tape","major":1,"minor":0}],"media_units":[{"unit":0,"interface":{"id":"fes.media.zx81-tape","major":1,"minor":0},"min_bytes":1,"max_bytes":16384,"chunk_bytes":512,"state":"empty"}]}}`, pkg)
	result, err := DecodeSession(http.StatusOK, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if !LiveMediaCapable(result.CorePackage) || !ComputerTapeCapable(result.CorePackage) {
		t.Fatal("projected fes.computer tape interface should be live-media capable")
	}
	binding, err := LiveMediaBinding(result)
	if err != nil || binding.PackageID != pkg || binding.Generation != 9 || binding.Target != "dev" || binding.TargetID != "dev-id" {
		t.Fatalf("binding = %+v err=%v", binding, err)
	}
	result.CorePackage.ActiveInterfaces = nil
	if LiveMediaCapable(result.CorePackage) {
		t.Fatal("computer session without the tape interface must stay closed")
	}
	blobOnly := &SessionCorePackage{ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: "fes.media.blob", Major: 1}}}
	if LiveMediaCapable(blobOnly) {
		t.Fatal("computer blob must not use the legacy mailbox predicate")
	}
}

func TestReplaceLiveMediaPostsComputerZx81Tape(t *testing.T) {
	t.Parallel()
	pkg := strings.Repeat("a", 64)
	mediaID := strings.Repeat("b", 64)
	posts := 0
	sessionBody := fmt.Sprintf(`{"id":"host-1","target":"dev","target_id":"dev-id","state":"active","core_package":{"package_id":%q,"generation":4,"abi":{"id":"fes.computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.zx81-tape","major":1,"minor":0}]}}`, pkg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, sessionBody)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			posts++
			if r.Header.Get("X-FogCast-Package-ID") != pkg || r.Header.Get("X-FogCast-Core-Generation") != "4" || r.Header.Get("X-FogCast-Target") != "dev" {
				t.Errorf("headers = %v", r.Header)
			}
			_, _ = io.WriteString(w, sessionBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	result, err := NewClient(server.URL, server.Client()).ReplaceLiveMedia(context.Background(), mediaID, "maze.p")
	if err != nil || result.State != "active" || posts != 1 {
		t.Fatalf("replace = %+v err=%v posts=%d", result, err, posts)
	}
}

func TestReplaceLiveMediaAndClearLiveMedia(t *testing.T) {
	t.Parallel()
	pkg := strings.Repeat("a", 64)
	mediaPayload := []byte("zx81-tape-bytes!!")
	mediaID := fmt.Sprintf("%x", sha256.Sum256(mediaPayload))
	var replaceCalls, clearCalls int
	var gotMediaID, gotName string
	sessionBody := func() string {
		return fmt.Sprintf(`{"id":"host-1","target":"dev","target_id":"dev-id","state":"active","execution":"fpga_native","core_package":{"package_id":%q,"generation":9,"abi":{"id":"fes.simple-computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.blob","major":1,"minor":0}]}}`, pkg)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, sessionBody())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			replaceCalls++
			if r.Header.Get("Content-Type") != "application/json" ||
				r.Header.Get("X-FogCast-Session-ID") != "host-1" ||
				r.Header.Get("X-FogCast-Package-ID") != pkg ||
				r.Header.Get("X-FogCast-Core-Generation") != "9" ||
				r.Header.Get("X-FogCast-Target") != "dev" ||
				r.Header.Get("X-FogCast-Target-ID") != "dev-id" {
				t.Errorf("replace headers = %v", r.Header)
			}
			var req protocol.LiveMediaRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode: %v", err)
			}
			gotMediaID, gotName = req.MediaID, req.Name
			_, _ = io.WriteString(w, sessionBody())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media/clear":
			clearCalls++
			if r.Header.Get("X-FogCast-Session-ID") != "host-1" ||
				r.Header.Get("X-FogCast-Package-ID") != pkg ||
				r.ContentLength != 0 {
				t.Errorf("clear headers = %v len=%d", r.Header, r.ContentLength)
			}
			_, _ = io.WriteString(w, sessionBody())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())

	result, err := client.ReplaceLiveMedia(context.Background(), mediaID, "maze.p")
	if err != nil || result.State != "active" || replaceCalls != 1 || gotMediaID != mediaID || gotName != "maze.p" {
		t.Fatalf("replace = %+v err=%v calls=%d id=%s name=%s", result, err, replaceCalls, gotMediaID, gotName)
	}
	cleared, err := client.ClearLiveMedia(context.Background())
	if err != nil || cleared.State != "active" || clearCalls != 1 {
		t.Fatalf("clear = %+v err=%v calls=%d", cleared, err, clearCalls)
	}
}

func TestReplaceLiveMediaSurfacesBusy(t *testing.T) {
	t.Parallel()
	pkg := strings.Repeat("a", 64)
	mediaID := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"host-1","target":"dev","state":"active","core_package":{"package_id":%q,"generation":9,"abi":{"id":"fes.simple-computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.blob","major":1,"minor":0}]}}`, pkg))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": "BUSY", "message": "tape loader is busy; retry after LOAD finishes"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	_, err := NewClient(server.URL, server.Client()).ReplaceLiveMedia(context.Background(), mediaID, "maze.p")
	apiErr, ok := err.(*protocol.APIError)
	if !ok || apiErr.Code != protocol.CodeBusy || apiErr.Phase != "input" || !strings.Contains(apiErr.Message, "tape loader is busy") {
		t.Fatalf("busy err = %#v", err)
	}
}

func TestReplaceLiveMediaRejectsBadAdmission(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1", nil)
	if _, err := client.ReplaceLiveMedia(context.Background(), "short", "maze.p"); err == nil {
		t.Fatal("expected bad media id")
	}
	if _, err := client.ReplaceLiveMedia(context.Background(), strings.Repeat("a", 64), "maze.tzx"); err == nil {
		t.Fatal("expected bad extension")
	}
}
