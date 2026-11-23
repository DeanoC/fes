// SPDX-License-Identifier: GPL-3.0-or-later

// Package fat mounts an existing FAT32 volume over fixed-sector storage.
// Reading is general within the bounded boot-volume limits. WriteFile updates
// preallocated files; CreateFile and Mkdir allocate new entries. Rename, removal
// and formatting are not implemented. Successful writes include device Flush.
// It establishes clean-reboot persistence, not power-loss atomicity.
package fat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

var ErrCorrupt = errors.New("invalid FAT32 volume")
var ErrCapacity = errors.New("file exceeds its preallocated FAT32 capacity")

const maxFileBytes = 64 << 20
const maxDirectoryBytes = 4 << 20
const maxTransferSectors = 128 // 64 KiB per coalesced ReadSectors/WriteSectors call

type Volume struct {
	mu                  sync.Mutex
	device              block.Device
	fatStart, dataStart uint64
	clusterCount        uint64
	sectorsPerCluster   uint32
	root                uint32
	fatCopies           []uint64
	fsInfo              uint64
	allocationValid     bool
	mutationErr         error
}

// Mount accepts a FAT32 superfloppy or one FAT32 MBR partition. Sector size is
// fixed at 512 bytes. It does not format or repair media.
func Mount(device block.Device) (*Volume, error) {
	if device == nil || device.SectorCount() == 0 {
		return nil, fmt.Errorf("%w: empty device", ErrCorrupt)
	}
	b := make([]byte, block.SectorSize)
	if err := device.ReadSectors(0, b); err != nil {
		return nil, err
	}
	if v, err := parseBoot(device, 0, device.SectorCount(), b); err == nil {
		return v, nil
	}
	if b[510] != 0x55 || b[511] != 0xaa {
		return nil, fmt.Errorf("%w: missing boot signature", ErrCorrupt)
	}
	var start, count uint64
	for i := 0; i < 4; i++ {
		p := b[446+i*16 : 446+(i+1)*16]
		if p[4] != 0x0b && p[4] != 0x0c {
			continue
		}
		if count != 0 {
			return nil, fmt.Errorf("%w: ambiguous FAT32 partitions", ErrCorrupt)
		}
		start = uint64(binary.LittleEndian.Uint32(p[8:]))
		count = uint64(binary.LittleEndian.Uint32(p[12:]))
		if start == 0 || count == 0 || start > device.SectorCount() || count > device.SectorCount()-start {
			return nil, fmt.Errorf("%w: partition bounds", ErrCorrupt)
		}
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: no FAT32 boot volume", ErrCorrupt)
	}
	if err := device.ReadSectors(start, b); err != nil {
		return nil, err
	}
	return parseBoot(device, start, count, b)
}

func parseBoot(device block.Device, base, limit uint64, b []byte) (*Volume, error) {
	if b[510] != 0x55 || b[511] != 0xaa || binary.LittleEndian.Uint16(b[11:]) != 512 ||
		binary.LittleEndian.Uint16(b[17:]) != 0 || binary.LittleEndian.Uint16(b[22:]) != 0 || binary.LittleEndian.Uint16(b[42:]) != 0 {
		return nil, fmt.Errorf("%w: unsupported BPB", ErrCorrupt)
	}
	spc := uint32(b[13])
	reserved := uint64(binary.LittleEndian.Uint16(b[14:]))
	fats := uint64(b[16])
	fatSectors := uint64(binary.LittleEndian.Uint32(b[36:]))
	sectors := uint64(binary.LittleEndian.Uint32(b[32:]))
	root := binary.LittleEndian.Uint32(b[44:])
	flags := binary.LittleEndian.Uint16(b[40:])
	if spc == 0 || spc > 128 || spc&(spc-1) != 0 || reserved == 0 || fats < 1 || fats > 2 || fatSectors == 0 || sectors == 0 || sectors > limit {
		return nil, fmt.Errorf("%w: geometry", ErrCorrupt)
	}
	overhead := reserved + fats*fatSectors
	if overhead >= sectors {
		return nil, fmt.Errorf("%w: no data area", ErrCorrupt)
	}
	clusters := (sectors - overhead) / uint64(spc)
	if clusters < 65525 || clusters >= 0x0ffffff0 || (clusters+2)*4 > fatSectors*512 || root < 2 || uint64(root) >= clusters+2 {
		return nil, fmt.Errorf("%w: cluster geometry", ErrCorrupt)
	}
	active := uint64(0)
	if flags&0x80 != 0 {
		active = uint64(flags & 0xf)
		if active >= fats {
			return nil, fmt.Errorf("%w: active FAT", ErrCorrupt)
		}
	}
	v := &Volume{device: device, fatStart: base + reserved + active*fatSectors, dataStart: base + overhead,
		clusterCount: clusters, sectorsPerCluster: spc, root: root, allocationValid: flags & ^uint16(0x8f) == 0}
	if flags&0x80 != 0 {
		v.fatCopies = []uint64{v.fatStart}
	} else {
		for i := uint64(0); i < fats; i++ {
			v.fatCopies = append(v.fatCopies, base+reserved+i*fatSectors)
		}
	}
	info := uint64(binary.LittleEndian.Uint16(b[48:]))
	if info != 0 && info != 0xffff {
		if info >= reserved {
			v.allocationValid = false
		} else {
			v.fsInfo = base + info
		}
	}
	return v, nil
}

func (v *Volume) chain(first uint32, limit int) ([]uint32, error) {
	return v.chainContext(context.Background(), first, limit)
}
func (v *Volume) chainContext(ctx context.Context, first uint32, limit int) ([]uint32, error) {
	if first == 0 {
		return nil, nil
	}
	clusterBytes := int(v.sectorsPerCluster) * block.SectorSize
	maxClusters := (limit + clusterBytes - 1) / clusterBytes
	seen := make(map[uint32]bool)
	result := make([]uint32, 0)
	sector := make([]byte, block.SectorSize)
	var sectorLBA uint64
	sectorLoaded := false
	for current := first; ; {
		if current < 2 || uint64(current) >= v.clusterCount+2 || seen[current] || len(result) >= maxClusters {
			return nil, fmt.Errorf("%w: invalid, cyclic or oversized chain", ErrCorrupt)
		}
		seen[current] = true
		result = append(result, current)
		fatOffset := uint64(current) * 4
		lba := v.fatStart + fatOffset/512
		// A FAT sector holds 128 entries. Reuse it only within this locked
		// chain walk; later opens and storage generations must read it anew.
		if !sectorLoaded || lba != sectorLBA {
			runtime.Gosched()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := v.device.ReadSectors(lba, sector); err != nil {
				return nil, err
			}
			sectorLBA, sectorLoaded = lba, true
		}
		next := binary.LittleEndian.Uint32(sector[fatOffset%512:]) & 0x0fffffff
		if next >= 0x0ffffff8 {
			return result, nil
		}
		if next >= 0x0ffffff0 {
			return nil, fmt.Errorf("%w: reserved/bad cluster", ErrCorrupt)
		}
		current = next
	}
}

func (v *Volume) clusterSector(cluster uint32) uint64 {
	return v.dataStart + uint64(cluster-2)*uint64(v.sectorsPerCluster)
}

// contiguousSectors returns the starting LBA and sector count (capped at
// maxSectors) of the contiguous on-disk run starting at chain[clusterIdx] +
// sectorInCluster.
func (v *Volume) contiguousSectors(chain []uint32, clusterIdx int, sectorInCluster uint32, maxSectors int) (uint64, int) {
	if maxSectors <= 0 {
		return 0, 0
	}
	spc := int(v.sectorsPerCluster)
	startLBA := v.clusterSector(chain[clusterIdx]) + uint64(sectorInCluster)
	count := min(spc-int(sectorInCluster), maxSectors)
	prev := chain[clusterIdx]
	for nextIdx := clusterIdx + 1; count < maxSectors && nextIdx < len(chain); nextIdx++ {
		if chain[nextIdx] != prev+1 {
			break
		}
		prev = chain[nextIdx]
		count += min(spc, maxSectors-count)
	}
	return startLBA, count
}

// writeChain writes payload (zero-padded to len(chain)*sectorsPerCluster*512)
// across contiguous runs of chain using up to maxTransferSectors per writeFn call.
func (v *Volume) writeChain(chain []uint32, payload []byte, writeFn func(lba uint64, buf []byte) error) error {
	if len(chain) == 0 {
		return nil
	}
	spc := int(v.sectorsPerCluster)
	totalSectors := len(chain) * spc
	var pad []byte
	sectorPos := 0
	for sectorPos < totalSectors {
		clusterIdx := sectorPos / spc
		sectorInCluster := uint32(sectorPos % spc)
		want := min(totalSectors-sectorPos, maxTransferSectors)
		lba, runSectors := v.contiguousSectors(chain, clusterIdx, sectorInCluster, want)
		bytePos := sectorPos * block.SectorSize
		runBytes := runSectors * block.SectorSize
		var chunk []byte
		if bytePos+runBytes <= len(payload) {
			chunk = payload[bytePos : bytePos+runBytes]
		} else {
			if len(pad) < runBytes {
				pad = make([]byte, runBytes)
			}
			chunk = pad[:runBytes]
			clear(chunk)
			if bytePos < len(payload) {
				copy(chunk, payload[bytePos:])
			}
		}
		if err := writeFn(lba, chunk); err != nil {
			return err
		}
		sectorPos += runSectors
	}
	return nil
}

type entry struct {
	name       string
	attributes byte
	cluster    uint32
	size       uint32
	sector     uint64
	offset     int
}

func shortName(b []byte) string {
	base := strings.TrimRight(string(b[:8]), " ")
	ext := strings.TrimRight(string(b[8:11]), " ")
	if b[0] == 5 {
		base = string([]byte{0xe5}) + base[1:]
	}
	if b[12]&8 != 0 {
		base = strings.ToLower(base)
	}
	if b[12]&16 != 0 {
		ext = strings.ToLower(ext)
	}
	if ext != "" {
		return base + "." + ext
	}
	return base
}

func shortChecksum(b []byte) byte {
	var sum byte
	for _, x := range b[:11] {
		sum = (sum >> 1) | (sum << 7)
		sum += x
	}
	return sum
}

func (v *Volume) entries(cluster uint32) ([]entry, error) {
	return v.entriesContext(context.Background(), cluster)
}
func (v *Volume) entriesContext(ctx context.Context, cluster uint32) ([]entry, error) {
	chain, err := v.chainContext(ctx, cluster, maxDirectoryBytes)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("%w: empty directory chain", ErrCorrupt)
	}
	var result []entry
	var lfn []uint16
	var checksum byte
	expected := 0
	spc := int(v.sectorsPerCluster)
	chunkCap := min(spc, maxTransferSectors)
	buf := make([]byte, chunkCap*block.SectorSize)
	for _, c := range chain {
		for s := 0; s < spc; {
			run := min(spc-s, chunkCap)
			runtime.Gosched()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			baseLBA := v.clusterSector(c) + uint64(s)
			if err := v.device.ReadSectors(baseLBA, buf[:run*block.SectorSize]); err != nil {
				return nil, err
			}
			for r := 0; r < run; r++ {
				sector := baseLBA + uint64(r)
				b := buf[r*block.SectorSize : (r+1)*block.SectorSize]
				for off := 0; off < block.SectorSize; off += 32 {
					d := b[off : off+32]
					if d[0] == 0 {
						return result, nil
					}
					if d[0] == 0xe5 {
						lfn = nil
						expected = 0
						continue
					}
					if d[11] == 0x0f {
						seq := int(d[0] & 0x1f)
						if d[0]&0x40 != 0 {
							if seq == 0 || seq > 20 {
								lfn = nil
								expected = 0
								continue
							}
							lfn = make([]uint16, seq*13)
							expected = seq
							checksum = d[13]
						}
						if seq == 0 || seq != expected || len(lfn) < seq*13 || d[13] != checksum || d[12] != 0 || binary.LittleEndian.Uint16(d[26:]) != 0 {
							lfn = nil
							expected = 0
							continue
						}
						positions := []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}
						for i, p := range positions {
							lfn[(seq-1)*13+i] = binary.LittleEndian.Uint16(d[p:])
						}
						expected--
						continue
					}
					name := shortName(d)
					if lfn != nil && expected == 0 && checksum == shortChecksum(d) {
						n := 0
						for n < len(lfn) && lfn[n] != 0 && lfn[n] != 0xffff {
							n++
						}
						name = string(utf16.Decode(lfn[:n]))
					}
					lfn = nil
					expected = 0
					if d[11]&8 != 0 || name == "." || name == ".." {
						continue
					}
					if name == "" || strings.ContainsAny(name, "/\\\x00") {
						return nil, fmt.Errorf("%w: directory name", ErrCorrupt)
					}
					first := uint32(binary.LittleEndian.Uint16(d[20:]))<<16 | uint32(binary.LittleEndian.Uint16(d[26:]))
					result = append(result, entry{name: name, attributes: d[11], cluster: first, size: binary.LittleEndian.Uint32(d[28:]), sector: sector, offset: off})
				}
			}
			s += run
		}
	}
	return result, nil
}

func (v *Volume) lookup(name string) (entry, error) {
	return v.lookupContext(context.Background(), name)
}
func (v *Volume) lookupContext(ctx context.Context, name string) (entry, error) {
	if !fs.ValidPath(name) {
		return entry{}, fs.ErrInvalid
	}
	current := entry{name: ".", attributes: 16, cluster: v.root}
	if name == "." {
		return current, nil
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if current.attributes&16 == 0 {
			return entry{}, fs.ErrNotExist
		}
		items, err := v.entriesContext(ctx, current.cluster)
		if err != nil {
			return entry{}, err
		}
		found := false
		for _, item := range items {
			if strings.EqualFold(item.name, part) {
				if found {
					return entry{}, fmt.Errorf("%w: ambiguous name", ErrCorrupt)
				}
				current = item
				found = true
			}
		}
		if !found {
			return entry{}, fs.ErrNotExist
		}
	}
	return current, nil
}

func (v *Volume) Open(name string) (fs.File, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return nil, v.mutationErr
	}
	e, err := v.lookup(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	f := &file{volume: v, entry: e}
	if e.attributes&16 != 0 {
		f.directory, err = v.entries(e.cluster)
		sort.Slice(f.directory, func(i, j int) bool { return f.directory[i].name < f.directory[j].name })
	} else {
		if e.size > maxFileBytes {
			err = ErrCapacity
		} else {
			f.chain, err = v.chain(e.cluster, maxFileBytes)
		}
		if err == nil && uint64(len(f.chain))*uint64(v.sectorsPerCluster)*512 < uint64(e.size) {
			err = ErrCorrupt
		}
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return f, nil
}

func (v *Volume) ReadFile(name string) ([]byte, error) {
	f, err := v.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && !fi.IsDir() {
		size := fi.Size()
		if size == 0 {
			return []byte{}, nil
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(f, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	return io.ReadAll(f)
}
func (v *Volume) ReadDir(name string) ([]fs.DirEntry, error) {
	f, err := v.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.(fs.ReadDirFile).ReadDir(-1)
}

// WriteFile replaces only an existing preallocated regular file. Its FAT chain
// remains allocated, allowing later writes up to the same capacity. File data
// is flushed before the directory size, followed by a second flush. This is a
// clean-reboot durability boundary, not a transactional power-cut guarantee.
func (v *Volume) WriteFile(name string, data []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return v.mutationErr
	}
	e, err := v.lookup(name)
	if err != nil {
		return &fs.PathError{Op: "write", Path: name, Err: err}
	}
	if e.attributes&0x11 != 0 {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrPermission}
	}
	chain, err := v.chain(e.cluster, maxFileBytes)
	if err != nil {
		return err
	}
	capacity := uint64(len(chain)) * uint64(v.sectorsPerCluster) * 512
	if uint64(len(data)) > capacity || len(data) > maxFileBytes {
		return ErrCapacity
	}
	if err := v.writeChain(chain, data, v.device.WriteSectors); err != nil {
		return err
	}
	if err := v.device.Flush(); err != nil {
		return err
	}
	b := make([]byte, block.SectorSize)
	if err := v.device.ReadSectors(e.sector, b); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b[e.offset+28:], uint32(len(data)))
	if err := v.device.WriteSectors(e.sector, b); err != nil {
		return err
	}
	return v.device.Flush()
}

type file struct {
	volume    *Volume
	entry     entry
	chain     []uint32
	directory []entry
	position  int64
	closed    bool
}

func (f *file) Close() error { f.closed = true; return nil }
func (f *file) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	return info{f.entry}, nil
}
func (f *file) Read(dst []byte) (int, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	if f.entry.attributes&16 != 0 {
		return 0, fs.ErrInvalid
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if f.position >= int64(f.entry.size) {
		return 0, io.EOF
	}
	f.volume.mu.Lock()
	defer f.volume.mu.Unlock()
	if f.volume.mutationErr != nil {
		return 0, f.volume.mutationErr
	}
	count := min(len(dst), int(int64(f.entry.size)-f.position))
	done := 0
	var scratch [block.SectorSize]byte
	clusterBytes := int64(f.volume.sectorsPerCluster) * block.SectorSize
	for done < count {
		clusterIdx := int(f.position / clusterBytes)
		within := f.position % clusterBytes
		sectorInCluster := uint32(within / block.SectorSize)
		off := int(within % block.SectorSize)
		if off == 0 && count-done >= block.SectorSize {
			wantSectors := min((count-done)/block.SectorSize, maxTransferSectors)
			lba, runSectors := f.volume.contiguousSectors(f.chain, clusterIdx, sectorInCluster, wantSectors)
			n := runSectors * block.SectorSize
			if err := f.volume.device.ReadSectors(lba, dst[done:done+n]); err != nil {
				return done, err
			}
			done += n
			f.position += int64(n)
			continue
		}
		lba := f.volume.clusterSector(f.chain[clusterIdx]) + uint64(sectorInCluster)
		if err := f.volume.device.ReadSectors(lba, scratch[:]); err != nil {
			return done, err
		}
		n := min(count-done, block.SectorSize-off)
		copy(dst[done:done+n], scratch[off:off+n])
		done += n
		f.position += int64(n)
	}
	return done, nil
}
func (f *file) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	if f.entry.attributes&16 == 0 {
		return nil, fs.ErrInvalid
	}
	f.volume.mu.Lock()
	defer f.volume.mu.Unlock()
	if f.volume.mutationErr != nil {
		return nil, f.volume.mutationErr
	}
	if f.position >= int64(len(f.directory)) {
		if n > 0 {
			return nil, io.EOF
		}
		return []fs.DirEntry{}, nil
	}
	end := len(f.directory)
	if n > 0 && n < end-int(f.position) {
		end = int(f.position) + n
	}
	result := make([]fs.DirEntry, 0, end-int(f.position))
	for _, e := range f.directory[f.position:int64(end)] {
		result = append(result, info{e})
	}
	f.position = int64(end)
	return result, nil
}

type info struct{ entry }

func (i info) Name() string { return i.name }
func (i info) Size() int64  { return int64(i.size) }
func (i info) Mode() fs.FileMode {
	if i.IsDir() {
		return fs.ModeDir | 0555
	}
	return 0444
}
func (i info) ModTime() time.Time         { return time.Time{} }
func (i info) IsDir() bool                { return i.attributes&16 != 0 }
func (i info) Sys() any                   { return nil }
func (i info) Type() fs.FileMode          { return i.Mode().Type() }
func (i info) Info() (fs.FileInfo, error) { return i, nil }
