// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sort"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// RemoveFile removes an existing regular file in crash-safe order: the short
// 8.3 directory entry's sector is marked deleted (0xE5) and flushed FIRST before
// any preceding LFN-only sectors are marked deleted, open file handles are
// invalidated with ErrReplaced, and only then are the file's clusters reclaimed
// across the mirrored FAT tables. A power cut before the short-entry commit
// leaves the file intact; a power cut afterward leaves the file absent without
// exposing freed clusters via a live directory entry. Directories and read-only
// files return fs.ErrPermission; missing targets return fs.ErrNotExist.
func (v *Volume) RemoveFile(ctx context.Context, name string) error {
	return v.removeEntry(ctx, "remove", name, false)
}

func (v *Volume) removeEntry(ctx context.Context, op, name string, wantDir bool) (err error) {
	if ctx == nil || !fs.ValidPath(name) || name == "." {
		return fs.ErrInvalid
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
	if err != nil {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	isDir := e.attributes&0x10 != 0
	if wantDir {
		if !isDir {
			return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
		}
		if e.attributes&0x01 != 0 {
			return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
		}
		items, err := v.entriesContext(ctx, e.cluster)
		if err != nil {
			return &fs.PathError{Op: op, Path: name, Err: err}
		}
		if len(items) > 0 {
			return &fs.PathError{Op: op, Path: name, Err: ErrNotEmpty}
		}
	} else if e.attributes&0x11 != 0 {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}

	chainLimit := maxFileBytes
	if isDir {
		chainLimit = maxDirectoryBytes
	}
	oldChain, err := v.chainContext(ctx, e.cluster, chainLimit)
	if err != nil {
		return err
	}
	if isDir && len(oldChain) == 0 {
		return fmt.Errorf("%w: empty directory chain", ErrCorrupt)
	}

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
	if len(freeChanges) > 0 {
		if err = v.applyFAT(ctx, freeChanges, nil); err != nil {
			return err
		}
	}
	if err = v.applyFAT(ctx, dirty, nil); err != nil {
		return err
	}

	var info []byte
	if len(oldChain) > 0 && v.fsInfo != 0 {
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

	shortBuf, extraSectors, sectorBufs, err := v.prepareDeleteSectors(e)
	if err != nil {
		return err
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

	// 1. Mark volume dirty and clear stale FSInfo hints before unlinking.
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

	// 2. Commit the deleted 8.3 short entry FIRST (and any co-located LFN slots)
	// before wiping preceding LFN-only sectors or reclaiming clusters.
	if err = write(e.sector, shortBuf); err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}
	for _, sec := range extraSectors {
		if err = write(sec, sectorBufs[sec]); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// Invalidate open file handles referencing this directory entry before freeing clusters.
	if v.replacedGen == nil {
		v.replacedGen = make(map[entryLocation]uint64)
	}
	v.replacedGen[entryLocation{sector: e.sector, offset: e.offset}]++

	// 3. Reclaim clusters across all mirrored FAT tables.
	if len(freeChanges) > 0 {
		if err = v.applyFAT(ctx, freeChanges, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 4. Restore clean volume bit if volume started clean.
	if oldFlags&0x08000000 != 0 {
		clean := []fatChange{{1, oldFlags &^ 0x08000000, oldFlags}}
		if err = v.applyFAT(ctx, clean, write); err != nil {
			return err
		}
		return flush()
	}
	return nil
}

// prepareDeleteSectors validates e's short and LFN slots on disk and stages
// 0xE5 markers across their sectors without writing to the device.
func (v *Volume) prepareDeleteSectors(e entry) (shortBuf []byte, extraSectors []uint64, sectorBufs map[uint64][]byte, err error) {
	if e.offset < 0 || e.offset+32 > block.SectorSize {
		return nil, nil, nil, ErrCorrupt
	}
	sectorBufs = make(map[uint64][]byte)
	readDirSector := func(sec uint64) ([]byte, error) {
		if buf, ok := sectorBufs[sec]; ok {
			return buf, nil
		}
		buf := make([]byte, block.SectorSize)
		if err := v.device.ReadSectors(sec, buf); err != nil {
			return nil, err
		}
		sectorBufs[sec] = buf
		return buf, nil
	}

	shortBuf, err = readDirSector(e.sector)
	if err != nil {
		return nil, nil, nil, err
	}
	sd := shortBuf[e.offset : e.offset+32]
	gotCluster := uint32(binary.LittleEndian.Uint16(sd[20:]))<<16 | uint32(binary.LittleEndian.Uint16(sd[26:]))
	gotSize := binary.LittleEndian.Uint32(sd[28:])
	if sd[0] == 0 || sd[0] == 0xe5 || sd[11] != e.attributes || gotCluster != e.cluster || gotSize != e.size {
		return nil, nil, nil, fmt.Errorf("%w: directory entry changed before remove", ErrCorrupt)
	}
	sd[0] = 0xe5

	extraSectors = make([]uint64, 0, 2)
	for _, loc := range e.lfnSlots {
		if loc.offset < 0 || loc.offset+32 > block.SectorSize {
			return nil, nil, nil, ErrCorrupt
		}
		if loc.sector != e.sector {
			if _, seen := sectorBufs[loc.sector]; !seen {
				extraSectors = append(extraSectors, loc.sector)
			}
		}
		lfnBuf, err := readDirSector(loc.sector)
		if err != nil {
			return nil, nil, nil, err
		}
		ld := lfnBuf[loc.offset : loc.offset+32]
		if ld[0] == 0 || ld[0] == 0xe5 || ld[11] != 0x0f || ld[13] != e.shortSum {
			return nil, nil, nil, fmt.Errorf("%w: LFN entry changed before remove", ErrCorrupt)
		}
		ld[0] = 0xe5
	}
	sort.Slice(extraSectors, func(i, j int) bool { return extraSectors[i] > extraSectors[j] })
	return shortBuf, extraSectors, sectorBufs, nil
}
