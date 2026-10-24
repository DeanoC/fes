package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in test runs against a disposable Samba fixture, never a household share.
func TestSMBIntegration(t *testing.T) {
	source := os.Getenv("FES_TEST_SMB_SHARE")
	if source == "" {
		t.Skip("set FES_TEST_SMB_SHARE for a disposable SMB fixture")
	}
	for _, user := range []string{"", "player"} {
		t.Run("user="+user, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			m := New(filepath.Join(t.TempDir(), "drives.json"))
			d, err := m.Save(Drive{Kind: "smb", Source: source, Username: user, Password: "test-password"})
			if err != nil {
				t.Fatal(err)
			}
			entries, err := m.Entries(ctx, d.ID, "Media")
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name != "game.p" {
				t.Fatalf("entries %+v", entries)
			}
			f, size, err := m.Open(ctx, d.ID, "Media/game.p")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			body, err := io.ReadAll(f)
			if err != nil || string(body) != "cassette bytes" || size != 14 {
				t.Fatalf("read %q %d %v", body, size, err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := New(filepath.Join(t.TempDir(), "drives.json"))
	d, err := m.Save(Drive{Kind: "smb", Source: source, Username: "player", Password: "wrong-password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Entries(ctx, d.ID, "."); err == nil {
		t.Fatal("accepted wrong SMB password")
	}
}
