// Package spectrumsnapshots reads ZX Spectrum .z80, .sna and .szx snapshots and
// writes the restore stream that fes.media.spectrum-snapshot 1.0 cores run.
//
// A restore stream is an 8-byte header followed by Z80 code. A core that
// finds the header at the start of its cassette unit resets the machine and
// serves every CPU memory read from the stream, in order, whatever the
// address, until the last byte has been read. Writes, I/O and contention
// behave as usual, so the code fills RAM with LD (HL),n, programs the border,
// AY and paging with OUT, loads the registers (pops read their bytes from the
// stream too) and ends with JP to the snapshot's PC, whose opcode is the
// first fetch from real memory.
//
// One stream runs on every core its class allows. Class 0 (48K) fills
// $4000-$FFFF without paging, which every model maps to banks 5, 2 and 0
// after reset. Classes 1 (128K) and 2 (+2A/+3) page each bank in at $C000
// through $7FFD. The stream writes $1FFD and then $7FFD: on the 128K and
// Pentagon $1FFD decodes as $7FFD, so the second write replaces it, and on
// the 48K neither port exists.
package spectrumsnapshots

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Machine is the hardware a snapshot was taken on, and the class byte of its
// restore stream.
type Machine uint8

const (
	// Machine48K runs on every Spectrum core.
	Machine48K Machine = iota
	// Machine128K needs a core with 128K paging and the AY.
	Machine128K
	// MachinePlus3 needs the +2A/+3's $1FFD paging.
	MachinePlus3
)

const (
	// Version is the restore stream format this package writes.
	Version = 1
	// HeaderBytes is the restore stream header: magic, version, class and
	// two zero bytes.
	HeaderBytes = 8
	// MaxInputBytes bounds a .z80 or .sna before it is read; the largest
	// uncompressed .z80 is 30+56 header bytes and nine 16 KiB pages.
	MaxInputBytes = 1 << 18
	bankBytes     = 16 << 10
)

// Magic starts every restore stream.
var Magic = [4]byte{'Z', 'X', 'R', 'S'}

// Extensions are the snapshot names Convert reads.
var Extensions = []string{".z80", ".sna", ".szx"}

// Admit reports whether name has a snapshot extension.
func Admit(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// Snapshot is the machine state a snapshot holds.
type Snapshot struct {
	Machine                Machine
	A, F, B, C, D, E, H, L uint8
	A2, F2, B2, C2, D2, E2 uint8
	H2, L2                 uint8
	IX, IY, SP, PC         uint16
	I, R, IM               uint8
	IFF1, IFF2             bool
	Border                 uint8
	// Port7FFD and Port1FFD are the last paging writes (128K-class and
	// +2A/+3 machines).
	Port7FFD, Port1FFD uint8
	// AY holds the sound chip's registers and AYSelect its selected register
	// when HasAY.
	HasAY    bool
	AYSelect uint8
	AY       [16]uint8
	// Banks holds the 16 KiB RAM banks: 5, 2 and 0 at $4000, $8000 and $C000
	// on a 48K; all eight on a 128K-class machine.
	Banks map[int]*[bankBytes]byte
}

// Recognize reports whether data is a .z80, .sna or .szx snapshot this
// package reads.
func Recognize(data []byte) bool {
	_, err := Read(data)
	return err == nil
}

// Read parses a .szx (by its signature), a .sna (by its exact size) or a
// .z80 snapshot.
func Read(data []byte) (*Snapshot, error) {
	if bytes.HasPrefix(data, szxMagic) {
		return readSZX(data)
	}
	if len(data) == 0 || len(data) > MaxInputBytes {
		return nil, errors.New("snapshot size is outside 1 byte..256 KiB")
	}
	switch len(data) {
	case 27 + 3*bankBytes, 27 + 3*bankBytes + 4 + 5*bankBytes, 27 + 3*bankBytes + 4 + 6*bankBytes:
		return readSNA(data)
	}
	return readZ80(data)
}

// Convert reads a snapshot and returns its machine and restore stream.
func Convert(data []byte) (Machine, []byte, error) {
	s, err := Read(data)
	if err != nil {
		return 0, nil, err
	}
	return s.Machine, Stream(s), nil
}

func newBank() *[bankBytes]byte { return new([bankBytes]byte) }

func readSNA(data []byte) (*Snapshot, error) {
	h := data[:27]
	s := &Snapshot{
		I: h[0], L2: h[1], H2: h[2], E2: h[3], D2: h[4], C2: h[5], B2: h[6], F2: h[7], A2: h[8],
		L: h[9], H: h[10], E: h[11], D: h[12], C: h[13], B: h[14],
		IY: le16(h[15:]), IX: le16(h[17:]), R: h[20], F: h[21], A: h[22], SP: le16(h[23:]),
		IM: h[25], Border: h[26] & 7,
	}
	s.IFF2 = h[19]&4 != 0
	s.IFF1 = s.IFF2
	if s.IM > 2 {
		return nil, errors.New(".sna interrupt mode is not 0, 1 or 2")
	}
	ram := data[27 : 27+3*bankBytes]
	s.Banks = map[int]*[bankBytes]byte{}
	if len(data) == 27+3*bankBytes {
		s.Machine = Machine48K
		for i, bank := range []int{5, 2, 0} {
			s.Banks[bank] = newBank()
			copy(s.Banks[bank][:], ram[i*bankBytes:])
		}
		// A 48K .sna keeps PC on the stack, as RETN would pop it.
		if s.SP < 0x4000 || s.SP == 0xffff {
			return nil, errors.New(".sna stack pointer does not address RAM")
		}
		lo := readRAM48(s, s.SP)
		hi := readRAM48(s, s.SP+1)
		s.PC = uint16(lo) | uint16(hi)<<8
		s.SP += 2
		return s, nil
	}
	tail := data[27+3*bankBytes:]
	s.Machine = Machine128K
	s.PC = le16(tail)
	s.Port7FFD = tail[2]
	if tail[3] != 0 {
		return nil, errors.New(".sna has the TR-DOS ROM paged in; TR-DOS is not supported")
	}
	paged := int(s.Port7FFD & 7)
	for i, bank := range []int{5, 2, paged} {
		page := ram[i*bankBytes : (i+1)*bankBytes]
		if previous := s.Banks[bank]; previous != nil {
			if !bytes.Equal(previous[:], page) {
				return nil, fmt.Errorf(".sna duplicate RAM bank %d differs", bank)
			}
			continue
		}
		s.Banks[bank] = newBank()
		copy(s.Banks[bank][:], page)
	}
	rest := tail[4:]
	for bank := 0; bank < 8; bank++ {
		if bank == 5 || bank == 2 || bank == paged {
			continue
		}
		if len(rest) < bankBytes {
			return nil, errors.New(".sna is missing a RAM bank")
		}
		s.Banks[bank] = newBank()
		copy(s.Banks[bank][:], rest)
		rest = rest[bankBytes:]
	}
	// With bank 2 or 5 at $C000 the file holds the six other banks, and
	// otherwise five.
	if len(rest) != 0 {
		return nil, errors.New(".sna size does not match its paging")
	}
	return s, nil
}

func readRAM48(s *Snapshot, a uint16) byte {
	return s.Banks[[]int{-1, 5, 2, 0}[a>>14]][a&0x3fff]
}

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

// .z80 hardware modes for versions 2 and 3.
func z80Machine(version int, mode, flags3, if1 byte) (Machine, error) {
	modify := flags3&0x80 != 0
	unsupported := func(name string) (Machine, error) {
		return 0, fmt.Errorf(".z80 snapshot is for %s, which no FES Spectrum core emulates", name)
	}
	if1Paged := func() error {
		if if1 == 0xff {
			return errors.New(".z80 snapshot has the Interface 1 ROM paged in")
		}
		return nil
	}
	switch {
	case mode == 0 || mode == 1:
		if modify {
			return unsupported("the 16K Spectrum")
		}
		if mode == 1 {
			if err := if1Paged(); err != nil {
				return 0, err
			}
		}
		return Machine48K, nil
	case mode == 2:
		return unsupported("SamRam")
	case version == 2 && (mode == 3 || mode == 4), version == 3 && (mode == 4 || mode == 5):
		if mode == 4 && version == 2 || mode == 5 {
			if err := if1Paged(); err != nil {
				return 0, err
			}
		}
		return Machine128K, nil
	case version == 3 && (mode == 3 || mode == 6):
		return unsupported("an MGT interface")
	case mode == 7 || mode == 8 || mode == 13:
		if err := if1Paged(); err != nil {
			return 0, err
		}
		return MachinePlus3, nil
	case mode == 9 || mode == 12:
		return Machine128K, nil
	case mode == 10:
		return unsupported("the Scorpion")
	case mode == 11:
		return unsupported("the Didaktik Kompakt")
	case mode == 14 || mode == 15 || mode == 128:
		return unsupported("a Timex machine")
	}
	return 0, fmt.Errorf(".z80 hardware mode %d is not known", mode)
}

func readZ80(data []byte) (*Snapshot, error) {
	if len(data) < 30 {
		return nil, errors.New("not a .z80, .sna or .szx snapshot")
	}
	h := data[:30]
	flags1 := h[12]
	if flags1 == 0xff {
		flags1 = 1
	}
	s := &Snapshot{
		A: h[0], F: h[1], C: h[2], B: h[3], L: h[4], H: h[5], PC: le16(h[6:]), SP: le16(h[8:]),
		I: h[10], R: h[11]&0x7f | (flags1&1)<<7, Border: flags1 >> 1 & 7,
		E: h[13], D: h[14], C2: h[15], B2: h[16], E2: h[17], D2: h[18], L2: h[19], H2: h[20],
		A2: h[21], F2: h[22], IY: le16(h[23:]), IX: le16(h[25:]),
		IFF1: h[27] != 0, IFF2: h[28] != 0, IM: h[29] & 3,
	}
	if s.IM > 2 {
		return nil, errors.New(".z80 interrupt mode is not 0, 1 or 2")
	}
	s.Banks = map[int]*[bankBytes]byte{}
	if s.PC != 0 {
		// Version 1: 48K, one 48 KiB block.
		if flags1&0x10 != 0 {
			return nil, errors.New(".z80 version 1 snapshot has BASIC SamRom switched in")
		}
		s.Machine = Machine48K
		ram := data[30:]
		if flags1&0x20 != 0 {
			if len(ram) < 4 || !bytes.Equal(ram[len(ram)-4:], []byte{0, 0xed, 0xed, 0}) {
				return nil, errors.New(".z80 version 1 block has no end marker")
			}
			var err error
			if ram, err = inflate(ram[:len(ram)-4], 3*bankBytes); err != nil {
				return nil, err
			}
		} else if len(ram) != 3*bankBytes {
			return nil, errors.New(".z80 version 1 block is not 48 KiB")
		}
		for i, bank := range []int{5, 2, 0} {
			s.Banks[bank] = newBank()
			copy(s.Banks[bank][:], ram[i*bankBytes:])
		}
		return s, nil
	}
	if len(data) < 32 {
		return nil, errors.New(".z80 extended header is truncated")
	}
	extra := int(le16(data[30:]))
	version := 0
	switch extra {
	case 23:
		version = 2
	case 54, 55:
		version = 3
	default:
		return nil, fmt.Errorf(".z80 extended header length %d is not 23, 54 or 55", extra)
	}
	if len(data) < 32+extra {
		return nil, errors.New(".z80 extended header is truncated")
	}
	x := data[32 : 32+extra]
	s.PC = le16(x)
	machine, err := z80Machine(version, x[2], x[5], x[4])
	if err != nil {
		return nil, err
	}
	s.Machine = machine
	if machine != Machine48K {
		s.Port7FFD = x[3]
	}
	// A 48K snapshot sets byte 37 bit 2 when an AY is in use: on the 128K
	// ports (a Melodik), unless bit 6 says a Fuller box. A 128K-class core
	// running it restores the chip.
	if machine != Machine48K || x[5]&0x44 == 0x04 {
		s.HasAY = true
		s.AYSelect = x[6] & 0x0f
		copy(s.AY[:], x[7:23])
	}
	if machine == MachinePlus3 && extra == 55 {
		s.Port1FFD = x[54]
	}
	pages := data[32+extra:]
	for len(pages) > 0 {
		if len(pages) < 3 {
			return nil, errors.New(".z80 memory block header is truncated")
		}
		n, page := int(le16(pages)), int(pages[2])
		pages = pages[3:]
		var raw []byte
		if n == 0xffff {
			if len(pages) < bankBytes {
				return nil, errors.New(".z80 memory block is truncated")
			}
			raw, pages = pages[:bankBytes], pages[bankBytes:]
		} else {
			if len(pages) < n {
				return nil, errors.New(".z80 memory block is truncated")
			}
			if raw, err = inflate(pages[:n], bankBytes); err != nil {
				return nil, err
			}
			pages = pages[n:]
		}
		bank := -1
		if machine == Machine48K {
			// Pages 8, 4 and 5 are $4000, $8000 and $C000.
			switch page {
			case 8:
				bank = 5
			case 4:
				bank = 2
			case 5:
				bank = 0
			}
		} else if page >= 3 && page <= 10 {
			bank = page - 3
		}
		if bank < 0 {
			return nil, fmt.Errorf(".z80 memory page %d is not RAM on this machine", page)
		}
		if s.Banks[bank] != nil {
			return nil, fmt.Errorf(".z80 memory page %d appears twice", page)
		}
		s.Banks[bank] = newBank()
		copy(s.Banks[bank][:], raw)
	}
	want := 8
	if machine == Machine48K {
		want = 3
	}
	if len(s.Banks) != want {
		return nil, errors.New(".z80 snapshot is missing RAM pages")
	}
	return s, nil
}

// inflate expands the .z80 run-length coding (ED ED count byte) to exactly
// size bytes.
func inflate(in []byte, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	for i := 0; i < len(in); {
		if i+1 < len(in) && in[i] == 0xed && in[i+1] == 0xed {
			if len(in)-i < 4 {
				return nil, errors.New(".z80 run-length token is truncated")
			}
			for n := 0; n < int(in[i+2]); n++ {
				out = append(out, in[i+3])
			}
			i += 4
		} else {
			out = append(out, in[i])
			i++
		}
		if len(out) > size {
			break
		}
	}
	if len(out) != size {
		return nil, fmt.Errorf(".z80 compressed block expands to %d bytes, not %d", len(out), size)
	}
	return out, nil
}
