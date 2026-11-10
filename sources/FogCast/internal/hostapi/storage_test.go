package hostapi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
)

type driveFixture struct{ root string }

func (f driveFixture) Discover(context.Context) ([]storage.Drive, error) {
	return []storage.Drive{{Kind: "usb", Source: "fixture-uuid", Label: "Test USB"}}, nil
}

// diskReadFixture creates sparse files, keeping the 128 MiB boundary tests
// cheap while still testing the full stream length and boundary bytes.
func diskReadFixture(t *testing.T, root string) {
	t.Helper()
	for name, size := range map[string]int64{
		"min.img":       protocol.AtaDiskMinBytes,
		"extended.img":  storage.MaxFileBytes + protocol.AtaDiskSectorBytes,
		"max.img":       protocol.AtaDiskMaxBytes,
		"over.img":      protocol.AtaDiskMaxBytes + protocol.AtaDiskSectorBytes,
		"too-small.img": protocol.AtaDiskMinBytes - protocol.AtaDiskSectorBytes,
		"unaligned.img": protocol.AtaDiskMinBytes + 1,
	} {
		f, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(size); err == nil {
			_, err = f.WriteAt([]byte{0x5a}, 0)
		}
		if err == nil {
			_, err = f.WriteAt([]byte{0xa5}, size-1)
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "directory.img"), 0755); err != nil {
		t.Fatal(err)
	}
	outside, err := os.Create(filepath.Join(filepath.Dir(root), "outside.img"))
	if err != nil {
		t.Fatal(err)
	}
	if err := outside.Truncate(protocol.AtaDiskMinBytes); err != nil {
		outside.Close()
		t.Fatal(err)
	}
	outside.Close()
	if err := os.Symlink("../outside.img", filepath.Join(root, "escape-link.img")); err != nil {
		t.Fatal(err)
	}
}

// Test both the browser and paired-launcher path against a saved drive. The
// SMB invocation uses a real disposable server, not the USB mount fake.
func assertHardDiskDriveReads(t *testing.T, manager *storage.Manager, drive storage.Drive) {
	t.Helper()
	d, err := manager.Save(drive)
	if err != nil {
		t.Fatal(err)
	}
	service := &settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{{Name: "kit-a", Enabled: true, TargetID: launcherID}}}}
	browser := hostapi.New(service, hostapi.WithStorage(manager))
	launcher, err := hostapi.NewLauncherHandler(browser, hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherID}}})
	if err != nil {
		t.Fatal(err)
	}
	browserServer, launcherServer := httptest.NewServer(browser), httptest.NewServer(launcher)
	t.Cleanup(browserServer.Close)
	t.Cleanup(launcherServer.Close)
	endpoint := "/api/v1/storage/drives/" + d.ID + "/file?"
	for _, tc := range []struct {
		name, path, extra string
		status            int
		size              int64
	}{
		{"minimum", "min.img", "&purpose=hard-disk", http.StatusOK, protocol.AtaDiskMinBytes},
		{"above-ordinary", "extended.img", "&purpose=hard-disk", http.StatusOK, storage.MaxFileBytes + protocol.AtaDiskSectorBytes},
		{"maximum", "max.img", "&purpose=hard-disk", http.StatusOK, protocol.AtaDiskMaxBytes},
		{"ordinary-default", "extended.img", "", http.StatusBadRequest, 0},
		{"ordinary-unaligned", "unaligned.img", "", http.StatusOK, protocol.AtaDiskMinBytes + 1},
		{"ordinary-expanded", "extended.img", "&max_bytes=34603008", http.StatusOK, storage.MaxFileBytes + protocol.AtaDiskSectorBytes},
		{"ordinary-cannot-ask-128", "max.img", "&max_bytes=134217728", http.StatusBadRequest, 0},
		{"over-maximum", "over.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"below-minimum", "too-small.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"unaligned", "unaligned.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"directory", "directory.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"traversal", "../outside.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"escaping-symlink", "escape-link.img", "&purpose=hard-disk", http.StatusBadRequest, 0},
		{"unknown-purpose", "min.img", "&purpose=other", http.StatusBadRequest, 0},
		{"empty-purpose", "min.img", "&purpose=", http.StatusBadRequest, 0},
		{"duplicate-purpose", "min.img", "&purpose=hard-disk&purpose=hard-disk", http.StatusBadRequest, 0},
		{"duplicate-path", "min.img", "&purpose=hard-disk&path=max.img", http.StatusBadRequest, 0},
		{"mixed-limit", "min.img", "&purpose=hard-disk&max_bytes=33554432", http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestURL := browserServer.URL + endpoint + "path=" + url.QueryEscape(tc.path) + tc.extra
			resp, err := browserServer.Client().Get(requestURL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.status, body)
			}
			if tc.status == http.StatusOK {
				if resp.ContentLength != tc.size {
					t.Fatalf("content length %d, want %d", resp.ContentLength, tc.size)
				}
				first := make([]byte, 1)
				if _, err := io.ReadFull(resp.Body, first); err != nil || first[0] != 0x5a {
					t.Fatalf("first byte %x, %v", first, err)
				}
				if n, err := io.CopyN(io.Discard, resp.Body, tc.size-2); err != nil || n != tc.size-2 {
					t.Fatalf("interior bytes %d, %v", n, err)
				}
				last := make([]byte, 1)
				if _, err := io.ReadFull(resp.Body, last); err != nil || last[0] != 0xa5 {
					t.Fatalf("last byte %x, %v", last, err)
				}
			}
		})
	}
	// An unpaired caller cannot obtain the extended read, even with its purpose.
	req, _ := http.NewRequest(http.MethodGet, launcherServer.URL+endpoint+"path=min.img&purpose=hard-disk", nil)
	resp, err := launcherServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unpaired purpose read: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, launcherServer.URL+endpoint+"path=min.img&purpose=hard-disk", nil)
	req.Header.Set("Authorization", "Bearer "+launcherToken)
	req.Header.Set("X-FogCast-Target-ID", launcherIDB)
	resp, err = launcherServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-target purpose read: %d", resp.StatusCode)
	}
	// An authenticated paired launcher can use the same scoped route.
	req, _ = http.NewRequest(http.MethodGet, launcherServer.URL+endpoint+"path=min.img&purpose=hard-disk", nil)
	req.Header.Set("Authorization", "Bearer "+launcherToken)
	req.Header.Set("X-FogCast-Target-ID", launcherID)
	resp, err = launcherServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength != protocol.AtaDiskMinBytes {
		t.Fatalf("paired purpose read: status %d length %d", resp.StatusCode, resp.ContentLength)
	}
	if _, err = io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
}

func TestHardDiskSavedUSBDriveReadLimits(t *testing.T) {
	root := t.TempDir()
	diskReadFixture(t, root)
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	manager.Backend = driveFixture{root}
	assertHardDiskDriveReads(t, manager, storage.Drive{Kind: "usb", Source: "fixture-usb"})
}

type deadlineDriveFixture struct {
	root      string
	deadlines chan time.Duration
}

func (f deadlineDriveFixture) Discover(context.Context) ([]storage.Drive, error) { return nil, nil }
func (f deadlineDriveFixture) Mount(ctx context.Context, _ storage.Drive, _ string) (string, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return "", errors.New("drive read has no deadline")
	}
	f.deadlines <- time.Until(deadline)
	return f.root, nil
}

func TestHardDiskDriveReadUsesScopedBudget(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "disk.img")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(protocol.AtaDiskMinBytes); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	deadlines := make(chan time.Duration, 3)
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	manager.Backend = deadlineDriveFixture{root: root, deadlines: deadlines}
	drive, err := manager.Save(storage.Drive{Kind: "usb", Source: "deadline-usb"})
	if err != nil {
		t.Fatal(err)
	}
	handler := hostapi.New(&fakeService{}, hostapi.WithStorage(manager))
	for _, tc := range []struct {
		name, query string
		parentLimit time.Duration
		want        time.Duration
	}{
		{"ordinary", "", 0, 30 * time.Second},
		{"hard-disk", "&purpose=hard-disk", 0, fogcast.HardDiskImportBudget(protocol.AtaDiskMaxBytes)},
		{"hard-disk-parent-deadline", "&purpose=hard-disk", 2 * time.Second, 2 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/drives/"+drive.ID+"/file?path=disk.img"+tc.query, nil)
			req.Host = "localhost"
			if tc.parentLimit != 0 {
				ctx, cancel := context.WithTimeout(req.Context(), tc.parentLimit)
				defer cancel()
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			got := <-deadlines
			if got > tc.want || got < tc.want-time.Second {
				t.Fatalf("read budget %s, want approximately %s", got, tc.want)
			}
		})
	}
}

type driveWriteDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	failWrite bool
}

func (w *driveWriteDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *driveWriteDeadlineRecorder) Write(p []byte) (int, error) {
	if w.failWrite {
		return 0, errors.New("disconnected client")
	}
	return w.ResponseRecorder.Write(p)
}

func TestHardDiskDriveWriteDeadlineIsScopedAndCleared(t *testing.T) {
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "disk.img"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(protocol.AtaDiskMinBytes); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	manager.Backend = driveFixture{root}
	drive, err := manager.Save(storage.Drive{Kind: "usb", Source: "writer-usb"})
	if err != nil {
		t.Fatal(err)
	}
	handler := hostapi.New(&fakeService{}, hostapi.WithStorage(manager))
	for _, tc := range []struct {
		name, query string
		parentLimit time.Duration
		failWrite   bool
		want        time.Duration
	}{
		{name: "ordinary"},
		{name: "hard-disk", query: "&purpose=hard-disk", want: fogcast.HardDiskImportBudget(protocol.AtaDiskMaxBytes)},
		{name: "shorter-parent", query: "&purpose=hard-disk", parentLimit: 2 * time.Second, want: 2 * time.Second},
		{name: "disconnected", query: "&purpose=hard-disk", failWrite: true, want: fogcast.HardDiskImportBudget(protocol.AtaDiskMaxBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/drives/"+drive.ID+"/file?path=disk.img"+tc.query, nil)
			req.Host = "localhost"
			if tc.parentLimit != 0 {
				ctx, cancel := context.WithTimeout(req.Context(), tc.parentLimit)
				defer cancel()
				req = req.WithContext(ctx)
			}
			w := &driveWriteDeadlineRecorder{ResponseRecorder: httptest.NewRecorder(), failWrite: tc.failWrite}
			start := time.Now()
			handler.ServeHTTP(w, req)
			if tc.query == "" {
				if len(w.deadlines) != 0 {
					t.Fatalf("ordinary read changed write deadline: %v", w.deadlines)
				}
				return
			}
			if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
				t.Fatalf("write deadlines %v, want set then clear", w.deadlines)
			}
			if got := w.deadlines[0].Sub(start); got > tc.want+time.Second || got < tc.want-time.Second {
				t.Fatalf("write deadline %s, want approximately %s", got, tc.want)
			}
		})
	}
}

// Run with a disposable Samba share containing the diskReadFixture files.
// FES_TEST_SMB_1566_SHARE must name that fixture, never a household share.
func TestHardDiskSavedSMBDriveReadLimits(t *testing.T) {
	source := os.Getenv("FES_TEST_SMB_1566_SHARE")
	if source == "" {
		t.Skip("set FES_TEST_SMB_1566_SHARE for a disposable Samba fixture")
	}
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	assertHardDiskDriveReads(t, manager, storage.Drive{Kind: "smb", Source: source, Username: "player", Password: "test-password"})
}
func (f driveFixture) Mount(context.Context, storage.Drive, string) (string, error) {
	return f.root, nil
}
func TestHostDriveClientBrowsesReadsAndRemembers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "BIOS"), 0755); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("b", 8192)
	if err := os.WriteFile(filepath.Join(root, "BIOS", "bios.bin"), []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	expansionPath := filepath.Join(root, "expansion.fexp")
	if err := os.WriteFile(expansionPath, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(expansionPath, storage.MaxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	manager.Backend = driveFixture{root}
	server := httptest.NewServer(hostapi.New(&fakeService{}, hostapi.WithStorage(manager)))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	found, err := client.StorageDrives(ctx, true)
	if err != nil || len(found.Drives) != 1 {
		t.Fatalf("discovery %+v %v", found, err)
	}
	d := found.Drives[0]
	d.BIOSFolder = "BIOS"
	d, err = client.SaveStorageDrive(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := client.StorageEntries(ctx, d.ID, "BIOS")
	if err != nil || len(entries) != 1 || entries[0].Name != "bios.bin" {
		t.Fatalf("browse %+v %v", entries, err)
	}
	f, size, err := client.OpenStorageFile(ctx, d.ID, "BIOS/bios.bin")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil || size != 8192 || string(body) != payload {
		t.Fatalf("read size=%d err=%v", size, err)
	}
	if _, _, err = client.OpenStorageFile(ctx, d.ID, "expansion.fexp"); err == nil {
		t.Fatal("ordinary host read exceeded the 32 MiB limit")
	}
	archive, archiveSize, err := client.OpenStorageFileWithLimit(ctx, d.ID, "expansion.fexp", storage.MaxFileBytes+1)
	if err != nil {
		t.Fatal(err)
	}
	archive.Close()
	if archiveSize != storage.MaxFileBytes+1 {
		t.Fatalf("extended host read size %d", archiveSize)
	}
	if _, err = client.StorageEntries(ctx, d.ID, "../../etc"); err == nil {
		t.Fatal("remote traversal accepted")
	}
	secret, err := client.SaveStorageDrive(ctx, storage.Drive{Kind: "smb", Source: "//nas/share", Username: "player", Password: "hidden-password"})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Password != "" || !secret.PasswordSet {
		t.Fatal("write response exposed password")
	}
	inventory, err := client.StorageDrives(ctx, false)
	if err != nil || len(inventory.Drives) != 2 {
		t.Fatalf("inventory %+v %v", inventory, err)
	}
	for _, d := range inventory.Drives {
		if d.Password != "" {
			t.Fatal("inventory exposed password")
		}
	}
	if err = client.ForgetStorageDrive(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.Open(ctx, d.ID, "BIOS/bios.bin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forgotten drive accessible: %v", err)
	}
}
func TestMalformedDriveRequestHasOneErrorResponse(t *testing.T) {
	manager := storage.New(filepath.Join(t.TempDir(), "drives.json"))
	manager.Backend = driveFixture{t.TempDir()}
	handler := hostapi.New(&fakeService{}, hostapi.WithStorage(manager))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/storage/drives", strings.NewReader(`{"kind":"smb","unknown":true}`))
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || strings.Count(rec.Body.String(), `"error"`) != 1 {
		t.Fatalf("response %d %s", rec.Code, rec.Body.String())
	}
}
