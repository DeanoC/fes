// Package zx81keys maps FogCast keyboard codes onto the ZX81 8×5 matrix.
package zx81keys

import "github.com/DeanoC/FogCast/remoteinput"

// Neutral is eight ULA rows with every key up (active-low 0x1f).
const Neutral uint64 = 0xffffffffff

// Codes are in a dedicated range so they do not collide with tenfoot debug keys.
const (
	KeyShift  remoteinput.Code = 256
	KeyEnter  remoteinput.Code = 257
	KeySpace  remoteinput.Code = 258
	KeyPeriod remoteinput.Code = 259
	KeyA      remoteinput.Code = 260
	KeyZ      remoteinput.Code = 285
	Key0      remoteinput.Code = 286
	Key9      remoteinput.Code = 295
)

type cell struct {
	row, bit uint8
}

// Sinclair ULA rows, bit 0 = leftmost key on that row.
var cells = map[remoteinput.Code]cell{
	KeyShift:    {0, 0},
	Letter('Z'): {0, 1},
	Letter('X'): {0, 2},
	Letter('C'): {0, 3},
	Letter('V'): {0, 4},
	Letter('A'): {1, 0},
	Letter('S'): {1, 1},
	Letter('D'): {1, 2},
	Letter('F'): {1, 3},
	Letter('G'): {1, 4},
	Letter('Q'): {2, 0},
	Letter('W'): {2, 1},
	Letter('E'): {2, 2},
	Letter('R'): {2, 3},
	Letter('T'): {2, 4},
	Digit(1):    {3, 0},
	Digit(2):    {3, 1},
	Digit(3):    {3, 2},
	Digit(4):    {3, 3},
	Digit(5):    {3, 4},
	Digit(0):    {4, 0},
	Digit(9):    {4, 1},
	Digit(8):    {4, 2},
	Digit(7):    {4, 3},
	Digit(6):    {4, 4},
	Letter('P'): {5, 0},
	Letter('O'): {5, 1},
	Letter('I'): {5, 2},
	Letter('U'): {5, 3},
	Letter('Y'): {5, 4},
	KeyEnter:    {6, 0},
	Letter('L'): {6, 1},
	Letter('K'): {6, 2},
	Letter('J'): {6, 3},
	Letter('H'): {6, 4},
	KeySpace:    {7, 0},
	KeyPeriod:   {7, 1},
	Letter('M'): {7, 2},
	Letter('N'): {7, 3},
	Letter('B'): {7, 4},
}

// Letter returns the FogCast code for A–Z.
func Letter(ch byte) remoteinput.Code {
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}
	if ch < 'A' || ch > 'Z' {
		return 0
	}
	return KeyA + remoteinput.Code(ch-'A')
}

// Digit returns the FogCast code for 0–9.
func Digit(n byte) remoteinput.Code {
	if n > 9 {
		return 0
	}
	return Key0 + remoteinput.Code(n)
}

// Name is the inverse of Letter/Digit and the named matrix keys.
func Name(code remoteinput.Code) (string, bool) {
	switch code {
	case KeyShift:
		return "shift", true
	case KeyEnter:
		return "return", true
	case KeySpace:
		return "space", true
	case KeyPeriod:
		return "period", true
	}
	if code >= KeyA && code <= KeyZ {
		return string(rune('a' + (code - KeyA))), true
	}
	if code >= Key0 && code <= Key9 {
		return string(rune('0' + (code - Key0))), true
	}
	return "", false
}

// Matrix packs pressed keys into the 40-bit active-low ULA matrix.
func Matrix(pressed map[remoteinput.Code]bool) uint64 {
	matrix := Neutral
	for code, down := range pressed {
		if !down {
			continue
		}
		cell, ok := cells[code]
		if !ok {
			continue
		}
		matrix &^= 1 << (uint64(cell.row)*5 + uint64(cell.bit))
	}
	return matrix
}
