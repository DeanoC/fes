// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
)

func makePackageVariant(t *testing.T, label string, payload []byte) ([]byte, string) {
	t.Helper()
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2"
	manifest := string(read(t, base+"/manifests/valid-basic.toml"))
	origPayload := read(t, base+"/payloads/fes-fixture.rbf")
	manifest = strings.Replace(manifest, "FES Pong", "FES "+label, 1)
	manifest = strings.Replace(manifest, "size = 12", fmt.Sprintf("size = %d", len(payload)), 1)
	manifest = strings.ReplaceAll(manifest, hash(origPayload), hash(payload))
	body := canonical(t, []byte(manifest), payload, nil)
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return body, archive.Inspection().PackageID
}

type cacheTestFS struct {
	*trackedFS
	files   fstest.MapFS
	replace func(string, []byte) error
}

func (f *cacheTestFS) Mkdir(_ context.Context, name string) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	if _, exists := f.files[name]; exists {
		return os.ErrExist
	}
	f.files[name] = &fstest.MapFile{Mode: os.ModeDir | 0755}
	return nil
}

func (f *cacheTestFS) CreateFile(_ context.Context, name string, data []byte) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	if _, exists := f.files[name]; exists {
		return os.ErrExist
	}
	f.files[name] = &fstest.MapFile{Data: bytes.Clone(data), Mode: 0444}
	return nil
}

func (f *cacheTestFS) ReplaceFile(ctx context.Context, path string, data []byte) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	if ctx == nil || ctx.Err() != nil {
		return errors.New("replace missing or cancelled context")
	}
	if f.replace != nil {
		return f.replace(path, data)
	}
	f.files[path] = &fstest.MapFile{Data: bytes.Clone(data)}
	return nil
}

func TestCacheLifecycleAndValidation(t *testing.T) {
	var nilStore *Store
	if err := nilStore.EnableCache(1024); !errors.Is(err, ErrState) {
		t.Fatalf("nil EnableCache err=%v, want ErrState", err)
	}
	if _, ok := nilStore.CacheStats(); ok {
		t.Fatal("nil CacheStats reported enabled")
	}
	nilStore.DisableCache()
	nilStore.InvalidateCache()

	body, req, _ := basic(t)
	l := lease()
	s, tracked := open(t, fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}, l)

	// Default disabled behavior: repeat Retain hits filesystem every time.
	if _, ok := s.CacheStats(); ok {
		t.Fatal("cache enabled by default")
	}
	if _, err := s.Retain(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	opensAfterFirst := tracked.opens.Load()
	if _, err := s.Retain(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() <= opensAfterFirst {
		t.Fatal("default uncached Retain skipped filesystem")
	}

	if err := s.EnableCache(0); err == nil {
		t.Fatal("EnableCache(0) succeeded")
	}
	if err := s.EnableCache(-64); err == nil {
		t.Fatal("EnableCache(-64) succeeded")
	}
	if err := s.EnableCache(256 << 20); err != nil {
		t.Fatalf("EnableCache over cap failed: %v", err)
	}
	stats, ok := s.CacheStats()
	if !ok || stats.CapacityBytes != 128<<20 {
		t.Fatalf("expected capacity capped at 128 MiB, got %+v ok=%t", stats, ok)
	}

	s.DisableCache()
	if _, ok := s.CacheStats(); ok {
		t.Fatal("CacheStats remained true after DisableCache")
	}
}

func TestCacheHitZeroFSReadsAcrossContainResumeAndReturn(t *testing.T) {
	body, req, wantPayload := basic(t)
	meta := []byte(`{"core":"pong"}`)
	tape := []byte("ZX81TAPE")
	req.MetadataPath = "FES/META.JSN"
	req.MetadataSHA256 = hash(meta)
	req.Tape = TapeRequest{Path: "GAMES/GAME.P", SHA256: hash(tape), Size: int64(len(tape))}

	files := fstest.MapFS{
		req.Path:         &fstest.MapFile{Data: body},
		req.MetadataPath: &fstest.MapFile{Data: meta},
		req.Tape.Path:    &fstest.MapFile{Data: tape},
	}
	l := &returnLease{testLease: lease()}
	tracked := &trackedFS{base: files, lease: l.testLease}
	s, err := New(tracked, l)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnableCache(1 << 20); err != nil {
		t.Fatal(err)
	}

	sel1, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := s.CacheStats()
	if !ok || stats.Misses != 1 || stats.Hits != 0 || stats.Entries != 1 {
		t.Fatalf("after miss stats=%+v ok=%t", stats, ok)
	}
	opensAfterMiss, readsAfterMiss := tracked.opens.Load(), tracked.reads.Load()
	if opensAfterMiss == 0 || readsAfterMiss == 0 {
		t.Fatal("first Retain did not read from filesystem")
	}

	// Second Retain before containment hits cache with 0 FS I/O and returns a fresh Selection.
	sel2, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if sel2 == sel1 {
		t.Fatal("cache hit returned same Selection pointer")
	}
	if tracked.opens.Load() != opensAfterMiss || tracked.reads.Load() != readsAfterMiss {
		t.Fatalf("cache hit performed FS I/O: opens %d->%d reads %d->%d",
			opensAfterMiss, tracked.opens.Load(), readsAfterMiss, tracked.reads.Load())
	}

	// Mutate returned slices and read archive payload to confirm cached data cannot be corrupted.
	m1 := sel1.Metadata()
	t1 := sel1.Tape()
	m1[0] ^= 0xff
	t1[0] ^= 0xff
	gotPayload, err := io.ReadAll(sel1.Archive().Payload())
	if err != nil || !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("payload read err=%v", err)
	}
	gotPayload[0] ^= 0xff

	// Contain -> Retain must still fail while contained without using cache.
	if _, err := s.Contain(sel2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Retain(context.Background(), req); !errors.Is(err, ErrState) {
		t.Fatalf("contained Retain err=%v, want ErrState", err)
	}

	// Resume advances generation; sel1/sel2 are stale, but new Retain hits cache and rebinds generation.
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Contain(sel1); !errors.Is(err, ErrState) {
		t.Fatalf("stale sel1 survived Resume: %v", err)
	}
	sel3, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() != opensAfterMiss || tracked.reads.Load() != readsAfterMiss {
		t.Fatal("Retain after Resume touched filesystem")
	}
	if sel3.generation != l.r.Generation {
		t.Fatalf("generation=%d, want %d", sel3.generation, l.r.Generation)
	}
	if !bytes.Equal(sel3.Metadata(), meta) || !bytes.Equal(sel3.Tape(), tape) {
		t.Fatal("cached metadata or tape mutated by caller")
	}
	recheckPayload, err := io.ReadAll(sel3.Archive().Payload())
	if err != nil || !bytes.Equal(recheckPayload, wantPayload) {
		t.Fatal("cached archive payload mutated by caller")
	}

	// Contain -> ResumeAfterReturn -> Retain again hits cache with 0 FS I/O and rebinds generation.
	if _, err := s.Contain(sel3); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeAfterReturn(17, l.r.Generation+1); err != nil {
		t.Fatal(err)
	}
	sel4, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() != opensAfterMiss || tracked.reads.Load() != readsAfterMiss {
		t.Fatal("Retain after ResumeAfterReturn touched filesystem")
	}
	if _, err := s.Contain(sel4); err != nil {
		t.Fatalf("Contain after ResumeAfterReturn failed: %v", err)
	}
	stats, _ = s.CacheStats()
	if stats.Hits != 3 || stats.Misses != 1 {
		t.Fatalf("unexpected stats after 3 hits: %+v", stats)
	}

	// Cancelled context must fail even on a cached request.
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Retain(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Retain err=%v, want context.Canceled", err)
	}
}

func TestCacheLRUEvictionAndOversizedBypass(t *testing.T) {
	p1 := bytes.Repeat([]byte{0x11}, 64)
	p2 := bytes.Repeat([]byte{0x22}, 64)
	p3 := bytes.Repeat([]byte{0x33}, 64)
	pBig := bytes.Repeat([]byte{0x44}, 256)

	b1, id1 := makePackageVariant(t, "One", p1)
	b2, id2 := makePackageVariant(t, "Two", p2)
	b3, id3 := makePackageVariant(t, "Three", p3)
	bBig, idBig := makePackageVariant(t, "Big", pBig)

	r1 := Request{Path: "FES/ONE.FCP", PackageID: id1}
	r2 := Request{Path: "FES/TWO.FCP", PackageID: id2}
	r3 := Request{Path: "FES/THREE.FCP", PackageID: id3}
	rBig := Request{Path: "FES/BIG.FCP", PackageID: idBig}

	files := fstest.MapFS{
		r1.Path:   &fstest.MapFile{Data: b1},
		r2.Path:   &fstest.MapFile{Data: b2},
		r3.Path:   &fstest.MapFile{Data: b3},
		rBig.Path: &fstest.MapFile{Data: bBig},
	}
	l := lease()
	s, tracked := open(t, files, l)
	if err := s.EnableCache(128); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Retain(context.Background(), r1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Retain(context.Background(), r2); err != nil {
		t.Fatal(err)
	}
	// Promote r1 to MRU so r2 is LRU.
	opens := tracked.opens.Load()
	if _, err := s.Retain(context.Background(), r1); err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() != opens {
		t.Fatal("r1 should have hit cache")
	}

	// Oversized entry rBig must succeed without caching or evicting r1/r2.
	selBig, err := s.Retain(context.Background(), rBig)
	if err != nil {
		t.Fatal(err)
	}
	gotBig, err := io.ReadAll(selBig.Archive().Payload())
	if err != nil || !bytes.Equal(gotBig, pBig) {
		t.Fatalf("oversized Retain payload mismatch err=%v", err)
	}
	stats, _ := s.CacheStats()
	if stats.Entries != 2 || stats.UsedBytes != 128 || stats.Evictions != 0 {
		t.Fatalf("oversized entry disturbed cache: %+v", stats)
	}

	// Inserting r3 evicts LRU r2, keeping r1.
	if _, err := s.Retain(context.Background(), r3); err != nil {
		t.Fatal(err)
	}
	stats, _ = s.CacheStats()
	if stats.Entries != 2 || stats.UsedBytes != 128 || stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction after r3, got %+v", stats)
	}

	opensBeforeR1 := tracked.opens.Load()
	if _, err := s.Retain(context.Background(), r1); err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() != opensBeforeR1 {
		t.Fatal("r1 was evicted instead of LRU r2")
	}

	// r2 was evicted, so retaining r2 must read FS and evict r3.
	if _, err := s.Retain(context.Background(), r2); err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() == opensBeforeR1 {
		t.Fatal("evicted r2 unexpectedly hit cache")
	}
	stats, _ = s.CacheStats()
	if stats.Evictions != 2 {
		t.Fatalf("expected Evictions=2, got %+v", stats)
	}
}

func TestCacheInvalidationOnMutationsAndRetire(t *testing.T) {
	body, req, _ := basic(t)
	prefs := []byte(`{"home":"library"}`)
	bonds := []byte(`{"version":1,"bonds":[]}`)

	files := fstest.MapFS{
		req.Path:        &fstest.MapFile{Data: body},
		PreferencesPath: &fstest.MapFile{Data: prefs},
		BTBondsPath:     &fstest.MapFile{Data: bonds},
	}
	l := lease()
	tracked := &trackedFS{base: files, lease: l}
	fsys := &cacheTestFS{trackedFS: tracked, files: files}
	s, err := New(fsys, l)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnableCache(1 << 20); err != nil {
		t.Fatal(err)
	}

	populate := func(wantInv uint64) {
		t.Helper()
		if _, err := s.Retain(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		st, ok := s.CacheStats()
		if !ok || st.Entries != 1 || st.Invalidations != wantInv {
			t.Fatalf("populate stats=%+v wantEntries=1 wantInv=%d", st, wantInv)
		}
	}

	// Manual InvalidateCache.
	populate(0)
	s.InvalidateCache()
	st, _ := s.CacheStats()
	if st.Entries != 0 || st.UsedBytes != 0 || st.Invalidations != 1 {
		t.Fatalf("after InvalidateCache: %+v", st)
	}

	// WritePreferences invalidates.
	populate(1)
	if err := s.WritePreferences(context.Background(), []byte(`{"home":"recent"}`)); err != nil {
		t.Fatal(err)
	}
	st, _ = s.CacheStats()
	if st.Entries != 0 || st.UsedBytes != 0 || st.Invalidations != 2 {
		t.Fatalf("after WritePreferences: %+v", st)
	}

	// WriteBTBonds invalidates.
	populate(2)
	if err := s.WriteBTBonds(context.Background(), []byte(`{"version":1,"bonds":[{"addr":"AA:BB:CC:DD:EE:FF"}]}`)); err != nil {
		t.Fatal(err)
	}
	st, _ = s.CacheStats()
	if st.Entries != 0 || st.UsedBytes != 0 || st.Invalidations != 3 {
		t.Fatalf("after WriteBTBonds: %+v", st)
	}

	// Install invalidates.
	populate(3)
	p2 := bytes.Repeat([]byte{0x55}, 24)
	b2, id2 := makePackageVariant(t, "Installed", p2)
	plan, err := PrepareInstall(context.Background(), Request{Path: "UPLOAD/NEW.FCP", PackageID: id2}, map[string][]byte{"UPLOAD/NEW.FCP": b2})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Install(context.Background(), plan, []byte(`{"id":"new"}`)); err != nil {
		t.Fatal(err)
	}
	st, _ = s.CacheStats()
	if st.Entries != 0 || st.UsedBytes != 0 || st.Invalidations != 4 {
		t.Fatalf("after Install: %+v", st)
	}

	// Store retirement invalidates and blocks EnableCache.
	populate(4)
	l.mu.Lock()
	l.r.Generation++
	l.mu.Unlock()
	if _, err := s.ActiveReceipt(); !errors.Is(err, ErrRetired) {
		t.Fatalf("ActiveReceipt err=%v, want ErrRetired", err)
	}
	st, ok := s.CacheStats()
	if !ok || st.Entries != 0 || st.UsedBytes != 0 || st.Invalidations != 5 {
		t.Fatalf("after retire: %+v ok=%t", st, ok)
	}
	if err := s.EnableCache(1024); !errors.Is(err, ErrRetired) {
		t.Fatalf("EnableCache after retire err=%v, want ErrRetired", err)
	}
}

func TestCacheROMRequestKeyAndSizing(t *testing.T) {
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2"
	manifest := string(read(t, base+"/manifests/valid-basic.toml"))
	original := read(t, base+"/payloads/fes-fixture.rbf")
	vectors := "../../../../sources/misteross/expansion/testdata/rom/"
	unzip := func(name string) []byte {
		t.Helper()
		f, e := os.Open(vectors + name + ".gz")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		z, e := gzip.NewReader(f)
		if e != nil {
			t.Fatal(e)
		}
		defer z.Close()
		v, e := io.ReadAll(z)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	rbf, mapping, rom := unzip("blank.rbf"), unzip("map.json"), unzip("ramp.rom")
	m := strings.Replace(manifest, "format = 2", "format = 3", 1)
	m = strings.Replace(m, "size = 12", fmt.Sprintf("size = %d", len(rbf)), 1)
	m = strings.ReplaceAll(m, hash(original), hash(rbf))
	m += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%s\"\n", len(rom), len(mapping), hash(mapping))
	body := canonical(t, []byte(m), rbf, mapping)
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	sources := []corepackage.ROMSource{{ID: "machine-rom", Role: "firmware", Data: rom}}
	linked, err := archive.LinkROMs(context.Background(), sources)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Path:          "FES/ROM.FCP",
		PackageID:     archive.Inspection().PackageID,
		ReceiptSHA256: linked.ReceiptSHA256(),
		Sources:       []Source{{ID: "machine-rom", Role: "firmware", Path: "FES/ROM0.BIN", SHA256: hash(rom)}},
	}
	files := fstest.MapFS{
		req.Path:            &fstest.MapFile{Data: body},
		req.Sources[0].Path: &fstest.MapFile{Data: rom},
	}
	l := lease()
	s, tracked := open(t, files, l)
	if err := s.EnableCache(64 << 20); err != nil {
		t.Fatal(err)
	}
	sel, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := s.CacheStats()
	if !ok || st.Entries != 1 || st.UsedBytes <= int64(len(rbf)+len(rom)) {
		t.Fatalf("unexpected ROM cache sizing stats=%+v", st)
	}
	opens := tracked.opens.Load()
	sel2, err := s.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tracked.opens.Load() != opens || sel2.Linked().ReceiptSHA256() != sel.Linked().ReceiptSHA256() {
		t.Fatal("ROM-linked cache hit failed or touched FS")
	}

	// Changing a Source field must miss cache (and fail validation against archive).
	badReq := req
	badReq.Sources = []Source{{ID: "machine-rom", Role: "firmware", Path: "FES/OTHER.BIN", SHA256: hash(rom)}}
	if _, err := s.Retain(context.Background(), badReq); err == nil {
		t.Fatal("modified Source field unexpectedly hit cache")
	}
}
