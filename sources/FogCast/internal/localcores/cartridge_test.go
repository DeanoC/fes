package localcores

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchCartridgeDeliversROMWithoutLoadingTheBareCore(t *testing.T) {
	runtime := &fakeRuntime{}
	service, _, selections, packages := testService(t, runtime)
	smsID := installCore(t, packages, selections, "fes.sms", "Master System", "fes.computer", coreOpts{})
	romPath := filepath.Join(t.TempDir(), "Data Storm 1.00.sms")
	rom := []byte("data-storm-fixture")
	if err := os.WriteFile(romPath, rom, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := Handler(service)
	blocked := post(handler, "/v1/local/cores/"+smsID+"/launch")
	if blocked.Code != http.StatusConflict || errorCode(t, blocked) != "blocked" {
		t.Fatalf("empty launch %d %s", blocked.Code, blocked.Body.String())
	}
	loads, stops := runtime.calls()
	if len(loads) != 0 || stops != 0 || len(runtime.roms()) != 0 {
		t.Fatalf("empty launch touched the runtime loads=%v stops=%d roms=%d", loads, stops, len(runtime.roms()))
	}

	body := []byte(`{"rom_path":"` + romPath + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/local/cores/"+smsID+"/launch", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cartridge launch %d %s", response.Code, response.Body.String())
	}
	loads, _ = runtime.calls()
	got := runtime.roms()
	if len(loads) != 0 || len(got) != 1 || !bytes.Equal(got[0], rom) {
		t.Fatalf("loads=%v roms=%q", loads, got)
	}

	link := filepath.Join(filepath.Dir(romPath), "storm-link.sms")
	if err := os.Symlink(romPath, link); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	linkBody := []byte(`{"rom_path":"` + link + `"}`)
	linkReq := httptest.NewRequest(http.MethodPost, "/v1/local/cores/"+smsID+"/launch", bytes.NewReader(linkBody))
	linkResp := httptest.NewRecorder()
	handler.ServeHTTP(linkResp, linkReq)
	if linkResp.Code != http.StatusServiceUnavailable || errorCode(t, linkResp) != "unavailable" {
		t.Fatalf("symlink %d %s", linkResp.Code, linkResp.Body.String())
	}
	if len(runtime.roms()) != 1 {
		t.Fatalf("symlink delivered %d roms", len(runtime.roms()))
	}
}
