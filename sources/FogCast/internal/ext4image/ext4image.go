// Package ext4image reads one small regular file from an unmounted ext4 image.
// It handles the layout FES images use: extent-mapped files and directories,
// 32- or 64-bit block group descriptors with flex_bg, and linear or hashed
// directories, which it scans linearly. It neither verifies checksums nor
// replays a journal, so callers authenticate the image bytes first.
package ext4image

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"
)

const (
	superblockOffset = 1024
	superMagic       = 0xEF53
	rootInode        = 2
	// Incompatible features that do not change where data lives: directory
	// entry file types, extents, 64-bit descriptors, flex_bg and the checksum
	// seed. Any other incompatible feature is refused.
	knownIncompat    = 0x2 | 0x40 | 0x80 | 0x200 | 0x2000
	requiredIncompat = 0x2 | 0x40
	incompat64Bit    = 0x80
	extentsFlag      = 0x80000
	inlineDataFlag   = 0x10000000
	extentMagic      = 0xF30A
	maxExtentDepth   = 5
	maxExtentNodes   = 4096
	maxDirectorySize = 16 << 20
	typeMask         = 0xF000
	typeDirectory    = 0x4000
	typeRegular      = 0x8000
)

var le = binary.LittleEndian

type image struct {
	r              io.ReaderAt
	blockSize      uint64
	inodeSize      uint64
	inodesPerGroup uint64
	inodes         uint64
	descriptors    uint64
	descriptorSize uint64
}

type inode struct {
	mode  uint16
	size  uint64
	flags uint32
	block []byte
}

type extent struct {
	logical, length uint64
	physical        uint64
	unwritten       bool
}

// ReadFile returns the regular file at the absolute path name. It refuses a
// file larger than limit bytes and does not follow symbolic links.
func ReadFile(r io.ReaderAt, name string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(name, "/") {
		return nil, fmt.Errorf("ext4 path %q is not absolute", name)
	}
	img, err := open(r)
	if err != nil {
		return nil, err
	}
	number := uint64(rootInode)
	for _, part := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("invalid ext4 path %q", name)
		}
		dir, err := img.inode(number)
		if err != nil {
			return nil, err
		}
		if dir.mode&typeMask != typeDirectory {
			return nil, fmt.Errorf("ext4 path %q: parent of %q is not a directory", name, part)
		}
		entries, err := img.content(dir, maxDirectorySize)
		if err != nil {
			return nil, err
		}
		if number, err = img.lookup(entries, part); err != nil {
			return nil, fmt.Errorf("ext4 path %q: %w", name, err)
		}
	}
	file, err := img.inode(number)
	if err != nil {
		return nil, err
	}
	if file.mode&typeMask != typeRegular {
		return nil, fmt.Errorf("ext4 path %q is not a regular file", name)
	}
	return img.content(file, limit)
}

func open(r io.ReaderAt) (*image, error) {
	sb := make([]byte, 1024)
	if _, err := r.ReadAt(sb, superblockOffset); err != nil {
		return nil, fmt.Errorf("read ext4 superblock: %w", err)
	}
	if le.Uint16(sb[56:]) != superMagic {
		return nil, errors.New("image is not ext4")
	}
	incompat := le.Uint32(sb[96:])
	if incompat&^knownIncompat != 0 || incompat&requiredIncompat != requiredIncompat {
		return nil, fmt.Errorf("unsupported ext4 incompatible features %#x", incompat)
	}
	logBlock := le.Uint32(sb[24:])
	if logBlock > 2 {
		return nil, errors.New("unsupported ext4 block size")
	}
	img := &image{r: r, blockSize: 1024 << logBlock, inodes: uint64(le.Uint32(sb[0:])),
		inodesPerGroup: uint64(le.Uint32(sb[40:])), inodeSize: 128, descriptorSize: 32}
	if le.Uint32(sb[76:]) >= 1 {
		img.inodeSize = uint64(le.Uint16(sb[88:]))
	}
	if incompat&incompat64Bit != 0 {
		img.descriptorSize = uint64(le.Uint16(sb[254:]))
	}
	if img.inodesPerGroup == 0 || img.inodeSize < 128 || img.inodeSize > img.blockSize ||
		img.descriptorSize < 32 || img.descriptorSize > img.blockSize {
		return nil, errors.New("invalid ext4 geometry")
	}
	img.descriptors = (uint64(le.Uint32(sb[20:])) + 1) * img.blockSize
	return img, nil
}

// at returns the byte offset of a block, or an error past any readable image.
func (img *image) at(block uint64) (int64, error) {
	if block >= math.MaxInt64/img.blockSize {
		return 0, errors.New("ext4 block outside the image")
	}
	return int64(block * img.blockSize), nil
}

func (img *image) inode(number uint64) (inode, error) {
	if number == 0 || number > img.inodes {
		return inode{}, fmt.Errorf("invalid ext4 inode %d", number)
	}
	group, index := (number-1)/img.inodesPerGroup, (number-1)%img.inodesPerGroup
	descriptor := make([]byte, img.descriptorSize)
	if _, err := img.r.ReadAt(descriptor, int64(img.descriptors+group*img.descriptorSize)); err != nil {
		return inode{}, fmt.Errorf("read ext4 group descriptor: %w", err)
	}
	table := uint64(le.Uint32(descriptor[8:]))
	if img.descriptorSize >= 64 {
		table |= uint64(le.Uint32(descriptor[0x28:])) << 32
	}
	offset, err := img.at(table)
	if err != nil {
		return inode{}, err
	}
	raw := make([]byte, 128)
	if _, err := img.r.ReadAt(raw, offset+int64(index*img.inodeSize)); err != nil {
		return inode{}, fmt.Errorf("read ext4 inode %d: %w", number, err)
	}
	in := inode{mode: le.Uint16(raw[0:]), size: uint64(le.Uint32(raw[4:])), flags: le.Uint32(raw[0x20:]), block: raw[0x28 : 0x28+60]}
	// Without largedir, which open refuses, only regular files use the high
	// size word; older directories may hold another value there.
	if in.mode&typeMask == typeRegular {
		in.size |= uint64(le.Uint32(raw[0x6C:])) << 32
	}
	return in, nil
}

// content reads an extent-mapped inode. Holes and unwritten extents read as zeros.
func (img *image) content(in inode, limit int64) ([]byte, error) {
	if in.flags&inlineDataFlag != 0 || in.flags&extentsFlag == 0 {
		return nil, errors.New("unsupported ext4 inode layout")
	}
	if limit < 0 || in.size > uint64(limit) {
		return nil, fmt.Errorf("ext4 file exceeds %d bytes", limit)
	}
	var extents []extent
	nodes := 0
	if err := img.walk(in.block, -1, &nodes, &extents); err != nil {
		return nil, err
	}
	data := make([]byte, in.size)
	for _, e := range extents {
		if e.unwritten {
			continue
		}
		for i := uint64(0); i < e.length; i++ {
			start := (e.logical + i) * img.blockSize
			if start >= in.size {
				break
			}
			offset, err := img.at(e.physical + i)
			if err != nil {
				return nil, err
			}
			if _, err := img.r.ReadAt(data[start:min(start+img.blockSize, in.size)], offset); err != nil {
				return nil, fmt.Errorf("read ext4 data: %w", err)
			}
		}
	}
	return data, nil
}

// walk collects the leaf extents below node. want is the node's depth, or -1
// for the root held in the inode.
func (img *image) walk(node []byte, want int, nodes *int, out *[]extent) error {
	if len(node) < 12 || le.Uint16(node[0:]) != extentMagic {
		return errors.New("invalid ext4 extent header")
	}
	entries, depth := int(le.Uint16(node[2:])), int(le.Uint16(node[6:]))
	if depth > maxExtentDepth || (want >= 0 && depth != want) || 12+entries*12 > len(node) {
		return errors.New("invalid ext4 extent tree")
	}
	for i := 0; i < entries; i++ {
		entry := node[12+i*12:]
		if depth == 0 {
			length, unwritten := uint64(le.Uint16(entry[4:])), false
			if length > 32768 {
				length, unwritten = length-32768, true
			}
			*out = append(*out, extent{logical: uint64(le.Uint32(entry[0:])), length: length,
				physical: uint64(le.Uint16(entry[6:]))<<32 | uint64(le.Uint32(entry[8:])), unwritten: unwritten})
			continue
		}
		if *nodes++; *nodes > maxExtentNodes {
			return errors.New("ext4 extent tree too large")
		}
		offset, err := img.at(uint64(le.Uint16(entry[8:]))<<32 | uint64(le.Uint32(entry[4:])))
		if err != nil {
			return err
		}
		child := make([]byte, img.blockSize)
		if _, err := img.r.ReadAt(child, offset); err != nil {
			return fmt.Errorf("read ext4 extent node: %w", err)
		}
		if err := img.walk(child, depth-1, nodes, out); err != nil {
			return err
		}
	}
	return nil
}

// lookup scans every directory entry block by block. Hashed directories keep
// their index inside entries with inode zero, which the scan skips.
func (img *image) lookup(dir []byte, name string) (uint64, error) {
	size := int(img.blockSize)
	for block := 0; block < len(dir); block += size {
		end := min(block+size, len(dir))
		for offset := block; offset+8 <= end; {
			number := le.Uint32(dir[offset:])
			length, nameLength := int(le.Uint16(dir[offset+4:])), int(dir[offset+6])
			if length < 8 || length%4 != 0 || offset+length > end || 8+nameLength > length {
				return 0, errors.New("invalid ext4 directory entry")
			}
			if number != 0 && string(dir[offset+8:offset+8+nameLength]) == name {
				return uint64(number), nil
			}
			offset += length
		}
	}
	return 0, fmt.Errorf("%q: %w", name, fs.ErrNotExist)
}
