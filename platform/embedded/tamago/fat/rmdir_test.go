// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func TestRemoveDirEmptyLFNReclaimAndRefusals(t *testing.T) {
	d, v := newRecordingDisk(t, 1) // 512 B clusters to test cross-sector LFN + reclaim
	ctx := context.Background()

	if err := v.Mkdir(ctx, "PARENT"); err != nil {
		t.Fatal(err)
	}
	longDir := "PARENT/Long Empty Directory Name Across Sectors With Unicode \u00e9"
	if err := v.Mkdir(ctx, longDir); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, longDir+"/CHILD.TXT", []byte("nonempty")); err != nil {
		t.Fatal(err)
	}
	if err := v.RemoveDir(ctx, longDir); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveDir non-empty err=%v, want ErrNotEmpty", err)
	}
	if err := v.RemoveFile(ctx, longDir+"/CHILD.TXT"); err != nil {
		t.Fatal(err)
	}
	if err := v.RemoveDir(ctx, longDir); err != nil {
		t.Fatalf("RemoveDir empty after child remove: %v", err)
	}
	if _, err := fs.Stat(v, longDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("fs.Stat(%q) after RemoveDir err=%v, want fs.ErrNotExist", longDir, err)
	}

	// Cycle Mkdir + RemoveDir beyond free-cluster count to prove directory clusters are reclaimed.
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("PARENT/DIR_%03d", i)
		if err := v.Mkdir(ctx, name); err != nil {
			t.Fatalf("cycle %d Mkdir: %v", i, err)
		}
		if err := v.RemoveDir(ctx, name); err != nil {
			t.Fatalf("cycle %d RemoveDir: %v", i, err)
		}
	}

	// Refusals: root, invalid path, regular file, missing target, read-only medium.
	if err := v.CreateFile(ctx, "PARENT/REG.TXT", []byte("file")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", ".", "/PARENT", "../x", "PARENT/REG.TXT"} {
		if err := v.RemoveDir(ctx, bad); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("RemoveDir(%q) err=%v, want fs.ErrInvalid", bad, err)
		}
	}
	if err := v.RemoveDir(ctx, "PARENT/NOPE"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("RemoveDir(missing) err=%v, want fs.ErrNotExist", err)
	}
	d.readOnly = true
	if err := v.RemoveDir(ctx, "PARENT"); !errors.Is(err, ErrNotEmpty) {
		// PARENT still holds REG.TXT so check ErrNotEmpty or ErrReadOnly after empty.
		t.Fatalf("PARENT non-empty err=%v, want ErrNotEmpty", err)
	}
	d.readOnly = false
	if err := v.RemoveFile(ctx, "PARENT/REG.TXT"); err != nil {
		t.Fatal(err)
	}
	d.readOnly = true
	if err := v.RemoveDir(ctx, "PARENT"); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("readOnly RemoveDir err=%v, want ErrReadOnly", err)
	}
	d.readOnly = false
	if err := v.RemoveDir(ctx, "PARENT"); err != nil {
		t.Fatalf("RemoveDir empty PARENT: %v", err)
	}

	// Cross-check mtools / clean mount: volume only has root cluster remaining allocated.
	sector := make([]byte, block.SectorSize)
	inUse := 0
	totalFAT := (v.clusterCount + 2 + 127) / 128
	for s := uint64(0); s < totalFAT; s++ {
		if err := d.ReadSectors(v.fatStart+s, sector); err != nil {
			t.Fatal(err)
		}
		for i := uint64(0); i < 128; i++ {
			c := s*128 + i
			if c < 2 || c >= v.clusterCount+2 {
				continue
			}
			if binary.LittleEndian.Uint32(sector[i*4:])&0x0fffffff != 0 {
				inUse++
			}
		}
	}
	if inUse != 1 { // only root directory cluster
		t.Fatalf("allocated clusters after removing all created dirs=%d, want 1", inUse)
	}
}

func TestRemoveDirCrashAtEveryWriteIsCrashSafe(t *testing.T) {
	d, v := newRecordingDisk(t, 1) // 512 B sectors so LFN spans sectors
	ctx := context.Background()

	if err := v.Mkdir(ctx, "KEEP"); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "KEEP/OK.TXT", []byte("safe")); err != nil {
		t.Fatal(err)
	}
	target := "KEEP/Long Directory Target With Cross Sector LFN Entries For Crash Safety"
	if err := v.Mkdir(ctx, target); err != nil {
		t.Fatal(err)
	}

	d.mu.Lock()
	d.snapshots = nil
	d.mu.Unlock()
	if err := v.RemoveDir(ctx, target); err != nil {
		t.Fatal(err)
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
		if got, err := remount.ReadFile("KEEP/OK.TXT"); err != nil || !bytes.Equal(got, []byte("safe")) {
			t.Fatalf("snapshot %d/%d KEEP/OK.TXT damaged: %q %v", i, len(snaps), got, err)
		}
		_, err = fs.Stat(remount, target)
		switch {
		case err == nil:
			if sawAbsent {
				t.Fatalf("snapshot %d/%d resurrected dir after deletion commit", i, len(snaps))
			}
			sawPresent = true
		case errors.Is(err, fs.ErrNotExist):
			sawAbsent = true
		default:
			t.Fatalf("snapshot %d/%d Stat err=%v", i, len(snaps), err)
		}
	}
	if !sawPresent || !sawAbsent {
		t.Fatalf("snapshots did not span commit: present=%t absent=%t", sawPresent, sawAbsent)
	}
}
