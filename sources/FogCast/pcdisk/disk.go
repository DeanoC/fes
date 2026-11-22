// Package pcdisk makes 360 KiB PC/XT floppy images for the library: a blank
// DOS-formatted disk, or a named copy of a bootable one. The bytes are an
// ordinary raw .img: a boot sector with a DOS 4 extended BPB, two FAT12
// tables, a root directory and the data area.
package pcdisk

import (
	"encoding/binary"
	"errors"
	"strings"
)

// Size is the only disk this package makes: 40 tracks, two sides, nine
// sectors of 512 bytes.
const Size = 40 * 2 * 9 * bytesPerSector

const (
	bytesPerSector    = 512
	sectorsPerCluster = 2
	reservedSectors   = 1
	fatCount          = 2
	rootEntries       = 112
	sectorsPerFAT     = 2
	mediaDescriptor   = 0xFD
	sectorsPerTrack   = 9
	heads             = 2
	rootOffset        = (reservedSectors + fatCount*sectorsPerFAT) * bytesPerSector
	labelAttribute    = 0x08
)

// ErrName rejects a volume label DOS cannot store.
var ErrName = errors.New("pcdisk: disk name must be 1–11 letters, digits, spaces or hyphens")

// ErrImage rejects an image that is not a 360 KiB DOS disk with an extended
// boot record.
var ErrImage = errors.New("pcdisk: not a 360 KiB DOS disk with a volume label")

// notBootable is the blank disk's boot code, at offset 0x3E. It prints the
// message at 0x62 and, on a key, asks the BIOS to try the boot again
// (INT 19h), as DOS's own non-system disks do.
//
//	cli; cld; xor ax,ax; mov ds,ax; mov ss,ax; mov sp,7C00h; sti
//	mov si,7C62h
//	next: lodsb; or al,al; jz wait
//	      mov ah,0Eh; mov bx,7; int 10h; jmp next
//	wait: xor ax,ax; int 16h; int 19h
var notBootable = []byte{
	0xFA, 0xFC, 0x31, 0xC0, 0x8E, 0xD8, 0x8E, 0xD0, 0xBC, 0x00, 0x7C, 0xFB,
	0xBE, 0x62, 0x7C,
	0xAC, 0x08, 0xC0, 0x74, 0x09,
	0xB4, 0x0E, 0xBB, 0x07, 0x00, 0xCD, 0x10, 0xEB, 0xF2,
	0x31, 0xC0, 0xCD, 0x16, 0xCD, 0x19,
}

const notBootableMessage = "\r\nThis disk does not start the computer.\r\nPut in a disk that does, then press a key.\r\n\x00"

// Label turns a disk's name into a DOS volume label, at most eleven
// characters, or reports ErrName when nothing storable is left.
func Label(name string) (string, error) {
	var label strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		if label.Len() == 11 {
			break
		}
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '-' {
			label.WriteRune(r)
		}
	}
	out := strings.TrimSpace(label.String())
	if out == "" {
		return "", ErrName
	}
	return out, nil
}

// Blank formats an empty, non-bootable 360 KiB disk named label.
func Blank(label string, serial [4]byte) ([]byte, error) {
	name, err := Label(label)
	if err != nil {
		return nil, err
	}
	disk := make([]byte, Size)
	boot := disk[:bytesPerSector]
	copy(boot, []byte{0xEB, 0x3C, 0x90})
	copy(boot[3:11], "FES     ")
	binary.LittleEndian.PutUint16(boot[11:], bytesPerSector)
	boot[13] = sectorsPerCluster
	binary.LittleEndian.PutUint16(boot[14:], reservedSectors)
	boot[16] = fatCount
	binary.LittleEndian.PutUint16(boot[17:], rootEntries)
	binary.LittleEndian.PutUint16(boot[19:], Size/bytesPerSector)
	boot[21] = mediaDescriptor
	binary.LittleEndian.PutUint16(boot[22:], sectorsPerFAT)
	binary.LittleEndian.PutUint16(boot[24:], sectorsPerTrack)
	binary.LittleEndian.PutUint16(boot[26:], heads)
	boot[0x26] = 0x29
	copy(boot[0x36:0x3E], "FAT12   ")
	copy(boot[0x3E:], notBootable)
	copy(boot[0x62:], notBootableMessage)
	boot[510], boot[511] = 0x55, 0xAA
	for fat := 0; fat < fatCount; fat++ {
		at := (reservedSectors + fat*sectorsPerFAT) * bytesPerSector
		copy(disk[at:], []byte{mediaDescriptor, 0xFF, 0xFF})
	}
	// The root's first entry is the volume label, which Named fills in.
	disk[rootOffset+11] = labelAttribute
	setLabel(disk, name, serial, rootOffset)
	return disk, nil
}

// Named returns a copy of image, a 360 KiB DOS disk, under a new volume
// label and serial number, in its boot record and in the root directory's
// label entry. A root without a label entry gets one in its first free slot.
// The files on the disk are unchanged.
func Named(image []byte, label string, serial [4]byte) ([]byte, error) {
	name, err := Label(label)
	if err != nil {
		return nil, err
	}
	if !dosDisk(image) {
		return nil, ErrImage
	}
	disk := append([]byte(nil), image...)
	at := -1
	for i := 0; i < rootEntries; i++ {
		entry := disk[rootOffset+32*i : rootOffset+32*i+32]
		if entry[0] != 0 && entry[0] != 0xE5 && entry[11]&0x0F == labelAttribute {
			at = rootOffset + 32*i
			break
		}
	}
	if at < 0 {
		for i := 0; i < rootEntries; i++ {
			if first := disk[rootOffset+32*i]; first == 0 || first == 0xE5 {
				at = rootOffset + 32*i
				clear(disk[at : at+32])
				disk[at+11] = labelAttribute
				break
			}
		}
	}
	if at < 0 {
		return nil, ErrImage
	}
	setLabel(disk, name, serial, at)
	return disk, nil
}

// dosDisk checks the boot record of a 360 KiB disk laid out as Blank lays
// it out, with the DOS 4 extended boot signature.
func dosDisk(image []byte) bool {
	if len(image) != Size || image[510] != 0x55 || image[511] != 0xAA || image[0x26] != 0x29 {
		return false
	}
	b := image
	return binary.LittleEndian.Uint16(b[11:]) == bytesPerSector && b[13] == sectorsPerCluster &&
		binary.LittleEndian.Uint16(b[14:]) == reservedSectors && b[16] == fatCount &&
		binary.LittleEndian.Uint16(b[17:]) == rootEntries && binary.LittleEndian.Uint16(b[19:]) == Size/bytesPerSector &&
		b[21] == mediaDescriptor && binary.LittleEndian.Uint16(b[22:]) == sectorsPerFAT
}

func setLabel(disk []byte, name string, serial [4]byte, entry int) {
	padded := []byte(name + strings.Repeat(" ", 11-len(name)))
	copy(disk[0x27:0x2B], serial[:])
	copy(disk[0x2B:0x36], padded)
	copy(disk[entry:entry+11], padded)
}
