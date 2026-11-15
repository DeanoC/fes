// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

const (
	InitialTapeArenaBase      uint64 = 0x95800000
	InitialTapeArenaBytes            = 2 << 20
	InitialTapePayloadBytes          = 0x4000
	InitialTapeMetadataOffset        = InitialTapeArenaBytes - ProgramMetadataBytes
)

// InitialTapeArena owns separate pinned storage. Its constructor establishes no
// physical reservation, mapping, firmware ownership or permission to run a core.
// This prototype is not yet selected by the board hook or fixed firmware supplier.
// The platform must supply and check those, including the complete program map.
// A completed new configuration is required for every publication; neither tape
// completion nor an increasing caller generation releases the retained source.
type InitialTapeArena struct {
	state *initialTapeArena
}

type initialTapeArena struct {
	mu                        sync.Mutex
	program                   *ConfigurationArena
	memory, metadata          []byte
	barrier                   func() error
	lastProgram               *Region
	tapeOwner, tapeGeneration uint64
	ready, failed             bool
}

func tapeStorageOverlaps(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	ap, bp := uintptr(unsafe.Pointer(unsafe.SliceData(a))), uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	if ap > ^uintptr(0)-uintptr(len(a)) || bp > ^uintptr(0)-uintptr(len(b)) {
		return true
	}
	return ap < bp+uintptr(len(b)) && bp < ap+uintptr(len(a))
}

func NewInitialTapeArena(address uint64, memory []byte, program *ConfigurationArena, barrier func() error) (*InitialTapeArena, error) {
	if address != InitialTapeArenaBase || len(memory) != InitialTapeArenaBytes || program == nil || barrier == nil {
		return nil, errors.New("native fabric: fixed independent tape reservation required")
	}
	program.mu.Lock()
	defer program.mu.Unlock()
	if program.self != program || program.address != 0x94800000 || len(program.memory) != MaxProgramBytes || len(program.metadata) != ProgramMetadataBytes ||
		tapeStorageOverlaps(memory, program.memory) || tapeStorageOverlaps(memory, program.metadata) {
		return nil, errors.New("native fabric: tape storage overlaps or lacks the complete program reservation")
	}
	return &InitialTapeArena{state: &initialTapeArena{program: program, memory: memory[:InitialTapePayloadBytes:InitialTapePayloadBytes], metadata: memory[InitialTapeMetadataOffset:], barrier: barrier, ready: true}}, nil
}

// The caller serializes its platform operation. Callbacks must not recursively
// enter the configuration arena; both leases stay locked throughout publication.
// Every error after publication retires this tape lease without freeing memory.
// The trusted FirmwareCaller must guard every SMC with live ownership and the
// paired mapping checks, including this tape range and the protected program.
func (a *InitialTapeArena) PublishInitialTape(ctx context.Context, caller FirmwareCaller, scope BridgeSnapshot, request computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	if a == nil || a.state == nil {
		return computer.InitialTapeReceipt{}, errors.New("native fabric: independent tape publisher missing")
	}
	return a.state.publishInitialTape(ctx, caller, scope, request)
}

func (a *initialTapeArena) publishInitialTape(ctx context.Context, caller FirmwareCaller, scope BridgeSnapshot, request computer.InitialTapeRequest) (out computer.InitialTapeReceipt, err error) {
	if a == nil || a.program == nil || ctx == nil || caller == nil {
		return out, errors.New("native fabric: initial tape publisher missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.program
	p.mu.Lock()
	defer p.mu.Unlock()
	if a.failed || !a.ready || p.failed || !p.ready || p.active == nil || p.sourceMatches == nil || !p.sourceMatches() ||
		a.lastProgram == p.active || a.tapeGeneration != 0 && (scope.Owner != a.tapeOwner || scope.Generation <= a.tapeGeneration) {
		return out, errors.New("native fabric: independent tape arena lacks a new completed program or is consumed")
	}
	program := p.active
	sourceUnchanged := func() bool {
		if p.active != program || !p.ready || p.failed || !p.sourceMatches() {
			p.failed = true
			return false
		}
		return true
	}
	request.Image = bytes.Clone(request.Image)
	header, err := initialTapeHeader(scope, request)
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = a.barrier(); err != nil {
		a.failed = true
		return out, err
	}
	if !sourceUnchanged() {
		return out, errors.New("native fabric: protected program source changed before tape publication")
	}
	a.lastProgram = program
	a.ready = false
	a.tapeOwner, a.tapeGeneration = scope.Owner, scope.Generation
	complete := false
	defer func() {
		if !complete {
			a.failed = true
		}
	}()
	clear(a.metadata)
	copy(a.metadata, header)
	copy(a.memory[:len(request.Image)], request.Image)
	if err = a.barrier(); err != nil {
		return out, err
	}
	unchanged := func() bool {
		return sourceUnchanged() && bytes.Equal(a.memory[:len(request.Image)], request.Image) && bytes.Equal(a.metadata[:len(header)], header) && bytes.Equal(a.metadata[len(header):], make([]byte, len(a.metadata)-len(header)))
	}
	if !unchanged() {
		return out, errors.New("native fabric: initial tape changed during publication")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	chunks := (len(request.Image) + 511) / 512
	sequence := uint64(0)
	pending := false
	// A valid pending reply identifies exactly one next step. Local cancel or
	// mutation can retire it once; an uncertain SMC response is never replayed.
	defer func() {
		if pending && err != nil {
			r, stopErr := caller.Invoke([7]uint64{uint64(g.FesEmbeddedTapeCall), uint64(g.FesEmbeddedTapeAbort) | sequence<<g.FesEmbeddedTapeSequenceShift, scope.Owner, uint64(g.FesEmbeddedTapeCookie), scope.Generation})
			if stopErr == nil && r != ([4]uint64{uint64(g.FesEmbeddedTapeCancelled), uint64(g.FesEmbeddedTapeTag) << 32}) {
				stopErr = errors.New("native fabric: initial tape cancellation not acknowledged")
			}
			err = errors.Join(err, stopErr)
		}
	}()
	var r [4]uint64
	for {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if !unchanged() {
			return out, errors.New("native fabric: initial tape source changed between steps")
		}
		operation := uint64(g.FesEmbeddedTapeLoad)
		if sequence != 0 {
			operation = uint64(g.FesEmbeddedTapeStep) | sequence<<g.FesEmbeddedTapeSequenceShift
		}
		pending = false // once issued, this ordinal is uncertain until validated
		r, err = caller.Invoke([7]uint64{uint64(g.FesEmbeddedTapeCall), operation, scope.Owner, uint64(g.FesEmbeddedTapeCookie), scope.Generation})
		if err != nil {
			return out, err
		}
		if r[1]>>32 != uint64(g.FesEmbeddedTapeTag) {
			return out, errors.New("native fabric: unknown initial tape firmware tag")
		}
		if r[0] == uint64(g.FesEmbeddedTapeLimit) {
			return out, fmt.Errorf("%w: secure initial tape limit", computer.ErrLimit)
		}
		if r[0] != uint64(g.FesEmbeddedTapePending) {
			break
		}
		progress := min(int(sequence)*512, len(request.Image))
		if sequence > uint64(chunks) || r[1] != uint64(g.FesEmbeddedTapeTag)<<32|uint64(progress) || r[2] != scope.Generation || r[3] != sequence+1 {
			return out, errors.New("native fabric: initial tape progress/sequence rejected")
		}
		sequence++
		pending = true
		runtime.Gosched()
	}
	if r[0] != uint64(g.FesEmbeddedTapeOK) || sequence != uint64(chunks+1) || r[1] != uint64(g.FesEmbeddedTapeTag)<<32|uint64(len(request.Image)) || r[2] != scope.Generation {
		return out, fmt.Errorf("native fabric: initial tape firmware response rejected (status=%d)", r[0])
	}
	receipt := computer.InitialTapeReceipt{GPO: uint32(r[3]), Bytes: uint32(r[1]), CRC32: uint32(r[3] >> 32)}
	if err = computer.ValidateInitialTapeReceipt(request, receipt); err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = a.barrier(); err != nil {
		return out, err
	}
	if !unchanged() {
		return out, errors.New("native fabric: initial tape publication changed before completion")
	}
	a.ready = true
	complete = true
	return receipt, nil
}
