// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

func TestFreshFATPackageCreationThenRetainAndContain(t *testing.T) {
	body, request, want := basic(t)
	image := filepath.Join(t.TempDir(), "fresh.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("mformat", "-i", image, "-F", "::").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	device, err := block.OpenFile(image, true)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	volume, err := fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Trusted startup owns storage before the runtime takes its lease. These
	// operations create the directory and archive without a preallocated file.
	if err = volume.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err = volume.CreateFile(ctx, request.Path, body); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command("mtype", "-i", image, "::"+request.Path).Output()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("independent archive readback", err)
	}
	volume, err = fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	l := lease()
	s, files := open(t, volume, l)
	selection, err := s.Retain(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := s.Contain(selection)
	if err != nil {
		t.Fatal(err)
	}
	reads := files.reads.Load()
	selection, err = handoff.Selection()
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(selection.Archive().Payload())
	if err != nil || !bytes.Equal(got, want) || files.reads.Load() != reads {
		t.Fatal("contained payload was not retained privately", err)
	}
}
