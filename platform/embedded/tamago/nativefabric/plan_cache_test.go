// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

func TestPlanCacheReusesPayloadAndDigestsForMenuROMAndUploadCores(t *testing.T) {
	r, _ := mixedMenuTest(t)
	loader, err := r.f.loader.WithSharedCoreShell()
	if err != nil {
		t.Fatal(err)
	}

	// 1. MENU preflight: miss then hit.
	menuReq, menuRec, _ := addMenuSelection(t, r, 0)
	menuSel1 := retainMenu(t, r, menuReq, menuRec)
	menuPlan1, err := loader.Preflight(menuSel1)
	if err != nil {
		t.Fatal(err)
	}
	stats := loader.planCache.stats()
	if stats.misses != 1 || stats.hits != 0 || stats.extracts != 1 || stats.entries != 1 {
		t.Fatalf("unexpected stats after first menu preflight: %+v", stats)
	}

	menuSel2 := retainMenu(t, r, menuReq, menuRec)
	menuPlan2, err := loader.Preflight(menuSel2)
	if err != nil {
		t.Fatal(err)
	}
	stats = loader.planCache.stats()
	if stats.misses != 1 || stats.hits != 1 || stats.extracts != 1 || stats.entries != 1 {
		t.Fatalf("expected cache hit without re-extraction on repeat menu preflight: %+v", stats)
	}
	if &menuPlan1.state.payload[0] != &menuPlan2.state.payload[0] {
		t.Fatal("menu cache hit allocated a new payload slice instead of reusing the immutable cached slice")
	}
	if menuPlan1.state.program.configuration.CoreSHA384 != menuPlan2.state.program.configuration.CoreSHA384 ||
		!slices.Equal(menuPlan1.state.program.configuration.ChunkSHA384, menuPlan2.state.program.configuration.ChunkSHA384) {
		t.Fatal("menu cache hit changed derived SHA-384 digests")
	}

	// 2. Normal ROM core preflight: miss then hit.
	romReq, _, _ := addROMProgram(t, r, 0, 8192)
	romSel1, err := r.f.store.Retain(context.Background(), romReq)
	if err != nil {
		t.Fatal(err)
	}
	romPlan1, err := loader.Preflight(romSel1)
	if err != nil {
		t.Fatal(err)
	}
	stats = loader.planCache.stats()
	if stats.misses != 2 || stats.hits != 1 || stats.extracts != 2 || stats.entries != 2 {
		t.Fatalf("unexpected stats after first ROM core preflight: %+v", stats)
	}

	romSel2, err := r.f.store.Retain(context.Background(), romReq)
	if err != nil {
		t.Fatal(err)
	}
	romPlan2, err := loader.Preflight(romSel2)
	if err != nil {
		t.Fatal(err)
	}
	stats = loader.planCache.stats()
	if stats.misses != 2 || stats.hits != 2 || stats.extracts != 2 || stats.entries != 2 {
		t.Fatalf("expected cache hit without re-extraction on repeat ROM core preflight: %+v", stats)
	}
	if &romPlan1.state.payload[0] != &romPlan2.state.payload[0] {
		t.Fatal("ROM core cache hit did not reuse cached payload slice")
	}

	// 3. Shared-core-shell upload package (Spectrum 48K): miss then hit.
	specReq, specFiles, _, _ := packageSpectrumFixture(t, loader, false)
	for name, data := range specFiles {
		r.f.files.files[name] = &fstest.MapFile{Data: data}
	}
	specSel1, err := r.f.store.Retain(context.Background(), specReq)
	if err != nil {
		t.Fatal(err)
	}
	specPlan1, err := loader.Preflight(specSel1)
	if err != nil {
		t.Fatal(err)
	}
	stats = loader.planCache.stats()
	if stats.misses != 3 || stats.hits != 2 || stats.extracts != 3 || stats.entries != 3 {
		t.Fatalf("unexpected stats after first upload-shell preflight: %+v", stats)
	}

	specSel2, err := r.f.store.Retain(context.Background(), specReq)
	if err != nil {
		t.Fatal(err)
	}
	specPlan2, err := loader.Preflight(specSel2)
	if err != nil {
		t.Fatal(err)
	}
	stats = loader.planCache.stats()
	if stats.misses != 3 || stats.hits != 3 || stats.extracts != 3 || stats.entries != 3 {
		t.Fatalf("expected cache hit without re-extraction on repeat upload-shell preflight: %+v", stats)
	}
	if &specPlan1.state.payload[0] != &specPlan2.state.payload[0] {
		t.Fatal("upload-shell cache hit did not reuse cached payload slice")
	}

	// Even on cache hit, a mismatched CoreSHA384 in the upload-shell metadata is rejected.
	var forged uploadShellRecord
	if err := json.Unmarshal(specFiles[specReq.MetadataPath], &forged); err != nil {
		t.Fatal(err)
	}
	forged.CoreSHA384 = strings.Repeat("0", 95) + "1"
	forgedMeta := programJSON(t, forged)
	forgedSum := sha256.Sum256(forgedMeta)
	forgedReq := specReq
	forgedReq.MetadataPath = "FES/SPEC_FORGED.JSN"
	forgedReq.MetadataSHA256 = hex.EncodeToString(forgedSum[:])
	r.f.files.files[forgedReq.MetadataPath] = &fstest.MapFile{Data: forgedMeta}
	forgedSel, err := r.f.store.Retain(context.Background(), forgedReq)
	if err != nil {
		t.Fatal(err)
	}
	if badPlan, err := loader.Preflight(forgedSel); err == nil || badPlan != nil {
		t.Fatal("upload-shell cache hit accepted mismatched CoreSHA384")
	}
}

func TestPlanCacheStillRunsMetadataAdmissionROMAndTapeChecksOnWarmCache(t *testing.T) {
	r, _ := mixedMenuTest(t)
	romReq, _, _ := addROMProgram(t, r, 0, 8192)
	romSel, err := r.f.store.Retain(context.Background(), romReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.f.loader.Preflight(romSel); err != nil {
		t.Fatal(err)
	}

	// Corrupt metadata on a second retain of the same package: decodeCompleteRecord / checks must reject.
	var rr selectedROMProgramRecord
	if err := json.Unmarshal(romSel.Metadata(), &rr); err != nil {
		t.Fatal(err)
	}
	rr.Control.CPUBase += 4
	badMeta := programJSON(t, rr)
	badSum := sha256.Sum256(badMeta)
	badMetaReq := romReq
	badMetaReq.MetadataPath = "FES/BADMETA.JSN"
	badMetaReq.MetadataSHA256 = hex.EncodeToString(badSum[:])
	r.f.files.files[badMetaReq.MetadataPath] = &fstest.MapFile{Data: badMeta}
	badMetaSel, err := r.f.store.Retain(context.Background(), badMetaReq)
	if err != nil {
		t.Fatal(err)
	}
	if plan, err := r.f.loader.Preflight(badMetaSel); err == nil || plan != nil {
		t.Fatal("warm cache skipped metadata hardware check")
	}

	// Tape check still runs on repeat preflight with invalid tape format.
	badTapeReq := romReq
	addGameTape(t, r.f, &badTapeReq, bytes.Repeat([]byte{0x11}, int(cg.FesComputerZx81TapeMinBytes)))
	badTapeReq.Tape.Format = packagestore.SpectrumTapeFormat
	badTapeSel, err := r.f.store.Retain(context.Background(), badTapeReq)
	if err != nil {
		t.Fatal(err)
	}
	if plan, err := r.f.loader.Preflight(badTapeSel); err == nil || plan != nil {
		t.Fatal("warm cache skipped tape validation")
	}

	// Compatibility admission check still runs even if payload bytes match a cached entry.
	l, _, _, files, store := programLoaderFixture(t)
	req0, rec0, _ := addProgram(t, l, files, 0)
	sel0 := retainProgram(t, store, files, req0, rec0)
	if _, err := l.Preflight(sel0); err != nil {
		t.Fatal(err)
	}
	reqBad, recBad, _ := addProgram(t, l, files, 0, func(s string) string {
		return strings.Replace(s, "interfaces = []", "[[interfaces]]\nid = \"fes.unsupported\"\nmajor = 1\nminor = 0\nrequired = true", 1)
	})
	selBad := retainProgram(t, store, files, reqBad, recBad)
	if plan, err := l.Preflight(selBad); err == nil || plan != nil {
		t.Fatal("warm cache skipped compatibility admission")
	}
}

func TestPlanCacheCallersCannotMutateCachedPayloadOrDigests(t *testing.T) {
	l, _, _, files, store := programLoaderFixture(t)
	req, rec, origData := addProgram(t, l, files, 0)

	sel1 := retainProgram(t, store, files, req, rec)
	plan1, err := l.Preflight(sel1)
	if err != nil {
		t.Fatal(err)
	}
	wantSHA384 := plan1.state.program.configuration.CoreSHA384
	wantChunks := slices.Clone(plan1.state.program.configuration.ChunkSHA384)
	// Mutating the plan's ChunkSHA384 slice copy must not affect cached digests.
	plan1.state.program.configuration.ChunkSHA384[0] = "corrupted"

	handoff, err := store.Contain(sel1)
	if err != nil {
		t.Fatal(err)
	}
	// A hostile/buggy reserve callback mutates the slice passed to it AND aliases
	// region.Bytes directly to that slice.
	transfer, _, err := plan1.NewTransfer(handoff, func(src []byte) (*Region, error) {
		src[0] ^= 0xff
		return &Region{
			Address:              0x94800000,
			Bytes:                src,
			PublishConfiguration: func([]byte) error { return nil },
			Release:              func() error { return nil },
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Because reserve received a clone, p.payload was untouched while region.Bytes was mutated,
	// so Advance() must catch the mutated region.Bytes before START.
	if _, _, err := transfer.Advance(); err == nil || !transfer.Status().Failed {
		t.Fatalf("expected mutated region.Bytes to fail before START: err=%v status=%+v", err, transfer.Status())
	}

	lease2 := &storedLease{sdcontrol.Receipt{Owner: 18, Generation: 3, Active: true, Prepared: true}}
	files2 := &programTestFiles{files: files.files, lease: lease2}
	store2, err := packagestore.New(files2, lease2)
	if err != nil {
		t.Fatal(err)
	}
	sel2 := retainProgram(t, store2, files2, req, rec)
	plan2, err := l.Preflight(sel2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plan2.state.payload, origData) {
		t.Fatal("caller reserve mutation corrupted cached payload")
	}
	if plan2.state.program.configuration.CoreSHA384 != wantSHA384 ||
		!slices.Equal(plan2.state.program.configuration.ChunkSHA384, wantChunks) {
		t.Fatal("caller slice mutation corrupted cached ChunkSHA384")
	}
}

func TestPlanCacheLRUEvictionAndByteBound(t *testing.T) {
	// Direct bounded LRU tests for count, MRU promotion, byte cap, and oversized bypass.
	cache := newPreflightCache(3, 256)
	k := func(i int) preflightCacheKey {
		return preflightCacheKey{packageID: fmt.Sprintf("pkg-%d", i), buildID: "b", coreSHA256: "h", coreSize: 100, chunkBytes: 64}
	}
	cache.put(k(1), make([]byte, 100), "a", []string{"c1"})
	cache.put(k(2), make([]byte, 100), "b", []string{"c2"})
	// Promote k(1) to MRU; k(2) becomes LRU.
	if _, _, _, ok := cache.get(k(1)); !ok {
		t.Fatal("expected k(1) hit")
	}
	// Inserting 100 bytes exceeds 256 maxBytes (200+100 > 256), evicting LRU k(2).
	cache.put(k(3), make([]byte, 100), "c", []string{"c3"})
	if _, _, _, ok := cache.get(k(2)); ok {
		t.Fatal("expected k(2) to be evicted by byte limit")
	}
	if _, _, _, ok := cache.get(k(1)); !ok {
		t.Fatal("expected promoted MRU k(1) to stay cached")
	}
	// Oversized entry (> 256 bytes) bypasses cache without evicting k(1) or k(3).
	cache.put(k(99), make([]byte, 300), "big", []string{"big"})
	if st := cache.stats(); st.entries != 2 || st.usedBytes != 200 {
		t.Fatalf("oversized entry should bypass cache without eviction: %+v", st)
	}

	// Integration test: > 8 distinct cores through ProgramLoader.Preflight.
	l, _, _, files, store := programLoaderFixture(t)
	reqs := make([]packagestore.Request, 9)
	recs := make([]selectedProgramRecord, 9)
	for i := range 9 {
		reqs[i], recs[i] = addCacheProgram(t, l, files, i)
	}
	for i := range 8 {
		sel := retainProgram(t, store, files, reqs[i], recs[i])
		if _, err := l.Preflight(sel); err != nil {
			t.Fatal(i, err)
		}
	}
	if st := l.planCache.stats(); st.entries != 8 || st.misses != 8 || st.evictions != 0 {
		t.Fatalf("unexpected stats after 8 distinct cores: %+v", st)
	}
	// Touch core 0 to promote it to MRU; core 1 becomes LRU.
	if _, err := l.Preflight(retainProgram(t, store, files, reqs[0], recs[0])); err != nil {
		t.Fatal(err)
	}
	// Preflight core 8 -> evicts core 1.
	if _, err := l.Preflight(retainProgram(t, store, files, reqs[8], recs[8])); err != nil {
		t.Fatal(err)
	}
	if st := l.planCache.stats(); st.entries != 8 || st.evictions != 1 || st.hits != 1 {
		t.Fatalf("unexpected stats after 9th core eviction: %+v", st)
	}
	// Core 0 must still be a cache hit.
	if _, err := l.Preflight(retainProgram(t, store, files, reqs[0], recs[0])); err != nil {
		t.Fatal(err)
	}
	if st := l.planCache.stats(); st.hits != 2 || st.misses != 9 {
		t.Fatalf("expected core 0 hit after LRU eviction of core 1: %+v", st)
	}
	// Core 1 must now be a cache miss (causing eviction of core 2).
	if _, err := l.Preflight(retainProgram(t, store, files, reqs[1], recs[1])); err != nil {
		t.Fatal(err)
	}
	if st := l.planCache.stats(); st.misses != 10 || st.evictions != 2 {
		t.Fatalf("expected core 1 miss after eviction: %+v", st)
	}
}

func TestPlanCacheConcurrentPreflight(t *testing.T) {
	l, _, _, files, store := programLoaderFixture(t)
	req, rec, orig := addProgram(t, l, files, 0)
	sel := retainProgram(t, store, files, req, rec)

	const workers = 8
	errs := make(chan error, workers)
	for range workers {
		go func() {
			plan, err := l.Preflight(sel)
			if err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(plan.state.payload, orig) {
				errs <- fmt.Errorf("payload mismatch")
				return
			}
			errs <- nil
		}()
	}
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	st := l.planCache.stats()
	if st.hits+st.misses != workers || st.entries != 1 {
		t.Fatalf("unexpected concurrent cache stats: %+v", st)
	}
}

func TestStoredConfigurationPlanCatchesUncachedDDRMutationBeforeStart(t *testing.T) {
	// Use a multi-scratch-buffer payload (> 64 KiB) to cover start, 32 KiB scratch boundary, and tail.
	const payloadSize = (64 << 10) + 128
	mutateOffsets := []int{0, (32 << 10) - 1, 32 << 10, payloadSize - 1}

	for _, at := range mutateOffsets {
		t.Run(fmt.Sprintf("cold/offset-%d", at), func(t *testing.T) {
			raw, control, mapping, data, lease, store, selection := storedSelectionWithSize(t, payloadSize)
			handoff, err := store.Contain(selection)
			if err != nil {
				t.Fatal(err)
			}
			starts := 0
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == start {
					starts++
					return [4]uint64{}, nil
				}
				digest := sha256.Sum256(raw)
				replies := hpsContextReplies(digest)
				replies[0][3] = 4
				replies[3][3] = 4
				switch a[1] {
				case 0:
					return replies[0], nil
				case 4:
					return replies[1], nil
				case 5:
					return replies[2], nil
				case 8:
					return [4]uint64{0, 0x4643503100000000 | programBoundReply | uint64(len(data)), 17, 4}, nil
				}
				t.Fatalf("unexpected firmware call: %v", a)
				return [4]uint64{}, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := v.PreflightStoredConfiguration(control, mapping, selection)
			if err != nil {
				t.Fatal(err)
			}
			var region *Region
			arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+len(data)), func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			transfer, _, err := plan.NewTransfer(handoff, func(b []byte) (*Region, error) {
				r, err := arena.Reserve(b)
				region = r
				return r, err
			})
			if err != nil {
				t.Fatal(err)
			}
			wantDigest := sha256.Sum256(data)
			if transfer.digest != wantDigest {
				t.Fatalf("transfer digest mismatch: got %x want %x", transfer.digest, wantDigest)
			}
			if lease.receipt.Active {
				t.Fatal("SD still active")
			}

			// Mutate uncached DDR region.Bytes after Reserve/NewTransfer and before Advance/START.
			region.Bytes[at] ^= 1
			_, _, err = transfer.Advance()
			if err == nil || starts != 0 || !transfer.Status().Failed || transfer.Status().RetainedBytes != len(data) {
				t.Fatalf("uncached DDR mutation at %d reached START or lost reservation: err=%v starts=%d status=%+v", at, err, starts, transfer.Status())
			}
		})
	}

	// Verify rearm path through MenuSession.NewStoredReconfigurationTransfer also sets precomputed digest
	// and catches uncached DDR mutation before START.
	r, _ := mixedMenuTest(t)
	l := r.f.loader
	f := newMenuFixture(t)
	f.cpu.computerCPU = *r.f.cpu.computerCPU
	r.f.cpu.computerCPU = &f.cpu.computerCPU
	port := &selectedRoleCPU{romProgramCPU: r.cpu}
	joined := &selectedMenuCPU{menuCPU: f.cpu, configuration: port}
	l.boot.cpu = joined
	initialRegisters := maps.Clone(f.cpu.registers)

	port.role = programRoleMenu
	menuReq, menuRec, _ := addMenuSelection(t, r, 0)
	menuSel := retainMenu(t, r, menuReq, menuRec)
	menuPlan, err := l.Preflight(menuSel)
	if err != nil {
		t.Fatal(err)
	}
	menuHandoff, err := r.f.store.Contain(menuSel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReserveMenuMemory(func(b []byte) (*Region, error) {
		return &Region{Address: 0x95000000, Bytes: bytes.Clone(b), Release: func() error { f.cpu.releases++; return nil }, OrderFrameStores: func() error { return nil }}, nil
	}); err != nil {
		t.Fatal(err)
	}
	menuTransfer, _, err := menuPlan.NewTransfer(menuHandoff, r.f.arena.Reserve)
	if err != nil {
		t.Fatal(err)
	}
	r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(menuTransfer.source), region: menuTransfer.region}
	port.activate = func() {
		joined.kind = programRoleMenu
		f.cpu.registers = maps.Clone(initialRegisters)
		f.cpu.accepted, f.cpu.displayed, f.cpu.slot = 0, 0, 0
		f.cpu.lastDrainCookie = 0
		build, _ := BuildWords(menuPlan.Identity().BuildID)
		for i, w := range build {
			f.cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = w
		}
	}
	for step := 0; step < 40 && !menuTransfer.Status().Done; step++ {
		if _, _, err = menuTransfer.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	menuSession, err := l.OpenMenu(context.Background(), menuTransfer, scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := menuSession.PresentFrame(context.Background(), menuFrame(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := menuSession.Close(); err != nil {
		t.Fatal(err)
	}
	if err := menuSession.ResumeStorageAfterReturn(); err != nil {
		t.Fatal(err)
	}

	port.role = programRoleComputer
	romReq, _, data := addROMProgram(t, r, 0, 8192)
	romSel, err := r.f.store.Retain(context.Background(), romReq)
	if err != nil {
		t.Fatal(err)
	}
	romPlan, err := l.Preflight(romSel)
	if err != nil {
		t.Fatal(err)
	}
	romHandoff, err := r.f.store.Contain(romSel)
	if err != nil {
		t.Fatal(err)
	}
	romTransfer, _, err := menuSession.NewStoredReconfigurationTransfer(romPlan, romHandoff, r.f.arena.Reserve)
	if err != nil {
		t.Fatal(err)
	}
	if romTransfer.digest != sha256.Sum256(data) {
		t.Fatal("reconfiguration transfer digest mismatch")
	}
	r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: romTransfer.region}
	romTransfer.region.Bytes[len(data)-1] ^= 1
	if _, _, err := romTransfer.Advance(); err == nil || len(r.f.cpu.wire.calls) != 0 || !romTransfer.Status().Failed {
		t.Fatalf("mutated rearm region reached START: err=%v calls=%d status=%+v", err, len(r.f.cpu.wire.calls), romTransfer.Status())
	}
}

func storedSelectionWithSize(t *testing.T, size int) ([]byte, []byte, []byte, []byte, *storedLease, *packagestore.Store, *packagestore.Selection) {
	t.Helper()
	var source HPSBootContext
	if err := json.Unmarshal(hpsContextFixture(t), &source); err != nil {
		t.Fatal(err)
	}
	chunk := 0xfffc0
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*19 ^ 0x5a)
	}
	sum, sum384 := sha256.Sum256(data), sha512.Sum384(data)
	source.CoreSize, source.CoreSHA256 = int64(len(data)), hex.EncodeToString(sum[:])
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Retained configuration fixture"
description = "Test only; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
[[interfaces]]
id = "fes.embedded.control"
major = 1
minor = 0
required = true
[build]
id = %q
repository = "https://example.invalid/retained-cold"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture; test only"
`, source.CoreID, source.Platform, source.Device, source.ProgrammingProfile, len(data), source.CoreSHA256, source.ABI, source.BuildID)
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	source.PackageID = inspection.PackageID
	var control hpsControlMap
	if err := json.Unmarshal(controlMapFixture(t), &control); err != nil {
		t.Fatal(err)
	}
	control.PackageID = source.PackageID
	mapping := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: source.PackageID,
		BuildID: source.BuildID, CoreSHA256: source.CoreSHA256, CoreSHA384: hex.EncodeToString(sum384[:]),
		CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: chunk}
	for offset := 0; offset < len(data); offset += chunk {
		digest := sha512.Sum384(data[offset:min(offset+chunk, len(data))])
		mapping.ChunkSHA384 = append(mapping.ChunkSHA384, hex.EncodeToString(digest[:]))
	}
	lease := &storedLease{sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}}
	files := &storedFiles{files: fstest.MapFS{"FES/PKGTEST.BIN": &fstest.MapFile{Data: archive}}, lease: lease}
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: source.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	return canonicalConfiguration(t, source), canonicalConfiguration(t, control), canonicalConfiguration(t, mapping), data, lease, store, selection
}

func addCacheProgram(t *testing.T, l *ProgramLoader, files *programTestFiles, index int) (packagestore.Request, selectedProgramRecord) {
	t.Helper()
	data := bytes.Repeat([]byte{byte(index + 1)}, 128)
	sum := sha256.Sum256(data)
	build := fmt.Sprintf("%032x", index+1)
	p := l.platform
	manifest := fmt.Sprintf(`format = 2
interfaces = []
[core]
id = "fes.cache-fixture-%d"
name = "Cache fixture"
description = "Synthetic software fixture; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = %d
minor = %d
[build]
id = %q
repository = "https://example.invalid/cache-program"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "software fixture; no FPGA build"
`, index, p.Platform, p.Device, p.ProgrammingProfile, len(data), hex.EncodeToString(sum[:]), p.ABI, p.Major, p.Minor, build)
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctrl, comp := l.control, l.computer
	ctrl.PackageID, ctrl.BuildID, ctrl.HandoffSHA256 = inspection.PackageID, build, fmt.Sprintf("%064x", index+100)
	ctrlRaw := programJSON(t, ctrl)
	controlHash := sha256.Sum256(ctrlRaw)
	comp.PackageID, comp.BuildID, comp.HandoffSHA256 = inspection.PackageID, build, ctrl.HandoffSHA256
	comp.ControlMapSHA256 = hex.EncodeToString(controlHash[:])
	record := selectedProgramRecord{1, "nano-selected-program-v1", inspection.PackageID, build, p, ctrl, comp}
	meta := programJSON(t, record)
	metaSum := sha256.Sum256(meta)
	request := packagestore.Request{Path: fmt.Sprintf("FES/C%d.BIN", index), PackageID: inspection.PackageID,
		MetadataPath: fmt.Sprintf("FES/C%d.JSN", index), MetadataSHA256: hex.EncodeToString(metaSum[:])}
	files.files[request.Path] = &fstest.MapFile{Data: archive}
	files.files[request.MetadataPath] = &fstest.MapFile{Data: meta}
	return request, record
}
