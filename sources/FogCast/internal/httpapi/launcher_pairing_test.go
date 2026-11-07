package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/launcherconfig"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLauncherPairingUsesExistingLeaseAndPersistsForRestart(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	const target = "67c5f4e2-d288-49bb-9049-39ecf39cf6f6"
	const secret = "0123456789abcdef0123456789abcdef"
	path := filepath.Join(t.TempDir(), "launcher.json")
	controller := &fakeController{health: protocol.Health{Ready: true}, status: protocol.Status{State: protocol.StateIdle}}
	local := httpapi.KitLocalRun{Phase: "idle"}
	handler := httpapi.New(controller, "bearer", "test", nil,
		httpapi.WithTargetID(target), httpapi.WithKitLease(manager), httpapi.WithLauncherPairing(path),
		httpapi.WithKitLocalRun(func() httpapi.KitLocalRun { return local }))
	body := `{"api":"http://host:18789/","token":"` + secret + `","target_id":"` + target + `"}`
	send := func(bearer, lease, body string, cancelled bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/v1/launcher/pairing", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set(httpapi.KitLeaseHeader, lease)
		if cancelled {
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("response disclosed launcher credential")
		}
		return response
	}
	for _, tc := range []struct {
		bearer, lease string
		want          int
	}{
		{"", grant.Token, 401}, {"bearer", "", 403}, {"bearer", "foreign", 403},
	} {
		if got := send(tc.bearer, tc.lease, body, false).Code; got != tc.want {
			t.Fatalf("ownership guard: got %d want %d", got, tc.want)
		}
	}
	for _, state := range []protocol.State{protocol.StateActive, protocol.StateLaunching, protocol.StateStopping, protocol.StateFailed} {
		controller.status.State = state
		if got := send("bearer", grant.Token, body, false).Code; got != 409 {
			t.Fatalf("active state %s: %d", state, got)
		}
	}
	controller.status.State = protocol.StateIdle
	local.Phase = "running"
	if got := send("bearer", grant.Token, body, false).Code; got != 409 {
		t.Fatalf("local core: %d", got)
	}
	local.Phase = "idle"
	controller.health.Ready = false
	if got := send("bearer", grant.Token, body, false).Code; got != 409 {
		t.Fatalf("unready: %d", got)
	}
	controller.health.Ready = true
	for _, invalid := range []string{
		body + `{}`, strings.Replace(body, target, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1),
		strings.Replace(body, "host:18789/", "host:18789/path", 1),
		strings.Replace(body, secret, "bearer", 1), strings.Replace(body, `"api":`, `"unknown":`, 1),
		strings.Replace(body, secret, strings.Repeat("x", 9000), 1),
	} {
		if got := send("bearer", grant.Token, invalid, false).Code; got != 400 {
			t.Fatalf("invalid pairing: %d", got)
		}
	}
	if got := send("bearer", grant.Token, body, true).Code; got == 200 {
		t.Fatal("cancelled setup succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("refused setup wrote configuration")
	}
	for i := 0; i < 2; i++ {
		response := send("bearer", grant.Token, body, false)
		if response.Code != 200 {
			t.Fatalf("save: %d %s", response.Code, response.Body.String())
		}
		var receipt struct {
			RestartRequired bool   `json:"restart_required"`
			SHA256          string `json:"sha256"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || !receipt.RestartRequired || len(receipt.SHA256) != 64 {
			t.Fatal("missing setup receipt")
		}
		config, err := launcherconfig.Load(path)
		if err != nil || config.API != "http://host:18789" || config.TargetID != target || config.Token != secret {
			t.Fatal("persisted pairing did not survive reload")
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("pairing is not private")
	}
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := send("bearer", grant.Token, body, false).Code; got != 409 {
		t.Fatalf("public config: %d", got)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || controller.stopCalls != 0 || controller.launchCalls != 0 {
		t.Fatal("setup changed refused bytes or hardware")
	}
}

func TestLauncherPairingUnavailableWithoutKitOwnership(t *testing.T) {
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil,
		httpapi.WithTargetID("67c5f4e2-d288-49bb-9049-39ecf39cf6f6"),
		httpapi.WithLauncherPairing(filepath.Join(t.TempDir(), "launcher.json")))
	request := httptest.NewRequest(http.MethodPost, "/v1/launcher/pairing", nil)
	request.Header.Set("Authorization", "Bearer bearer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("unleased setup exposed: %d", response.Code)
	}
}
