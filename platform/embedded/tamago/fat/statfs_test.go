// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func setFATEntry(d *sparseFATDisk, cluster uint32, val uint32) {
	secLBA := uint64(32) + uint64(cluster)/128
	off := (uint64(cluster) % 128) * 4
	sec := d.data[secLBA]
	binary.LittleEndian.PutUint32(sec[off:], val)
	d.data[secLBA] = sec
}

func TestStatFSAccountingFSInfoDirtyAndLabel(t *testing.T) {
	d, v := newRecordingDisk(t, 4) // 2 KiB clusters, 65536 total clusters
	ctx := context.Background()

	// Seed FSInfo hints, BPB label, and one bad cluster.
	fsInfoSec := d.data[1]
	binary.LittleEndian.PutUint32(fsInfoSec[488:], 65534)
	binary.LittleEndian.PutUint32(fsInfoSec[492:], 3)
	d.data[1] = fsInfoSec

	boot := d.data[0]
	boot[66] = 0x29
	copy(boot[71:82], "FESBOOT    ")
	d.data[0] = boot

	setFATEntry(d.sparseFATDisk, 10, 0x0ffffff7) // bad cluster
	remount, err := Mount(d)
	if err != nil {
		t.Fatal(err)
	}
	v = remount

	st, err := v.StatFS(ctx)
	if err != nil {
		t.Fatalf("StatFS initial: %v", err)
	}
	if st.Label != "FESBOOT" || st.ClusterBytes != 2048 || st.SectorsPerCluster != 4 {
		t.Fatalf("unexpected geometry/label: %+v", st)
	}
	if st.TotalClusters != 65536 || st.UsedClusters != 1 || st.BadClusters != 1 || st.FreeClusters != 65534 {
		t.Fatalf("unexpected cluster counts: %+v", st)
	}
	if st.TotalBytes != 65536*2048 || st.FreeBytes != 65534*2048 || st.UsedBytes != 2048 {
		t.Fatalf("unexpected byte counts: %+v", st)
	}
	if st.Dirty || !st.FSInfoValid || st.FSInfoFreeHint != 65534 || st.FSInfoNextHint != 3 {
		t.Fatalf("unexpected dirty/FSInfo state: %+v", st)
	}

	// Add root directory volume label (0x08) preceded by a deleted label (0xE5).
	rootSec := d.data[v.clusterSector(v.root)]
	copy(rootSec[0:11], "OLDLABEL   ")
	rootSec[0] = 0xe5
	rootSec[11] = 0x08
	copy(rootSec[32:43], "FESDATA    ")
	rootSec[43] = 0x08
	d.data[v.clusterSector(v.root)] = rootSec

	// Create a 3-cluster file (5000 bytes / 2048 = 3 clusters) -> CreateFile invalidates FSInfo hints.
	if err := v.CreateFile(ctx, "BIG.BIN", bytes.Repeat([]byte{0x42}, 5000)); err != nil {
		t.Fatal(err)
	}
	st2, err := v.StatFS(ctx)
	if err != nil {
		t.Fatalf("StatFS after CreateFile: %v", err)
	}
	if st2.Label != "FESDATA" {
		t.Fatalf("root label=%q, want FESDATA", st2.Label)
	}
	if st2.UsedClusters != 4 || st2.FreeClusters != 65531 || st2.BadClusters != 1 {
		t.Fatalf("counts after CreateFile: %+v", st2)
	}
	if !st2.FSInfoValid || st2.FSInfoFreeHint != 0xffffffff || st2.FSInfoNextHint != 0xffffffff {
		t.Fatalf("expected invalidated FSInfo hints after CreateFile, got %+v", st2)
	}

	// Removing the file reclaims its 3 clusters.
	if err := v.RemoveFile(ctx, "BIG.BIN"); err != nil {
		t.Fatal(err)
	}
	st3, err := v.StatFS(ctx)
	if err != nil {
		t.Fatalf("StatFS after RemoveFile: %v", err)
	}
	if st3.UsedClusters != 1 || st3.FreeClusters != 65534 {
		t.Fatalf("counts after RemoveFile: %+v", st3)
	}

	// Dirty flag (FAT[1] bit 27 cleared) and invalid FSInfo signature.
	setFATEntry(d.sparseFATDisk, 1, 0x0ffffff8&^0x08000000)
	fsBad := d.data[1]
	binary.LittleEndian.PutUint32(fsBad[0:], 0)
	d.data[1] = fsBad
	stDirty, err := v.StatFS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !stDirty.Dirty || stDirty.FSInfoValid {
		t.Fatalf("expected Dirty=true FSInfoValid=false, got %+v", stDirty)
	}

	// NO NAME BPB label must not leak as a volume label.
	d.data[v.clusterSector(v.root)] = [block.SectorSize]byte{}
	boot2 := d.data[0]
	copy(boot2[71:82], "NO NAME    ")
	d.data[0] = boot2
	binary.LittleEndian.PutUint16(boot2[48:], 0xffff)
	d.data[0] = boot2
	noNameVol, err := Mount(d)
	if err != nil {
		t.Fatal(err)
	}
	stNoName, err := noNameVol.StatFS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stNoName.Label != "" || stNoName.FSInfoValid {
		t.Fatalf("expected empty label and FSInfoValid=false, got %+v", stNoName)
	}
}

func TestStatFSErrors(t *testing.T) {
	d, v := newRecordingDisk(t, 4)

	if _, err := v.StatFS(nil); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("StatFS(nil) = %v, want fs.ErrInvalid", err)
	}

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.StatFS(cctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("StatFS(canceled) = %v, want context.Canceled", err)
	}

	d.mu.Lock()
	d.failReadAt = d.readCalls + 1
	d.mu.Unlock()
	if _, err := v.StatFS(context.Background()); err == nil {
		t.Fatal("StatFS succeeded across read failure")
	}

	v.mutationErr = ErrMutation
	if _, err := v.StatFS(context.Background()); !errors.Is(err, ErrMutation) {
		t.Fatalf("StatFS after ErrMutation = %v, want ErrMutation", err)
	}
}

func TestMkdirAllNestedIdempotentAndErrors(t *testing.T) {
	d, v := newRecordingDisk(t, 2) // 1 KiB clusters
	ctx := context.Background()

	if err := v.MkdirAll(ctx, "."); err != nil {
		t.Fatalf("MkdirAll(\".\") = %v", err)
	}
	if err := v.MkdirAll(ctx, "Saves/Nintendo/Game Boy/Pokemon"); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	if err := v.CreateFile(ctx, "Saves/Nintendo/Game Boy/Pokemon/red.sav", []byte("SRAM")); err != nil {
		t.Fatal(err)
	}

	// Idempotent re-call on existing dirs (case-insensitive) performs zero writes, even read-only.
	d.readOnly = true
	if err := v.MkdirAll(ctx, "saves/nintendo/Game Boy/Pokemon"); err != nil {
		t.Fatalf("idempotent MkdirAll read-only failed: %v", err)
	}
	if err := v.MkdirAll(ctx, "Saves/NewLeaf"); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("missing dir on read-only = %v, want ErrReadOnly", err)
	}
	d.readOnly = false

	// Partial existing prefix extends only missing tail.
	if err := v.MkdirAll(ctx, "Saves/Nintendo/Extra/Deep"); err != nil {
		t.Fatalf("partial prefix MkdirAll: %v", err)
	}
	got, err := v.ReadFile("Saves/Nintendo/Game Boy/Pokemon/red.sav")
	if err != nil || string(got) != "SRAM" {
		t.Fatalf("existing file damaged: %q %v", got, err)
	}

	// Regular file blocking leaf or intermediate component returns fs.ErrExist without retiring mount.
	if err := v.MkdirAll(ctx, "Saves/Nintendo/Game Boy/Pokemon/red.sav"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("leaf file conflict = %v, want fs.ErrExist", err)
	}
	if err := v.MkdirAll(ctx, "Saves/Nintendo/Game Boy/Pokemon/red.sav/child"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("parent file conflict = %v, want fs.ErrExist", err)
	}

	// Invalid trailing component must not leave partial parent directories behind.
	if err := v.MkdirAll(ctx, "GhostParent/bad:leaf"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("invalid leaf = %v, want fs.ErrInvalid", err)
	}
	if _, err := v.Open("GhostParent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("GhostParent created despite invalid trailing component: err=%v", err)
	}

	for _, bad := range []string{"", "/Abs", "../rel", "a//b"} {
		if err := v.MkdirAll(ctx, bad); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("MkdirAll(%q) = %v, want fs.ErrInvalid", bad, err)
		}
	}
	if err := v.MkdirAll(nil, "x"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("MkdirAll(nil) = %v, want fs.ErrInvalid", err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := v.MkdirAll(cctx, "CanceledDir"); !errors.Is(err, context.Canceled) {
		t.Fatalf("MkdirAll(canceled) = %v, want context.Canceled", err)
	}

	// Full disk pre-check refuses nested MkdirAll atomically with ErrNoSpace before creating partial parents.
	dSparse, vSparse := newRecordingDisk(t, 2)
	// Leave only cluster 3 and cluster 4 free; mark 5..65537 allocated.
	for c := uint32(5); c < uint32(vSparse.clusterCount+2); c++ {
		setFATEntry(dSparse.sparseFATDisk, c, 0x0fffffff)
	}
	if err := vSparse.MkdirAll(ctx, "NoPartial/Level2/Level3"); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("MkdirAll low-space = %v, want ErrNoSpace", err)
	}
	if _, err := vSparse.Open("NoPartial"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("NoPartial created despite insufficient space for full chain: %v", err)
	}
	stFull, err := vSparse.StatFS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stFull.FreeClusters != 2 || !strings.EqualFold(stFull.Label, "") {
		t.Fatalf("expected 2 free clusters untouched, got %+v", stFull)
	}

	// Directory growth preflight: 512 B cluster + long child (>169 chars) under a new parent needs
	// 1 cluster (parent) + 1 cluster (child) + 1 extra cluster (parent growth) = 3 clusters total.
	dGrow, vGrow := newRecordingDisk(t, 1) // 512 B clusters
	for c := uint32(5); c < uint32(vGrow.clusterCount+2); c++ {
		setFATEntry(dGrow.sparseFATDisk, c, 0x0fffffff)
	}
	longLeaf := "ShortParent/" + strings.Repeat("a", 200)
	if err := vGrow.MkdirAll(ctx, longLeaf); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("MkdirAll needing parent extension = %v, want ErrNoSpace", err)
	}
	if _, err := vGrow.Open("ShortParent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ShortParent left behind when space insufficient for child extension: %v", err)
	}
	stGrow, err := vGrow.StatFS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stGrow.FreeClusters != 2 {
		t.Fatalf("expected 2 free clusters preserved after ErrNoSpace, got %+v", stGrow)
	}

	// Existing full parent directory needing extra cluster extension: fill root cluster except 1 slot,
	// leave 2 free clusters (enough only for 1 dir + 1 parent growth = 2, NOT 2 levels = 3 clusters).
	dFullParent, vFullParent := newRecordingDisk(t, 1)
	for i := 0; i < 15; i++ {
		if err := vFullParent.CreateFile(ctx, strings.Repeat(string(rune('A'+i)), 1), nil); err != nil {
			t.Fatal(err)
		}
	}
	// Root now uses clusters 2 (15*32 = 480 bytes used, only 32 bytes left -> adding a slot + 32B EOC forces dirExtra=1).
	for c := uint32(5); c < uint32(vFullParent.clusterCount+2); c++ {
		setFATEntry(dFullParent.sparseFATDisk, c, 0x0fffffff)
	}
	if err := vFullParent.MkdirAll(ctx, "ParentNeedGrow/Child"); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("MkdirAll on full existing parent = %v, want ErrNoSpace", err)
	}
	if _, err := vFullParent.Open("ParentNeedGrow"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ParentNeedGrow created despite insufficient space for Child: %v", err)
	}
}
