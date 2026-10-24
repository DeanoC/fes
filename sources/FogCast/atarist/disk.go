// Package atarist builds blank formatted Atari ST floppy images for the
// library. The bytes are an ordinary raw .st: a GEMDOS boot sector, two
// FAT12 tables and an empty root directory. Drive-A persistence is unchanged.
package atarist

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

const (
	bytesPerSector    = 512
	sectorsPerCluster = 2
	reservedSectors   = 1
	fatCount          = 2
	rootEntries       = 112
	sectorsPerFAT     = 5
	mediaDescriptor   = 0xF9
)

// ErrName rejects a volume label GEMDOS cannot store.
var ErrName = errors.New("atarist: disk name must be 1–11 letters, digits, spaces or hyphens")

// ErrGeometry rejects a length outside the twelve admitted ST layouts.
var ErrGeometry = errors.New("atarist: disk geometry is not an admitted ST layout")

// Disk is one formatted image. Name is the uppercase volume label.
type Disk struct {
	Name    string
	Tracks  int
	Heads   int
	Sectors int
	Bytes   []byte
}

// Layouts is every raw geometry the ST floppy contract admits.
func Layouts() []corepackage.AtariStGeometry {
	out := make([]corepackage.AtariStGeometry, 0, 12)
	for tracks := 80; tracks <= 82; tracks++ {
		for heads := 1; heads <= 2; heads++ {
			for sectors := 9; sectors <= 10; sectors++ {
				out = append(out, corepackage.AtariStGeometry{Tracks: tracks, Heads: heads, Sectors: sectors})
			}
		}
	}
	return out
}

// Geometry resolves a requested layout. Zero tracks, heads and sectors select
// the 720 KiB double-sided nine-sector disk. Any other request must be one
// of the twelve admitted lengths, with the counts matching that length.
func Geometry(tracks, heads, sectors int) (corepackage.AtariStGeometry, bool) {
	if tracks == 0 && heads == 0 && sectors == 0 {
		tracks, heads, sectors = 80, 2, 9
	}
	if tracks < 1 || heads < 1 || sectors < 1 {
		return corepackage.AtariStGeometry{}, false
	}
	size := int64(tracks) * int64(heads) * int64(sectors) * bytesPerSector
	geometry, ok := corepackage.AtariStGeometryForSize(size)
	if !ok || geometry.Tracks != tracks || geometry.Heads != heads || geometry.Sectors != sectors {
		return corepackage.AtariStGeometry{}, false
	}
	return geometry, true
}

// Format builds a non-bootable FAT12 disk. Zero tracks, heads and sectors
// select the 720 KiB double-sided nine-sector layout.
func Format(name string, tracks, heads, sectors int) (Disk, error) {
	label, err := volumeName(name)
	if err != nil {
		return Disk{}, err
	}
	geometry, ok := Geometry(tracks, heads, sectors)
	if !ok {
		return Disk{}, ErrGeometry
	}
	image := blankImage(label, geometry)
	if !corepackage.ValidAtariStBase(image, true) {
		return Disk{}, ErrGeometry
	}
	return Disk{Name: label, Tracks: geometry.Tracks, Heads: geometry.Heads, Sectors: geometry.Sectors, Bytes: image}, nil
}

func volumeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 11 || strings.ContainsFunc(name, func(r rune) bool { return r > 127 }) {
		return "", ErrName
	}
	upper := strings.ToUpper(name)
	for _, r := range upper {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '-' {
			continue
		}
		return "", ErrName
	}
	return upper, nil
}

// blankImage follows the EmuTOS protobt BPB used for 360 KiB single-sided,
// 720 KiB and 800 KiB disks (512-byte sectors, 2 per cluster, 1 reserved,
// 2 FATs, 112 root entries, 5 sectors per FAT, media 0xF9). The other
// admitted lengths use that same BPB with their own sector and head counts.
func blankImage(label string, geometry corepackage.AtariStGeometry) []byte {
	total := geometry.Tracks * geometry.Heads * geometry.Sectors
	image := make([]byte, total*bytesPerSector)
	boot := image[:bytesPerSector]
	boot[0], boot[1] = 0xEB, 0x3C
	copy(boot[2:8], "FES1.0")
	boot[8], boot[9], boot[10] = 'E', 'S', 'F'
	le := binary.LittleEndian
	le.PutUint16(boot[11:13], bytesPerSector)
	boot[13] = sectorsPerCluster
	le.PutUint16(boot[14:16], reservedSectors)
	boot[16] = fatCount
	le.PutUint16(boot[17:19], rootEntries)
	le.PutUint16(boot[19:21], uint16(total))
	boot[21] = mediaDescriptor
	le.PutUint16(boot[22:24], sectorsPerFAT)
	le.PutUint16(boot[24:26], uint16(geometry.Sectors))
	le.PutUint16(boot[26:28], uint16(geometry.Heads))
	sealDataDisk(boot)

	fat := make([]byte, sectorsPerFAT*bytesPerSector)
	fat[0], fat[1], fat[2] = mediaDescriptor, 0xFF, 0xFF
	copy(image[reservedSectors*bytesPerSector:], fat)
	copy(image[(reservedSectors+sectorsPerFAT)*bytesPerSector:], fat)

	entry := image[(reservedSectors+fatCount*sectorsPerFAT)*bytesPerSector:][:32]
	padded := label + strings.Repeat(" ", 11-len(label))
	copy(entry[:11], padded)
	entry[11] = 0x08
	return image
}

// sealDataDisk stores the EmuTOS non-executable checksum (word sum 0x1235)
// so TOS will not run the boot sector.
func sealDataDisk(sector []byte) {
	sector[510], sector[511] = 0, 0
	sum := wordSum(sector)
	ck := uint16(0x1234) - sum
	sector[510] = byte(ck >> 8)
	sector[511] = byte(ck) + 1
	if wordSum(sector) == 0x1234 {
		panic(fmt.Sprintf("atarist: boot sector became executable"))
	}
}

func wordSum(sector []byte) uint16 {
	var sum uint16
	for i := 0; i < bytesPerSector; i += 2 {
		sum += uint16(sector[i])<<8 | uint16(sector[i+1])
	}
	return sum
}
