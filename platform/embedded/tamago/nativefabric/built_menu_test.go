// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestBuiltMenuAndDistinctComputersShareOneLoaderWithoutFirmwareIO(t *testing.T) {
	dir := os.Getenv("FES_NANO_MIXED_INPUTS")
	if dir == "" {
		t.Skip("set FES_NANO_MIXED_INPUTS to source-checked mixed-role inputs")
	}
	read := func(path string, limit int64) []byte {
		t.Helper()
		p := filepath.Join(dir, path)
		i, err := os.Lstat(p)
		if err != nil || !i.Mode().IsRegular() || i.Size() <= 0 || i.Size() > limit {
			t.Fatal("bounded regular input required", path, err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	cpu := &preflightOnlyROMCPU{}
	control, comp := read("computer/hps-control.json", 8192), read("computer/hps-computer.json", 8192)
	v, err := NewComputerHPSContextVerifier(cpu, read("computer/hps-context.json", 8192), control, comp, read("computer/hps-rom.json", 8192))
	if err != nil {
		t.Fatal(err)
	}
	bootID, bootDigest := v.Identity(), v.digest
	menu := CompiledMenuProgram{read("menu/hps-context.json", 8192), read("menu/program-platform.json", 8192), read("menu/hps-control.json", 8192), read("menu/hps-reader-memory.json", 8192), read("menu/hps-menu-read.json", 8192)}
	l, err := NewProgramLoaderWithMenu(v, read("computer/program-platform.json", 8192), control, read("computer/hps-configuration.json", 32768), comp, menu)
	if err != nil {
		t.Fatal(err)
	}
	_, _, lease, files, store := programLoaderFixture(t)
	entries, err := os.ReadDir(filepath.Join(dir, "FES"))
	if err != nil || len(entries) > 16 {
		t.Fatal(err)
	}
	for _, entry := range entries {
		files.files["FES/"+entry.Name()] = &fstest.MapFile{Data: read("FES/"+entry.Name(), 8<<20)}
	}
	requests, err := ReadProgramSelections(context.Background(), files, "FES/CORESEL.JSN")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	coreIDs := map[string]bool{}
	roles := map[programRole]int{}
	for _, request := range requests {
		s, err := store.Retain(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := l.Preflight(s)
		if err != nil {
			t.Fatal(request.Path, err)
		}
		if plan.Identity().PackageID != request.PackageID || plan.state.v != v {
			t.Fatal("lost package or boot binding")
		}
		if plan.state.program.role == programRoleComputer && (plan.ROMReceiptSHA256() == "" || plan.ROMReceiptSHA256() != request.ReceiptSHA256) {
			t.Fatal("lost retained ROM receipt")
		}
		if plan.state.program.role == programRoleMenu && (plan.ROMReceiptSHA256() != "" || len(plan.state.program.rom) != 0) {
			t.Fatal("menu adopted ROM")
		}
		ids[plan.Identity().PackageID] = true
		coreIDs[plan.Identity().CoreID] = true
		roles[plan.state.program.role]++
	}
	if len(ids) != 3 || !coreIDs["fes.zx81"] || !coreIDs["fes.zx80"] || roles[programRoleMenu] != 1 || roles[programRoleComputer] != 2 || cpu.calls != 0 || !lease.r.Active || v.Identity() != bootID || v.digest != bootDigest || v.menuMemory != nil {
		t.Fatal("real packages did not preserve independent roles and immutable boot", roles, coreIDs, cpu.calls)
	}
	t.Log("retained real ZX81, ZX80 and menu packages in one source-checked loader; zero firmware calls or SD containment")
}
