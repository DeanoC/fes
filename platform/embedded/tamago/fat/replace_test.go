// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

type recordingFATDisk struct {
	*sparseFATDisk
	mu        sync.Mutex
	snapshots []map[uint64][block.SectorSize]byte
	failFlush bool
	readOnly  bool
}

func newRecordingDisk(t *testing.T, spc uint8) (*recordingFATDisk, *Volume) {
	t.Helper()
	d, v := newSparseFAT32(t, spc)
	rec := &recordingFATDisk{sparseFATDisk: d}
	v.device = rec
	return rec, v
}

func (r *recordingFATDisk) WriteSectors(lba uint64, src []byte) error {
	if r.readOnly {
		return block.ErrReadOnly
	}
	if err := r.sparseFATDisk.WriteSectors(lba, src); err != nil {
		return err
	}
	r.mu.Lock()
	snap := maps.Clone(r.sparseFATDisk.data)
	r.snapshots = append(r.snapshots, snap)
	r.mu.Unlock()
	return nil
}

func (r *recordingFATDisk) Flush() error {
	if r.failFlush {
		return errors.New("injected flush error")
	}
	return nil
}

func TestReplaceFileLifecycleGrowShrinkEmptyAndSpaceReclaim(t *testing.T) {
	d, v := newRecordingDisk(t, 2) // 1 KiB clusters
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}

	// 1. ReplaceFile creates when absent.
	v1 := bytes.Repeat([]byte{0x11}, 1500)
	if err := v.ReplaceFile(ctx, "FES/SETTINGS.BIN", v1); err != nil {
		t.Fatalf("ReplaceFile create: %v", err)
	}
	got, err := v.ReadFile("FES/SETTINGS.BIN")
	if err != nil || !bytes.Equal(got, v1) {
		t.Fatalf("after create len=%d err=%v", len(got), err)
	}

	// 2. Grow larger with clock timestamp set.
	stamp := time.Date(2026, 11, 24, 15, 30, 12, 0, time.UTC)
	v.SetClock(func() time.Time { return stamp })
	v2 := bytes.Repeat([]byte{0x22}, 9000) // 9 clusters
	if err := v.ReplaceFile(ctx, "FES/SETTINGS.BIN", v2); err != nil {
		t.Fatalf("ReplaceFile grow: %v", err)
	}
	got, err = v.ReadFile("FES/SETTINGS.BIN")
	if err != nil || !bytes.Equal(got, v2) {
		t.Fatalf("after grow len=%d err=%v", len(got), err)
	}
	e, err := v.lookup("FES/SETTINGS.BIN")
	if err != nil {
		t.Fatal(err)
	}
	if !e.modTime.Equal(stamp) {
		t.Fatalf("modTime=%v, want %v", e.modTime, stamp)
	}

	// 3. Shrink to empty (0 bytes) frees all clusters and clears start cluster.
	if err := v.ReplaceFile(ctx, "FES/SETTINGS.BIN", []byte{}); err != nil {
		t.Fatalf("ReplaceFile empty: %v", err)
	}
	got, err = v.ReadFile("FES/SETTINGS.BIN")
	if err != nil || len(got) != 0 {
		t.Fatalf("after empty len=%d err=%v", len(got), err)
	}
	eEmpty, err := v.lookup("FES/SETTINGS.BIN")
	if err != nil {
		t.Fatal(err)
	}
	if eEmpty.cluster != 0 || eEmpty.size != 0 {
		t.Fatalf("empty entry cluster=%d size=%d, want 0/0", eEmpty.cluster, eEmpty.size)
	}

	// 4. Replace empty -> non-empty, then repeat many times to ensure no cluster leak.
	for i := 0; i < 40; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, 12*1024)
		if err := v.ReplaceFile(ctx, "FES/SETTINGS.BIN", payload); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	// Verify free clusters count matches expectation (only 12 clusters used by SETTINGS.BIN + 1 for FES dir + 1 for root).
	// Count allocated FAT entries directly in FAT0.
	allocated := 0
	for c := uint32(2); c < 1000; c++ {
		off := uint64(c) * 4
		sec := d.data[32+off/512]
		val := binary.LittleEndian.Uint32(sec[off%512:]) & 0x0fffffff
		if val != 0 {
			allocated++
		}
	}
	// root(1) + FES(1) + SETTINGS.BIN(12) = 14 allocated clusters; none leaked!
	if allocated != 14 {
		t.Fatalf("allocated clusters=%d, want 14 (leaked old clusters after 40 replacements)", allocated)
	}
}

func TestReplaceFileCrashAtEveryWritePreservesOldOrNewData(t *testing.T) {
	d, v := newRecordingDisk(t, 4) // 2 KiB clusters
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	oldData := make([]byte, 5*1024+13)
	for i := range oldData {
		oldData[i] = byte(0xa0 ^ (i & 0x3f))
	}
	newData := make([]byte, 9*1024+77)
	for i := range newData {
		newData[i] = byte(0x50 ^ ((i * 7) & 0x3f))
	}
	if err := v.CreateFile(ctx, "FES/SAVE.DAT", oldData); err != nil {
		t.Fatal(err)
	}

	d.mu.Lock()
	d.snapshots = nil
	d.mu.Unlock()

	if err := v.ReplaceFile(ctx, "FES/SAVE.DAT", newData); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	d.mu.Lock()
	snaps := append([]map[uint64][block.SectorSize]byte(nil), d.snapshots...)
	d.mu.Unlock()
	if len(snaps) < 5 {
		t.Fatalf("expected >= 5 write snapshots, got %d", len(snaps))
	}

	sawOld, sawNew := false, false
	for i, snap := range snaps {
		crashDisk := &sparseFATDisk{sectors: d.sectors, data: snap}
		remount, err := Mount(crashDisk)
		if err != nil {
			t.Fatalf("snapshot %d/%d failed Mount: %v", i, len(snaps), err)
		}
		got, err := remount.ReadFile("FES/SAVE.DAT")
		if err != nil {
			t.Fatalf("snapshot %d/%d failed ReadFile: %v", i, len(snaps), err)
		}
		switch {
		case bytes.Equal(got, oldData):
			sawOld = true
		case bytes.Equal(got, newData):
			sawNew = true
		default:
			t.Fatalf("snapshot %d/%d returned torn/corrupted data (len=%d)", i, len(snaps), len(got))
		}
	}
	if !sawOld || !sawNew {
		t.Fatalf("crash snapshots did not span commit boundary: sawOld=%t sawNew=%t", sawOld, sawNew)
	}
}

func TestReplaceFileErrorsAndCoalescedLongDirectoryWrite(t *testing.T) {
	d, v := newRecordingDisk(t, 8) // 4 KiB clusters (8 sectors/cluster)
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateFile(ctx, "FES/ORIG.BIN", []byte("orig")); err != nil {
		t.Fatal(err)
	}

	// Directory and read-only refusal before mutation keep volume healthy.
	if err := v.ReplaceFile(ctx, "FES", []byte("x")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("replace dir err=%v, want fs.ErrPermission", err)
	}
	d.readOnly = true
	if err := v.ReplaceFile(ctx, "FES/ORIG.BIN", []byte("new")); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("read-only replace err=%v, want ErrReadOnly", err)
	}
	d.readOnly = false
	if got, err := v.ReadFile("FES/ORIG.BIN"); err != nil || string(got) != "orig" {
		t.Fatalf("volume corrupted after pre-mutation readOnly refusal: %q %v", got, err)
	}

	// Create enough entries so an LFN spans sector boundary in CreateFile and exercises coalesced directory writes.
	for i := 0; i < 10; i++ {
		name := "FES/" + string(bytes.Repeat([]byte{'a' + byte(i)}, 120))
		if err := v.CreateFile(ctx, name, []byte{byte(i)}); err != nil {
			t.Fatalf("long name %d: %v", i, err)
		}
	}

	// Flush failure mid-ReplaceFile must retire the volume with ErrMutation.
	d.failFlush = true
	if err := v.ReplaceFile(ctx, "FES/ORIG.BIN", []byte("boom")); !errors.Is(err, ErrMutation) {
		t.Fatalf("flush fail err=%v, want ErrMutation", err)
	}
	d.failFlush = false
	if _, err := v.ReadFile("FES/ORIG.BIN"); !errors.Is(err, ErrMutation) {
		t.Fatalf("ReadFile after ErrMutation err=%v, want ErrMutation", err)
	}
}
