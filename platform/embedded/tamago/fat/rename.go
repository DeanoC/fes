// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// Rename renames or moves an existing regular file or directory within the
// volume without copying file clusters or directory contents. The destination
// directory entry is committed and flushed before the source directory entry
// is marked deleted (0xE5), guaranteeing no crash point drops the target's
// data or directory tree. Moving a directory to a different parent updates its
// ".." cluster link before unlinking the old entry, and open handles bound to
// the source entry are invalidated with ErrReplaced.
func (v *Volume) Rename(ctx context.Context, oldName, newName string) (err error) {
	if ctx == nil || !fs.ValidPath(oldName) || !fs.ValidPath(newName) || oldName == "." || newName == "." {
		return fs.ErrInvalid
	}
	units, err := createName(path.Base(newName))
	if err != nil {
		return err
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

	src, err := v.lookupContext(ctx, oldName)
	if err != nil {
		return &fs.PathError{Op: "rename", Path: oldName, Err: err}
	}
	if src.attributes&0x01 != 0 {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrPermission}
	}
	if oldName == newName {
		return nil
	}
	isDir := src.attributes&0x10 != 0

	srcParent, err := v.lookupContext(ctx, path.Dir(oldName))
	if err != nil {
		return &fs.PathError{Op: "rename", Path: oldName, Err: err}
	}
	if srcParent.attributes&0x11 != 0x10 {
		return &fs.PathError{Op: "rename", Path: oldName, Err: fs.ErrPermission}
	}
	dstDirPath := path.Dir(newName)
	dstParent, err := v.lookupContext(ctx, dstDirPath)
	if err != nil {
		return &fs.PathError{Op: "rename", Path: newName, Err: err}
	}
	if dstParent.attributes&0x11 != 0x10 {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrPermission}
	}
	if srcParent.cluster == dstParent.cluster && path.Base(oldName) == path.Base(newName) {
		return nil
	}
	if isDir {
		if dstParent.cluster == src.cluster {
			return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrInvalid}
		}
		if dstDirPath != "." {
			prefix := ""
			for _, part := range strings.Split(dstDirPath, "/") {
				if prefix == "" {
					prefix = part
				} else {
					prefix += "/" + part
				}
				anc, err := v.lookupContext(ctx, prefix)
				if err != nil {
					return &fs.PathError{Op: "rename", Path: newName, Err: err}
				}
				if anc.cluster == src.cluster {
					return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrInvalid}
				}
			}
		}
	}

	p, err := v.appendDirectory(ctx, dstParent.cluster)
	if err != nil {
		return err
	}
	if p.hasName(path.Base(newName)) {
		return &fs.PathError{Op: "rename", Path: newName, Err: fs.ErrExist}
	}
	alias, err := shortAlias(ctx, path.Base(newName), p)
	if err != nil {
		return err
	}

	clusterBytes := int(v.sectorsPerCluster) * block.SectorSize
	recordBytes := ((len(units)+12)/13 + 1) * 32
	end := p.offset + recordBytes + 32 // preserve a zero end marker after publication
	if end > maxDirectoryBytes {
		return ErrNoSpace
	}
	extra := (max(0, end-len(p.data)) + clusterBytes - 1) / clusterBytes
	var dirExtra []uint32
	var newChanges, linkChanges []fatChange
	if extra > 0 {
		dirExtra, err = v.freeClusters(ctx, extra)
		if err != nil {
			return err
		}
		for i, c := range dirExtra {
			next := uint32(0x0fffffff)
			if i+1 < len(dirExtra) {
				next = dirExtra[i+1]
			}
			newChanges = append(newChanges, fatChange{cluster: c, next: next})
		}
		sort.Slice(newChanges, func(i, j int) bool { return newChanges[i].cluster < newChanges[j].cluster })
		if err = v.applyFAT(ctx, newChanges, nil); err != nil {
			return err
		}
		lastCluster := p.chain[len(p.chain)-1]
		b := make([]byte, block.SectorSize)
		off := uint64(lastCluster) * 4
		if err = v.device.ReadSectors(v.fatStart+off/block.SectorSize, b); err != nil {
			return err
		}
		old := binary.LittleEndian.Uint32(b[off%block.SectorSize:]) & 0x0fffffff
		if old < 0x0ffffff8 {
			return ErrCorrupt
		}
		linkChanges = []fatChange{{lastCluster, old, dirExtra[0]}}
		if err = v.applyFAT(ctx, linkChanges, nil); err != nil {
			return err
		}
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
	if err = v.applyFAT(ctx, dirty, nil); err != nil {
		return err
	}

	var info []byte
	if len(dirExtra) > 0 && v.fsInfo != 0 {
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

	// Validate source sectors pre-mutation and copy original attributes and timestamps.
	srcShortPre, _, _, err := v.prepareDeleteSectors(src)
	if err != nil {
		return err
	}
	srcRawShort := append([]byte(nil), srcShortPre[src.offset:src.offset+32]...)

	var dotDotSector uint64
	var dotDotBuf []byte
	if isDir && srcParent.cluster != dstParent.cluster {
		dirChain, err := v.chainContext(ctx, src.cluster, maxDirectoryBytes)
		if err != nil {
			return err
		}
		if len(dirChain) == 0 {
			return fmt.Errorf("%w: empty directory chain", ErrCorrupt)
		}
		dotDotSector = v.clusterSector(dirChain[0])
		dotDotBuf = make([]byte, block.SectorSize)
		if err = v.device.ReadSectors(dotDotSector, dotDotBuf); err != nil {
			return err
		}
		dd := dotDotBuf[32:64]
		if !bytes.Equal(dd[:11], []byte("..         ")) || dd[11]&0x10 == 0 {
			return fmt.Errorf("%w: missing '..' entry", ErrCorrupt)
		}
		nextParentCluster := dstParent.cluster
		if nextParentCluster == v.root {
			nextParentCluster = 0
		}
		binary.LittleEndian.PutUint16(dd[20:], uint16(nextParentCluster>>16))
		binary.LittleEndian.PutUint16(dd[26:], uint16(nextParentCluster))
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

	// 1. Mark volume dirty and clear FSInfo if clusters are being allocated.
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

	// 2. Grow destination directory chain on disk: initialize data and commit the
	// new FAT chain entries FIRST before linking the old tail cluster to dirExtra[0].
	if len(dirExtra) > 0 {
		if err = v.writeChain(dirExtra, nil, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
		if err = v.applyFAT(ctx, newChanges, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
		if err = v.applyFAT(ctx, linkChanges, write); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 3. Build and commit the destination LFN + 8.3 entry preserving cluster,
	// size, attributes, and original timestamps.
	recs := directoryRecords(units, alias, src.cluster, src.size, isDir, v.currentTimeLocked())
	dstShort := recs[len(recs)-32:]
	dstShort[11] = srcRawShort[11]
	copy(dstShort[13:20], srcRawShort[13:20])
	copy(dstShort[22:26], srcRawShort[22:26])

	p.chain = append(p.chain, dirExtra...)
	p.data = append(p.data, make([]byte, len(dirExtra)*clusterBytes)...)
	copy(p.data[p.offset:], recs)
	clear(p.data[p.offset+recordBytes : p.offset+recordBytes+32])
	spc := int(v.sectorsPerCluster)
	startSector := p.offset / block.SectorSize
	endSector := (end + block.SectorSize - 1) / block.SectorSize
	for sectorPos := startSector; sectorPos < endSector; {
		clusterIdx := sectorPos / spc
		sectorInCluster := uint32(sectorPos % spc)
		want := min(endSector-sectorPos, maxTransferSectors)
		lba, runSectors := v.contiguousSectors(p.chain, clusterIdx, sectorInCluster, want)
		startByte := sectorPos * block.SectorSize
		endByte := (sectorPos + runSectors) * block.SectorSize
		if err = write(lba, p.data[startByte:endByte]); err != nil {
			return err
		}
		sectorPos += runSectors
	}
	if err = flush(); err != nil {
		return err
	}

	// 4. Update moved directory's ".." entry before unlinking the old parent slot.
	if dotDotBuf != nil {
		if err = write(dotDotSector, dotDotBuf); err != nil {
			return err
		}
		if err = flush(); err != nil {
			return err
		}
	}

	// 5. Re-read source sectors from disk (preserving step 3's write when src and dst
	// share a sector), mark short entry 0xE5 FIRST, then wipe remaining LFN sectors.
	shortBuf, extraSectors, sectorBufs, err := v.prepareDeleteSectors(src)
	if err != nil {
		return err
	}
	if err = write(src.sector, shortBuf); err != nil {
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

	// Invalidate open handles referencing the old directory entry.
	if v.replacedGen == nil {
		v.replacedGen = make(map[entryLocation]uint64)
	}
	v.replacedGen[entryLocation{sector: src.sector, offset: src.offset}]++

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
