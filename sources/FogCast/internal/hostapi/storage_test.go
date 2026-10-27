package hostapi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/storage"
)

type driveFixture struct{ root string }

func (f driveFixture) Discover(context.Context) ([]storage.Drive, error) {
	return []storage.Drive{{Kind: "usb", Source: "fixture-uuid", Label: "Test USB"}}, nil
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
