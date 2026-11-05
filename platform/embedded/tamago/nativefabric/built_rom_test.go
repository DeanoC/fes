// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

type preflightOnlyROMCPU struct{ calls int }

func (c *preflightOnlyROMCPU) Invoke([7]uint64) ([4]uint64, error) {
	c.calls++
	return [4]uint64{}, errors.New("artifact preflight cannot call firmware")
}

// This opt-in artifact check uses a freshly built SDK's actual package, maps,
// receipt and catalog, with a modeled active SD lease and no firmware access.
func TestBuiltROMSelectionPreflightsWithoutFirmwareIO(t *testing.T) {
	dir := os.Getenv("FES_NANO_ROM_BUILD")
	if dir == "" {
		t.Skip("set FES_NANO_ROM_BUILD to a freshly built held-ROM SDK directory")
	}
	read := func(name string, limit int64) []byte {
		t.Helper()
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil || len(data) == 0 || int64(len(data)) > limit {
			t.Fatal("bounded built resource required", name, err)
		}
		return data
	}
	cpu := &preflightOnlyROMCPU{}
	control, computer := read("hps-control.json", 8192), read("hps-computer.json", 8192)
	v, err := NewROMHPSContextVerifier(cpu, read("hps-context.json", 8192), control, computer, read("hps-rom.json", 8192))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewProgramLoader(v, read("program-platform.json", 8192), control, read("hps-configuration.json", 32768), computer)
	if err != nil {
		t.Fatal(err)
	}
	_, _, lease, files, store := programLoaderFixture(t)
	for name, artifact := range map[string]string{"FES/PKGTEST.BIN": "package.fcore", "FES/FIRMWARE.BIN": "FIRMWARE.BIN", "FES/PROGRAM.JSN": "PROGRAM.JSN", "FES/CORESEL.JSN": "CORESEL.JSN"} {
		limit := int64(16384)
		if name == "FES/PKGTEST.BIN" {
			limit = 8 << 20
		}
		files.files[name] = &fstest.MapFile{Data: read(artifact, limit)}
	}
	requests, err := ReadProgramSelections(context.Background(), files, "FES/CORESEL.JSN")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		selection, err := store.Retain(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := l.Preflight(selection)
		if err != nil {
			t.Fatal(err)
		}
		expected := v.Identity()
		expected.ActivationReceiptSHA256 = request.ReceiptSHA256
		if plan.Identity() != expected || plan.ROMReceiptSHA256() != request.ReceiptSHA256 || plan.ROMReceiptSHA256() == "" {
			t.Fatal("built selection lost its source identity or retained receipt")
		}
	}
	if cpu.calls != 0 || !lease.r.Active || !lease.r.Prepared {
		t.Fatal("artifact preflight touched firmware or contained SD")
	}
	t.Logf("preflighted %d built choices with real retained ROMs; no firmware calls or SD containment", len(requests))
}
