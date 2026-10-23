package atarist

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlankDiskMountsEmpty(t *testing.T) {
	if _, err := exec.LookPath("mdir"); err != nil {
		t.Fatal("mdir from mtools is required to read the generated FAT12 image")
	}
	for _, geometry := range Layouts() {
		geometry := geometry
		t.Run(geometryName(geometry.Tracks, geometry.Heads, geometry.Sectors), func(t *testing.T) {
			disk, err := Format("Work", geometry.Tracks, geometry.Heads, geometry.Sectors)
			if err != nil {
				t.Fatal(err)
			}
			if disk.Name != "WORK" || len(disk.Bytes) != geometry.Tracks*geometry.Heads*geometry.Sectors*512 {
				t.Fatalf("disk %+v size %d", disk.Name, len(disk.Bytes))
			}
			if wordSum(disk.Bytes[:512]) == 0x1234 {
				t.Fatal("boot sector is executable")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "disk.st")
			if err := os.WriteFile(path, disk.Bytes, 0600); err != nil {
				t.Fatal(err)
			}
			out := mtool(t, "mdir", "-i", path, "::")
			if !strings.Contains(out, "No files") {
				t.Fatalf("directory is not empty:\n%s", out)
			}
			if !strings.Contains(out, "WORK") {
				t.Fatalf("volume label missing:\n%s", out)
			}
		})
	}
}

func TestBlankDiskAcceptsAnIndependentWrite(t *testing.T) {
	for _, name := range []string{"mcopy", "mtype"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s is required", name)
		}
	}
	for _, spec := range []struct {
		tracks, heads, sectors int
	}{{80, 2, 9}, {82, 1, 10}} {
		disk, err := Format("notes", spec.tracks, spec.heads, spec.sectors)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		image := filepath.Join(dir, "disk.st")
		note := filepath.Join(dir, "NOTE.TXT")
		if err := os.WriteFile(image, disk.Bytes, 0600); err != nil {
			t.Fatal(err)
		}
		payload := []byte("saved-work\n")
		if err := os.WriteFile(note, payload, 0600); err != nil {
			t.Fatal(err)
		}
		mtool(t, "mcopy", "-i", image, note, "::NOTE.TXT")
		got := mtool(t, "mtype", "-i", image, "::NOTE.TXT")
		if got != string(payload) {
			t.Fatalf("read %q", got)
		}
	}
}

func TestBlankDiskRejectsBadNamesAndShapes(t *testing.T) {
	if _, err := Format("", 80, 2, 9); err != ErrName {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := Format("too-long-name", 80, 2, 9); err != ErrName {
		t.Fatalf("long name: %v", err)
	}
	if _, err := Format("bad/name", 80, 2, 9); err != ErrName {
		t.Fatalf("slash: %v", err)
	}
	if _, err := Format("ok", 40, 2, 9); err != ErrGeometry {
		t.Fatalf("40-track: %v", err)
	}
	if _, err := Format("ok", 80, 2, 0); err != ErrGeometry {
		t.Fatalf("partial: %v", err)
	}
	disk, err := Format("  desk ", 0, 0, 0)
	if err != nil || disk.Tracks != 80 || disk.Heads != 2 || disk.Sectors != 9 || disk.Name != "DESK" || !bytes.Contains(disk.Bytes, []byte("DESK")) {
		t.Fatalf("default disk: %+v %v", disk, err)
	}
}

func geometryName(tracks, heads, sectors int) string {
	return strings.Join([]string{
		itoa(tracks), itoa(heads), itoa(sectors),
	}, "-")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func mtool(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
