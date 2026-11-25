// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func TestRenameSameAndCrossDirectoryFilesAndDirs(t *testing.T) {
	d, v := newRecordingDisk(t, 1) // 512 B sectors to exercise cross-sector LFNs & parent growth
	ctx := context.Background()

	if err := v.Mkdir(ctx, "SRC"); err != nil {
		t.Fatal(err)
	}
	if err := v.Mkdir(ctx, "DST"); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("RENAME-DATA-"), 200)
	if err := v.CreateFile(ctx, "SRC/OLDNAME.BIN", payload); err != nil {
		t.Fatal(err)
	}

	// Open handle before rename so we verify ErrReplaced invalidation.
	f, err := v.Open("SRC/OLDNAME.BIN")
	if err != nil {
		t.Fatal(err)
	}

	// Same-directory rename sharing sector + LFN.
	sameNew := "SRC/Renamed Long File Name Inside Same Sector Scope.bin"
	if err := v.Rename(ctx, "SRC/OLDNAME.BIN", sameNew); err != nil {
		t.Fatalf("same-dir Rename: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := f.Read(buf); !errors.Is(err, ErrReplaced) {
		t.Fatalf("stale handle Read err=%v, want ErrReplaced", err)
	}
	_ = f.Close()
	if _, err := fs.Stat(v, "SRC/OLDNAME.BIN"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old path still exists: %v", err)
	}
	if got, err := v.ReadFile(sameNew); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("same-dir ReadFile len=%d err=%v", len(got), err)
	}

	// Cross-directory file move preserves cluster chain and bytes.
	oldEntry, err := v.lookup(sameNew)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Rename(ctx, sameNew, "DST/MOVED.BIN"); err != nil {
		t.Fatalf("cross-dir Rename: %v", err)
	}
	newEntry, err := v.lookup("DST/MOVED.BIN")
	if err != nil {
		t.Fatal(err)
	}
	if newEntry.cluster != oldEntry.cluster || newEntry.size != oldEntry.size {
		t.Fatalf("moved entry cluster/size=(%d,%d) want (%d,%d)", newEntry.cluster, newEntry.size, oldEntry.cluster, oldEntry.size)
	}
	if got, err := v.ReadFile("DST/MOVED.BIN"); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("cross-dir ReadFile len=%d err=%v", len(got), err)
	}

	// Move a non-empty directory from SRC/SUB -> DST/SUB and verify ".." updates to DST cluster,
	// then move DST/SUB -> TOPSUB under root and verify ".." updates to 0.
	if err := v.Mkdir(ctx, "SRC/SUB"); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "SRC/SUB/NOTE.TXT", []byte("nested-ok")); err != nil {
		t.Fatal(err)
	}
	if err := v.Rename(ctx, "SRC/SUB", "DST/SUB"); err != nil {
		t.Fatalf("dir move to DST/SUB: %v", err)
	}
	if got, err := v.ReadFile("DST/SUB/NOTE.TXT"); err != nil || string(got) != "nested-ok" {
		t.Fatalf("moved dir file ReadFile=%q err=%v", got, err)
	}
	dstEnt, err := v.lookup("DST")
	if err != nil {
		t.Fatal(err)
	}
	subEnt, err := v.lookup("DST/SUB")
	if err != nil {
		t.Fatal(err)
	}
	sec := make([]byte, block.SectorSize)
	if err := d.ReadSectors(v.clusterSector(subEnt.cluster), sec); err != nil {
		t.Fatal(err)
	}
	dotDot := uint32(binary.LittleEndian.Uint16(sec[32+20:]))<<16 | uint32(binary.LittleEndian.Uint16(sec[32+26:]))
	if dotDot != dstEnt.cluster {
		t.Fatalf("DST/SUB '..' cluster=%d, want %d", dotDot, dstEnt.cluster)
	}

	if err := v.Rename(ctx, "DST/SUB", "TOPSUB"); err != nil {
		t.Fatalf("dir move to root TOPSUB: %v", err)
	}
	if got, err := v.ReadFile("TOPSUB/NOTE.TXT"); err != nil || string(got) != "nested-ok" {
		t.Fatalf("root-moved dir file ReadFile=%q err=%v", got, err)
	}
	if err := d.ReadSectors(v.clusterSector(subEnt.cluster), sec); err != nil {
		t.Fatal(err)
	}
	dotDotRoot := uint32(binary.LittleEndian.Uint16(sec[32+20:]))<<16 | uint32(binary.LittleEndian.Uint16(sec[32+26:]))
	if dotDotRoot != 0 {
		t.Fatalf("TOPSUB '..' cluster=%d, want 0 (FAT32 root convention)", dotDotRoot)
	}
}

func TestRenameRefusalsAndCrashAtEveryWritePreservesData(t *testing.T) {
	d, v := newRecordingDisk(t, 2)
	ctx := context.Background()

	if err := v.Mkdir(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	if err := v.Mkdir(ctx, "A/CHILD"); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("CRASH-SAFE-RENAME-"), 128)
	if err := v.CreateFile(ctx, "A/EXISTING.TXT", []byte("exists")); err != nil {
		t.Fatal(err)
	}
	srcPath := "A/Very Long Source Name With Multiple LFN Slots For Crash Testing.bin"
	dstPath := "Very Long Root Destination Name Across Directory Entry Sectors.bin"
	if err := v.CreateFile(ctx, srcPath, payload); err != nil {
		t.Fatal(err)
	}

	// Refusals before mutation (including mixed-case descendant self-move).
	if err := v.Rename(ctx, "A", "A/CHILD/CYCLE"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("self-descendant Rename err=%v, want fs.ErrInvalid", err)
	}
	if err := v.Rename(ctx, "A", "a/child/CYCLE"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("mixed-case self-descendant Rename err=%v, want fs.ErrInvalid", err)
	}
	if err := v.Rename(ctx, srcPath, "A/EXISTING.TXT"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("collision Rename err=%v, want fs.ErrExist", err)
	}
	if err := v.Rename(ctx, srcPath, srcPath); err != nil {
		t.Fatalf("self Rename err=%v, want nil", err)
	}
	d.readOnly = true
	if err := v.Rename(ctx, srcPath, dstPath); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("readOnly Rename err=%v, want ErrReadOnly", err)
	}
	d.readOnly = false

	// Record every write/flush boundary during Rename.
	d.mu.Lock()
	d.snapshots = nil
	d.mu.Unlock()
	if err := v.Rename(ctx, srcPath, dstPath); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	snaps := append([]map[uint64][block.SectorSize]byte(nil), d.snapshots...)
	d.mu.Unlock()
	if len(snaps) < 4 {
		t.Fatalf("expected >= 4 write snapshots, got %d", len(snaps))
	}

	sawOldOnly, sawNewOnly := false, false
	for i, snap := range snaps {
		crashDisk := &sparseFATDisk{sectors: d.sectors, data: snap}
		remount, err := Mount(crashDisk)
		if err != nil {
			t.Fatalf("snapshot %d/%d Mount: %v", i, len(snaps), err)
		}
		if got, err := remount.ReadFile("A/EXISTING.TXT"); err != nil || string(got) != "exists" {
			t.Fatalf("snapshot %d/%d neighbor lost: %q %v", i, len(snaps), got, err)
		}
		oldData, oldErr := remount.ReadFile(srcPath)
		newData, newErr := remount.ReadFile(dstPath)
		oldOK := oldErr == nil && bytes.Equal(oldData, payload)
		newOK := newErr == nil && bytes.Equal(newData, payload)
		if !oldOK && !newOK {
			t.Fatalf("snapshot %d/%d lost payload at crash point (oldErr=%v newErr=%v)", i, len(snaps), oldErr, newErr)
		}
		if oldOK && newErr != nil && !errors.Is(newErr, fs.ErrNotExist) {
			t.Fatalf("snapshot %d/%d torn new target: %v", i, len(snaps), newErr)
		}
		if newOK && oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
			t.Fatalf("snapshot %d/%d torn old target: %v", i, len(snaps), oldErr)
		}
		if oldOK && !newOK {
			sawOldOnly = true
		}
		if newOK && !oldOK {
			sawNewOnly = true
		}
	}
	if !sawOldOnly || !sawNewOnly {
		t.Fatalf("snapshots did not span rename commit: oldOnly=%t newOnly=%t", sawOldOnly, sawNewOnly)
	}

	// Remount final image cleanly and verify readable via io.ReadAll.
	rc, err := v.Open(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(all, payload) {
		t.Fatalf("final ReadAll len=%d err=%v", len(all), err)
	}
}

func TestRenameCrossFATSectorDirGrowthIsCrashSafe(t *testing.T) {
	d, v := newRecordingDisk(t, 1) // 512 B clusters; 128 FAT entries per sector
	ctx := context.Background()

	if err := v.Mkdir(ctx, "DIR"); err != nil {
		t.Fatal(err)
	}
	// Allocate clusters past FAT sector 0 (cluster >= 128) so newly allocated DIR
	// growth clusters sit in FAT sector 1 while DIR's initial cluster is in FAT sector 0.
	if err := v.CreateFile(ctx, "PAD.BIN", bytes.Repeat([]byte("P"), 130*block.SectorSize)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 15; i++ {
		if err := v.CreateFile(ctx, "DIR/F"+string([]byte{'A' + byte(i)})+".TXT", []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	target := "DIR/S_LAST.TXT"
	payload := []byte("cross-fat-sector-rename")
	if err := v.CreateFile(ctx, target, payload); err != nil {
		t.Fatal(err)
	}

	d.mu.Lock()
	d.snapshots = nil
	d.mu.Unlock()
	dst := "DIR/Very Long Renamed Entry Requiring Cross FAT Sector Directory Growth.txt"
	if err := v.Rename(ctx, target, dst); err != nil {
		t.Fatalf("cross-FAT-sector Rename: %v", err)
	}
	d.mu.Lock()
	snaps := append([]map[uint64][block.SectorSize]byte(nil), d.snapshots...)
	d.mu.Unlock()
	for i, snap := range snaps {
		crashDisk := &sparseFATDisk{sectors: d.sectors, data: snap}
		remount, err := Mount(crashDisk)
		if err != nil {
			t.Fatalf("snapshot %d/%d Mount: %v", i, len(snaps), err)
		}
		_, errOld := remount.ReadFile(target)
		_, errNew := remount.ReadFile(dst)
		if errOld != nil && errNew != nil {
			t.Fatalf("snapshot %d/%d lost file across growth rename: old=%v new=%v", i, len(snaps), errOld, errNew)
		}
	}
}
