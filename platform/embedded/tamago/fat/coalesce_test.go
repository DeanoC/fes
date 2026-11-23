// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"sync"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

type sparseFATDisk struct {
	mu           sync.Mutex
	sectors      uint64
	data         map[uint64][block.SectorSize]byte
	readCalls    int
	writeCalls   int
	maxReadSecs  int
	maxWriteSecs int
	failReadAt   int
	failWriteAt  int
}

func newSparseFAT32(t *testing.T, spc uint8) (*sparseFATDisk, *Volume) {
	t.Helper()
	const (
		reserved   = uint64(32)
		fats       = uint64(2)
		clusters   = uint64(65536)
		fatSectors = uint64(516)
	)
	totalSectors := reserved + fats*fatSectors + clusters*uint64(spc)
	d := &sparseFATDisk{
		sectors: totalSectors,
		data:    make(map[uint64][block.SectorSize]byte),
	}
	var boot [block.SectorSize]byte
	binary.LittleEndian.PutUint16(boot[11:], block.SectorSize)
	boot[13] = spc
	binary.LittleEndian.PutUint16(boot[14:], uint16(reserved))
	boot[16] = byte(fats)
	binary.LittleEndian.PutUint32(boot[32:], uint32(totalSectors))
	binary.LittleEndian.PutUint32(boot[36:], uint32(fatSectors))
	binary.LittleEndian.PutUint32(boot[44:], 2) // root cluster
	binary.LittleEndian.PutUint16(boot[48:], 1) // FSInfo sector
	boot[510], boot[511] = 0x55, 0xaa
	d.data[0] = boot

	var fsInfo [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fsInfo[0:], 0x41615252)
	binary.LittleEndian.PutUint32(fsInfo[484:], 0x61417272)
	binary.LittleEndian.PutUint32(fsInfo[488:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[492:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[508:], 0xaa550000)
	d.data[1] = fsInfo

	var fat0 [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fat0[0:], 0x0ffffff8)
	binary.LittleEndian.PutUint32(fat0[4:], 0x0fffffff)
	binary.LittleEndian.PutUint32(fat0[8:], 0x0fffffff) // root dir EOC
	d.data[reserved] = fat0
	d.data[reserved+fatSectors] = fat0

	v, err := Mount(d)
	if err != nil {
		t.Fatalf("Mount synthetic FAT32: %v", err)
	}
	return d, v
}

func (d *sparseFATDisk) SectorCount() uint64 { return d.sectors }

func (d *sparseFATDisk) ReadSectors(lba uint64, dst []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := block.CheckRange(d.sectors, lba, len(dst)); err != nil {
		return err
	}
	d.readCalls++
	secs := len(dst) / block.SectorSize
	if secs > d.maxReadSecs {
		d.maxReadSecs = secs
	}
	if d.failReadAt > 0 && d.readCalls == d.failReadAt {
		return errors.New("injected read error")
	}
	for i := 0; i < secs; i++ {
		if blk, ok := d.data[lba+uint64(i)]; ok {
			copy(dst[i*block.SectorSize:(i+1)*block.SectorSize], blk[:])
		} else {
			clear(dst[i*block.SectorSize : (i+1)*block.SectorSize])
		}
	}
	return nil
}

func (d *sparseFATDisk) WriteSectors(lba uint64, src []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := block.CheckRange(d.sectors, lba, len(src)); err != nil {
		return err
	}
	d.writeCalls++
	secs := len(src) / block.SectorSize
	if secs > d.maxWriteSecs {
		d.maxWriteSecs = secs
	}
	if d.failWriteAt > 0 && d.writeCalls == d.failWriteAt {
		return errors.New("injected write error")
	}
	for i := 0; i < secs; i++ {
		var blk [block.SectorSize]byte
		copy(blk[:], src[i*block.SectorSize:(i+1)*block.SectorSize])
		d.data[lba+uint64(i)] = blk
	}
	return nil
}

func (d *sparseFATDisk) Flush() error { return nil }

func (d *sparseFATDisk) resetCounters() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readCalls, d.writeCalls = 0, 0
	d.maxReadSecs, d.maxWriteSecs = 0, 0
}

func TestCoalescedCreateReadWriteLargeFile(t *testing.T) {
	d, v := newSparseFAT32(t, 8) // 4 KiB clusters
	ctx := context.Background()

	if err := v.Mkdir(ctx, "FES"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// Create a 200 KiB + 333 byte file (51 clusters, 401 sectors -> > 3x 64 KiB chunks).
	payload := make([]byte, 200*1024+333)
	for i := range payload {
		payload[i] = byte((i*37 + i/512) & 0xff)
	}
	d.resetCounters()
	if err := v.CreateFile(ctx, "FES/LARGE.BIN", payload); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if d.maxWriteSecs != maxTransferSectors {
		t.Fatalf("CreateFile maxWriteSecs=%d, want %d", d.maxWriteSecs, maxTransferSectors)
	}

	// ReadFile must coalesce reads into 64 KiB chunks (plus 1 partial tail sector).
	d.resetCounters()
	got, err := v.ReadFile("FES/LARGE.BIN")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("ReadFile data mismatch")
	}
	if d.maxReadSecs != maxTransferSectors {
		t.Fatalf("ReadFile maxReadSecs=%d, want %d", d.maxReadSecs, maxTransferSectors)
	}
	// Expect far fewer reads than 401 single-sector reads (lookup + chain + 4 data calls).
	if d.readCalls > 20 {
		t.Fatalf("ReadFile used %d ReadSectors calls, expected coalesced <= 20", d.readCalls)
	}

	// Unaligned partial reads (head offset + aligned middle + partial tail + EOF).
	f, err := v.Open("FES/LARGE.BIN")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head := make([]byte, 137)
	if n, err := io.ReadFull(f, head); err != nil || n != 137 || !bytes.Equal(head, payload[:137]) {
		t.Fatalf("head read n=%d err=%v", n, err)
	}
	mid := make([]byte, 375) // completes sector 0 (137+375=512)
	if n, err := io.ReadFull(f, mid); err != nil || n != 375 || !bytes.Equal(mid, payload[137:512]) {
		t.Fatalf("mid read n=%d err=%v", n, err)
	}
	d.resetCounters()
	rest := make([]byte, len(payload)-512)
	if n, err := io.ReadFull(f, rest); err != nil || n != len(rest) || !bytes.Equal(rest, payload[512:]) {
		t.Fatalf("aligned rest read n=%d err=%v", n, err)
	}
	if d.maxReadSecs != maxTransferSectors {
		t.Fatalf("aligned file.Read maxReadSecs=%d, want %d", d.maxReadSecs, maxTransferSectors)
	}
	if n, err := f.Read(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("post-EOF read=(%d, %v)", n, err)
	}

	// WriteFile shrinks file and must coalesce writes AND zero-pad trailing sectors.
	shorter := bytes.Repeat([]byte{0xab}, 70*1024+17)
	d.resetCounters()
	if err := v.WriteFile("FES/LARGE.BIN", shorter); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if d.maxWriteSecs != maxTransferSectors {
		t.Fatalf("WriteFile maxWriteSecs=%d, want %d", d.maxWriteSecs, maxTransferSectors)
	}
	gotShort, err := v.ReadFile("FES/LARGE.BIN")
	if err != nil || !bytes.Equal(gotShort, shorter) {
		t.Fatalf("WriteFile readback len=%d err=%v", len(gotShort), err)
	}

	// Verify trailing sectors past len(shorter) were zero-padded on disk.
	e, err := v.lookup("FES/LARGE.BIN")
	if err != nil {
		t.Fatal(err)
	}
	chain, err := v.chain(e.cluster, maxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	lastLBA := v.clusterSector(chain[len(chain)-1]) + uint64(v.sectorsPerCluster-1)
	var zero [block.SectorSize]byte
	if d.data[lastLBA] != zero {
		t.Fatal("WriteFile failed to zero-pad trailing allocated cluster")
	}

	// Empty file & directory ReadFile edge cases.
	if err := v.CreateFile(ctx, "FES/EMPTY.BIN", nil); err != nil {
		t.Fatal(err)
	}
	empty, err := v.ReadFile("FES/EMPTY.BIN")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty ReadFile=(%v, %v)", empty, err)
	}
	if _, err := v.ReadFile("FES"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("directory ReadFile err=%v, want fs.ErrInvalid", err)
	}
	entries, err := v.ReadDir("FES")
	if err != nil || len(entries) != 2 {
		t.Fatalf("ReadDir len=%d err=%v", len(entries), err)
	}
}

func TestCoalescedFragmentedChainAndErrors(t *testing.T) {
	d, v := newSparseFAT32(t, 4) // 2 KiB clusters (4 sectors each)
	ctx := context.Background()

	// Create three files so we can manually craft a fragmented chain cleanly,
	// or directly verify contiguousSectors on a non-contiguous cluster slice.
	lba, count := v.contiguousSectors([]uint32{5, 9, 10, 11, 20}, 0, 0, 128)
	if lba != v.clusterSector(5) || count != 4 {
		t.Fatalf("fragmented run 0=(%d, %d), want (%d, 4)", lba, count, v.clusterSector(5))
	}
	lba, count = v.contiguousSectors([]uint32{5, 9, 10, 11, 20}, 1, 1, 128)
	if lba != v.clusterSector(9)+1 || count != 11 {
		t.Fatalf("fragmented run 1=(%d, %d), want (%d, 11)", lba, count, v.clusterSector(9)+1)
	}
	if _, zeroCount := v.contiguousSectors([]uint32{5}, 0, 0, 0); zeroCount != 0 {
		t.Fatalf("maxSectors=0 returned %d", zeroCount)
	}

	// Verify read error mid-coalesced transfer propagates cleanly.
	data := bytes.Repeat([]byte{0x42}, 10*1024)
	if err := v.CreateFile(ctx, "FRAG.BIN", data); err != nil {
		t.Fatal(err)
	}
	f, err := v.Open("FRAG.BIN")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d.resetCounters()
	d.failReadAt = 1
	if _, err := io.ReadAll(f); err == nil {
		t.Fatal("injected read error was swallowed")
	}
	d.failReadAt = 0

	// Verify write error in WriteFile propagates cleanly.
	d.resetCounters()
	d.failWriteAt = 1
	if err := v.WriteFile("FRAG.BIN", data); err == nil {
		t.Fatal("injected write error was swallowed")
	}
	d.failWriteAt = 0
}
