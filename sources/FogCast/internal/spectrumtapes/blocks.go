package spectrumtapes

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// tstatesPerMS converts TZX and TAP pause milliseconds, as libspectrum does.
const tstatesPerMS = 3500

type blockKind int

// The block kinds of libspectrum's tape model, plus the TZX call and return
// blocks it does not read.
const (
	kindROM blockKind = iota
	kindTurbo
	kindTone
	kindPulses
	kindPureData
	kindRawData
	kindGeneralised
	kindPause
	kindJump
	kindLoopStart
	kindLoopEnd
	kindCall
	kindReturn
	kindStop48
	kindSetLevel
	kindMetadata
	kindRLE
	kindPulseSequence
	kindDataBlock
)

type symbol struct {
	edge    byte
	lengths []uint32
}

type symbolTable struct {
	inBlock   uint32 // symbols in the block (TOTP or TOTD)
	maxPulses int    // NPP or NPD
	inTable   int    // ASP or ASD; 0 means 256 when the block has symbols
	symbols   []symbol
}

type block struct {
	kind  blockKind
	data  []byte
	pause uint32 // tail pause, T states

	// ROM, turbo, pure and raw data.
	pilot, sync1, sync2, bit0, bit1 uint32
	pilotPulses                     int
	lastBits                        int
	rawSample                       uint32

	// Pure tone and pulse sequences.
	length  uint32
	count   int
	lengths []uint32
	repeats []uint32

	// Pause and set-level: -1 leaves the level to the pause edge.
	level int

	// Jump, loop and call blocks.
	offset  int
	offsets []int

	// Generalised data.
	pilotTable, dataTable symbolTable
	pilotSymbols          []byte
	pilotRepeats          []uint16
	symbolBits            int

	// RLE pulses (CSW): T states per sample. A TZX CSW block ends with the
	// TZX tail pause; a CSW file has none.
	scale     uint32
	tailPause bool

	// PZX data blocks.
	bitCount   uint32
	tail       uint32
	bit0Pulses []uint32
	bit1Pulses []uint32
}

var errCorrupt = errors.New("corrupt tape image")

type reader struct {
	b []byte
	p int
}

func (r *reader) left() int { return len(r.b) - r.p }

func (r *reader) need(n int) error {
	if n < 0 || r.left() < n {
		return errCorrupt
	}
	return nil
}

func (r *reader) u8() byte { v := r.b[r.p]; r.p++; return v }

func (r *reader) u16() uint32 {
	v := binary.LittleEndian.Uint16(r.b[r.p:])
	r.p += 2
	return uint32(v)
}

func (r *reader) u24() uint32 {
	v := uint32(r.b[r.p]) | uint32(r.b[r.p+1])<<8 | uint32(r.b[r.p+2])<<16
	r.p += 3
	return v
}

func (r *reader) u32() uint32 {
	v := binary.LittleEndian.Uint32(r.b[r.p:])
	r.p += 4
	return v
}

func (r *reader) bytes(n int) ([]byte, error) {
	if err := r.need(n); err != nil {
		return nil, err
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v, nil
}

// lastByteBits applies libspectrum's handling of a TZX "used bits in the
// last byte" field: 0 drops the last byte, more than 8 means 8. Pure data
// drops it only when more than one byte remains; libspectrum then plays
// zero bits forever from a single byte, so here that byte has 8 bits.
func lastByteBits(bitsUsed int, data []byte, minimum int) (int, []byte) {
	if bitsUsed == 0 && len(data) >= minimum {
		bitsUsed = 8
		data = data[:len(data)-1]
	}
	if bitsUsed == 0 || bitsUsed > 8 {
		bitsUsed = 8
	}
	return bitsUsed, data
}

func readTAP(b []byte) ([]block, error) {
	r := &reader{b: b}
	var blocks []block
	for r.left() > 0 {
		if err := r.need(2); err != nil {
			return nil, err
		}
		data, err := r.bytes(int(r.u16()))
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block{kind: kindROM, data: data, pause: 1000 * tstatesPerMS})
	}
	return blocks, nil
}

var tzxSignature = []byte("ZXTape!\x1a")

func readTZX(b []byte) ([]block, error) {
	if len(b) < len(tzxSignature)+2 || !bytes.Equal(b[:len(tzxSignature)], tzxSignature) {
		return nil, errors.New("not a TZX image")
	}
	r := &reader{b: b, p: len(tzxSignature) + 2}
	var blocks []block
	for r.left() > 0 {
		id := r.u8()
		blk, keep, err := readTZXBlock(r, id)
		if err != nil {
			return nil, fmt.Errorf("TZX block 0x%02x: %w", id, err)
		}
		if keep {
			blocks = append(blocks, blk)
		}
	}
	return blocks, nil
}

func readString(r *reader) error {
	if err := r.need(1); err != nil {
		return err
	}
	_, err := r.bytes(int(r.u8()))
	return err
}

// readTZXBlock reads one block after its ID as libspectrum's tzx_read.c
// does, and additionally reads CSW (0x18), call and return (0x26, 0x27) and
// skips the blocks libspectrum rejects, by their length field.
func readTZXBlock(r *reader, id byte) (block, bool, error) {
	switch id {
	case 0x10:
		if err := r.need(4); err != nil {
			return block{}, false, err
		}
		pause := r.u16()
		data, err := r.bytes(int(r.u16()))
		return block{kind: kindROM, data: data, pause: pause * tstatesPerMS}, true, err
	case 0x11:
		if err := r.need(18); err != nil {
			return block{}, false, err
		}
		blk := block{kind: kindTurbo, pilot: r.u16(), sync1: r.u16(), sync2: r.u16(), bit0: r.u16(), bit1: r.u16()}
		blk.pilotPulses = int(r.u16())
		used := int(r.u8())
		blk.pause = r.u16() * tstatesPerMS
		data, err := r.bytes(int(r.u24()))
		if err != nil {
			return block{}, false, err
		}
		blk.lastBits, blk.data = lastByteBits(used, data, 1)
		return blk, true, nil
	case 0x12:
		if err := r.need(4); err != nil {
			return block{}, false, err
		}
		return block{kind: kindTone, length: r.u16(), count: int(r.u16())}, true, nil
	case 0x13:
		if err := r.need(1); err != nil {
			return block{}, false, err
		}
		count := int(r.u8())
		if err := r.need(2 * count); err != nil {
			return block{}, false, err
		}
		blk := block{kind: kindPulses, count: count, lengths: make([]uint32, count)}
		for i := range blk.lengths {
			blk.lengths[i] = r.u16()
		}
		return blk, true, nil
	case 0x14:
		if err := r.need(10); err != nil {
			return block{}, false, err
		}
		blk := block{kind: kindPureData, bit0: r.u16(), bit1: r.u16()}
		used := int(r.u8())
		blk.pause = r.u16() * tstatesPerMS
		data, err := r.bytes(int(r.u24()))
		if err != nil {
			return block{}, false, err
		}
		blk.lastBits, blk.data = lastByteBits(used, data, 2)
		return blk, true, nil
	case 0x15:
		if err := r.need(8); err != nil {
			return block{}, false, err
		}
		blk := block{kind: kindRawData, rawSample: r.u16()}
		blk.pause = r.u16() * tstatesPerMS
		used := int(r.u8())
		data, err := r.bytes(int(r.u24()))
		if err != nil {
			return block{}, false, err
		}
		blk.lastBits, blk.data = lastByteBits(used, data, 1)
		return blk, true, nil
	case 0x18:
		return readTZXCSW(r)
	case 0x19:
		return readGeneralised(r)
	case 0x20:
		if err := r.need(2); err != nil {
			return block{}, false, err
		}
		return block{kind: kindPause, pause: r.u16() * tstatesPerMS, level: -1}, true, nil
	case 0x21, 0x30:
		return block{kind: kindMetadata}, true, readString(r)
	case 0x22:
		return block{kind: kindMetadata}, true, nil
	case 0x25:
		return block{kind: kindLoopEnd}, true, nil
	case 0x27:
		return block{kind: kindReturn}, true, nil
	case 0x23:
		if err := r.need(2); err != nil {
			return block{}, false, err
		}
		return block{kind: kindJump, offset: int(int16(r.u16()))}, true, nil
	case 0x24:
		if err := r.need(2); err != nil {
			return block{}, false, err
		}
		return block{kind: kindLoopStart, count: int(r.u16())}, true, nil
	case 0x26:
		if err := r.need(2); err != nil {
			return block{}, false, err
		}
		count := int(r.u16())
		if err := r.need(2 * count); err != nil {
			return block{}, false, err
		}
		blk := block{kind: kindCall, offsets: make([]int, count)}
		for i := range blk.offsets {
			blk.offsets[i] = int(int16(r.u16()))
		}
		return blk, true, nil
	case 0x28:
		if err := r.need(3); err != nil {
			return block{}, false, err
		}
		if err := r.need(int(r.u16())); err != nil {
			return block{}, false, err
		}
		count := int(r.u8())
		for i := 0; i < count; i++ {
			if err := r.need(3); err != nil {
				return block{}, false, err
			}
			r.p += 2
			if err := readString(r); err != nil {
				return block{}, false, err
			}
		}
		return block{kind: kindMetadata}, true, nil
	case 0x2a:
		if err := r.need(4); err != nil {
			return block{}, false, err
		}
		r.p += 4
		return block{kind: kindStop48}, true, nil
	case 0x2b:
		if err := r.need(5); err != nil {
			return block{}, false, err
		}
		r.p += 4
		level := 0
		if r.u8() != 0 {
			level = 1
		}
		return block{kind: kindSetLevel, level: level}, true, nil
	case 0x31:
		if err := r.need(2); err != nil {
			return block{}, false, err
		}
		r.p++
		return block{kind: kindMetadata}, true, readString(r)
	case 0x32:
		if err := r.need(3); err != nil {
			return block{}, false, err
		}
		r.p += 2
		count := int(r.u8())
		for i := 0; i < count; i++ {
			if err := r.need(2); err != nil {
				return block{}, false, err
			}
			r.p++
			if err := readString(r); err != nil {
				return block{}, false, err
			}
		}
		return block{kind: kindMetadata}, true, nil
	case 0x33:
		if err := r.need(1); err != nil {
			return block{}, false, err
		}
		_, err := r.bytes(3 * int(r.u8()))
		return block{kind: kindMetadata}, true, err
	case 0x34:
		_, err := r.bytes(8)
		return block{kind: kindMetadata}, true, err
	case 0x35:
		if err := r.need(20); err != nil {
			return block{}, false, err
		}
		r.p += 16
		_, err := r.bytes(int(r.u32()))
		return block{kind: kindMetadata}, true, err
	case 0x40:
		if err := r.need(4); err != nil {
			return block{}, false, err
		}
		r.p++
		_, err := r.bytes(int(r.u24()))
		return block{kind: kindMetadata}, true, err
	case 0x5a:
		// The glue block repeats the rest of the signature and the version.
		_, err := r.bytes(len(tzxSignature) + 2 - 1)
		return block{}, false, err
	default:
		// TZX 1.10 and later give every new block a 32-bit length; the
		// deprecated C64 blocks 0x16 and 0x17 do too. Skipped blocks still
		// count for jump and call offsets.
		if err := r.need(4); err != nil {
			return block{}, false, err
		}
		_, err := r.bytes(int(r.u32()))
		return block{kind: kindMetadata}, true, err
	}
}

func readSymbolTable(r *reader, table *symbolTable, length int) (int, error) {
	if table.inBlock == 0 {
		return 0, nil
	}
	if table.maxPulses == 0 {
		return 0, errors.New("zero pulses per symbol")
	}
	size := (2*table.maxPulses + 1) * table.inTable
	if length < size || r.need(size) != nil {
		return 0, errCorrupt
	}
	table.symbols = make([]symbol, table.inTable)
	for i := range table.symbols {
		table.symbols[i].edge = r.u8()
		table.symbols[i].lengths = make([]uint32, table.maxPulses)
		for j := range table.symbols[i].lengths {
			table.symbols[i].lengths[j] = r.u16()
		}
	}
	return size, nil
}

func readTableParameters(r *reader, table *symbolTable) {
	table.inBlock = r.u32()
	table.maxPulses = int(r.u8())
	table.inTable = int(r.u8())
	if table.inTable == 0 && table.inBlock != 0 {
		table.inTable = 256
	}
}

// readGeneralised follows libspectrum's tzx_read_generalised_data.
func readGeneralised(r *reader) (block, bool, error) {
	if err := r.need(4); err != nil {
		return block{}, false, err
	}
	length := int(r.u32())
	end := r.p + length
	if length < 14 || r.need(length) != nil {
		return block{}, false, errCorrupt
	}
	blk := block{kind: kindGeneralised}
	blk.pause = r.u16() * tstatesPerMS
	readTableParameters(r, &blk.pilotTable)
	readTableParameters(r, &blk.dataTable)
	length -= 14
	used, err := readSymbolTable(r, &blk.pilotTable, length)
	if err != nil {
		return block{}, false, err
	}
	length -= used
	count := int(blk.pilotTable.inBlock)
	if count > length/3 {
		return block{}, false, errCorrupt
	}
	blk.pilotSymbols = make([]byte, count)
	blk.pilotRepeats = make([]uint16, count)
	for i := 0; i < count; i++ {
		blk.pilotSymbols[i] = r.u8()
		blk.pilotRepeats[i] = uint16(r.u16())
		if int(blk.pilotSymbols[i]) >= blk.pilotTable.inTable {
			return block{}, false, errors.New("pilot symbol outside alphabet")
		}
	}
	length -= 3 * count
	used, err = readSymbolTable(r, &blk.dataTable, length)
	if err != nil {
		return block{}, false, err
	}
	length -= used
	if blk.dataTable.inTable > 0 {
		blk.symbolBits = bits.Len(uint(blk.dataTable.inTable - 1))
	}
	dataBytes := (uint64(blk.symbolBits)*uint64(blk.dataTable.inBlock) + 7) / 8
	if dataBytes > uint64(length) {
		return block{}, false, errors.New("data extends beyond end of block")
	}
	blk.data = r.b[r.p : r.p+int(dataBytes)]
	r.p += int(dataBytes)
	if r.p != end {
		return block{}, false, errors.New("generalised data block size mismatch")
	}
	return blk, true, nil
}

// readTZXCSW reads TZX block 0x18, a CSW recording, as an RLE pulse block
// with the TZX tail pause. libspectrum 1.5 does not read this block.
func readTZXCSW(r *reader) (block, bool, error) {
	if err := r.need(4); err != nil {
		return block{}, false, err
	}
	length := int(r.u32())
	body, err := r.bytes(length)
	if err != nil || length < 10 {
		return block{}, false, errCorrupt
	}
	br := &reader{b: body}
	pause := br.u16() * tstatesPerMS
	rate := br.u24()
	compression := br.u8()
	br.u32() // pulse count after decompression
	data, err := cswPulses(body[br.p:], compression == 2, compression)
	if err != nil {
		return block{}, false, err
	}
	return block{kind: kindRLE, data: data, scale: cswScale(rate), pause: pause, tailPause: true}, true, nil
}

func cswScale(rate uint32) uint32 {
	if rate == 0 {
		return 0
	}
	return 3500000 / rate
}

func cswPulses(data []byte, zrle bool, compression byte) ([]byte, error) {
	if compression != 1 && compression != 2 {
		return nil, errors.New("unknown CSW compression")
	}
	if !zrle {
		return data, nil
	}
	z, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	out, err := io.ReadAll(io.LimitReader(z, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > MaxInputBytes {
		return nil, errTooLarge
	}
	return out, nil
}

var cswSignature = []byte("Compressed Square Wave\x1a")

// readCSW follows libspectrum's csw.c: version 1 or 2, RLE or Z-RLE, the
// sample period truncated to whole T states.
func readCSW(b []byte) ([]block, error) {
	if len(b) < len(cswSignature)+2 || !bytes.Equal(b[:len(cswSignature)], cswSignature) {
		return nil, errors.New("not a CSW image")
	}
	r := &reader{b: b, p: len(cswSignature)}
	var rate uint32
	var compression byte
	switch r.b[r.p] {
	case 1:
		if r.need(9) != nil {
			return nil, errCorrupt
		}
		rate = uint32(r.b[r.p+2]) | uint32(r.b[r.p+3])<<8
		compression = r.b[r.p+4]
		if compression != 1 {
			return nil, errors.New("unknown CSW compression")
		}
		r.p += 9
	case 2:
		if r.need(29) != nil {
			return nil, errCorrupt
		}
		rate = binary.LittleEndian.Uint32(r.b[r.p+2:])
		compression = r.b[r.p+10]
		extension := int(r.b[r.p+12])
		if r.need(29+extension) != nil {
			return nil, errCorrupt
		}
		r.p += 29 + extension
	default:
		return nil, errors.New("unknown CSW version")
	}
	scale := cswScale(rate)
	if scale >= 0x80000 {
		return nil, errors.New("bad CSW sample rate")
	}
	if r.left() == 0 {
		return nil, errors.New("empty CSW image")
	}
	data, err := cswPulses(r.b[r.p:], compression == 2, compression)
	if err != nil {
		return nil, err
	}
	return []block{{kind: kindRLE, data: data, scale: scale}}, nil
}

var pzxSignature = []byte("PZXT")

// readPZX reads PZX 1.x blocks. Unlike libspectrum it always resumes at the
// end of each block, as the format requires.
func readPZX(b []byte) ([]block, error) {
	if len(b) < 8 || !bytes.Equal(b[:4], pzxSignature) {
		return nil, errors.New("not a PZX image")
	}
	r := &reader{b: b}
	var blocks []block
	for r.left() > 0 {
		if err := r.need(8); err != nil {
			return nil, err
		}
		tag := string(r.b[r.p : r.p+4])
		r.p += 4
		body, err := r.bytes(int(r.u32()))
		if err != nil {
			return nil, err
		}
		br := &reader{b: body}
		switch tag {
		case "PZXT":
			if len(body) < 2 || body[0] != 1 {
				return nil, errors.New("unsupported PZX version")
			}
		case "PULS":
			blk := block{kind: kindPulseSequence}
			for br.left() > 0 {
				if br.need(2) != nil {
					return nil, errCorrupt
				}
				repeats, length := uint32(1), br.u16()
				if length > 0x8000 {
					if br.need(2) != nil {
						return nil, errCorrupt
					}
					repeats, length = length&0x7fff, br.u16()
				}
				if length >= 0x8000 {
					if br.need(2) != nil {
						return nil, errCorrupt
					}
					length = (length&0x7fff)<<16 | br.u16()
				}
				blk.lengths = append(blk.lengths, length)
				blk.repeats = append(blk.repeats, repeats)
			}
			if len(blk.lengths) == 0 {
				return nil, errors.New("empty PZX PULS block")
			}
			blocks = append(blocks, blk)
		case "DATA":
			if br.need(8) != nil {
				return nil, errCorrupt
			}
			count := br.u32()
			blk := block{kind: kindDataBlock, bitCount: count & 0x7fffffff, level: int(count >> 31), tail: br.u16()}
			p0, p1 := int(br.u8()), int(br.u8())
			if br.need(2*(p0+p1)) != nil {
				return nil, errCorrupt
			}
			for i := 0; i < p0; i++ {
				blk.bit0Pulses = append(blk.bit0Pulses, br.u16())
			}
			for i := 0; i < p1; i++ {
				blk.bit1Pulses = append(blk.bit1Pulses, br.u16())
			}
			if blk.data, err = br.bytes(int((uint64(blk.bitCount) + 7) / 8)); err != nil {
				return nil, err
			}
			blocks = append(blocks, blk)
		case "PAUS":
			if br.need(4) != nil {
				return nil, errCorrupt
			}
			v := br.u32()
			blocks = append(blocks, block{kind: kindPause, pause: v & 0x7fffffff, level: int(v >> 31)})
		case "STOP":
			if br.need(2) != nil {
				return nil, errCorrupt
			}
			if br.u16() == 1 {
				blocks = append(blocks, block{kind: kindStop48})
			} else {
				blocks = append(blocks, block{kind: kindPause, level: -1})
			}
		case "BRWS":
			blocks = append(blocks, block{kind: kindMetadata})
		}
	}
	return blocks, nil
}
