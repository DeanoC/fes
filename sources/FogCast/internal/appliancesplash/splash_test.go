package appliancesplash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/DeanoC/FogCast/appliance/store"
)

func writeIdle(t *testing.T, dir string, payload []byte) (idle, record, sum string) {
	t.Helper()
	got := sha256.Sum256(payload)
	sum = hex.EncodeToString(got[:])
	idle = filepath.Join(dir, "idle.rbf")
	record = filepath.Join(dir, "build-inputs")
	if err := os.WriteFile(idle, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	body := "format=1\nidle_sha256=" + sum + "\nidle_size=" + itoa(len(payload)) + "\n"
	if err := os.WriteFile(record, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return idle, record, sum
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func installerFor(t *testing.T, payload []byte) (*Installer, string, string) {
	t.Helper()
	dir := t.TempDir()
	idle, record, sum := writeIdle(t, dir, payload)
	fat := filepath.Join(dir, "fat", "idle.rbf")
	if err := os.Mkdir(filepath.Dir(fat), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fat, []byte("previous-splash"), 0o644); err != nil {
		t.Fatal(err)
	}
	image := strings.Repeat("ab", 32)
	p := New(Paths{Idle: idle, Record: record, FAT: fat, ReleaseRoot: filepath.Join(dir, "releases")})
	return p, fat, image + ":" + sum
}

func TestStageKeepsFATUntilPublish(t *testing.T) {
	payload := []byte("splash-bytes-for-image")
	p, fat, marked := installerFor(t, payload)
	image, sum, _ := strings.Cut(marked, ":")
	if err := p.Stage(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(fat)
	if err != nil || string(got) != "previous-splash" {
		t.Fatalf("FAT changed during stage: %q %v", got, err)
	}
	rbf, digest, err := store.SplashPaths(p.ReleaseRoot, image)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(rbf)
	if err != nil || !bytes.Equal(staged, payload) {
		t.Fatalf("staged splash %q %v", staged, err)
	}
	side, err := os.ReadFile(digest)
	if err != nil || strings.TrimSpace(string(side)) != sum {
		t.Fatalf("sidecar %q %v", side, err)
	}
	if err := p.Publish(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(fat)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("published FAT %q %v", got, err)
	}
	if err := p.Install(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(fat))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("temporary left beside FAT splash: %s", entry.Name())
		}
	}
	if p.FATSHA256() != sum || p.IdleSHA256() != sum {
		t.Fatalf("digests fat=%s idle=%s want %s", p.FATSHA256(), p.IdleSHA256(), sum)
	}
}

func TestRejectsRecordMismatchWithoutWriting(t *testing.T) {
	payload := []byte("real-splash")
	p, fat, marked := installerFor(t, payload)
	image, _, _ := strings.Cut(marked, ":")
	if err := os.WriteFile(p.Record, []byte("format=1\nidle_sha256="+strings.Repeat("c", 64)+"\nidle_size=11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := p.Install(context.Background(), image)
	if err == nil || !strings.Contains(err.Error(), "differs from the image record") {
		t.Fatal(err)
	}
	got, readErr := os.ReadFile(fat)
	if readErr != nil || string(got) != "previous-splash" {
		t.Fatalf("FAT changed on mismatch: %q %v", got, readErr)
	}
	rbf, _, pathErr := store.SplashPaths(p.ReleaseRoot, image)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Lstat(rbf); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("staged splash exists after mismatch: %v", statErr)
	}
}

func TestRejectsSymlinkedIdle(t *testing.T) {
	payload := []byte("real-splash")
	p, fat, marked := installerFor(t, payload)
	image, _, _ := strings.Cut(marked, ":")
	linked := p.Idle + ".link"
	if err := os.Rename(p.Idle, linked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, p.Idle); err != nil {
		t.Fatal(err)
	}
	if err := p.Stage(context.Background(), image); err == nil {
		t.Fatal("symlinked idle accepted")
	}
	got, err := os.ReadFile(fat)
	if err != nil || string(got) != "previous-splash" {
		t.Fatalf("FAT changed: %q %v", got, err)
	}
}

func TestPublishReplacesSymlinkAndRefusesDirectory(t *testing.T) {
	payload := []byte("real-splash")
	p, fat, marked := installerFor(t, payload)
	image, _, _ := strings.Cut(marked, ":")
	if err := p.Stage(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fat); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), image); err == nil {
		t.Fatal("directory destination accepted")
	}
	if err := os.Remove(fat); err != nil {
		t.Fatal(err)
	}
	other := fat + ".target"
	if err := os.WriteFile(other, []byte("elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, fat); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(fat)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("FAT splash is still a symlink: %v %v", info, err)
	}
	got, err := os.ReadFile(fat)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("replaced FAT %q %v", got, err)
	}
	kept, err := os.ReadFile(other)
	if err != nil || string(kept) != "elsewhere" {
		t.Fatalf("symlink target changed: %q %v", kept, err)
	}
}
