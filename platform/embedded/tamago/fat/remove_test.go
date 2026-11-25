// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func TestRemoveFileShortLFNEmptyNestedAndClusterReclaim(t *testing.T) {
	d, v := newRecordingDisk(t, 2) // 1 KiB clusters
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err := v.Mkdir(ctx, "FES/SUB"); err != nil {
		t.Fatal(err)
	}

	// 1. Short 8.3 file, nested LFN file, and 0-byte file.
	if err := v.CreateFile(ctx, "FES/SHORT.TXT", bytes.Repeat([]byte("S"), 1500)); err != nil {
		t.Fatal(err)
	}
	longRel := "FES/SUB/Long File Name With Spaces And Unicode \u00e9.bin"
	if err := v.CreateFile(ctx, longRel, bytes.Repeat([]byte("L"), 2500)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "FES/EMPTY.DAT", nil); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"FES/SHORT.TXT", longRel, "FES/EMPTY.DAT"} {
		if err := v.RemoveFile(ctx, rel); err != nil {
			t.Fatalf("RemoveFile(%q): %v", rel, err)
		}
		if _, err := v.Open(rel); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Open(%q) after remove err=%v, want fs.ErrNotExist", rel, err)
		}
		// Removing again returns fs.ErrNotExist cleanly without retiring the mount.
		if err := v.RemoveFile(ctx, rel); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("second RemoveFile(%q) err=%v, want fs.ErrNotExist", rel, err)
		}
	}

	// Recreating a previously removed path succeeds cleanly.
	if err := v.CreateFile(ctx, longRel, []byte("recreated")); err != nil {
		t.Fatalf("recreate after RemoveFile: %v", err)
	}
	if got, err := v.ReadFile(longRel); err != nil || string(got) != "recreated" {
		t.Fatalf("recreated ReadFile=%q err=%v", got, err)
	}
	if err := v.RemoveFile(ctx, longRel); err != nil {
		t.Fatal(err)
	}

	// 2. Repeated CreateFile + RemoveFile cycle reclaims clusters so > volume capacity can cycle cleanly.
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("FES/CYCLE_%02d.BIN", i)
		payload := bytes.Repeat([]byte{byte(i + 1)}, 12*1024)
		if err := v.CreateFile(ctx, name, payload); err != nil {
			t.Fatalf("cycle %d CreateFile: %v", i, err)
		}
		if err := v.RemoveFile(ctx, name); err != nil {
			t.Fatalf("cycle %d RemoveFile: %v", i, err)
		}
	}

	// Only directory chains (root, FES, FES/SUB) should remain allocated in FAT0;
	// zero removed file clusters may remain allocated.
	fesDir, err := v.lookup("FES")
	if err != nil {
		t.Fatal(err)
	}
	fesChain, err := v.chain(fesDir.cluster, maxDirectoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	subDir, err := v.lookup("FES/SUB")
	if err != nil {
		t.Fatal(err)
	}
	subChain, err := v.chain(subDir.cluster, maxDirectoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	allocated := 0
	for c := uint32(2); c < 1000; c++ {
		off := uint64(c) * 4
		sec := d.data[32+off/512]
		val := binary.LittleEndian.Uint32(sec[off%512:]) & 0x0fffffff
		if val != 0 {
			allocated++
		}
	}
	wantDirs := 1 + len(fesChain) + len(subChain)
	if allocated != wantDirs {
		t.Fatalf("allocated clusters=%d, want %d (dirs only; leaked file clusters after RemoveFile loop)", allocated, wantDirs)
	}
}

func TestRemoveFileCrossSectorLFNAndHandleInvalidation(t *testing.T) {
	_, v := newRecordingDisk(t, 8) // 4 KiB clusters (8 sectors per dir cluster)
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 8)
	var targetIdx = -1
	// Create long-named files so an LFN sequence crosses a 512-byte sector boundary.
	for i := range names {
		name := fmt.Sprintf("FES/cross_sector_%02d_%s.dat", i, string(bytes.Repeat([]byte{'a' + byte(i)}, 95)))
		names[i] = name
		if err := v.CreateFile(ctx, name, []byte{byte(i)}); err != nil {
			t.Fatalf("CreateFile %d: %v", i, err)
		}
		if targetIdx == -1 {
			e, err := v.lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, slot := range e.lfnSlots {
				if slot.sector != e.sector {
					targetIdx = i
					break
				}
			}
		}
	}
	if targetIdx < 0 {
		t.Fatal("expected at least one LFN sequence to span across a sector boundary")
	}
	target := names[targetIdx]
	keepPath := names[(targetIdx+1)%len(names)]
	keepVal := byte((targetIdx + 1) % len(names))
	fTarget, err := v.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer fTarget.Close()
	fKeep, err := v.Open(keepPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fKeep.Close()

	if err := v.RemoveFile(ctx, target); err != nil {
		t.Fatalf("RemoveFile cross-sector LFN: %v", err)
	}

	if _, err := fTarget.Read(make([]byte, 8)); !errors.Is(err, ErrReplaced) {
		t.Fatalf("removed handle Read err=%v, want ErrReplaced", err)
	}
	gotKeep, err := io.ReadAll(fKeep)
	if err != nil || len(gotKeep) != 1 || gotKeep[0] != keepVal {
		t.Fatalf("unrelated neighbor handle affected: %v %v", gotKeep, err)
	}
	for i, name := range names {
		got, err := v.ReadFile(name)
		if i == targetIdx {
			if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("removed target still readable: %v", err)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0] != byte(i) {
			t.Fatalf("neighbor %d corrupted after remove: %v %v", i, got, err)
		}
	}
}

func TestRemoveFileCrashAtEveryWritePreservesValidState(t *testing.T) {
	d, v := newRecordingDisk(t, 4) // 2 KiB clusters
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 5*1024+17)
	for i := range payload {
		payload[i] = byte(0x5a ^ (i & 0x3f))
	}
	neighbor := []byte("keep-me-safe")
	if err := v.CreateFile(ctx, "FES/Very Long Target Name For Crash Check.dat", payload); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "FES/KEEP.TXT", neighbor); err != nil {
		t.Fatal(err)
	}

	d.mu.Lock()
	d.snapshots = nil
	d.mu.Unlock()

	if err := v.RemoveFile(ctx, "FES/Very Long Target Name For Crash Check.dat"); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	d.mu.Lock()
	snaps := append([]map[uint64][block.SectorSize]byte(nil), d.snapshots...)
	d.mu.Unlock()
	if len(snaps) < 4 {
		t.Fatalf("expected >= 4 write snapshots, got %d", len(snaps))
	}

	sawPresent, sawAbsent := false, false
	for i, snap := range snaps {
		crashDisk := &sparseFATDisk{sectors: d.sectors, data: snap}
		remount, err := Mount(crashDisk)
		if err != nil {
			t.Fatalf("snapshot %d/%d Mount: %v", i, len(snaps), err)
		}
		gotKeep, err := remount.ReadFile("FES/KEEP.TXT")
		if err != nil || !bytes.Equal(gotKeep, neighbor) {
			t.Fatalf("snapshot %d/%d neighbor lost: %q %v", i, len(snaps), gotKeep, err)
		}
		got, err := remount.ReadFile("FES/Very Long Target Name For Crash Check.dat")
		switch {
		case err == nil && bytes.Equal(got, payload):
			if sawAbsent {
				t.Fatalf("snapshot %d/%d resurrected file after deletion commit", i, len(snaps))
			}
			sawPresent = true
		case errors.Is(err, fs.ErrNotExist):
			sawAbsent = true
		default:
			t.Fatalf("snapshot %d/%d returned torn state (len=%d err=%v)", i, len(snaps), len(got), err)
		}
	}
	if !sawPresent || !sawAbsent {
		t.Fatalf("snapshots did not span deletion commit: present=%t absent=%t", sawPresent, sawAbsent)
	}
}

func TestRemoveFileRefusalsAndMutationError(t *testing.T) {
	d, v := newRecordingDisk(t, 4)
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "FES/FILE.BIN", []byte("ok")); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", ".", "/FES/FILE.BIN", "../x"} {
		if err := v.RemoveFile(ctx, bad); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("RemoveFile(%q) err=%v, want fs.ErrInvalid", bad, err)
		}
	}
	if err := v.RemoveFile(ctx, "FES"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("RemoveFile(dir) err=%v, want fs.ErrPermission", err)
	}
	d.readOnly = true
	if err := v.RemoveFile(ctx, "FES/FILE.BIN"); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("readOnly RemoveFile err=%v, want ErrReadOnly", err)
	}
	d.readOnly = false
	if got, err := v.ReadFile("FES/FILE.BIN"); err != nil || string(got) != "ok" {
		t.Fatalf("volume damaged after ErrReadOnly: %q %v", got, err)
	}

	d.failFlush = true
	if err := v.RemoveFile(ctx, "FES/FILE.BIN"); !errors.Is(err, ErrMutation) {
		t.Fatalf("flush fail err=%v, want ErrMutation", err)
	}
	d.failFlush = false
	if _, err := v.ReadFile("FES/FILE.BIN"); !errors.Is(err, ErrMutation) {
		t.Fatalf("ReadFile after ErrMutation err=%v, want ErrMutation", err)
	}
}
