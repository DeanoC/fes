package spectrumtapes

import "errors"

// Edge flags, as libspectrum's LIBSPECTRUM_TAPE_FLAGS_*.
const (
	flagStop      = 1 << 1
	flagStop48    = 1 << 2
	flagNoEdge    = 1 << 3
	flagLevelLow  = 1 << 4
	flagLevelHigh = 1 << 5
	flagTape      = 1 << 8
)

const (
	pilotHeader = 8063
	pilotData   = 3223
	romPilot    = 2168
	romSync1    = 667
	romSync2    = 735
	romBit0     = 855
	romBit1     = 1710
)

const (
	statePilot = iota
	stateSync1
	stateSync2
	stateData1
	stateData2
	statePause
	stateTail
)

// engine plays a block list as libspectrum 1.5's tape iterator does
// (libspectrum_tape_get_next_edge): each call returns the T states until the
// next edge and the flags of the edge that starts now. It also follows TZX
// call sequences, which libspectrum does not read.
type engine struct {
	blocks    []block
	current   int
	loopBlock int
	loopCount int
	calls     []callFrame

	state      int
	edgeCount  int
	bitT       uint32
	bitsInByte int
	bytes      int
	currentB   byte
	lastBit    byte
	index      int
	pulseCount int
	level      int

	// Generalised data.
	run, symbolsThroughRun, edgesThroughSymbol int
	currentSymbol                              int
	symbolsThroughStream                       uint32
	bitsThroughByte, bytesThroughStream        int

	// PZX data blocks.
	bitPulses []uint32

	// dataStart: a ROM, turbo or pure-data block is at its first data bit,
	// so a caller may take all of its bits at once (see Convert).
	dataStart bool
}

type callFrame struct {
	block, next int
}

func newEngine(blocks []block) (*engine, error) {
	if len(blocks) == 0 {
		return nil, errors.New("tape has no blocks")
	}
	for _, b := range blocks {
		if (b.kind == kindTone && b.count == 0) || (b.kind == kindPulses && b.count == 0) {
			return nil, errCorrupt
		}
	}
	e := &engine{blocks: blocks, loopBlock: -1}
	e.initBlock()
	return e, nil
}

func (e *engine) block() *block { return &e.blocks[e.current] }

// initBlock is libspectrum_tape_block_init for the current block.
func (e *engine) initBlock() {
	b := e.block()
	e.bytes = -1
	e.bitsInByte = 7
	switch b.kind {
	case kindROM:
		e.edgeCount = pilotHeader
		if len(b.data) > 0 && b.data[0]&0x80 != 0 {
			e.edgeCount = pilotData
		}
		e.state = statePilot
	case kindTurbo:
		e.edgeCount = b.pilotPulses
		e.state = statePilot
	case kindTone:
		e.edgeCount = b.count
	case kindPulses:
		e.edgeCount = 0
	case kindPureData:
		e.pureNextBit()
		e.dataStart = e.state == stateData1
	case kindRawData:
		if len(b.data) > 0 {
			e.state = stateData1
			e.bytes, e.bitsInByte = 0, 0
			e.lastBit = (b.data[0] & 0x80) ^ 0x80
			e.rawNextBit()
		} else {
			e.state = statePause
		}
	case kindGeneralised:
		e.run, e.symbolsThroughRun, e.edgesThroughSymbol = 0, 0, 0
		e.currentSymbol, e.symbolsThroughStream = 0, 0
		e.currentB, e.bitsThroughByte, e.bytesThroughStream = 0, 0, 0
		switch {
		case b.pilotTable.inBlock != 0:
			e.state = statePilot
		case b.dataTable.inBlock != 0:
			e.state = stateData1
			if len(b.data) > 0 {
				e.currentB = b.data[0]
			}
			e.currentSymbol = e.dataSymbol()
		default:
			e.state = statePause
		}
	case kindRLE:
		e.index = 0
		e.state = stateData1
	case kindPulseSequence:
		e.index, e.pulseCount, e.level = 0, 0, -1
	case kindDataBlock:
		if b.level != -1 {
			e.level = b.level
		}
		e.dataBlockNextBit()
	}
}

// next is libspectrum_tape_get_next_edge_internal.
func (e *engine) next() (uint32, int, error) {
	e.dataStart = false
	b := e.block()
	var t uint32
	flags := 0
	end, noAdvance := false, false
	switch b.kind {
	case kindROM, kindTurbo:
		t, end, flags = e.romEdge(b)
	case kindTone:
		t = b.length
		e.edgeCount--
		end = e.edgeCount == 0
	case kindPulses:
		t = b.lengths[e.edgeCount]
		e.edgeCount++
		end = e.edgeCount == b.count
	case kindPureData:
		t, end, flags = e.pureEdge(b)
	case kindRawData:
		t, end, flags = e.rawEdge(b)
	case kindGeneralised:
		t, end, flags = e.generalisedEdge(b)
	case kindPause:
		t, end = b.pause, true
		if b.level != -1 && b.pause != 0 {
			flags |= levelFlag(b.level)
		}
		if t == 0 {
			flags |= flagStop
		}
	case kindJump:
		target := e.current + b.offset
		if target < 0 || target >= len(e.blocks) {
			return 0, 0, errCorrupt
		}
		e.current = target
		t, flags, end, noAdvance = 0, flagNoEdge, true, true
	case kindLoopStart:
		if e.current+1 < len(e.blocks) && b.count != 0 {
			e.loopBlock = e.current + 1
			e.loopCount = b.count
		}
		t, flags, end = 0, flagNoEdge, true
	case kindLoopEnd:
		if e.loopBlock >= 0 {
			e.loopCount--
			if e.loopCount != 0 {
				e.current = e.loopBlock
				noAdvance = true
			} else {
				e.loopBlock = -1
			}
		}
		t, flags, end = 0, flagNoEdge, true
	case kindCall:
		t, flags, end = 0, flagNoEdge, true
		if len(b.offsets) > 0 {
			target := e.current + b.offsets[0]
			if target < 0 || target >= len(e.blocks) {
				return 0, 0, errCorrupt
			}
			e.calls = append(e.calls, callFrame{block: e.current, next: 1})
			e.current = target
			noAdvance = true
		}
	case kindReturn:
		// The next call of the innermost sequence, or the block after the
		// call block once its calls are done. A return outside a call is
		// ignored.
		t, flags, end = 0, flagNoEdge, true
		if n := len(e.calls); n > 0 {
			frame := &e.calls[n-1]
			call := &e.blocks[frame.block]
			if frame.next < len(call.offsets) {
				target := frame.block + call.offsets[frame.next]
				if target < 0 || target >= len(e.blocks) {
					return 0, 0, errCorrupt
				}
				frame.next++
				e.current = target
				noAdvance = true
			} else {
				e.calls = e.calls[:n-1]
				e.current = frame.block
			}
		}
	case kindStop48:
		t, flags, end = 0, flagStop48|flagNoEdge, true
	case kindSetLevel:
		// Inverted: the next block flips the level before its edge.
		t, end = 0, true
		if b.level != 0 {
			flags |= flagLevelLow
		} else {
			flags |= flagLevelHigh
		}
	case kindMetadata:
		t, flags, end = 0, flagNoEdge, true
	case kindRLE:
		var err error
		t, end, flags, err = e.rleEdge(b)
		if err != nil {
			return 0, 0, err
		}
	case kindPulseSequence:
		t, end, flags = e.pulseSequenceEdge(b)
	case kindDataBlock:
		t, end, flags = e.dataBlockEdge(b)
	}
	if end {
		if !noAdvance {
			e.current++
			if e.current == len(e.blocks) {
				flags |= flagStop | flagTape
				flags &^= flagNoEdge
				e.current = 0
				e.loopBlock = -1
				e.calls = nil
			}
		}
		e.initBlock()
	}
	return t, flags, nil
}

func levelFlag(level int) int {
	if level != 0 {
		return flagLevelHigh
	}
	return flagLevelLow
}

// tailPause is do_tail_pause: a zero pause has no edge.
func tailPause(t uint32, flags int) (uint32, bool, int) {
	if t == 0 {
		flags |= flagNoEdge
	}
	return t, true, flags
}

func (e *engine) romEdge(b *block) (uint32, bool, int) {
	pilot, sync1, sync2 := uint32(romPilot), uint32(romSync1), uint32(romSync2)
	if b.kind == kindTurbo {
		pilot, sync1, sync2 = b.pilot, b.sync1, b.sync2
	}
	switch e.state {
	case statePilot:
		if b.kind == kindROM {
			e.edgeCount--
			if e.edgeCount == 0 {
				e.state = stateSync1
			}
			return pilot, false, 0
		}
		if e.edgeCount != 0 {
			e.edgeCount--
			return pilot, false, 0
		}
		e.state = stateSync2
		return sync1, false, 0
	case stateSync1:
		e.state = stateSync2
		return sync1, false, 0
	case stateSync2:
		e.romNextBit(b)
		e.dataStart = e.state == stateData1
		return sync2, false, 0
	case stateData1:
		e.state = stateData2
		return e.bitT, false, 0
	case stateData2:
		t := e.bitT
		e.romNextBit(b)
		return t, false, 0
	default:
		return tailPause(b.pause, 0)
	}
}

// romNextBit is rom_next_bit and turbo_next_bit; a ROM block uses every bit
// of its last byte.
func (e *engine) romNextBit(b *block) {
	lastBits := 8
	if b.kind == kindTurbo {
		lastBits = b.lastBits
	}
	if e.nextByte(b, lastBits) {
		e.state = statePause
		return
	}
	bit0, bit1 := uint32(romBit0), uint32(romBit1)
	if b.kind == kindTurbo {
		bit0, bit1 = b.bit0, b.bit1
	}
	e.bitT = e.takeBit(bit0, bit1)
	e.state = stateData1
}

// nextByte advances to the next bit; it reports the end of the data.
func (e *engine) nextByte(b *block, lastBits int) bool {
	e.bitsInByte++
	if e.bitsInByte == 8 {
		e.bytes++
		if e.bytes == len(b.data) {
			return true
		}
		e.currentB = b.data[e.bytes]
		if e.bytes == len(b.data)-1 {
			e.bitsInByte = 8 - lastBits
		} else {
			e.bitsInByte = 0
		}
	}
	return false
}

func (e *engine) takeBit(zero, one uint32) uint32 {
	bit := e.currentB & 0x80
	e.currentB <<= 1
	if bit != 0 {
		return one
	}
	return zero
}

func (e *engine) pureNextBit() {
	b := e.block()
	if e.nextByte(b, b.lastBits) {
		e.state = statePause
		return
	}
	e.bitT = e.takeBit(b.bit0, b.bit1)
	e.state = stateData1
}

func (e *engine) pureEdge(b *block) (uint32, bool, int) {
	switch e.state {
	case stateData1:
		e.state = stateData2
		return e.bitT, false, 0
	case stateData2:
		t := e.bitT
		e.pureNextBit()
		return t, false, 0
	default:
		return tailPause(b.pause, 0)
	}
}

// rawNextBit is libspectrum_tape_raw_data_next_bit: one pulse covers a run
// of equal samples.
func (e *engine) rawNextBit() {
	b := e.block()
	if e.bytes == len(b.data) {
		e.state = statePause
		e.lastBit ^= 0x80
		return
	}
	e.state = stateData1
	length := uint32(0)
	for {
		inByte := 8
		if e.bytes == len(b.data)-1 {
			inByte = b.lastBits
		}
		length++
		e.bitsInByte++
		if e.bitsInByte == inByte {
			e.bitsInByte = 0
			e.bytes++
			if e.bytes == len(b.data) {
				break
			}
		}
		if (b.data[e.bytes]<<uint(e.bitsInByte))&0x80 == e.lastBit {
			break
		}
	}
	e.bitT = length * b.rawSample
	e.lastBit ^= 0x80
}

func (e *engine) rawEdge(b *block) (uint32, bool, int) {
	if e.state == stateData1 {
		t := e.bitT
		e.rawNextBit()
		// lastBit already names the next run's level.
		if e.lastBit != 0 {
			return t, false, flagLevelLow
		}
		return t, false, flagLevelHigh
	}
	return tailPause(b.pause, 0)
}

func (e *engine) generalisedBit(b *block) byte {
	if e.bitsThroughByte == 8 {
		e.bitsThroughByte = 0
		e.bytesThroughStream++
		e.currentB = b.data[e.bytesThroughStream]
	}
	bit := e.currentB >> 7
	e.currentB <<= 1
	e.bitsThroughByte++
	return bit
}

func (e *engine) dataSymbol() int {
	b := e.block()
	symbol := 0
	for i := 0; i < b.symbolBits; i++ {
		symbol = symbol<<1 | int(e.generalisedBit(b))
	}
	return symbol
}

func symbolEdge(s *symbol, edge int) (uint32, int) {
	if edge != 0 {
		return s.lengths[edge], 0
	}
	switch s.edge {
	case 1:
		return s.lengths[0], flagNoEdge
	case 2:
		return s.lengths[0], flagLevelLow
	case 3:
		return s.lengths[0], flagLevelHigh
	default:
		return s.lengths[0], 0
	}
}

// generalisedEdge is generalised_data_edge.
func (e *engine) generalisedEdge(b *block) (uint32, bool, int) {
	switch e.state {
	case statePilot:
		table := &b.pilotTable
		index := int(b.pilotSymbols[e.run])
		s := &table.symbols[index]
		t, flags := symbolEdge(s, e.edgesThroughSymbol)
		e.edgesThroughSymbol++
		if e.edgesThroughSymbol == table.maxPulses || s.lengths[e.edgesThroughSymbol] == 0 {
			e.edgesThroughSymbol = 0
			e.symbolsThroughRun++
			if e.symbolsThroughRun == int(b.pilotRepeats[e.run]) {
				e.symbolsThroughRun = 0
				e.run++
				if uint32(e.run) == table.inBlock {
					// A one-symbol alphabet takes zero bits a symbol, so its
					// data stream is empty while the block still has
					// symbols; libspectrum on glibc plays them (malloc(0) is
					// not NULL), as the TZX format means.
					if b.dataTable.symbols == nil || b.dataTable.inBlock == 0 {
						e.state = statePause
					} else {
						e.state = stateData1
						e.bitsThroughByte, e.bytesThroughStream, e.symbolsThroughStream = 0, 0, 0
						e.currentB = 0
						if len(b.data) > 0 {
							e.currentB = b.data[0]
						}
						e.currentSymbol = e.dataSymbol()
					}
				}
			}
		}
		return t, false, flags
	case stateData1:
		table := &b.dataTable
		if table.symbols == nil || e.currentSymbol >= table.inTable {
			e.state = statePause
			return tailPause(b.pause, 0)
		}
		s := &table.symbols[e.currentSymbol]
		t, flags := symbolEdge(s, e.edgesThroughSymbol)
		e.edgesThroughSymbol++
		if e.edgesThroughSymbol == table.maxPulses || s.lengths[e.edgesThroughSymbol] == 0 {
			e.symbolsThroughStream++
			if e.symbolsThroughStream == table.inBlock {
				e.state = statePause
			} else {
				e.edgesThroughSymbol = 0
				e.currentSymbol = e.dataSymbol()
			}
		}
		return t, false, flags
	default:
		return tailPause(b.pause, 0)
	}
}

// rleEdge is rle_pulse_edge; a TZX CSW block then has the TZX tail pause.
func (e *engine) rleEdge(b *block) (uint32, bool, int, error) {
	if e.state == statePause {
		t, end, flags := tailPause(b.pause, 0)
		return t, end, flags, nil
	}
	if e.index >= len(b.data) {
		e.state = statePause
		t, end, flags := tailPause(b.pause, 0)
		return t, end, flags, nil
	}
	var t uint32
	if b.data[e.index] != 0 {
		t = b.scale * uint32(b.data[e.index])
		e.index++
	} else {
		if e.index+5 > len(b.data) {
			return 0, false, 0, errCorrupt
		}
		d := b.data[e.index+1:]
		t = b.scale * (uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16 | uint32(d[3])<<24)
		e.index += 5
	}
	if e.index == len(b.data) {
		if !b.tailPause {
			return t, true, 0, nil
		}
		e.state = statePause
	}
	return t, false, 0, nil
}

// pulseSequenceEdge is pulse_sequence_edge: zero-length pulses only move
// the level.
func (e *engine) pulseSequenceEdge(b *block) (uint32, bool, int) {
	newLevel := e.level
	var t uint32
	end := false
	for t == 0 && !end {
		t = b.lengths[e.index]
		if newLevel == 0 {
			newLevel = 1
		} else {
			newLevel = 0
		}
		e.pulseCount++
		if uint32(e.pulseCount) == b.repeats[e.index] {
			e.index++
			if e.index >= len(b.lengths) {
				end = true
			} else {
				e.pulseCount = 0
			}
		}
	}
	flags := 0
	if newLevel != e.level {
		flags |= levelFlag(newLevel)
		e.level = newLevel
	} else if t == 0 {
		flags |= flagNoEdge
	}
	return t, end, flags
}

// dataBlockNextBit is libspectrum_tape_data_block_next_bit. A bit whose
// sequence has no pulses has nothing to play (libspectrum reads past it).
func (e *engine) dataBlockNextBit() {
	b := e.block()
	lastBits := int(b.bitCount % 8)
	if lastBits == 0 {
		lastBits = 8
	}
	for {
		if e.nextByte(b, lastBits) {
			e.state = stateTail
			return
		}
		bit := e.currentB & 0x80
		e.currentB <<= 1
		if bit != 0 {
			e.bitPulses = b.bit1Pulses
		} else {
			e.bitPulses = b.bit0Pulses
		}
		if len(e.bitPulses) > 0 {
			break
		}
	}
	e.index = 0
	e.state = stateData1
}

// dataBlockEdge is data_block_edge.
func (e *engine) dataBlockEdge(b *block) (uint32, bool, int) {
	var t uint32
	end := false
	flags := 0
	if e.state == stateData1 {
		t = e.bitPulses[e.index]
		e.index++
		if e.index == len(e.bitPulses) {
			e.dataBlockNextBit()
		}
	} else {
		t, end, flags = tailPause(b.tail, 0)
	}
	if flags&flagNoEdge == 0 {
		flags |= levelFlag(e.level)
		if e.level != 0 {
			e.level = 0
		} else {
			e.level = 1
		}
	}
	return t, end, flags
}
