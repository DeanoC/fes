package spectrumsnapshots

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
)

// MaxSZXBytes bounds a .szx before it is read. Its optional chunks can embed
// a tape or disk image, so it is the cassette unit's image limit rather than
// a snapshot's.
const MaxSZXBytes = 32 << 20

var szxMagic = []byte("ZXST")

// szxMachines maps the .szx machine byte to the class a restore stream
// loads. A Pentagon 128 snapshot is a 128K one: the Pentagon and 128K cores
// share its paging.
var szxMachines = map[byte]struct {
	machine Machine
	name    string
}{
	1: {Machine48K, ""}, 2: {Machine128K, ""}, 3: {Machine128K, ""}, 4: {MachinePlus3, ""},
	5: {MachinePlus3, ""}, 7: {Machine128K, ""},
	0: {name: "the 16K Spectrum"}, 6: {name: "the +3e"}, 8: {name: "a Timex machine"},
	9: {name: "a Timex machine"}, 10: {name: "the Scorpion"}, 11: {name: "the Spectrum SE"},
	12: {name: "a Timex machine"}, 13: {name: "the Pentagon 512"}, 14: {name: "the Pentagon 1024"},
	15: {name: "the NTSC 48K Spectrum"}, 16: {name: "the 128Ke"},
}

// szxRefused names the chunks whose state a restore stream cannot reproduce:
// a ROM other than the machine's own in place, or memory the cores lack. Each
// test reports whether the chunk's data has that state.
var szxRefused = map[string]struct {
	name      string
	flagsSize int
	engaged   func(data []byte) bool
}{
	"B128":    {name: "the TR-DOS ROM paged in", flagsSize: 4, engaged: func(d []byte) bool { return flags32(d)&4 != 0 }},
	"IF1\x00": {name: "the Interface 1 ROM paged in", flagsSize: 2, engaged: func(d []byte) bool { return flags16(d)&4 != 0 }},
	"MFCE":    {name: "the Multiface ROM paged in", engaged: func(d []byte) bool { return len(d) >= 2 && d[1]&1 != 0 }},
	"PLSD":    {name: "the +D ROM paged in", flagsSize: 4, engaged: func(d []byte) bool { return flags32(d)&1 != 0 }},
	"OPUS":    {name: "the Opus Discovery ROM paged in", flagsSize: 4, engaged: func(d []byte) bool { return flags32(d)&1 != 0 }},
	"DIDE":    {name: "the DivIDE memory paged in", flagsSize: 2, engaged: func(d []byte) bool { return flags16(d)&2 != 0 }},
	"DMMC":    {name: "the DivMMC memory paged in", flagsSize: 2, engaged: func(d []byte) bool { return flags16(d)&2 != 0 }},
	"SNET":    {name: "Spectranet paging or its programmable trap is engaged", flagsSize: 2, engaged: func(d []byte) bool { return flags16(d)&7 != 0 }},
	"PLTT":    {name: "the ULAplus palette enabled", engaged: func(d []byte) bool { return len(d) >= 1 && d[0]&1 != 0 }},
	"IF2R":    {name: "an Interface 2 cartridge", engaged: func([]byte) bool { return true }},
	"ROM\x00": {name: "a custom ROM", engaged: func([]byte) bool { return true }},
	"LEC\x00": {name: "an LEC memory expansion", engaged: func([]byte) bool { return true }},
	"ZXAT":    {name: "a ZXATASP interface", engaged: func([]byte) bool { return true }},
	"ZXCF":    {name: "a ZXCF interface", engaged: func([]byte) bool { return true }},
}

func flags16(d []byte) uint16 {
	if len(d) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(d)
}

func flags32(d []byte) uint32 {
	if len(d) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(d)
}

// libspectrum before 0.5.1 wrote A and F swapped in Z80R; libspectrum reads
// its creator chunk to undo that, and so does readSZX.
var oldLibspectrum = regexp.MustCompile(`libspectrum: (\d+)\.(\d+)\.(\d+)`)

func swapsAF(creator []byte) bool {
	if len(creator) <= 36 {
		return false
	}
	m := oldLibspectrum.FindSubmatch(creator[36:])
	if m == nil {
		return false
	}
	v := make([]int, 3)
	for i := range v {
		v[i], _ = strconv.Atoi(string(m[i+1]))
	}
	return v[0] == 0 && (v[1] < 5 || v[1] == 5 && v[2] == 0)
}

// readSZX parses a ZX-State (.szx) snapshot, as Fuse's libspectrum writes
// and reads it: the header, then chunks of a 4-byte id and 32-bit length.
// Z80R holds the registers, SPCR the border and paging, RAMP each RAM page
// (zlib compressed when its flag is set) and AY the sound chip. Chunks for
// peripherals a core lacks are skipped unless they hold state the restore
// would lose (szxRefused).
func readSZX(data []byte) (*Snapshot, error) {
	if len(data) > MaxSZXBytes {
		return nil, errors.New(".szx snapshot is larger than 32 MiB")
	}
	if len(data) < 8 {
		return nil, errors.New(".szx header is truncated")
	}
	if data[4] != 1 {
		return nil, fmt.Errorf(".szx major version %d is not 1", data[4])
	}
	kind, ok := szxMachines[data[6]]
	if !ok {
		return nil, fmt.Errorf(".szx machine %d is not known", data[6])
	}
	if kind.name != "" {
		return nil, fmt.Errorf(".szx snapshot is for %s, which no FES Spectrum core emulates", kind.name)
	}
	s := &Snapshot{Machine: kind.machine, Banks: map[int]*[bankBytes]byte{}}
	var regs, spcr, ay []byte
	swap := false
	for rest := data[8:]; len(rest) > 0; {
		// libspectrum takes an all-$FF tail (Fuse's libretro core pads its
		// saves) as the end: no chunk id starts with $FF.
		if len(bytes.TrimLeft(rest, "\xff")) == 0 {
			break
		}
		if len(rest) < 8 {
			return nil, errors.New(".szx chunk header is truncated")
		}
		id, n := string(rest[:4]), binary.LittleEndian.Uint32(rest[4:])
		if uint64(n) > uint64(len(rest)-8) {
			return nil, fmt.Errorf(".szx chunk %q is truncated", id)
		}
		body := rest[8 : 8+n]
		rest = rest[8+n:]
		if refused, ok := szxRefused[id]; ok {
			if len(body) < refused.flagsSize {
				return nil, fmt.Errorf(".szx chunk %q flags are truncated", id)
			}
			if refused.engaged(body) {
				return nil, fmt.Errorf(".szx snapshot has %s", refused.name)
			}
		}
		switch id {
		case "CRTR":
			swap = swapsAF(body)
		case "Z80R":
			regs = body
		case "SPCR":
			spcr = body
		case "AY\x00\x00":
			ay = body
		case "RAMP":
			if err := readRAMP(s, body); err != nil {
				return nil, err
			}
		}
	}
	if len(regs) != 37 {
		return nil, errors.New(".szx has no Z80R register chunk of 37 bytes")
	}
	// ZXSTZF_HALTED is not represented by the restore stream.
	if regs[34]&2 != 0 {
		return nil, errors.New(".szx snapshot is halted; the restore stream cannot preserve HALT")
	}
	if len(spcr) != 8 {
		return nil, errors.New(".szx has no SPCR chunk of 8 bytes")
	}
	s.F, s.A, s.F2, s.A2 = regs[0], regs[1], regs[8], regs[9]
	if swap {
		s.F, s.A, s.F2, s.A2 = s.A, s.F, s.A2, s.F2
	}
	s.C, s.B, s.E, s.D, s.L, s.H = regs[2], regs[3], regs[4], regs[5], regs[6], regs[7]
	s.C2, s.B2, s.E2, s.D2, s.L2, s.H2 = regs[10], regs[11], regs[12], regs[13], regs[14], regs[15]
	s.IX, s.IY, s.SP, s.PC = le16(regs[16:]), le16(regs[18:]), le16(regs[20:]), le16(regs[22:])
	s.I, s.R, s.IFF1, s.IFF2, s.IM = regs[24], regs[25], regs[26] != 0, regs[27] != 0, regs[28]
	if s.IM > 2 {
		return nil, errors.New(".szx interrupt mode is not 0, 1 or 2")
	}
	s.Border = spcr[0] & 7
	if s.Machine != Machine48K {
		s.Port7FFD = spcr[1]
	}
	if ay != nil && len(ay) != 18 {
		return nil, errors.New(".szx AY chunk is not 18 bytes")
	}
	// A 48K snapshot's AY is restored when it is a Melodik, on the 128K ports
	// (flag 2); a Fuller box (flag 1) sits on other ports.
	if ay != nil && (s.Machine != Machine48K || ay[0]&3 == 2) {
		s.HasAY = true
		s.AYSelect = ay[1] & 0x0f
		copy(s.AY[:], ay[2:])
	}
	if s.Machine == MachinePlus3 {
		s.Port1FFD = spcr[2]
	}
	want := []int{5, 2, 0}
	if s.Machine != Machine48K {
		want = []int{0, 1, 2, 3, 4, 5, 6, 7}
	}
	for _, bank := range want {
		if s.Banks[bank] == nil {
			return nil, fmt.Errorf(".szx snapshot is missing RAM page %d", bank)
		}
	}
	if len(s.Banks) != len(want) {
		return nil, errors.New(".szx snapshot has RAM pages this machine does not have")
	}
	return s, nil
}

// readRAMP stores one RAMP chunk: a flags word, the page number (a 128K bank
// number on every machine) and the page, zlib compressed or raw.
func readRAMP(s *Snapshot, body []byte) error {
	if len(body) < 3 {
		return errors.New(".szx RAMP chunk is truncated")
	}
	flags, page, raw := le16(body), int(body[2]), body[3:]
	if page > 7 {
		return fmt.Errorf(".szx RAM page %d is not RAM on this machine", page)
	}
	if s.Banks[page] != nil {
		return fmt.Errorf(".szx RAM page %d appears twice", page)
	}
	if flags&1 != 0 {
		z, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf(".szx RAM page %d: %w", page, err)
		}
		if raw, err = io.ReadAll(io.LimitReader(z, bankBytes)); err != nil {
			return fmt.Errorf(".szx RAM page %d: %w", page, err)
		}
		if len(raw) == bankBytes {
			// Consume once beyond the page to force zlib to verify its Adler32
			// trailer when decompression ended at exactly bankBytes.
			var extra [1]byte
			n, readErr := io.ReadFull(z, extra[:])
			if n != 0 {
				return fmt.Errorf(".szx RAM page %d holds more than 16384 bytes", page)
			}
			if readErr != io.EOF {
				return fmt.Errorf(".szx RAM page %d: %w", page, readErr)
			}
		}
	}
	if len(raw) != bankBytes {
		return fmt.Errorf(".szx RAM page %d holds %d bytes, not 16384", page, len(raw))
	}
	s.Banks[page] = newBank()
	copy(s.Banks[page][:], raw)
	return nil
}
