// SPDX-License-Identifier: GPL-3.0-or-later
package sdhci

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

func TestColdPIOFAT32(t *testing.T) {
	for _, tool := range []string{"mformat", "mmd", "mcopy"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("mtools unavailable")
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	payload := []byte(`{"input":"usb","model":true}`)
	config := filepath.Join(dir, "BOOT.JSON")
	if err = os.WriteFile(config, payload, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mformat", "-i", path, "-F", "::"}, {"mmd", "-i", path, "::FES"}, {"mcopy", "-i", path, config, "::FES/BOOT.JSON"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	c := newCard()
	c.image, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(c.image)
	disk, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	if disk.SectorCount() != uint64(len(c.image)/512) {
		t.Fatal("CSD capacity differs from image")
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := volume.ReadFile("FES/BOOT.JSON")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("FAT32 read: %s %v", got, err)
	}
	if err = disk.Flush(); err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(c.image) != before {
		t.Fatal("read-only transport changed image")
	}
	c.coldFault = "removed"
	if _, err = volume.ReadFile("FES/BOOT.JSON"); err == nil {
		t.Fatal("removed card returned cached/successful data")
	}
}
