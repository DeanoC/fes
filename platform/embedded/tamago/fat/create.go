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
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

var ErrNoSpace = errors.New("FAT32 has insufficient free space")
var ErrMutation = errors.New("FAT32 allocation outcome uncertain; mount retired")

// CreateFile creates a new file in an existing directory, never overwriting an
// existing long name or short alias. Data may not be mutated during the call.
// Success includes data, FAT and directory flushes. Cancellation or an IO error
// after mutation retires this mount without retry, rollback or cluster reuse.
// Power loss can leave orphan allocation or partial metadata; this is not an
// atomic replacement operation. Storage ownership remains the caller's duty.
func (v *Volume) CreateFile(ctx context.Context, name string, data []byte) error {
	return v.create(ctx, name, data, false)
}

// Mkdir creates one directory, including its dot entries. The parent must exist.
func (v *Volume) Mkdir(ctx context.Context, name string) error {
	return v.create(ctx, name, nil, true)
}

type fatChange struct{ cluster, old, next uint32 }
type directoryAppend struct {
	chain   []uint32
	data    []byte
	offset  int
	aliases map[string]bool
	names   map[string]bool
}

func createName(name string) ([]uint16, error) {
	if !utf8.ValidString(name) || name == "" || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return nil, fs.ErrInvalid
	}
	for _, r := range name {
		if r < 32 || r == 127 || r == 0xffff || strings.ContainsRune(`\/:*?"<>|`, r) {
			return nil, fs.ErrInvalid
		}
	}
	units := utf16.Encode([]rune(name))
	if len(units) == 0 || len(units) > 255 {
		return nil, fs.ErrInvalid
	}
	return units, nil
}

func (v *Volume) appendDirectory(ctx context.Context, cluster uint32) (*directoryAppend, error) {
	chain, err := v.chainContext(ctx, cluster, maxDirectoryBytes)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, ErrCorrupt
	}
	p := &directoryAppend{chain: chain, offset: -1, aliases: map[string]bool{}, names: map[string]bool{}}
	items, err := v.entriesContext(ctx, cluster)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		p.names[item.name] = true
	}
	b := make([]byte, 512)
	for _, c := range chain {
		for s := uint32(0); s < v.sectorsPerCluster; s++ {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if err = v.device.ReadSectors(v.clusterSector(c)+uint64(s), b); err != nil {
				return nil, err
			}
			for off := 0; off < 512 && p.offset < 0; off += 32 {
				if b[off] == 0 {
					p.offset = len(p.data) + off
					break
				}
				if b[off] != 0xe5 && b[off+11] != 0x0f {
					p.aliases[string(b[off:off+11])] = true
					p.names[shortName(b[off:off+32])] = true
				}
			}
			p.data = append(p.data, b...)
			runtime.Gosched()
		}
	}
	if p.offset < 0 {
		p.offset = len(p.data)
	}
	return p, nil
}

func shortPart(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(value) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'()-@^_`{}~", r) {
			b.WriteRune(r)
		} else if r != ' ' && r != '.' {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (p *directoryAppend) hasName(name string) bool {
	for existing := range p.names {
		if strings.EqualFold(name, existing) {
			return true
		}
	}
	return false
}

func shortAlias(ctx context.Context, name string, p *directoryAppend) ([11]byte, error) {
	var alias [11]byte
	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		base, ext = name[:i], name[i+1:]
	}
	b, e := shortPart(base), shortPart(ext)
	available := func(base, ext string) bool {
		for i := range alias {
			alias[i] = ' '
		}
		copy(alias[:8], base)
		copy(alias[8:], ext)
		var raw [32]byte
		copy(raw[:], alias[:])
		return !p.aliases[string(alias[:])] && !p.hasName(shortName(raw[:]))
	}
	if b != "" && len(b) <= 8 && len(e) <= 3 && strings.EqualFold(base, b) && strings.EqualFold(ext, e) && available(b, e) {
		return alias, nil
	}
	if b == "" {
		b = "FILE"
	}
	if len(e) > 3 {
		e = e[:3]
	}
	for n := 1; n <= 999999; n++ {
		runtime.Gosched()
		if err := ctx.Err(); err != nil {
			return alias, err
		}
		suffix := fmt.Sprintf("~%d", n)
		prefix := b[:min(len(b), 8-len(suffix))]
		if available(prefix+suffix, e) {
			return alias, nil
		}
	}
	return alias, ErrNoSpace
}

func directoryRecords(units []uint16, alias [11]byte, cluster, size uint32, isDir bool) []byte {
	n := (len(units) + 12) / 13
	data := make([]byte, (n+1)*32)
	positions := [13]int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}
	for i := 0; i < n; i++ {
		seq := n - i
		d := data[i*32 : (i+1)*32]
		d[0] = byte(seq)
		if i == 0 {
			d[0] |= 0x40
		}
		d[11] = 0x0f
		d[13] = shortChecksum(alias[:])
		for j, pos := range positions {
			index := (seq-1)*13 + j
			word := uint16(0xffff)
			if index < len(units) {
				word = units[index]
			} else if index == len(units) {
				word = 0
			}
			binary.LittleEndian.PutUint16(d[pos:], word)
		}
	}
	d := data[n*32:]
	copy(d, alias[:])
	d[11] = 0x20
	if isDir {
		d[11] = 0x10
	}
	binary.LittleEndian.PutUint16(d[20:], uint16(cluster>>16))
	binary.LittleEndian.PutUint16(d[26:], uint16(cluster))
	binary.LittleEndian.PutUint32(d[28:], size)
	// No wall-clock source is supplied: use the earliest valid FAT date.
	binary.LittleEndian.PutUint16(d[16:], 0x21)
	binary.LittleEndian.PutUint16(d[18:], 0x21)
	binary.LittleEndian.PutUint16(d[24:], 0x21)
	return data
}

func (v *Volume) freeClusters(ctx context.Context, count int) ([]uint32, error) {
	result := make([]uint32, 0, count)
	if count == 0 {
		return result, nil
	}
	b := make([]byte, 512)
	for sector := uint64(0); sector*128 < v.clusterCount+2; sector++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := v.device.ReadSectors(v.fatStart+sector, b); err != nil {
			return nil, err
		}
		for i := uint64(0); i < 128; i++ {
			c := sector*128 + i
			if c < 2 || c >= v.clusterCount+2 {
				continue
			}
			if binary.LittleEndian.Uint32(b[i*4:])&0x0fffffff == 0 {
				result = append(result, uint32(c))
				if len(result) == count {
					return result, nil
				}
			}
		}
		runtime.Gosched()
	}
	return nil, ErrNoSpace
}

// applyFAT rereads and preserves every unchanged bit, including the high nibble.
// validate-only runs before any mutation and checks every affected mirror.
func (v *Volume) applyFAT(ctx context.Context, changes []fatChange, write func(uint64, []byte) error) error {
	b := make([]byte, 512)
	for start := 0; start < len(changes); {
		sector := uint64(changes[start].cluster) / 128
		end := start + 1
		for end < len(changes) && uint64(changes[end].cluster)/128 == sector {
			end++
		}
		for _, base := range v.fatCopies {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := v.device.ReadSectors(base+sector, b); err != nil {
				return err
			}
			for _, c := range changes[start:end] {
				off := (c.cluster % 128) * 4
				old := binary.LittleEndian.Uint32(b[off:])
				if old&0x0fffffff != c.old {
					return fmt.Errorf("%w: allocation FAT mirror changed", ErrCorrupt)
				}
				binary.LittleEndian.PutUint32(b[off:], old&0xf0000000|c.next)
			}
			if write != nil {
				if err := write(base+sector, b); err != nil {
					return err
				}
			}
		}
		start = end
		runtime.Gosched()
	}
	return nil
}

func (v *Volume) create(ctx context.Context, name string, data []byte, isDir bool) (err error) {
	if ctx == nil || !fs.ValidPath(name) || name == "." {
		return fs.ErrInvalid
	}
	units, err := createName(path.Base(name))
	if err != nil {
		return err
	}
	if len(data) > maxFileBytes {
		return ErrCapacity
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mutationErr != nil {
		return v.mutationErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !v.allocationValid {
		return fmt.Errorf("%w: unsupported allocation metadata", ErrCorrupt)
	}
	parent, err := v.lookupContext(ctx, path.Dir(name))
	if err != nil {
		return err
	}
	if parent.attributes&0x11 != 0x10 {
		return fs.ErrPermission
	}
	p, err := v.appendDirectory(ctx, parent.cluster)
	if err != nil {
		return err
	}
	if p.hasName(path.Base(name)) {
		return fs.ErrExist
	}
	alias, err := shortAlias(ctx, path.Base(name), p)
	if err != nil {
		return err
	}
	clusterBytes := int(v.sectorsPerCluster) * 512
	recordBytes := ((len(units)+12)/13 + 1) * 32
	end := p.offset + recordBytes + 32 // preserve a zero end marker after publication
	if end > maxDirectoryBytes {
		return ErrNoSpace
	}
	extra := (max(0, end-len(p.data)) + clusterBytes - 1) / clusterBytes
	fileClusters := (len(data) + clusterBytes - 1) / clusterBytes
	if isDir {
		fileClusters = 1
	}
	allocated, err := v.freeClusters(ctx, extra+fileClusters)
	if err != nil {
		return err
	}
	fileChain, dirExtra := allocated[:fileClusters], allocated[fileClusters:]
	var first uint32
	if len(fileChain) > 0 {
		first = fileChain[0]
	}
	var changes []fatChange
	for _, chain := range [][]uint32{fileChain, dirExtra} {
		for i, c := range chain {
			next := uint32(0x0fffffff)
			if i+1 < len(chain) {
				next = chain[i+1]
			}
			changes = append(changes, fatChange{cluster: c, next: next})
		}
	}
	if len(dirExtra) > 0 {
		lastCluster := p.chain[len(p.chain)-1]
		b := make([]byte, 512)
		off := uint64(lastCluster) * 4
		if err = v.device.ReadSectors(v.fatStart+off/512, b); err != nil {
			return err
		}
		old := binary.LittleEndian.Uint32(b[off%512:]) & 0x0fffffff
		if old < 0x0ffffff8 {
			return ErrCorrupt
		}
		changes = append(changes, fatChange{lastCluster, old, dirExtra[0]})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].cluster < changes[j].cluster })
	if err = v.applyFAT(ctx, changes, nil); err != nil {
		return err
	}
	flags := make([]byte, 512)
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
	if len(allocated) > 0 && v.fsInfo != 0 {
		info = make([]byte, 512)
		if err = v.device.ReadSectors(v.fsInfo, info); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(info) != 0x41615252 || binary.LittleEndian.Uint32(info[484:]) != 0x61417272 || binary.LittleEndian.Uint32(info[508:]) != 0xaa550000 {
			return fmt.Errorf("%w: FSInfo signatures", ErrCorrupt)
		}
		binary.LittleEndian.PutUint32(info[488:], 0xffffffff)
		binary.LittleEndian.PutUint32(info[492:], 0xffffffff)
	}
	started := false
	defer func() {
		if err != nil && started {
			v.mutationErr = errors.Join(ErrMutation, err)
			err = v.mutationErr
		}
	}()
	write := func(lba uint64, b []byte) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		e := v.device.WriteSectors(lba, b)
		if !errors.Is(e, block.ErrReadOnly) || started {
			started = true
		}
		if e != nil {
			return e
		}
		runtime.Gosched()
		return ctx.Err()
	}
	flush := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := v.device.Flush(); e != nil {
			return e
		}
		return ctx.Err()
	}
	// Advertise an incomplete mutation before touching allocation metadata.
	// An originally dirty volume stays dirty even after a successful call.
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
	b := make([]byte, 512)
	for i, c := range allocated {
		for s := uint32(0); s < v.sectorsPerCluster; s++ {
			clear(b)
			if i < fileClusters && !isDir {
				offset := i*clusterBytes + int(s)*512
				if offset < len(data) {
					copy(b, data[offset:min(offset+512, len(data))])
				}
			}
			if isDir && i == 0 && s == 0 {
				for j, dot := range []string{".", ".."} {
					d := b[j*32 : (j+1)*32]
					for k := 0; k < 11; k++ {
						d[k] = ' '
					}
					copy(d, dot)
					d[11] = 0x10
					cluster := first
					if j == 1 {
						cluster = parent.cluster
						if cluster == v.root {
							cluster = 0
						}
					}
					binary.LittleEndian.PutUint16(d[20:], uint16(cluster>>16))
					binary.LittleEndian.PutUint16(d[26:], uint16(cluster))
					binary.LittleEndian.PutUint16(d[16:], 0x21)
					binary.LittleEndian.PutUint16(d[18:], 0x21)
					binary.LittleEndian.PutUint16(d[24:], 0x21)
				}
			}
			if err = write(v.clusterSector(c)+uint64(s), b); err != nil {
				return err
			}
		}
	}
	if len(allocated) > 0 {
		if err = flush(); err != nil {
			return err
		}
	}
	if err = v.applyFAT(ctx, changes, write); err != nil {
		return err
	}
	if len(changes) > 0 {
		if err = flush(); err != nil {
			return err
		}
	}
	p.chain = append(p.chain, dirExtra...)
	p.data = append(p.data, make([]byte, len(dirExtra)*clusterBytes)...)
	size := uint32(len(data))
	if isDir {
		size = 0
	}
	copy(p.data[p.offset:], directoryRecords(units, alias, first, size, isDir))
	clear(p.data[p.offset+recordBytes : p.offset+recordBytes+32])
	for pos := p.offset / 512 * 512; pos < end; pos += 512 {
		cluster := p.chain[pos/clusterBytes]
		sector := v.clusterSector(cluster) + uint64(pos%clusterBytes/512)
		if err = write(sector, p.data[pos:pos+512]); err != nil {
			return err
		}
	}
	if err = flush(); err != nil {
		return err
	}
	if oldFlags&0x08000000 != 0 {
		clean := []fatChange{{1, oldFlags &^ 0x08000000, oldFlags}}
		if err = v.applyFAT(ctx, clean, write); err != nil {
			return err
		}
		return flush()
	}
	return nil
}
