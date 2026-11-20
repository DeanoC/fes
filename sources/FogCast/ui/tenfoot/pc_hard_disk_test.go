package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
)

func TestPCHardDiskImportUsesDedicatedRoute(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		nextStart    bool
	}{
		{"next start local file", "file", true},
		{"initial setup host drive", "host", false},
		{"next start local drive", "drive", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const size = int64(64 << 20)
			pkg, prior, next := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
			root := t.TempDir()
			path := filepath.Join(root, "dos.img")
			diskPath := path
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(size); err != nil {
				file.Close()
				t.Fatal(err)
			}
			file.Close()
			imports, reads := 0, 0
			var selected map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/storage/drives/" + strings.Repeat("d", 24) + "/file":
					reads++
					if r.URL.Query().Get("purpose") != "hard-disk" || r.URL.Query().Get("path") != "dos.img" || r.URL.Query().Has("max_bytes") {
						t.Errorf("host read did not use dedicated purpose: %s", r.URL)
					}
					w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
					f, err := os.Open(diskPath)
					if err != nil {
						t.Error(err)
						http.Error(w, "missing", 500)
						return
					}
					defer f.Close()
					io.Copy(w, f)
				case "/api/v1/core-media/hard-disk":
					imports++
					if r.ContentLength != size || r.Header.Get("Content-Type") != "application/octet-stream" {
						t.Errorf("bad upload: length=%d", r.ContentLength)
					}
					if n, err := io.Copy(io.Discard, r.Body); err != nil || n != size {
						t.Errorf("streamed %d bytes: %v", n, err)
					}
					json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: next, Size: size})
				case "/api/v1/library/core-entries/pc/media":
					json.NewDecoder(r.Body).Decode(&selected)
					json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "pc", CoreID: "fes.pc386", PackageID: pkg, MediaRole: protocol.HardDiskRole, MediaID: next})
				case "/api/v1/games":
					io.WriteString(w, `{"games":[]}`)
				default:
					t.Errorf("unexpected route %s", r.URL)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
			t.Cleanup(a.Stop)
			a.ctx = context.Background()
			a.coreLibrary = coreLibraryState{Open: true, Online: true, HardwareGameID: "pc", HardwarePackageID: pkg, ExpectedMediaID: prior, PickID: "media:" + protocol.HardDiskRole,
				Setup: &hostclient.CoreSetup{Capabilities: protocol.CoreMediaCapabilities{Media: []protocol.CoreMediaCapability{{Role: protocol.HardDiskRole, MinBytes: protocol.AtaDiskMinBytes, MaxBytes: protocol.AtaDiskMaxBytes, Extensions: []string{".img"}}}}}}
			if tc.nextStart {
				a.coreLibrary.HardwareMode = "system-media"
			}
			if tc.source == "host" {
				path = "host-drive:" + strings.Repeat("d", 24) + "/dos.img"
			}
			if tc.source == "drive" {
				manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
				manager.Backend = pickerDriveBackend{root: root}
				drive, err := manager.Save(storage.Drive{Kind: "usb", Source: "picker-uuid"})
				if err != nil {
					t.Fatal(err)
				}
				a.driveManager = manager
				path = drivePath(false, drive.ID, "dos.img")
			}
			a.mu.Lock()
			a.importCoreLibraryFileLocked(path)
			a.mu.Unlock()
			waitCoreLibrary(t, a)
			a.mu.Lock()
			defer a.mu.Unlock()
			if imports != 1 {
				t.Fatalf("imports=%d state=%+v", imports, a.coreLibrary)
			}
			if tc.source == "host" && reads != 1 {
				t.Fatalf("host reads=%d", reads)
			}
			if tc.nextStart {
				if selected["expected_package_id"] != pkg || selected["expected_media_id"] != prior || selected["media_role"] != protocol.HardDiskRole || selected["media_id"] != next {
					t.Fatalf("selection=%+v", selected)
				}
			} else if a.coreLibrary.MediaRole != protocol.HardDiskRole || a.coreLibrary.MediaID != next || selected != nil {
				t.Fatalf("initial setup=%+v selection=%+v", a.coreLibrary, selected)
			}
		})
	}
}

func TestPCHardDiskPickerRefusesInvalidSizesWithoutWideningOrdinaryFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, size := range []int64{protocol.AtaDiskMinBytes - 512, protocol.AtaDiskMinBytes + 1, protocol.AtaDiskMaxBytes + 512} {
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if f, _, _, err := openHardDiskPickerFile(context.Background(), nil, nil, path); err == nil {
			f.Close()
			t.Fatalf("admitted invalid size=%d", size)
		}
	}
	if err := file.Truncate(protocol.AtaDiskMaxBytes); err != nil {
		t.Fatal(err)
	}
	f, size, _, err := openHardDiskPickerFile(context.Background(), nil, nil, path)
	if err != nil || size != protocol.AtaDiskMaxBytes {
		t.Fatalf("maximum size=%d err=%v", size, err)
	}
	f.Close()
	if f, _, _, err := openPickerFile(context.Background(), nil, nil, path, storage.MaxFileBytes); err == nil {
		f.Close()
		t.Fatal("ordinary file allowance was widened")
	}
}
