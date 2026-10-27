package spectrumtapes

import (
	"encoding/binary"
	"errors"
)

var errTooLarge = errors.New("tape is too large once converted")

// pendingData is a DATA block of ROM, turbo or pure-data bits that has not
// been written yet: a following edge may still extend its last pulse.
type pendingData struct {
	data      []byte
	bits      int
	zero, one uint32
	first     int // level of the first pulse
}

func (d *pendingData) bit(i int) bool { return d.data[i/8]&(0x80>>(i%8)) != 0 }

// emitter turns libspectrum edge events into the PZX blocks the deck plays
// (cores/fes-spectrum/rtl/spectrum_tape.v): a pulse holds its level for its
// duration and the next starts toggled, a block's first pulse takes the
// level the block names, zero-length pulses only move the level, and a STOP
// or the end takes the level the next pulse would start at. Edges are
// applied as fuse/tape.c tape_next_edge applies them.
type emitter struct {
	out   []byte
	limit int
	err   error
	level int

	// Pending PULS pulses, alternating levels from first; zero-length
	// entries split runs longer than PZX can store.
	pulses []uint32
	first  int
	data   *pendingData
}

func newEmitter(limit int) *emitter {
	m := &emitter{limit: limit}
	m.block("PZXT", []byte{1, 0})
	return m
}

func (m *emitter) block(tag string, body []byte) {
	if m.err != nil {
		return
	}
	if len(m.out)+8+len(body) > m.limit {
		m.err = errTooLarge
		return
	}
	m.out = append(m.out, tag...)
	m.out = binary.LittleEndian.AppendUint32(m.out, uint32(len(body)))
	m.out = append(m.out, body...)
}

// event applies one edge: T states until the next edge and its flags.
func (m *emitter) event(t uint32, flags int) {
	if t != 0 || flags&flagNoEdge == 0 || flags&(flagStop|flagLevelLow|flagLevelHigh) != 0 {
		switch {
		case flags&flagNoEdge != 0:
		case flags&flagLevelLow != 0:
			m.level = 0
		case flags&flagLevelHigh != 0:
			m.level = 1
		default:
			m.level ^= 1
		}
	}
	switch {
	case flags&flagTape != 0:
		m.flush()
		m.force()
	case flags&flagStop != 0:
		// Fuse stops before timing this edge.
		m.stop(0)
	case flags&flagStop48 != 0:
		// A 128K ignores this zero-length edge; a 48K stops without an edge.
		m.stop(1)
	case t != 0:
		m.pulse(t)
	}
}

func (m *emitter) lastPulseLevel() int {
	return m.first ^ (len(m.pulses)-1)&1
}

// pulse adds t T states at the current level.
func (m *emitter) pulse(t uint32) {
	if m.data != nil {
		if m.dataLastLevel() == m.level {
			m.spill()
		} else {
			m.flushData()
		}
	}
	if len(m.pulses) > 0 && m.lastPulseLevel() == m.level {
		last := &m.pulses[len(m.pulses)-1]
		if uint64(*last)+uint64(t) <= 0x7fffffff {
			*last += t
			return
		}
		m.pulses = append(m.pulses, 0)
	} else if len(m.pulses) == 0 {
		m.first = m.level
	}
	m.pulses = append(m.pulses, t)
}

// data adds ROM, turbo or pure-data bits: two edges per bit.
func (m *emitter) addData(data []byte, bits int, zero, one uint32) {
	if bits == 0 {
		return
	}
	m.flush()
	m.data = &pendingData{data: data, bits: bits, zero: zero, one: one, first: m.level ^ 1}
}

// dataLastLevel is the level of the pending DATA block's last pulse: each
// bit is two pulses, so it is the level before the block.
func (m *emitter) dataLastLevel() int { return m.data.first ^ 1 }

// spill moves the last bit of the pending DATA block into PULS pulses, so an
// edge-less event can extend its last pulse.
func (m *emitter) spill() {
	d := m.data
	d.bits--
	length := d.zero
	if d.bit(d.bits) {
		length = d.one
	}
	if d.bits == 0 {
		m.data = nil
	} else {
		m.flushData()
	}
	m.first = d.first
	m.pulses = append(m.pulses[:0], length, length)
}

func (m *emitter) flushData() {
	d := m.data
	m.data = nil
	body := binary.LittleEndian.AppendUint32(nil, uint32(d.bits)|uint32(d.first)<<31)
	body = append(body, 0, 0, 2, 2)
	for _, v := range []uint32{d.zero, d.zero, d.one, d.one} {
		body = binary.LittleEndian.AppendUint16(body, uint16(v))
	}
	body = append(body, d.data[:(d.bits+7)/8]...)
	m.block("DATA", body)
}

func appendPulse(body []byte, count int, length uint32) []byte {
	if count > 1 || length > 0x7fff {
		body = binary.LittleEndian.AppendUint16(body, uint16(0x8000|count))
	}
	if length > 0x7fff {
		body = binary.LittleEndian.AppendUint16(body, uint16(0x8000|length>>16))
		return binary.LittleEndian.AppendUint16(body, uint16(length))
	}
	return binary.LittleEndian.AppendUint16(body, uint16(length))
}

func (m *emitter) flushPulses() {
	if len(m.pulses) == 0 {
		return
	}
	var body []byte
	if m.first != 0 {
		// A PULS block starts low; a zero-length pulse raises it.
		body = appendPulse(body, 1, 0)
	}
	for i := 0; i < len(m.pulses); {
		j := i + 1
		for j < len(m.pulses) && m.pulses[j] == m.pulses[i] && j-i < 0x7fff {
			j++
		}
		body = appendPulse(body, j-i, m.pulses[i])
		i = j
	}
	m.pulses = m.pulses[:0]
	m.block("PULS", body)
}

func (m *emitter) flush() {
	if m.data != nil {
		m.flushData()
	}
	m.flushPulses()
}

// force makes the next STOP or the end show the current level: a PULS of
// zero-length pulses starts low and each one toggles it.
func (m *emitter) force() {
	if m.level != 0 {
		m.block("PULS", appendPulse(nil, 1, 0))
	} else {
		m.block("PULS", appendPulse(appendPulse(nil, 1, 0), 1, 0))
	}
}

func (m *emitter) stop(flags uint16) {
	m.flush()
	m.force()
	m.block("STOP", binary.LittleEndian.AppendUint16(nil, flags))
}
