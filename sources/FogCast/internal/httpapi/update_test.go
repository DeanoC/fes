package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type updateControl struct{}

func (updateControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (c updateControl) Protocol2Stop(ctx context.Context) (misterruntime.Protocol2Response, error) {
	return c.Protocol2Status(ctx)
}
func (updateControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	panic("unexpected development")
}

func updateFixture(t *testing.T, trial bool, reboot func(context.Context) error, extra ...httpapi.Option) (http.Handler, *appliance.Store, release.Manifest, string, []byte) {
	t.Helper()
	return updateFixtureLogged(t, trial, reboot, nil, extra...)
}

func updateFixtureLogged(t *testing.T, trial bool, reboot func(context.Context) error, logger *slog.Logger, extra ...httpapi.Option) (http.Handler, *appliance.Store, release.Manifest, string, []byte) {
	return updateFixtureForBoard(t, release.Board, trial, reboot, logger, extra...)
}

func updateFixtureForBoard(t *testing.T, board string, trial bool, reboot func(context.Context) error, logger *slog.Logger, extra ...httpapi.Option) (http.Handler, *appliance.Store, release.Manifest, string, []byte) {
	t.Helper()
	b := make([]byte, 4096)
	b[1080] = 0x53
	b[1081] = 0xef
	b[1120] = 0x40
	m := release.Manifest{Format: 1, Board: release.Board, BootABI: release.BootABI, Version: "test", KernelSHA256: strings.Repeat("a", 64), ImageSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), ImageSize: int64(len(b)), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
	m.Board = board
	s, e := appliance.NewForBoard(t.TempDir(), m, board)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	boot := applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: m.ImageSHA256}
	b[0] = 1
	n := m
	n.ImageSHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
	if trial {
		if e = s.Stage(context.Background(), n, n.ImageSize, bytes.NewReader(b)); e != nil {
			t.Fatal(e)
		}
		if e = s.Activate(n.ImageSHA256); e != nil {
			t.Fatal(e)
		}
		if _, e = s.BeginBoot("boot-a"); e != nil {
			t.Fatal(e)
		}
		boot.ImageSHA256 = n.ImageSHA256
		boot.Trial = true
	}
	c := agent.New(misterruntime.NewRuntime(updateControl{}, "", time.Millisecond, time.Second), time.Second, time.Second)
	svc := applianceupdate.New(s, c, boot, reboot, nil)
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(func() { manager.Close() })
	grant := claimKit(t, manager)
	options := append([]httpapi.Option{httpapi.WithKitLease(manager), httpapi.WithUpdate(svc)}, extra...)
	return httpapi.New(c, "bearer", "test", logger, options...), s, n, grant.Token, b
}

type updateAdmissionReader struct {
	reader io.Reader
	reads  int
}

func (r *updateAdmissionReader) Read(body []byte) (int, error) {
	r.reads++
	return r.reader.Read(body)
}

func TestCorePackageWritesBlockedDuringUpdateTrialAndPending(t *testing.T) {
	for _, trial := range []bool{true, false} {
		name := "pending"
		if trial {
			name = "trial"
		}
		t.Run(name, func(t *testing.T) {
			archive, inspection := installedPackageFixture(t, "valid-basic")
			packages, err := kitpackages.New(durableHTTPStoreRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := packages.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
				t.Fatal(err)
			}
			inspections := 0
			inspect := func(context.Context, int64, io.Reader) (protocol.CoreInspection, *protocol.APIError) {
				inspections++
				return protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true}, nil
			}
			h, releases, next, lease, image := updateFixture(t, trial, func(context.Context) error { return nil }, httpapi.WithCorePackages(packages, inspect))
			if !trial {
				if err := releases.Stage(context.Background(), next, next.ImageSize, bytes.NewReader(image)); err != nil {
					t.Fatal(err)
				}
				response := updateRequest(h, "/v1/update/activate", `{"image_sha256":"`+next.ImageSHA256+`"}`, lease)
				if response.Code != http.StatusAccepted {
					t.Fatalf("activation=%d %s", response.Code, response.Body.String())
				}
				status, err := releases.Status()
				if err != nil || status.Pending != next.ImageSHA256 {
					t.Fatalf("pending=%+v,%v", status, err)
				}
			}
			for _, operation := range []struct {
				method, path, contentType string
				body                      []byte
			}{
				{http.MethodPost, "/v1/core-packages", "application/octet-stream", archive},
				{http.MethodPut, "/v1/core-selections/fes.pong", "application/json", packageSelectionBody(t, "", inspection.PackageID)},
			} {
				body := &updateAdmissionReader{reader: bytes.NewReader(operation.body)}
				request := httptest.NewRequest(operation.method, operation.path, body)
				request.ContentLength = int64(len(operation.body))
				request.Header.Set("Authorization", "Bearer bearer")
				request.Header.Set("Content-Type", operation.contentType)
				response := httptest.NewRecorder()
				h.ServeHTTP(response, request)
				if response.Code != http.StatusConflict {
					t.Fatalf("%s %s=%d %s", operation.method, operation.path, response.Code, response.Body.String())
				}
				if body.reads != 0 {
					t.Fatalf("blocked operation read request before admission: %s", operation.path)
				}
			}
			if inspections != 0 {
				t.Fatal("blocked selection called runtime inspector")
			}
			selections, err := packages.Selections(context.Background())
			if err != nil || len(selections) != 0 {
				t.Fatalf("blocked selection mutated store=%v,%v", selections, err)
			}
			for _, path := range []string{"/v1/core-packages", "/v1/core-selections"} {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("Authorization", "Bearer bearer")
				response := httptest.NewRecorder()
				h.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("read inventory blocked: %s=%d %s", path, response.Code, response.Body.String())
				}
			}
		})
	}
}

func updateRequest(h http.Handler, path, body, lease string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer bearer")
	r.Header.Set(httpapi.KitLeaseHeader, lease)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUpdateHTTPAuthenticationLeaseAndTrialGate(t *testing.T) {
	h, s, m, lease, _ := updateFixture(t, true, nil)
	for _, path := range []string{"/v1/update", "/v1/update/stage", "/v1/update/activate", "/v1/update/rollback", "/v1/update/confirm"} {
		method := http.MethodPost
		if path == "/v1/update" {
			method = http.MethodGet
		}
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/v1/update/stage", "/v1/update/activate", "/v1/update/rollback", "/v1/update/confirm"} {
		if w := updateRequest(h, path, "", "foreign"); w.Code != 403 {
			t.Fatalf("unleased %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/v1/development/media-stream", "/v1/development/media", "/v1/library/core/load", "/v1/library/core/parts", "/v1/development/rbf", "/v1/development/core", "/v1/input/attach", "/v1/input/stream", "/v1/cast/start"} {
		if w := updateRequest(h, path, "", lease); w.Code != 409 {
			t.Fatalf("trial %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	bad := updateRequest(h, "/v1/update/confirm", `{"boot_id":"old","image_sha256":"`+m.ImageSHA256+`"}`, lease)
	if bad.Code != 409 {
		t.Fatalf("wrong boot: %d", bad.Code)
	}
	good := updateRequest(h, "/v1/update/confirm", `{"boot_id":"boot-a","image_sha256":"`+m.ImageSHA256+`"}`, lease)
	if good.Code != 200 {
		t.Fatalf("confirm: %d %s", good.Code, good.Body.String())
	}
	st, e := s.Status()
	if e != nil || st.Good != m.ImageSHA256 {
		t.Fatalf("not durably confirmed: %+v %v", st, e)
	}
}

func TestUpdateHTTPStagesThenFlushesBeforeReboot(t *testing.T) {
	var response *httptest.ResponseRecorder
	rebooted := false
	h, s, m, lease, b := updateFixture(t, false, func(context.Context) error {
		if response == nil || !response.Flushed {
			t.Error("response not flushed")
		}
		rebooted = true
		return nil
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/update/stage", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer bearer")
	r.Header.Set(httpapi.KitLeaseHeader, lease)
	r.Header.Set("Content-Type", "application/octet-stream")
	manifest, _ := json.Marshal(m)
	r.Header.Set(httpapi.ReleaseManifestHeader, base64.StdEncoding.EncodeToString(manifest))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("stage: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/update/activate", strings.NewReader(`{"image_sha256":"`+m.ImageSHA256+`"}`))
	r.Header.Set("Authorization", "Bearer bearer")
	r.Header.Set(httpapi.KitLeaseHeader, lease)
	r.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, r)
	if response.Code != 202 || !rebooted {
		t.Fatalf("activate: %d reboot %v %s", response.Code, rebooted, response.Body.String())
	}
	st, e := s.Status()
	if e != nil || st.Pending != m.ImageSHA256 {
		t.Fatalf("pending: %+v %v", st, e)
	}
}

func TestUpdateUploadRevocationInterruptsBodyAndNeverPublishes(t *testing.T) {
	h, s, m, lease, _ := updateFixture(t, false, nil)
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	manifest, _ := json.Marshal(m)
	_, err = fmt.Fprintf(conn, "POST /v1/update/stage HTTP/1.1\r\nHost: kit\r\nAuthorization: Bearer bearer\r\n%s: %s\r\n%s: %s\r\nContent-Type: application/octet-stream\r\nContent-Length: 4096\r\n\r\nx", httpapi.KitLeaseHeader, lease, httpapi.ReleaseManifestHeader, base64.StdEncoding.EncodeToString(manifest))
	if err != nil {
		t.Fatal(err)
	}
	imagePath, err := s.ImagePath(m.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		entries, err := os.ReadDir(filepath.Dir(imagePath))
		if err != nil {
			t.Fatal(err)
		}
		staging := false
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".stage-") {
				staging = true
			}
		}
		if staging {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upload never entered staging")
		}
		time.Sleep(time.Millisecond)
	}
	// Revoke only after the incomplete production upload owns the store lock.
	response := updateRequest(h, "/v1/kit/release", "", lease)
	if response.Code != 409 && response.Code != 200 {
		t.Fatalf("release: %d", response.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = s.StatusContext(ctx); err != nil {
		t.Fatalf("upload retained store lock after revocation: %v", err)
	}
	if _, err = s.Verify(m.ImageSHA256); err == nil {
		t.Fatal("incomplete upload published")
	}
}

func TestUpdateStageManifestFormat(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h, _, m, lease, b := updateFixtureLogged(t, false, nil, logger)
	stage := func(raw []byte) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/v1/update/stage", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer bearer")
		r.Header.Set(httpapi.KitLeaseHeader, lease)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set(httpapi.ReleaseManifestHeader, base64.StdEncoding.EncodeToString(raw))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	known, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	higher := bytes.Replace(known, []byte(`"format":1`), []byte(`"format":1,"manifest_format":"1.1","future_field":true`), 1)
	accepted := stage(higher)
	if accepted.Code != 201 {
		t.Fatalf("higher minor: %d %s", accepted.Code, accepted.Body.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte("future_field")) {
		t.Fatalf("ignored field was not logged: %s", logs.String())
	}
	if bytes.Contains(accepted.Body.Bytes(), []byte("future_field")) {
		t.Fatalf("stage response kept unknown field: %s", accepted.Body.Bytes())
	}
	logs.Reset()
	major := bytes.Replace(known, []byte(`"format":1`), []byte(`"format":1,"manifest_format":"2.0"`), 1)
	rejected := stage(major)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("unknown major: %d %s", rejected.Code, rejected.Body.String())
	}
	if bytes.Contains(logs.Bytes(), []byte("ignoring unknown release manifest fields")) {
		t.Fatalf("unknown major logged ignored fields: %s", logs.String())
	}
}
