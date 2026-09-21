// Package expansion links independently routed carts into a frozen Cyclone V
// shell. It contains no compiler, hardware access or session policy.
package expansion

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Geometry and framing are pinned to the DE10-Nano sx120f die and the ZX81
// ZX81 expansion bus v1. Callers cannot enlarge the permitted overlay rectangle.
const (
	cramWidth      = 7605
	cramHeight     = 7024
	frameBytes     = 916
	headerBytes    = 40408
	cramBytes      = (cramWidth*cramHeight + 7) / 8
	postambleFirst = 190
	postambleLast  = 412
	maxRBFBytes    = 32 * 1024 * 1024
)

var crcZones = [...]int{318, 1121, 2099, 3059, 3491, 4174, 4940, 5862, 6530, 7605, 0, 0}

type loadedRBF struct{ header, cram []byte }

var crc16Table = func() [256]uint16 {
	var table [256]uint16
	for value := range table {
		crc := uint16(value)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
		table[value] = crc
	}
	return table
}()

func crc16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, value := range data {
		crc = crc>>8 ^ crc16Table[byte(crc)^value]
	}
	return crc
}

var frameDataMask = func() [frameBytes]byte {
	var mask [frameBytes]byte
	for y := 32; y < cramHeight; y++ {
		pos := frameBit(y)
		mask[pos/8] |= 1 << (pos % 8)
	}
	return mask
}()

type framedRBF struct{ header, frames []byte }

func crc32Frame(data []byte) uint32 {
	crc := uint32(1)
	for bit := 0; bit < 32; bit++ {
		for block := 0; block < 220; block++ {
			pos := 224 + (31 - bit) + 32*block
			feedback := (uint32(data[pos/8]>>(pos%8)) ^ (crc >> 31)) & 1
			crc <<= 1
			if feedback != 0 {
				crc ^= 0xf4acfb13
			}
		}
	}
	return crc
}

func cramBit(data []byte, x, y int) byte {
	pos := x + cramWidth*y
	return (data[pos/8] >> (pos % 8)) & 1
}

func setCramBit(data []byte, x, y int, value byte) {
	pos := x + cramWidth*y
	mask := byte(1 << (pos % 8))
	data[pos/8] = (data[pos/8] &^ mask) | ((value & 1) << (pos % 8))
}

func frameBit(y int) int {
	return 224 + (31 ^ ((y + 16) / 220)) + 32*((y+16)%220)
}

func decompress(data []byte) ([]byte, int, error) {
	framed := make([]byte, cramWidth*frameBytes)
	nibble := 0
	read := func() (byte, error) {
		if nibble/2 >= len(data) {
			return 0, errors.New("truncated compressed CRAM")
		}
		value := (data[nibble/2] >> (4 * (nibble % 2))) & 15
		nibble++
		return value, nil
	}
	for offset := 0; offset < len(framed); offset += 2 {
		mask, err := read()
		if err != nil {
			return nil, 0, err
		}
		for part := 0; part < 4; part++ {
			if mask&(1<<part) == 0 {
				continue
			}
			value, err := read()
			if err != nil {
				return nil, 0, err
			}
			framed[offset+part/2] |= value << (4 * (part % 2))
		}
	}
	if nibble%2 != 0 && data[nibble/2]>>4 != 15 {
		return nil, 0, errors.New("invalid compressed CRAM padding")
	}
	return framed, (nibble + 1) / 2, nil
}

func compress(framed []byte) []byte {
	result := make([]byte, 0, len(framed)/3)
	high := false
	write := func(value byte) {
		if high {
			result[len(result)-1] |= value << 4
		} else {
			result = append(result, value&15)
		}
		high = !high
	}
	for offset := 0; offset < len(framed); offset += 2 {
		values := [4]byte{framed[offset] & 15, framed[offset] >> 4, framed[offset+1] & 15, framed[offset+1] >> 4}
		var mask byte
		for i, value := range values {
			if value != 0 {
				mask |= 1 << i
			}
		}
		write(mask)
		for _, value := range values {
			if value != 0 {
				write(value)
			}
		}
	}
	if high {
		write(15)
	}
	return result
}

func postamble() []byte {
	result := make([]byte, postambleFirst+2+12+postambleLast)
	result[0], result[1] = 0xec, 0x64
	binary.LittleEndian.PutUint16(result[postambleFirst:], crc16(result[:postambleFirst]))
	start := postambleFirst + 2
	result[start], result[start+1] = 0xae, 0xfb
	binary.LittleEndian.PutUint16(result[start+10:], crc16(result[start:start+10]))
	for i := start + 12; i < len(result); i++ {
		result[i] = 0xff
	}
	return result
}

// loadFrames validates the same canonical frame representation as packFrames,
// directly in wire order. Avoiding row/column bit transposition is important on
// the target ARM CPU; framing, padding, EDCRC and outer CRC remain checked.
func loadFrames(data []byte) (framedRBF, error) {
	if len(data) < headerBytes || len(data) > maxRBFBytes {
		return framedRBF{}, errors.New("invalid RBF size")
	}
	rest := data[headerBytes:]
	var framed []byte
	consumed := cramWidth * frameBytes
	if len(rest) != consumed+len(postamble()) {
		var err error
		framed, consumed, err = decompress(rest)
		if err != nil {
			return framedRBF{}, err
		}
	} else {
		framed = bytes.Clone(rest[:consumed])
	}
	if !bytes.Equal(rest[consumed:], postamble()) {
		return framedRBF{}, errors.New("invalid RBF postamble")
	}
	zoneIndex := 0
	for x := 0; x < cramWidth; x++ {
		frame := framed[x*frameBytes : (x+1)*frameBytes]
		var expected [frameBytes]byte
		for i, mask := range frameDataMask {
			expected[i] = frame[i] & mask
		}
		if x == 0 {
			expected[0], expected[1], expected[2] = 0x84, 0x3e, 0x01
		}
		if x == cramWidth-1 {
			expected[0], expected[1], expected[2] = 0x42, 0x9f, 0
		}
		zone := crcZones[zoneIndex]
		if x < zone {
			binary.LittleEndian.PutUint32(expected[frameBytes-8:], crc32Frame(expected[:]))
		}
		if x == zone+255 {
			zoneIndex++
		}
		binary.LittleEndian.PutUint16(expected[frameBytes-2:], crc16(expected[:frameBytes-2]))
		if !bytes.Equal(frame, expected[:]) {
			return framedRBF{}, fmt.Errorf("invalid CRAM framing, padding or CRC/EDCRC at column %d", x)
		}
	}
	return framedRBF{header: bytes.Clone(data[:headerBytes]), frames: framed}, nil
}

func loadRBF(data []byte) (loadedRBF, error) {
	framed, err := loadFrames(data)
	if err != nil {
		return loadedRBF{}, err
	}
	cram := make([]byte, cramBytes)
	for x := 0; x < cramWidth; x++ {
		frame := framed.frames[x*frameBytes : (x+1)*frameBytes]
		for y := 32; y < cramHeight; y++ {
			pos := frameBit(y)
			if frame[pos/8]&(1<<(pos%8)) != 0 {
				setCramBit(cram, x, y, 1)
			}
		}
	}
	return loadedRBF{header: framed.header, cram: cram}, nil
}

func packFrames(cram []byte) []byte {
	framed := make([]byte, cramWidth*frameBytes)
	framed[0], framed[1], framed[2] = 0x84, 0x3e, 0x01
	last := (cramWidth - 1) * frameBytes
	framed[last], framed[last+1], framed[last+2] = 0x42, 0x9f, 0
	zoneIndex := 0
	for x := 0; x < cramWidth; x++ {
		frame := framed[x*frameBytes : (x+1)*frameBytes]
		for y := 32; y < cramHeight; y++ {
			if cramBit(cram, x, y) == 0 {
				continue
			}
			pos := frameBit(y)
			frame[pos/8] |= 1 << (pos % 8)
		}
		zone := crcZones[zoneIndex]
		if x < zone {
			binary.LittleEndian.PutUint32(frame[frameBytes-8:], crc32Frame(frame))
		}
		if x == zone+255 {
			zoneIndex++
		}
		binary.LittleEndian.PutUint16(frame[frameBytes-2:], crc16(frame[:frameBytes-2]))
	}
	return framed
}

func saveRBF(loaded loadedRBF) []byte {
	framed := packFrames(loaded.cram)
	result := bytes.Clone(loaded.header)
	result = append(result, compress(framed)...)
	return append(result, postamble()...)
}

func insideSocket(x, y int) bool { return x >= 1769 && x < 2806 && y >= 32 && y < cramHeight }

func crcCompanionColumn(x int) bool {
	// Same pinned non-routing CRC strips as cyclonev_rbf.SX120F.
	return (x >= 3488 && x < 3847) || (x >= 3921 && x < 3980) || (x >= 4171 && x < 4471)
}

// Link overlays the fixed ZX81 expansion bus. Every non-CRC change outside that
// region, any ORAM/PRAM header change, malformed framing or CRC rejects before
// returning an artifact. Caller-owned input slices are never modified.
func Link(shell, cart []byte) ([]byte, error) {
	base, err := loadFrames(shell)
	if err != nil {
		return nil, fmt.Errorf("shell: %w", err)
	}
	addition, err := loadFrames(cart)
	if err != nil {
		return nil, fmt.Errorf("cart: %w", err)
	}
	if !bytes.Equal(base.header, addition.header) {
		return nil, errors.New("cart changes shell ORAM/PRAM header")
	}
	for x := 0; x < cramWidth; x++ {
		before := base.frames[x*frameBytes : (x+1)*frameBytes]
		after := addition.frames[x*frameBytes : (x+1)*frameBytes]
		if insideSocket(x, 32) {
			copy(before, after)
			continue
		}
		if crcCompanionColumn(x) || bytes.Equal(before, after) {
			continue
		}
		for y := 32; y < cramHeight; y++ {
			pos := frameBit(y)
			if (before[pos/8]^after[pos/8])&(1<<(pos%8)) != 0 {
				return nil, fmt.Errorf("cart changes CRAM outside socket at %d,%d", x, y)
			}
		}
	}
	result := append(base.header, compress(base.frames)...)
	return append(result, postamble()...), nil
}
