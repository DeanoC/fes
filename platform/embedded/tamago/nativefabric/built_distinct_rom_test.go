// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// These opt-in checks consume real sealed archives and an independently built
// SDK anchor. Only secure calls, SD ownership and the register endpoint are
// modeled. Every actual descriptor publication also passes the C decoder.
func builtDistinctROMPrograms(t *testing.T) (*romProgramFixture, []packagestore.Request, []*StoredConfigurationPlan) {
	t.Helper()
	anchor, catalog := os.Getenv("FES_NANO_DISTINCT_COMPUTER_BUILD"), os.Getenv("FES_NANO_DISTINCT_ROM_CATALOG")
	if anchor == "" && catalog == "" {
		t.Skip("provide a native computer SDK anchor and two distinct sealed ROM packages")
	}
	if anchor == "" || catalog == "" {
		t.Fatal("both distinct-package artifact paths are required")
	}
	read := func(path string, limit int64) []byte {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
			t.Fatal("bounded regular artifact required", path, err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil || int64(len(data)) != info.Size() {
			t.Fatal("artifact changed or read failed", path, err)
		}
		return data
	}
	r := romProgramTestFor(t, NativeComputerProfile)
	contextRaw := read(filepath.Join(anchor, "hps-context.json"), 8192)
	control := read(filepath.Join(anchor, "hps-control.json"), 8192)
	computer := read(filepath.Join(anchor, "hps-computer.json"), 8192)
	r.cpu.digest = sha256.Sum256(contextRaw)
	v, err := NewComputerHPSContextVerifier(r.cpu, contextRaw, control, computer, read(filepath.Join(anchor, "hps-rom.json"), 8192))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewProgramLoader(v, read(filepath.Join(anchor, "program-platform.json"), 8192), control,
		read(filepath.Join(anchor, "hps-configuration.json"), 32768), computer)
	if err != nil {
		t.Fatal(err)
	}
	r.f.loader = l
	r.cpu.chunkBytes = l.configuration.ChunkBytes
	r.cpu.memory = make([]byte, l.configuration.Size)
	r.f.arena, err = NewProgramArena(l.configuration.Base, r.cpu.memory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	r.f.files.files = fstest.MapFS{}
	entries, err := os.ReadDir(filepath.Join(catalog, "FES"))
	if err != nil || len(entries) == 0 || len(entries) > 16 {
		t.Fatal("bounded FES catalog required", err)
	}
	for _, entry := range entries {
		r.f.files.files["FES/"+entry.Name()] = &fstest.MapFile{Data: read(filepath.Join(catalog, "FES", entry.Name()), 8<<20)}
	}
	requests, err := ReadProgramSelections(context.Background(), r.f.files, "FES/CORESEL.JSN")
	if err != nil || len(requests) != 2 || requests[0].PackageID == requests[1].PackageID {
		t.Fatal("two distinct sealed packages required", requests, err)
	}
	var plans []*StoredConfigurationPlan
	for _, request := range requests {
		selected, err := r.f.store.Retain(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := l.Preflight(selected)
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	a, b := plans[0].Identity(), plans[1].Identity()
	if a.BuildID == b.BuildID || a.CoreID == b.CoreID || a.ProgrammedSHA256 == b.ProgrammedSHA256 ||
		len(plans[0].state.program.rom) != 8192 || len(plans[1].state.program.rom) != 4096 || r.cpu.calls != 0 {
		t.Fatal("actual distinct ZX81/ZX80 payloads and 8/4 KiB ROMs required; no firmware access", a, b)
	}
	reserve := func(data []byte) (*Region, error) {
		region, err := r.f.arena.Reserve(data)
		if err != nil {
			return region, err
		}
		var plan *StoredConfigurationPlan
		for _, candidate := range plans {
			if bytes.Equal(data, candidate.state.payload) {
				plan = candidate
			}
		}
		if plan == nil {
			t.Fatal("service reserved bytes outside the actual selected packages")
		}
		r.cpu.expectedROM = bytes.Clone(plan.state.program.rom)
		build, err := hex.DecodeString(plan.Identity().BuildID)
		if err != nil || len(build) != 16 {
			t.Fatal("invalid actual build identity", err)
		}
		for i := range 8 {
			r.cpu.words[8+i] = uint16(build[2*i]) | uint16(build[2*i+1])<<8
		}
		if r.f.fault == "identity" {
			r.cpu.words[8] ^= 1
		}
		r.cpu.wire = &transferWire{t: t, region: region, source: bytes.Clone(data), chunkBytes: r.cpu.chunkBytes}
		if r.f.fault == "write" {
			r.cpu.wire.fault = "write"
		}
		return region, nil
	}
	r.f.s, err = NewProgramService(l, r.f.store, reserve, computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	return r, requests, plans
}

func TestBuiltDistinctROMProgramsLaunchAndReturn(t *testing.T) {
	r, requests, plans := builtDistinctROMPrograms(t)
	b, err := NewProgramBackend(r.f.s, []ProgramEntry{
		{ID: "zx81", Title: "ZX81", Request: requests[0]}, {ID: "zx80", Title: "ZX80", Request: requests[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	ctx := context.Background()
	var old hostclient.SessionResult
	for cycle, index := range []int{0, 1, 0} {
		play, err := client.Launch(ctx, []string{"zx81", "zx80"}[index])
		view := r.f.s.Status()
		if err != nil || play.State != "active" || view.State != ProgramRunning || view.Identity != plans[index].Identity() ||
			view.Generation != uint64(cycle*2+1) || r.f.lease.r.Active ||
			!bytes.Equal(r.cpu.wire.received, plans[index].state.payload) ||
			play.CorePackage == nil || play.CorePackage.ROMLink == nil ||
			play.CorePackage.ROMLink.SourceSize != int64(len(r.cpu.expectedROM)) {
			t.Fatal("actual package launch or retained ROM identity changed", play, view, err)
		}
		if cycle > 0 {
			calls := r.cpu.calls
			stale, err := client.StopExpectedStamped(ctx, old, hostclient.ClientStamp{})
			if (err == nil && stale.ErrorCode == "") || r.cpu.calls != calls {
				t.Fatal("old package session controlled its replacement")
			}
		}
		returned, err := client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{})
		if err != nil || returned.State != "idle" || r.f.s.Status().State != ProgramReady ||
			!r.f.lease.r.Active || r.f.s.Status().Generation != uint64(cycle*2+2) {
			t.Fatal("actual package return did not restore owned storage", returned, err)
		}
		old = play
		t.Logf("PASS actual sealed package cycle=%d core=%s package=%s payload=%s; modeled hardware", cycle, view.Identity.CoreID, view.Identity.PackageID, view.Identity.ProgrammedSHA256)
	}
	if r.cpu.preparedHeaders != 3 || r.cpu.romOpens != 3 || r.cpu.configurations != 3 || r.f.lease.returns != 3 {
		t.Fatal("missing C descriptor checks, transfers or owned returns")
	}
}

func TestBuiltDistinctROMProgramsRejectCrossedAndDamagedContent(t *testing.T) {
	for _, fault := range []string{"archive", "metadata", "rom", "receipt"} {
		t.Run(fault, func(t *testing.T) {
			r, requests, _ := builtDistinctROMPrograms(t)
			request := requests[1]
			switch fault {
			case "archive":
				r.f.files.files[request.Path].Data[512] ^= 1
			case "metadata":
				request.MetadataPath, request.MetadataSHA256 = requests[0].MetadataPath, requests[0].MetadataSHA256
			case "rom":
				r.f.files.files[request.Sources[0].Path].Data[0] ^= 1
			case "receipt":
				request.ReceiptSHA256 = requests[0].ReceiptSHA256
			}
			if _, err := r.f.s.Launch(context.Background(), 0, request); err == nil || r.cpu.calls != 0 ||
				r.f.s.Status().State != ProgramReady || !r.f.lease.r.Active || r.f.lease.returns != 0 {
				t.Fatal("bad actual content crossed storage containment", fault, err)
			}
		})
	}
}

func TestBuiltDistinctROMProgramsRetireHardwareFailures(t *testing.T) {
	for _, fault := range []string{"write", "identity", "return"} {
		t.Run(fault, func(t *testing.T) {
			r, requests, _ := builtDistinctROMPrograms(t)
			r.f.fault = fault
			play, err := r.f.s.Launch(context.Background(), 0, requests[1])
			if fault == "return" {
				if err != nil {
					t.Fatal(err)
				}
				r.cpu.loseClose = true
				_, err = r.f.s.Return(context.Background(), play)
			}
			if err == nil || r.f.s.Status().State != ProgramRebootRequired || r.f.lease.r.Active || r.f.lease.returns != 0 {
				t.Fatal("uncertain actual-package operation resumed storage", fault, err)
			}
			calls := r.cpu.calls
			if _, err := r.f.s.Launch(context.Background(), r.f.s.Status().Generation, requests[0]); err == nil || r.cpu.calls != calls {
				t.Fatal("failed actual-package service retried")
			}
		})
	}
}
