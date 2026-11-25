// SPDX-License-Identifier: GPL-3.0-or-later
package fat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// FSStats reports FAT32 volume geometry, free/used cluster accounting, dirty
// bit state, FSInfo hint metadata, and volume label.
type FSStats struct {
	Label             string
	ClusterBytes      uint32
	SectorsPerCluster uint32
	TotalClusters     uint64
	FreeClusters      uint64
	UsedClusters      uint64
	BadClusters       uint64
	TotalBytes        uint64
	FreeBytes         uint64
	UsedBytes         uint64
	Dirty             bool
	FSInfoValid       bool
	FSInfoFreeHint    uint32
	FSInfoNextHint    uint32
}

// StatFS scans the active FAT and root directory to compute exact volume usage,
// dirty-volume state, FSInfo hints, and volume label without mutating media.
func (v *Volume) StatFS(ctx context.Context) (FSStats, error) {
	if ctx == nil {
		return FSStats{}, fs.ErrInvalid
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return FSStats{}, v.mutationErr
	}
	if err := ctx.Err(); err != nil {
		return FSStats{}, err
	}

	const fatScanSectors = 16
	totalFATSectors := (v.clusterCount + 2 + 127) / 128
	buf := make([]byte, min(totalFATSectors, fatScanSectors)*block.SectorSize)

	var freeClusters, usedClusters, badClusters uint64
	var dirty bool
	for sector := uint64(0); sector < totalFATSectors; {
		if err := ctx.Err(); err != nil {
			return FSStats{}, err
		}
		run := min(totalFATSectors-sector, fatScanSectors)
		chunk := buf[:run*block.SectorSize]
		if err := v.device.ReadSectors(v.fatStart+sector, chunk); err != nil {
			return FSStats{}, err
		}
		if sector == 0 {
			fat1 := binary.LittleEndian.Uint32(chunk[4:]) & 0x0fffffff
			dirty = fat1&0x08000000 == 0
		}
		entries := run * 128
		for i := uint64(0); i < entries; i++ {
			c := sector*128 + i
			if c < 2 || c >= v.clusterCount+2 {
				continue
			}
			val := binary.LittleEndian.Uint32(chunk[i*4:]) & 0x0fffffff
			switch val {
			case 0:
				freeClusters++
			case 0x0ffffff7:
				badClusters++
			default:
				usedClusters++
			}
		}
		sector += run
		runtime.Gosched()
	}

	fsInfoFreeHint := uint32(0xffffffff)
	fsInfoNextHint := uint32(0xffffffff)
	fsInfoValid := false
	if v.fsInfo != 0 {
		if err := ctx.Err(); err != nil {
			return FSStats{}, err
		}
		info := make([]byte, block.SectorSize)
		if err := v.device.ReadSectors(v.fsInfo, info); err != nil {
			return FSStats{}, err
		}
		if binary.LittleEndian.Uint32(info) == 0x41615252 &&
			binary.LittleEndian.Uint32(info[484:]) == 0x61417272 &&
			binary.LittleEndian.Uint32(info[508:]) == 0xaa550000 {
			fsInfoValid = true
			fsInfoFreeHint = binary.LittleEndian.Uint32(info[488:])
			fsInfoNextHint = binary.LittleEndian.Uint32(info[492:])
		}
	}

	label, err := v.rootVolumeLabelLocked(ctx)
	if err != nil {
		return FSStats{}, err
	}
	if label == "" {
		label = v.bpbLabel
	}

	clusterBytes := v.sectorsPerCluster * block.SectorSize
	return FSStats{
		Label:             label,
		ClusterBytes:      clusterBytes,
		SectorsPerCluster: v.sectorsPerCluster,
		TotalClusters:     v.clusterCount,
		FreeClusters:      freeClusters,
		UsedClusters:      usedClusters,
		BadClusters:       badClusters,
		TotalBytes:        v.clusterCount * uint64(clusterBytes),
		FreeBytes:         freeClusters * uint64(clusterBytes),
		UsedBytes:         usedClusters * uint64(clusterBytes),
		Dirty:             dirty,
		FSInfoValid:       fsInfoValid,
		FSInfoFreeHint:    fsInfoFreeHint,
		FSInfoNextHint:    fsInfoNextHint,
	}, nil
}

func (v *Volume) rootVolumeLabelLocked(ctx context.Context) (string, error) {
	chain, err := v.chainContext(ctx, v.root, maxDirectoryBytes)
	if err != nil {
		return "", err
	}
	spc := int(v.sectorsPerCluster)
	chunkCap := min(spc, maxTransferSectors)
	buf := make([]byte, chunkCap*block.SectorSize)
	for _, c := range chain {
		for s := 0; s < spc; {
			run := min(spc-s, chunkCap)
			runtime.Gosched()
			if err := ctx.Err(); err != nil {
				return "", err
			}
			lba := v.clusterSector(c) + uint64(s)
			if err := v.device.ReadSectors(lba, buf[:run*block.SectorSize]); err != nil {
				return "", err
			}
			for r := 0; r < run; r++ {
				b := buf[r*block.SectorSize : (r+1)*block.SectorSize]
				for off := 0; off < block.SectorSize; off += 32 {
					d := b[off : off+32]
					if d[0] == 0 {
						return "", nil
					}
					if d[0] == 0xe5 || d[11] == 0x0f {
						continue
					}
					if d[11]&0x18 == 0x08 {
						var raw [11]byte
						copy(raw[:], d[:11])
						if raw[0] == 5 {
							raw[0] = 0xe5
						}
						return strings.TrimRight(string(raw[:]), " \x00"), nil
					}
				}
			}
			s += run
		}
	}
	return "", nil
}

// MkdirAll creates name along with any missing parent directories. If name
// already exists as a directory (including "."), MkdirAll returns nil without
// modifying media. If any path component exists as a non-directory, MkdirAll
// returns fs.ErrExist.
func (v *Volume) MkdirAll(ctx context.Context, name string) error {
	if ctx == nil || !fs.ValidPath(name) {
		return fs.ErrInvalid
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return v.mutationErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if name == "." {
		return nil
	}

	parts := strings.Split(name, "/")
	firstMissing := len(parts)
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		e, err := v.lookupContext(ctx, prefix)
		if err == nil {
			if e.attributes&0x10 == 0 {
				return fmt.Errorf("fat: %q exists and is not a directory: %w", prefix, fs.ErrExist)
			}
			continue
		}
		if errors.Is(err, fs.ErrNotExist) {
			firstMissing = i
			break
		}
		return err
	}
	if firstMissing == len(parts) {
		return nil
	}

	missingCount := len(parts) - firstMissing
	allUnits := make([][]uint16, missingCount)
	for i := 0; i < missingCount; i++ {
		units, err := createName(parts[firstMissing+i])
		if err != nil {
			return err
		}
		allUnits[i] = units
	}
	if !v.allocationValid {
		return fmt.Errorf("%w: unsupported allocation metadata", ErrCorrupt)
	}
	if _, err := v.freeClusters(ctx, missingCount); err != nil {
		return err
	}

	for i := 0; i < missingCount; i++ {
		prefix := strings.Join(parts[:firstMissing+i+1], "/")
		if err := v.createLocked(ctx, prefix, allUnits[i], nil, true); err != nil {
			return err
		}
	}
	return nil
}
