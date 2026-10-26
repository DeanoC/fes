// Package spectrumtapes converts ZX Spectrum tape images into the PZX
// stream that fes.media.spectrum-tape 2.0 plays.
//
// .tap, .tzx, .csw and .pzx images are read into libspectrum's tape model
// and played by a port of libspectrum 1.5's edge generator, so the converted
// tape plays on the FES deck exactly as the original plays in Fuse 1.6.0:
// the same EAR level for the same T states, and the same stops. TZX CSW
// recordings (0x18), call sequences (0x26/0x27) and blocks with a length
// field that libspectrum does not read are also accepted. ROM, turbo and
// pure-data blocks keep their bits in PZX DATA blocks; every other pulse is
// written to PULS blocks.
package spectrumtapes

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
)

const (
	// MinBytes and MaxBytes bound a converted image; they are the unit limits
	// of fes.media.spectrum-tape 2.0.
	MinBytes = 10
	MaxBytes = 16 << 20
	// MaxInputBytes bounds an image before conversion, and an inflated CSW.
	MaxInputBytes = 32 << 20
	// maxEdges stops a tape whose jumps or loops never reach its end.
	maxEdges = 1 << 26
)

// Extensions are the tape image names Convert reads.
var Extensions = []string{".tzx", ".tap", ".pzx", ".csw"}

// Admit reports whether name has a tape image extension.
func Admit(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// AdmitSize reports whether a source image of size bytes may be converted.
func AdmitSize(size int64) bool { return size > 0 && size <= MaxInputBytes }

// Recognize reports whether data reads as a tape image: a TZX, PZX or CSW
// signature, or .tap blocks that cover it exactly.
func Recognize(data []byte) bool {
	if !AdmitSize(int64(len(data))) {
		return false
	}
	_, err := read(data)
	return err == nil
}

// CheckedTAP reports whether data is a .tap image whose every block passes
// the ROM loader's check: a flag byte, the data and a checksum byte that XOR
// to zero. A file of snapshot size can also tile as .tap blocks; the host
// takes one whose blocks all check as a tape.
func CheckedTAP(data []byte) bool {
	if !AdmitSize(int64(len(data))) || bytes.HasPrefix(data, tzxSignature) ||
		bytes.HasPrefix(data, pzxSignature) || bytes.HasPrefix(data, cswSignature) {
		return false
	}
	blocks, err := readTAP(data)
	if err != nil {
		return false
	}
	for _, b := range blocks {
		sum := byte(0)
		for _, v := range b.data {
			sum ^= v
		}
		if len(b.data) < 2 || sum != 0 {
			return false
		}
	}
	return true
}

// Convert reads a tape image and returns its PZX stream. TZX, PZX and CSW
// images are known by their signatures; anything else must be a .tap image
// whose blocks cover it exactly.
func Convert(data []byte) ([]byte, error) {
	if !AdmitSize(int64(len(data))) {
		return nil, errors.New("tape image size is outside 1..32 MiB")
	}
	blocks, err := read(data)
	if err != nil {
		return nil, err
	}
	e, err := newEngine(blocks)
	if err != nil {
		return nil, err
	}
	m := newEmitter(MaxBytes)
	for edges := 0; ; edges++ {
		if edges == maxEdges {
			return nil, errors.New("tape does not reach its end")
		}
		if e.dataStart {
			b := e.block()
			m.addData(b.data, dataBits(b), e.bitZero(b), e.bitOne(b))
			e.dataStart = false
			e.state = statePause
			continue
		}
		t, flags, err := e.next()
		if err != nil {
			return nil, err
		}
		m.event(t, flags)
		if m.err != nil {
			return nil, m.err
		}
		if flags&flagTape != 0 {
			return m.out, nil
		}
	}
}

func read(data []byte) ([]block, error) {
	switch {
	case bytes.HasPrefix(data, tzxSignature):
		return readTZX(data)
	case bytes.HasPrefix(data, pzxSignature):
		return readPZX(data)
	case bytes.HasPrefix(data, cswSignature):
		return readCSW(data)
	default:
		blocks, err := readTAP(data)
		if err != nil {
			return nil, errors.New("not a .tap, .tzx, .pzx or .csw tape image")
		}
		return blocks, nil
	}
}

// dataBits is the number of bits a ROM, turbo or pure-data block plays.
func dataBits(b *block) int {
	if len(b.data) == 0 {
		return 0
	}
	last := 8
	if b.kind != kindROM {
		last = b.lastBits
	}
	return 8*(len(b.data)-1) + last
}

func (e *engine) bitZero(b *block) uint32 {
	if b.kind == kindROM {
		return romBit0
	}
	return b.bit0
}

func (e *engine) bitOne(b *block) uint32 {
	if b.kind == kindROM {
		return romBit1
	}
	return b.bit1
}
