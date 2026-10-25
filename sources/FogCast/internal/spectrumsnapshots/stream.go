package spectrumsnapshots

// Stream writes the restore stream that loads s.
//
// A 48K snapshot on a paged core selects the 48 BASIC ROM and locks paging.
// A 128K snapshot's ROM 1 (48 BASIC) is ROM 3 on the +2A/+3.
func Stream(s *Snapshot) []byte {
	p7ffd, p1ffd := s.Port7FFD, s.Port1FFD&0x07
	switch s.Machine {
	case Machine48K:
		p7ffd, p1ffd = 0x30, 0x04
	case Machine128K:
		p1ffd = p7ffd >> 2 & 0x04
	}
	w := &writer{}
	w.out = append(w.out, Magic[:]...)
	w.out = append(w.out, Version, byte(s.Machine), 0, 0)
	w.op(0xf3) // DI
	if s.Machine == Machine48K {
		w.fill(0x4000, s.Banks[5][:])
		w.fill(0x8000, s.Banks[2][:])
		w.fill(0xc000, s.Banks[0][:])
	} else {
		for bank := 0; bank < 8; bank++ {
			w.out16(0x7ffd, uint8(bank))
			w.fill(0xc000, s.Banks[bank][:])
		}
	}
	w.op(0x3e, s.Border&7, 0xd3, 0xfe) // LD A,border; OUT ($FE),A
	if s.HasAY {
		for r := 0; r < 16; r++ {
			w.out16(0xfffd, uint8(r))
			w.op(0x06, 0xbf, 0x3e, s.AY[r], 0xed, 0x79) // LD B,$BF; LD A,v; OUT (C),A
		}
		w.out16(0xfffd, s.AYSelect&0x0f)
	}
	// $1FFD before $7FFD, whose bit 5 locks both.
	w.out16(0x1ffd, p1ffd)
	w.out16(0x7ffd, p7ffd)
	// Alternate set, then the main set.
	w.op(0x01, s.C2, s.B2, 0x11, s.E2, s.D2, 0x21, s.L2, s.H2, 0xd9) // LD BC,DE,HL; EXX
	w.op(0xf1, s.F2, s.A2, 0x08)                                     // POP AF; EX AF,AF'
	w.op(0x01, s.C, s.B, 0x11, s.E, s.D, 0x21, s.L, s.H)
	w.op(0xdd, 0x21, uint8(s.IX), uint8(s.IX>>8), 0xfd, 0x21, uint8(s.IY), uint8(s.IY>>8))
	w.op(0x3e, s.I, 0xed, 0x47) // LD A,i; LD I,A
	w.op(0xed, []uint8{0x46, 0x56, 0x5e}[s.IM])
	// R counts the opcode fetches after LD R,A: POP AF, LD SP, EI and JP.
	after := uint8(3)
	if s.IFF1 {
		after = 4
	}
	r := s.R&0x80 | (s.R-after)&0x7f
	w.op(0x3e, r, 0xed, 0x4f)               // LD A,r; LD R,A
	w.op(0xf1, s.F, s.A)                    // POP AF
	w.op(0x31, uint8(s.SP), uint8(s.SP>>8)) // LD SP,sp
	if s.IFF1 {
		w.op(0xfb) // EI: interrupts are taken from the instruction at PC.
	}
	w.op(0xc3, uint8(s.PC), uint8(s.PC>>8)) // JP pc
	return w.out
}

type writer struct{ out []byte }

func (w *writer) op(b ...uint8) { w.out = append(w.out, b...) }

// out16 writes v to port: LD BC,port; LD A,v; OUT (C),A.
func (w *writer) out16(port uint16, v uint8) {
	w.op(0x01, uint8(port), uint8(port>>8), 0x3e, v, 0xed, 0x79)
}

// fill writes data from addr: LD HL,addr, then LD (HL),n; INC HL per byte.
func (w *writer) fill(addr uint16, data []byte) {
	w.op(0x21, uint8(addr), uint8(addr>>8))
	for _, b := range data {
		w.op(0x36, b, 0x23)
	}
}
