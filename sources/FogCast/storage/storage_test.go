package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureBackend struct {
	root    string
	offline bool
}

func (b *fixtureBackend) Discover(context.Context) ([]Drive, error) {
	return []Drive{{Kind: "usb", Source: "test-uuid", Label: "Games USB"}}, nil
}
func (b *fixtureBackend) Mount(context.Context, Drive, string) (string, error) {
	if b.offline {
		return "", errors.New("drive disconnected")
	}
	return b.root, nil
}
func fixtureManager(t *testing.T) (*Manager, *fixtureBackend) {
	t.Helper()
	root := t.TempDir()
	b := &fixtureBackend{root: root}
	m := New(filepath.Join(t.TempDir(), "drives.json"))
	m.Backend = b
	return m, b
}
func TestRememberDriveRestartOfflineAndReconnect(t *testing.T) {
	m, b := fixtureManager(t)
	ctx := context.Background()
	discovered, err := m.List(ctx, true)
	if err != nil || len(discovered) != 1 || discovered[0].Saved {
		t.Fatalf("discover: %+v %v", discovered, err)
	}
	d := discovered[0]
	d.BIOSFolder = "BIOS"
	d.MediaFolder = "Media"
	d, err = m.Save(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"BIOS", "Media"} {
		if err = os.Mkdir(filepath.Join(b.root, folder), 0755); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("cassette bytes")
	if err = os.WriteFile(filepath.Join(b.root, "Media", "game.p"), payload, 0644); err != nil {
		t.Fatal(err)
	}
	restarted := New(m.ConfigPath)
	restarted.Backend = b
	b.offline = true
	saved, err := restarted.List(ctx, false)
	if err != nil || len(saved) != 1 || saved[0].BIOSFolder != "BIOS" || saved[0].MediaFolder != "Media" {
		t.Fatalf("saved %+v %v", saved, err)
	}
	if _, err = restarted.Entries(ctx, d.ID, "Media"); err == nil {
		t.Fatal("offline drive opened")
	}
	b.offline = false
	entries, err := restarted.Entries(ctx, d.ID, "Media")
	if err != nil || len(entries) != 1 || entries[0].Name != "game.p" {
		t.Fatalf("reconnect %+v %v", entries, err)
	}
	f, size, err := restarted.Open(ctx, d.ID, "Media/game.p")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil || string(got) != string(payload) || size != int64(len(payload)) {
		t.Fatalf("bytes %q %d %v", got, size, err)
	}
	if err = restarted.Forget(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = restarted.Open(ctx, d.ID, "Media/game.p"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forgotten drive: %v", err)
	}
}
func TestSMBCredentialsStayPrivateAndSurviveFolderSave(t *testing.T) {
	m, _ := fixtureManager(t)
	d, err := m.Save(Drive{Kind: "smb", Source: `\\nas\Games`, Username: "player", Domain: "HOME", Password: "secret,password", BIOSFolder: "BIOS"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Password != "" || !d.PasswordSet {
		t.Fatalf("public secret %+v", d)
	}
	d.MediaFolder = "Media"
	if _, err = m.Save(d); err != nil {
		t.Fatal(err)
	}
	saved, err := m.drive(d.ID)
	if err != nil || saved.Password != "secret,password" {
		t.Fatalf("secret not retained: %v", err)
	}
	info, err := os.Stat(m.ConfigPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", info, err)
	}
	list, err := m.List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(list)
	if strings.Contains(string(data), "secret") {
		t.Fatal("inventory exposed secret")
	}
	d.PasswordSet = false
	if _, err = m.Save(d); err != nil {
		t.Fatal(err)
	}
	saved, _ = m.drive(d.ID)
	if saved.Password != "" {
		t.Fatal("password clear was ignored")
	}
}
func TestDriveRootRejectsTraversalSymlinksAndNonRegularFiles(t *testing.T) {
	m, b := fixtureManager(t)
	d, err := m.Save(Drive{Kind: "usb", Source: "test-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.bin")
	if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(b.root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../private.bin", "/etc/passwd", `..\private.bin`, "escape", "."} {
		file, _, err := m.Open(context.Background(), d.ID, path)
		if err == nil {
			file.Close()
			t.Errorf("opened %q", path)
		}
	}
	for _, path := range []string{"../", "/etc", `..\`} {
		if _, err = m.Entries(context.Background(), d.ID, path); err == nil {
			t.Errorf("listed %q", path)
		}
	}
}
func TestRejectInvalidDriveSettings(t *testing.T) {
	m, _ := fixtureManager(t)
	for _, d := range []Drive{{Kind: "smb", Source: "//nas"}, {Kind: "smb", Source: "//nas/share/folder"}, {Kind: "smb", Source: "//nas/share", Password: "a\nusername=other"}, {Kind: "usb", Source: "/dev/sda"}, {Kind: "usb", Source: "uuid", BIOSFolder: "../escape"}, {Kind: "folder", Source: "/etc"}} {
		if _, err := m.Save(d); err == nil {
			t.Errorf("saved invalid drive: %+v", d)
		}
	}
}
