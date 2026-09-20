package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

func TestSettingsPreparePersistsIdentityWithoutPublishingCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("selected_target='kit'\nrequest_timeout_seconds=7\nupload_timeout_seconds=60\n[[targets]]\nname='kit'\nenabled=false\naddress='http://127.0.0.1:1'\nagent='private-secret'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := fogcast.Open(context.Background(), fogcast.Paths{Config: path, Index: filepath.Join(dir, "index.sqlite"), Staging: filepath.Join(dir, "staging")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := hostapi.New(service)
	var first string
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/v1/library/settings", strings.NewReader(`{"prepare_target":"kit"}`))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
		if strings.Contains(response.Body.String(), "private-secret") {
			t.Fatal("credential published")
		}
		var settings struct {
			Targets []struct {
				TargetID string `json:"target_id"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		id := settings.Targets[0].TargetID
		if !discovery.ValidID(id) {
			t.Fatal("missing identity")
		}
		if i == 0 {
			first = id
		} else if first != id {
			t.Fatal("identity rotated")
		}
		config, _ := os.ReadFile(path)
		if !strings.Contains(string(config), id) || !strings.Contains(string(config), "private-secret") {
			t.Fatal("private config lost fields")
		}
	}
}

type unavailableDiscoveryService struct{ fakeService }

func (*unavailableDiscoveryService) TargetConnection() fogcast.TargetConnection {
	return fogcast.TargetConnection{State: "disconnected", Address: "http://127.0.0.1:1"}
}
func TestUnavailableStatusIncludesConnection(t *testing.T) {
	service := &unavailableDiscoveryService{fakeService: fakeService{statusErr: errors.New("unavailable")}}
	response := httptest.NewRecorder()
	hostapi.New(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/status", nil))
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Connection fogcast.TargetConnection `json:"connection"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 503 || result.Error.Code != "TARGET_UNAVAILABLE" || result.Connection.State != "disconnected" || result.Connection.Address != "http://127.0.0.1:1" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
}
