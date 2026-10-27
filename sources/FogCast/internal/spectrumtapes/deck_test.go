package spectrumtapes

import (
	"encoding/binary"
	"fmt"
)

// runs records an EAR waveform as tapedump prints it: "level T" for each run
// of one level, merged across zero-length edges, and "stop level" where the
// deck stops. A stop ends the run before it.
type runs struct {
	lines []string
	level int
	run   uint64
}

func newRuns() *runs { return &runs{level: -1} }

func (r *runs) add(level int, t uint64) {
	if t == 0 {
		return
	}
	if level == r.level {
		r.run += t
		return
	}
	r.flush()
	r.level, r.run = level, t
}

func (r *runs) flush() {
	if r.run != 0 {
		r.lines = append(r.lines, fmt.Sprintf("%d %d", r.level, r.run))
	}
	r.level, r.run = -1, 0
}

func (r *runs) stop(level int) {
	r.flush()
	r.lines = append(r.lines, fmt.Sprintf("stop %d", level))
}

// fuseRuns plays blocks through the engine bit by bit and applies each edge
// as fuse/tape.c does: the waveform Fuse 1.6.0 plays.
func fuseRuns(blocks []block, is48 bool) ([]string, error) {
	e, err := newEngine(blocks)
	if err != nil {
		return nil, err
	}
	r := newRuns()
	level := 0
	for edges := 0; edges < maxEdges; edges++ {
		t, flags, err := e.next()
		if err != nil {
			return nil, err
		}
		if t != 0 || flags&flagNoEdge == 0 || flags&(flagStop|flagLevelLow|flagLevelHigh) != 0 {
			switch {
			case flags&flagNoEdge != 0:
			case flags&flagLevelLow != 0:
				level = 0
			case flags&flagLevelHigh != 0:
				level = 1
			default:
				level ^= 1
			}
		}
		if flags&flagStop != 0 || (flags&flagStop48 != 0 && is48) {
			r.stop(level)
			if flags&flagTape != 0 {
				return r.lines, nil
			}
			continue
		}
		r.add(level, uint64(t))
	}
	return nil, fmt.Errorf("tape did not end")
}

// deckRuns plays a PZX stream as cores/fes-spectrum/rtl/spectrum_tape.v
// does: a block's level and zero-length pulses wait for the next pulse,
// STOP or end, which starts at that level, or else the toggle that ended the
// last pulse. STOP flag 1 only stops the 48K.
func deckRuns(pzx []byte, is48 bool) ([]string, error) {
	r := newRuns()
	ear, afterPulse := 0, false
	set, setLevel, flip := false, 0, 0
	next := func() int {
		level := ear
		if set {
			level = setLevel
		} else if afterPulse {
			level ^= 1
		}
		level ^= flip
		set, flip = false, 0
		return level
	}
	pulse := func(count int, t uint32) {
		if t == 0 {
			flip ^= count & 1
			return
		}
		ear = next()
		r.add(ear, uint64(t))
		for i := 1; i < count; i++ {
			ear ^= 1
			r.add(ear, uint64(t))
		}
		afterPulse = true
	}
	stop := func() {
		ear = next()
		r.stop(ear)
		afterPulse = false
	}
	begin := func(level int) { set, setLevel, flip = true, level, 0 }
	for p := 0; ; {
		if p+8 > len(pzx) {
			stop()
			return r.lines, nil
		}
		tag := string(pzx[p : p+4])
		size := int(binary.LittleEndian.Uint32(pzx[p+4:]))
		body := pzx[p+8 : p+8+size]
		p += 8 + size
		switch tag {
		case "PULS":
			begin(0)
			for q := 0; q+2 <= len(body); {
				count, t := 1, uint32(binary.LittleEndian.Uint16(body[q:]))
				q += 2
				if t > 0x8000 {
					count, t = int(t&0x7fff), uint32(binary.LittleEndian.Uint16(body[q:]))
					q += 2
				}
				if t >= 0x8000 {
					t = (t&0x7fff)<<16 | uint32(binary.LittleEndian.Uint16(body[q:]))
					q += 2
				}
				pulse(count, t)
			}
		case "DATA":
			word := binary.LittleEndian.Uint32(body)
			begin(int(word >> 31))
			bits := int(word & 0x7fffffff)
			tail := uint32(binary.LittleEndian.Uint16(body[4:]))
			p0, p1 := int(body[6]), int(body[7])
			seq := func(bit, i int) uint32 {
				at := 8 + 2*i
				if bit != 0 {
					at += 2 * p0
				}
				return uint32(binary.LittleEndian.Uint16(body[at:]))
			}
			data := body[8+2*(p0+p1):]
			for i := 0; i < bits; i++ {
				bit, n := 0, p0
				if data[i/8]&(0x80>>(i%8)) != 0 {
					bit, n = 1, p1
				}
				for j := 0; j < n; j++ {
					pulse(1, seq(bit, j))
				}
			}
			if tail != 0 {
				pulse(1, tail)
			}
		case "PAUS":
			word := binary.LittleEndian.Uint32(body)
			begin(int(word >> 31))
			pulse(1, word&0x7fffffff)
		case "STOP":
			if binary.LittleEndian.Uint16(body) != 1 || is48 {
				stop()
			}
		}
	}
}
