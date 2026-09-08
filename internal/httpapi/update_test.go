package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/appliance"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/misterruntime"
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

func (updateControl) Status(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none"}, nil
}
func (c updateControl) Stop(ctx context.Context) (misterruntime.Response, error) {
	return c.Status(ctx)
}
func (updateControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	panic("unexpected launch")
}
func (updateControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	panic("unexpected development")
}

func updateFixture(t *testing.T, trial bool, reboot func(context.Context) error) (http.Handler, *appliance.Store, release.Manifest, string, []byte) {
	t.Helper()
	b := make([]byte, 4096)
	b[1080] = 0x53
	b[1081] = 0xef
	b[1120] = 0x40
	m := release.Manifest{Format: 1, Board: release.Board, BootABI: release.BootABI, Version: "test", KernelSHA256: strings.Repeat("a", 64), ImageSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), ImageSize: int64(len(b)), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
	s, e := appliance.New(t.TempDir(), m)
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
	c := agent.New(misterruntime.NewRuntime(updateControl{}, "", time.Millisecond, time.Second), core.DefaultRegistry(), time.Second, time.Second)
	svc := applianceupdate.New(s, c, boot, reboot, nil)
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(func() { manager.Close() })
	grant := claimKit(t, manager)
	return httpapi.New(c, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithUpdate(svc)), s, n, grant.Token, b
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
	for _, path := range []string{"/v1/launch", "/v2/launch", "/v1/development/rbf", "/v1/input/attach", "/v1/input/stream", "/v1/cast/start"} {
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
