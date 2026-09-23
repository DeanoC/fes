package expansion

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// ROMMap is producer metadata for an exact blank bitstream. It is not an
// authorization boundary: the caller must obtain it from a trusted producer
// (eventually a sealed package), never from a ROM/content upload. Coordinates
// are linear CRAM addresses, not offsets into the compressed RBF.
type ROMMap struct {
	Format     int        `json:"format"`
	Device     string     `json:"device"`
	Encoding   string     `json:"encoding"`
	BaseSHA256 string     `json:"base_sha256"`
	SourceSize int        `json:"source_size"`
	Blocks     []ROMBlock `json:"blocks"`
}

// ROMBlock describes a 1024-byte ROM in one 1024x10 M10K. WordBits contains
// 256 words of 40 physical destinations in Mistral stored-bit order.
type ROMBlock struct {
	BEL          string     `json:"bel"`
	SourceOffset int        `json:"source_offset"`
	WordBits     [][]uint32 `json:"word_bits"`
}

var initPermutation = [...]uint{0, 20, 10, 30, 1, 21, 11, 31, 2, 22, 12, 32, 3, 23, 13, 33, 4, 24, 14, 34, 5, 25, 15, 35, 6, 26, 16, 36, 7, 27, 17, 37, 8, 28, 18, 38, 9, 29, 19, 39}

// LinkROM patches a producer-described blank ROM, preserving all other CRAM
// bits and the ORAM/PRAM header. It returns a full RBF with the input compression mode.
// Inputs remain caller-owned and must not be mutated concurrently. The current
// encoding supports up to 256 KiB, with two zero padding bits per logical byte.
func LinkROM(ctx context.Context, base []byte, m ROMMap, rom []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(base) < headerBytes || len(base) > maxRBFBytes {
		return nil, fmt.Errorf("invalid RBF size")
	}
	if err := validateROMMap(ctx, m, fmt.Sprintf("%x", sha256.Sum256(base)), len(rom)); err != nil {
		return nil, err
	}
	return patchROM(ctx, base, m, rom)
}

func patchROM(ctx context.Context, base []byte, m ROMMap, rom []byte) ([]byte, error) {
	framed, err := loadFramesContext(ctx, base)
	if err != nil {
		return nil, err
	}
	dirty := make([]bool, cramWidth)
	for _, b := range m.Blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for w, bits := range b.WordBits {
			var logical uint64
			for lane := 0; lane < 4; lane++ {
				logical |= uint64(rom[b.SourceOffset+4*w+lane]) << uint(10*lane)
			}
			for bit, p := range bits {
				x, y := int(p)%cramWidth, int(p)/cramWidth
				pos := x*frameBytes + frameBit(y)/8
				mask := byte(1 << uint(frameBit(y)%8))
				if framed.frames[pos]&mask == 0 {
					return nil, fmt.Errorf("ROM destination is not blank at %d,%d", x, y)
				}
				// Mistral stores the permuted complement of the padded logical word.
				if logical&(1<<initPermutation[bit]) != 0 {
					framed.frames[pos] &^= mask
					dirty[x] = true
				}
			}
		}
	}
	zoneIndex := 0
	for x, changed := range dirty {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		zone := crcZones[zoneIndex]
		if changed {
			frame := framed.frames[x*frameBytes : (x+1)*frameBytes]
			if x < zone {
				binary.LittleEndian.PutUint32(frame[frameBytes-8:], crc32Frame(frame))
			}
			binary.LittleEndian.PutUint16(frame[frameBytes-2:], crc16(frame[:frameBytes-2]))
		}
		if x == zone+255 {
			zoneIndex++
		}
	}
	packed := framed.frames
	if len(base) != headerBytes+cramWidth*frameBytes+len(postamble()) {
		var err error
		packed, err = compressContext(ctx, framed.frames)
		if err != nil {
			return nil, err
		}
	}
	out := append(framed.header, packed...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append(out, postamble()...), nil
}

func validateROMMap(ctx context.Context, m ROMMap, baseSHA256 string, sourceSize int) error {
	if m.Format != 1 || m.Device != Device || m.Encoding != "m10k-1024x10-v1" {
		return fmt.Errorf("unsupported ROM map format, device or encoding")
	}
	if len(m.Blocks) == 0 || len(m.Blocks) > 256 || m.SourceSize != len(m.Blocks)*1024 || sourceSize != m.SourceSize {
		return fmt.Errorf("invalid ROM size or block count")
	}
	if baseSHA256 != m.BaseSHA256 {
		return fmt.Errorf("ROM map base SHA256 mismatch")
	}
	seen := make([]byte, cramBytes)
	sources := make([]bool, len(m.Blocks))
	bels := make(map[string]bool, len(m.Blocks))
	for _, b := range m.Blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.BEL == "" || len(b.BEL) > 64 || bels[b.BEL] {
			return fmt.Errorf("invalid or duplicate ROM BEL")
		}
		for _, r := range b.BEL {
			if r < 32 || (r >= 127 && r <= 159) {
				return fmt.Errorf("ROM BEL contains control character")
			}
		}
		bels[b.BEL] = true
		if b.SourceOffset < 0 || b.SourceOffset%1024 != 0 || b.SourceOffset > m.SourceSize-1024 || sources[b.SourceOffset/1024] {
			return fmt.Errorf("invalid or overlapping ROM source range")
		}
		sources[b.SourceOffset/1024] = true
		if len(b.WordBits) != 256 {
			return fmt.Errorf("ROM block must have 256 words")
		}
		for _, bits := range b.WordBits {
			if len(bits) != 40 {
				return fmt.Errorf("ROM word must have 40 destinations")
			}
			for _, p := range bits {
				if p < 32*cramWidth || p >= cramWidth*cramHeight {
					return fmt.Errorf("ROM destination outside CRAM data")
				}
				mask := byte(1 << (p % 8))
				if seen[p/8]&mask != 0 {
					return fmt.Errorf("overlapping ROM destinations")
				}
				seen[p/8] |= mask
			}
		}
	}
	return nil
}

// ComposeROM binds the ROM map to the original sealed shell, rejects all ROM
// destinations in the expansion socket, and patches a freshly verified overlay.
// It returns both the independently composed overlay and final programmed RBF.
func ComposeROM(ctx context.Context, shell Shell, asset Asset, m ROMMap, rom []byte) (Composition, []byte, []byte, error) {
	if err := validateROMMap(ctx, m, hash(shell.Payload), len(rom)); err != nil {
		return Composition{}, nil, nil, err
	}
	for _, block := range m.Blocks {
		for _, bits := range block.WordBits {
			for _, p := range bits {
				if insideSocket(int(p)%cramWidth, int(p)/cramWidth) {
					return Composition{}, nil, nil, fmt.Errorf("ROM destination overlaps expansion socket")
				}
			}
		}
	}
	composition, overlay, err := ComposeContext(ctx, shell, asset)
	if err != nil {
		return Composition{}, nil, nil, err
	}
	programmed, err := patchROM(ctx, overlay, m, rom)
	if err != nil {
		return Composition{}, nil, nil, err
	}
	return composition, overlay, programmed, nil
}
