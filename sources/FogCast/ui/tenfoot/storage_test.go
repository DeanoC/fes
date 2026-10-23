package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
)

type pickerDriveBackend struct{ root string }

func (b pickerDriveBackend) Discover(context.Context) ([]storage.Drive, error) {
	return []storage.Drive{{Kind: "usb", Source: "picker-uuid", Label: "USB games"}}, nil
}
func (b pickerDriveBackend) Mount(context.Context, storage.Drive, string) (string, error) {
	return b.root, nil
}
func pickerDriveManager(t *testing.T) *storage.Manager {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "BIOS"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "BIOS", "coleco.bios"), bytes.Repeat([]byte{0x55}, 8192), 0644); err != nil {
		t.Fatal(err)
	}
	m := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	m.Backend = pickerDriveBackend{root}
	return m
}
func selectStorageKind(t *testing.T, a *App, kind string) {
	t.Helper()
	a.mu.Lock()
	found := false
	for i, r := range a.driveSettingsRowsLocked() {
		if r.Kind == kind {
			a.driveSettings.Index = i
			found = true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		t.Fatalf("storage action %q missing: %+v", kind, a.Snapshot().Storage)
	}
	a.HandleCommand(CmdSelect, time.Now())
}
func TestPairedKitSettingsRemembersUSBFolderWithoutHostSettings(t *testing.T) {
	app := NewApp(&Client{Client: hostclient.NewClient("http://127.0.0.1:1", nil), paired: true}, 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	manager := pickerDriveManager(t)
	app.driveManager = manager
	app.HandleCommand(CmdSettings, time.Now())
	app.mu.Lock()
	app.settingsIndex = settingsRowDrives
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "USB discovery", func(s Snapshot) bool { return s.Storage.Open && !s.Storage.Busy && len(s.Storage.Rows) > 4 })
	id := ""
	app.mu.Lock()
	id = app.driveSettings.Drives[0].ID
	app.mu.Unlock()
	selectStorageKind(t, app, "drive:"+id)
	waitFor(t, app, "USB remembered", func(s Snapshot) bool {
		return !s.Storage.Busy && len(s.Storage.Rows) > 0 && s.Storage.Rows[0].Kind == "label"
	})
	selectStorageKind(t, app, "bios")
	waitFor(t, app, "USB folders", func(s Snapshot) bool { return !s.Storage.Busy && s.Storage.Path != "" })
	app.mu.Lock()
	for i, r := range app.driveSettingsRowsLocked() {
		if r.Name == "BIOS/" {
			app.driveSettings.Index = i
		}
	}
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "BIOS folder", func(s Snapshot) bool { return !s.Storage.Busy && strings.HasSuffix(s.Storage.Path, "/BIOS") })
	selectStorageKind(t, app, "use-folder")
	waitFor(t, app, "folder saved", func(s Snapshot) bool { return !s.Storage.Busy && s.Storage.Path == "" })
	restarted := storage.New(manager.ConfigPath)
	drives, err := restarted.List(context.Background(), false)
	if err != nil || len(drives) != 1 || drives[0].BIOSFolder != "BIOS" {
		t.Fatalf("restart %+v %v", drives, err)
	}
	app.mu.Lock()
	app.localDrives = drives
	rows := app.storagePickerRootsLocked("bios")
	app.mu.Unlock()
	found := false
	for _, r := range rows {
		if r.Path == drivePath(false, id, "BIOS") {
			found = true
		}
	}
	if !found {
		t.Fatalf("BIOS roots did not use saved folder: %+v", rows)
	}
}
func TestRemoteDriveBIOSPickerImportsExactBytes(t *testing.T) {
	host := newFirmwareHost(t)
	payload := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	id := strings.Repeat("a", 24)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/storage/drives":
			json.NewEncoder(w).Encode(map[string]any{"drives": []storage.Drive{{ID: id, Kind: "usb", Label: "Host USB", Saved: true, BIOSFolder: "BIOS"}}})
		case "/api/v1/storage/drives/" + id + "/entries":
			json.NewEncoder(w).Encode(map[string]any{"entries": []storage.Entry{{Name: "coleco.bios", Size: 8192}}})
		case "/api/v1/storage/drives/" + id + "/file":
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			w.Write(payload)
		default:
			host.server.Config.Handler.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "remote drive roots", func(s Snapshot) bool {
		for _, r := range s.FirmwarePicker.Rows {
			if r.Path == drivePath(true, id, "BIOS") {
				return true
			}
		}
		return false
	})
	selectLibraryRoot(t, app, drivePath(true, id, "BIOS"))
	waitFor(t, app, "remote BIOS folder", func(s Snapshot) bool { return !s.FirmwarePicker.Busy && s.FirmwarePicker.Path != "" })
	selectNamedRow(t, app, "coleco.bios")
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "remote BIOS import", func(s Snapshot) bool { return !s.FirmwarePicker.Open && len(s.Games) == 1 && s.Games[0].FirmwareReady })
	if host.importCount() != 1 || host.selectCount() != 1 {
		t.Fatalf("imports=%d selections=%d", host.importCount(), host.selectCount())
	}
}
func TestDrivePathKeepsOwnerAndRelativeName(t *testing.T) {
	for _, host := range []bool{false, true} {
		path := drivePath(host, strings.Repeat("a", 24), "Media/Game disk.p")
		owner, id, rel, ok := splitDrivePath(path)
		if !ok || owner != host || len(id) != 24 || rel != "Media/Game disk.p" {
			t.Fatalf("path %q => %v %q %q %v", path, owner, id, rel, ok)
		}
	}
}
