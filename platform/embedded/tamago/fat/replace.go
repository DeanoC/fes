// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// ErrReplaced is returned when reading from an open file handle whose underlying
// file was replaced via ReplaceFile after the handle was opened.
var ErrReplaced = errors.New("FAT32 file replaced after open")

// ReplaceFile updates name using copy-on-write cluster allocation and a
// single-sector directory entry commit. If name does not exist, ReplaceFile
// delegates to CreateFile.
//
// For an existing regular file, data is written to newly allocated clusters,
// flushed, and linked in the FAT tables and flushed BEFORE the single directory
// sector pointing at the new chain/size is written and flushed; only after that
// commit are the old clusters reclaimed. A power loss before the directory
// commit leaves the previous file contents and directory entry intact (leaving
// at most unreferenced/dirty clusters to external repair), and never exposes
// torn half-written file contents. Cancellation or I/O failure after the first
// write retires the volume with ErrMutation.
func (v *Volume) ReplaceFile(ctx context.Context, name string, data []byte) (err error) {
	if ctx == nil || !fs.ValidPath(name) || name == "." {
		return fs.ErrInvalid
	}
	if len(data) > maxFileBytes {
		return ErrCapacity
	}
	if err = ctx.Err(); err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return v.mutationErr
	}
	if !v.allocationValid {
		return fmt.Errorf("%w: unsupported allocation metadata", ErrCorrupt)
	}
	e, err := v.lookupContext(ctx, name)
	if errors.Is(err, fs.ErrNotExist) {
		units, err := createName(path.Base(name))
		if err != nil {
			return err
		}
		return v.createLocked(ctx, name, units, data, false)
	}
	if err != nil {
		return &fs.PathError{Op: "replace", Path: name, Err: err}
	}
	if e.attributes&0x11 != 0 {
		return &fs.PathError{Op: "replace", Path: name, Err: fs.ErrPermission}
	}

	oldChain, err := v.chainContext(ctx, e.cluster, maxFileBytes)
	if err != nil {
		return err
	}
	clusterBytes := int(v.sectorsPerCluster) * block.SectorSize
	newClusters := (len(data) + clusterBytes - 1) / clusterBytes
	newChain, err := v.freeClusters(ctx, newClusters)
	if err != nil {
		return err
	}

	allocChanges := make([]fatChange, 0, len(newChain))
	for i, c := range newChain {
		next := uint32(0x0fffffff)
		if i+1 < len(newChain) {
			next = newChain[i+1]
		}
		allocChanges = append(allocChanges, fatChange{cluster: c, old: 0, next: next})
	}
	sort.Slice(allocChanges, func(i, j int) bool { return allocChanges[i].cluster < allocChanges[j].cluster })

	freeChanges := make([]fatChange, 0, len(oldChain))
	if len(oldChain) > 0 {
		lastCluster := oldChain[len(oldChain)-1]
		lastSector := make([]byte, block.SectorSize)
		lastOff := uint64(lastCluster) * 4
		if err = v.device.ReadSectors(v.fatStart+lastOff/block.SectorSize, lastSector); err != nil {
			return err
		}
		lastVal := binary.LittleEndian.Uint32(lastSector[lastOff%block.SectorSize:]) & 0x0fffffff
		if lastVal < 0x0ffffff8 {
			return ErrCorrupt
		}
		for i, c := range oldChain {
			cur := lastVal
			if i+1 < len(oldChain) {
				cur = oldChain[i+1]
			}
			freeChanges = append(freeChanges, fatChange{cluster: c, old: cur, next: 0})
		}
		sort.Slice(freeChanges, func(i, j int) bool { return freeChanges[i].cluster < freeChanges[j].cluster })
	}

	flags := make([]byte, block.SectorSize)
	if err = v.device.ReadSectors(v.fatStart, flags); err != nil {
		return err
	}
	oldFlags := binary.LittleEndian.Uint32(flags[4:]) & 0x0fffffff
	if oldFlags&0x03ffffff != 0x03ffffff {
		return fmt.Errorf("%w: reserved FAT entry", ErrCorrupt)
	}
	dirty := []fatChange{{1, oldFlags, oldFlags &^ 0x08000000}}

	for _, batch := range [][]fatChange{allocChanges, freeChanges, dirty} {
		if err = v.applyFAT(ctx, batch, nil); err != nil {
			return err
		}
	}

	var info []byte
	if (len(newChain) > 0 || len(oldChain) > 0) && v.fsInfo != 0 {
		info = make([]byte, block.SectorSize)
		if err = v.device.ReadSectors(v.fsInfo, info); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(info) != 0x41615252 || binary.LittleEndian.Uint32(info[484:]) != 0x61417272 || binary.LittleEndian.Uint32(info[508:]) != 0xaa550000 {
			return fmt.Errorf("%w: FSInfo signatures", ErrCorrupt)
		}
		binary.LittleEndian.PutUint32(info[488:], 0xffffffff)
		binary.LittleEndian.PutUint32(info[492:], 0xffffffff)
	}

	dirSector := make([]byte, block.SectorSize)
	if err = v.device.ReadSectors(e.sector, dirSector); err != nil {
		return err
	}
	if e.offset < 0 || e.offset+32 > block.SectorSize {
		return ErrCorrupt
	}
	d := dirSector[e.offset : e.offset+32]
	gotCluster := uint32(binary.LittleEndian.Uint16(d[20:]))<<16 | uint32(binary.LittleEndian.Uint16(d[26:]))
	gotSize := binary.LittleEndian.Uint32(d[28:])
	if gotCluster != e.cluster || gotSize != e.size {
		return fmt.Errorf("%w: directory entry changed before replace", ErrCorrupt)
	}

	started := false
	defer func() {
		if err != nil && started {
			v.mutationErr = errors.Join(ErrMutation, err)
			err = v.mutationErr
		}
	}()
	write := func(lba uint64, b []byte) error {
		if ce := ctx.Err(); ce != nil {
			return ce
		}
		we := v.device.WriteSectors(lba, b)
		if !errors.Is(we, block.ErrReadOnly) || started {
			started = true
		}
		if we != nil {
			return we
		}
		runtime.Gosched()
		return ctx.Err()
	}
	flush := func() error {
		if ce := ctx.Err(); ce != nil {
			return ce
		}
		if fe := v.device.Flush(); fe != nil {
			return fe
		}
		return ctx.Err()
	}

	// 1. Mark volume dirty before mutating allocation metadata or clusters.
	if err = v.applyFAT(ctx, dirty, write); err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}
	if info != nil {
		if err = write(v.fsInfo, info); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 2. Write and flush new data blocks into fresh clusters (old chain untouched).
	if len(newChain) > 0 {
		if err = v.writeChain(newChain, data, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 3. Commit new cluster chain in all FAT tables before updating directory entry.
	if len(allocChanges) > 0 {
		if err = v.applyFAT(ctx, allocChanges, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 4. Single-sector directory entry commit (cluster, size, timestamps).
	var first uint32
	if len(newChain) > 0 {
		first = newChain[0]
	}
	binary.LittleEndian.PutUint16(d[20:], uint16(first>>16))
	binary.LittleEndian.PutUint16(d[26:], uint16(first))
	binary.LittleEndian.PutUint32(d[28:], uint32(len(data)))
	if now := v.currentTimeLocked(); !now.IsZero() {
		date, clockTime, _ := encodeFATTimestamp(now)
		binary.LittleEndian.PutUint16(d[18:], date)
		binary.LittleEndian.PutUint16(d[22:], clockTime)
		binary.LittleEndian.PutUint16(d[24:], date)
	}
	if err = write(e.sector, dirSector); err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}

	// Invalidate any open file handles pointing at the superseded cluster chain
	// before freeing those clusters for reuse.
	if v.replacedGen == nil {
		v.replacedGen = make(map[entryLocation]uint64)
	}
	v.replacedGen[entryLocation{sector: e.sector, offset: e.offset}]++

	// 5. Reclaim previous clusters only after the directory commit is durable.
	if len(freeChanges) > 0 {
		if err = v.applyFAT(ctx, freeChanges, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 6. Restore clean volume bit if volume started clean.
	if oldFlags&0x08000000 != 0 {
		clean := []fatChange{{1, oldFlags &^ 0x08000000, oldFlags}}
		if err = v.applyFAT(ctx, clean, write); err != nil {
			return err
		}
		return flush()
	}
	return nil
}
