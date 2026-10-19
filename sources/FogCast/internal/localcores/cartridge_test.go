package localcores

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCartridgeZIPUsesSMSMember(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cart.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	member, err := w.Create("games/cart.sms")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("ROM bytes")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := readCartridgeFile(root, "cart.zip")
	if err != nil || string(got) != "ROM bytes" {
		t.Fatalf("cartridge = %q, %v", got, err)
	}
}

func TestReadCartridgeZIPRejectsMultipleSMSROMs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "multi.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range []string{"one.sms", "two.sms"} {
		member, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write([]byte("ROM")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readCartridgeFile(root, "multi.zip"); err == nil {
		t.Fatal("accepted ambiguous ZIP")
	}
}

func TestReadCartridgeRejectsParentDirectorySymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "library")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "game.sms"), []byte("inside-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readCartridgeFile(root, "nested/game.sms")
	if err != nil || string(got) != "inside-rom" {
		t.Fatalf("nested cartridge = %q, %v", got, err)
	}

	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "game.sms"), []byte("outside-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(nested, nested+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, nested); err != nil {
		t.Fatal(err)
	}
	naive, err := os.ReadFile(filepath.Join(root, "nested", "game.sms"))
	if err != nil || string(naive) != "outside-rom" {
		t.Fatalf("fixture did not follow the parent symlink: %q %v", naive, err)
	}
	got, err = readCartridgeFile(root, "nested/game.sms")
	if err == nil || bytes.Equal(got, []byte("outside-rom")) || bytes.Equal(got, []byte("inside-rom")) {
		t.Fatalf("parent symlink returned %q %v", got, err)
	}
}

func TestReadCartridgeAllowsSymlinkAboveLibraryRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	library := filepath.Join(real, "sms")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "game.sms"), []byte("inside-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(base, "via")
	if err := os.Symlink(real, via); err != nil {
		t.Fatal(err)
	}
	got, err := readCartridgeFile(filepath.Join(via, "sms"), "game.sms")
	if err != nil || string(got) != "inside-rom" {
		t.Fatalf("symlink above the library root: %q %v", got, err)
	}
}

func TestReadCartridgeRejectsSymlinkLibraryRootAndEscape(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "sms")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "game.sms"), []byte("inside-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(library, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readCartridgeFile(link, "game.sms"); err == nil {
		t.Fatal("accepted a symlink library root")
	}
	if _, err := readCartridgeFile(library, "../sms/game.sms"); err == nil {
		t.Fatal("accepted a relative path that leaves the root")
	}
	if _, err := readCartridgeFile(library, "/game.sms"); err == nil {
		t.Fatal("accepted an absolute cartridge path")
	}
}

func TestLaunchCartridgeDeliversROMWithoutLoadingTheBareCore(t *testing.T) {
	runtime := &fakeRuntime{}
	service, _, selections, packages := testService(t, runtime)
	smsID := installCore(t, packages, selections, "fes.sms", "Master System", "fes.computer", coreOpts{cartridge: true})
	library := t.TempDir()
	romPath := filepath.Join(library, "Data Storm 1.00.sms")
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

	body := cartridgeLaunchBody(t, library, "Data Storm 1.00.sms")
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

	link := filepath.Join(library, "storm-link.sms")
	if err := os.Symlink(romPath, link); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(t.Context(), service.RunStatus().Generation); err != nil {
		t.Fatal(err)
	}
	linkBody := cartridgeLaunchBody(t, library, "storm-link.sms")
	linkReq := httptest.NewRequest(http.MethodPost, "/v1/local/cores/"+smsID+"/launch", bytes.NewReader(linkBody))
	linkResp := httptest.NewRecorder()
	handler.ServeHTTP(linkResp, linkReq)
	if linkResp.Code != http.StatusServiceUnavailable || errorCode(t, linkResp) != "unavailable" {
		t.Fatalf("symlink %d %s", linkResp.Code, linkResp.Body.String())
	}
	if len(runtime.roms()) != 1 {
		t.Fatalf("symlink delivered %d roms", len(runtime.roms()))
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "game.sms"), []byte("outside-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(library, "swapped")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	swapBody := cartridgeLaunchBody(t, library, "swapped/game.sms")
	swapReq := httptest.NewRequest(http.MethodPost, "/v1/local/cores/"+smsID+"/launch", bytes.NewReader(swapBody))
	swapResp := httptest.NewRecorder()
	handler.ServeHTTP(swapResp, swapReq)
	if swapResp.Code != http.StatusServiceUnavailable || errorCode(t, swapResp) != "unavailable" {
		t.Fatalf("parent symlink %d %s", swapResp.Code, swapResp.Body.String())
	}
	if len(runtime.roms()) != 1 {
		t.Fatalf("parent symlink delivered %d roms", len(runtime.roms()))
	}

	oldBody := []byte(`{"rom_path":"` + romPath + `"}`)
	oldReq := httptest.NewRequest(http.MethodPost, "/v1/local/cores/"+smsID+"/launch", bytes.NewReader(oldBody))
	oldResp := httptest.NewRecorder()
	handler.ServeHTTP(oldResp, oldReq)
	if oldResp.Code != http.StatusServiceUnavailable || errorCode(t, oldResp) != "unavailable" {
		t.Fatalf("absolute rom_path %d %s", oldResp.Code, oldResp.Body.String())
	}
	loads, _ = runtime.calls()
	if len(loads) != 0 || len(runtime.roms()) != 1 {
		t.Fatalf("absolute rom_path launched loads=%v roms=%d", loads, len(runtime.roms()))
	}
}

func cartridgeLaunchBody(t *testing.T, libraryRoot, relativePath string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"library_root":  libraryRoot,
		"relative_path": relativePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
