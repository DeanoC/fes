// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"hash/crc32"
	stdimage "image"
	"math"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// WindowSession keeps the admitted, fixed-source DDR scope exclusive until
// Close. Only its stopped reader may be reenabled between CPU publications.
// The region stays pinned; no operation releases or reassigns the allocation.
type WindowSession struct {
	self                                       *WindowSession
	region                                     *Region
	bytes                                      []byte
	arenaHash                                  *windowArenaHash
	verifier                                   *HPSContextVerifier
	context                                    context.Context
	source                                     HPSBootObservation
	opened                                     BridgeSnapshot
	bridges                                    *FirmwareBridges
	port                                       *windowIO
	identity                                   embeddedcontrol.Identity
	reader                                     *scanout.WindowReader
	limits                                     scanout.ReaderLimits
	controlSHA, accessSHA                      [32]byte
	generation, nextDrain, drainCookie, frames uint64
	known, drained, closed                     bool
	fault, closeErr                            error
	active                                     atomic.Bool
	surface                                    *scanout.Surface
	memory                                     *windowFrameMemory
	last                                       scanout.FrameRead
	returned                                   WindowSessionReturn
}

// WindowPresentation records a complete CPU publication, its frame result and
// local reader drain within the still-owned scope. It grants no global return
// or allocation release; Close supplies the independent firmware receipt.
type WindowPresentation struct {
	Frame  scanout.FrameRead
	Memory WindowMemoryObservation
	Drain  scanout.ReaderDrain
}

// WindowSessionReturn exists after complete firmware master stop and bridge
// closure. Frames counts completed reads; it supplies no HDMI or release grant.
type WindowSessionReturn struct {
	Source                            HPSBootObservation
	After                             BridgeSnapshot
	Memory                            WindowMemoryObservation
	LastFrame                         scanout.FrameRead
	Frames                            uint64
	ControlMapSHA256, AccessMapSHA256 [32]byte
}

func (s *WindowSession) fail(err error) {
	if s.fault == nil {
		s.fault = err
	}
	if s.port != nil {
		s.port.retire()
	}
}
func (s *WindowSession) take(ctx context.Context) error {
	if s == nil || ctx == nil || s.self != s {
		return scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return scanout.ErrReaderBusy
	}
	if s.closed || s.fault != nil {
		s.active.Store(false)
		return scanout.ErrReaderRetired
	}
	return nil
}
func (s *WindowSession) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.context.Err(); err != nil {
		return err
	}
	if err := s.checkRegion(); err != nil {
		return err
	}
	observed, err := s.bridges.ObserveBridges()
	if err != nil || observed != s.opened {
		return errors.New("native fabric: frame publication lost owned DDR scope")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.context.Err(); err != nil {
		return err
	}
	return s.checkRegion()
}
func (s *WindowSession) checkRegion() error {
	v := s.verifier
	r := v.readerMemoryRegion
	if r == nil || r != s.region || r.Address != v.windowMemory.CPUBase || uint64(len(r.Bytes)) != v.windowMemory.ArenaBytes || len(r.Bytes) != len(s.bytes) || &r.Bytes[0] != &s.bytes[0] || s.memory != nil && r.OrderFrameStores == nil {
		return errors.New("native fabric: pinned frame region changed")
	}
	return nil
}
func (s *WindowSession) checkArena() error {
	if err := s.checkRegion(); err != nil {
		return err
	}
	if s.arenaHash.sum(s.bytes) != s.verifier.windowArenaSHA {
		return errors.New("native fabric: published frame arena changed")
	}
	return nil
}
func (s *WindowSession) stopped() error {
	if !s.drained {
		return errors.New("native fabric: CPU frame write requires reader drain")
	}
	return (embeddedcontrol.Control{IO: s.port}).Drained(s.identity, s.generation, s.drainCookie)
}
func (s *WindowSession) drain(ctx context.Context) error {
	if s.drained {
		return s.stopped()
	}
	if s.nextDrain == 0 {
		return scanout.ErrReaderLimit
	}
	s.port.mu.Lock()
	s.port.drainCookie = s.nextDrain
	s.port.mu.Unlock()
	got, err := s.reader.Drain(ctx, s.nextDrain)
	if err != nil {
		return err
	}
	if got != (scanout.ReaderDrain{Generation: s.generation, Cookie: s.nextDrain}) {
		return scanout.ErrReaderProtocol
	}
	s.drained = true
	s.drainCookie = s.nextDrain
	s.nextDrain++
	return nil
}
func (s *WindowSession) enable(ctx context.Context) error {
	if s.generation == math.MaxUint64 || s.nextDrain == 0 {
		return scanout.ErrReaderLimit
	}
	if err := s.stopped(); err != nil {
		return err
	}
	s.generation++
	s.port.mu.Lock()
	s.port.generation = s.generation
	s.port.drainCookie = s.nextDrain
	s.port.mu.Unlock()
	s.drained = false
	if err := (embeddedcontrol.Control{IO: s.port}).Enable(s.identity, s.generation); err != nil {
		return err
	}
	var err error
	s.reader, err = scanout.NewWindowReader(ctx, s.port, s.identity, s.generation, s.verifier.windowPublication.Geometry, s.limits)
	return err
}

// Present implements the CPU presenter's existing application interface.
func (s *WindowSession) Present(frame *stdimage.RGBA) error {
	if s == nil || s.self != s {
		return scanout.ErrReaderArgument
	}
	_, err := s.PresentFrame(s.context, frame)
	return err
}

// PresentFrame stops the previous reader, copies and orders the complete CPU
// frame, reads it under a newer generation, then obtains its actual local drain.
// A failed operation returns no frame and retires access until platform Close.
func (s *WindowSession) PresentFrame(ctx context.Context, frame *stdimage.RGBA) (result WindowPresentation, err error) {
	if err = s.take(ctx); err != nil {
		return result, err
	}
	defer s.active.Store(false)
	if s.surface == nil {
		return result, errors.New("native fabric: ordered frame publication hook required")
	}
	if err = s.surface.CheckFrame(frame); err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			s.fail(err)
			result = WindowPresentation{}
		}
	}()
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	if s.generation == math.MaxUint64 || s.nextDrain == 0 {
		return result, scanout.ErrReaderLimit
	}
	if err = s.drain(ctx); err != nil {
		return result, err
	}
	if s.nextDrain == 0 {
		return result, scanout.ErrReaderLimit
	}
	s.memory.reset(ctx)
	if err = s.surface.Present(frame); err != nil {
		return result, err
	}
	if err = s.enable(ctx); err != nil {
		return result, err
	}
	frameRead, readErr := s.reader.ReadFrame(ctx, s.verifier.windowPublication.CRC32)
	err = readErr
	if err != nil {
		return result, err
	}
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	if err = s.drain(ctx); err != nil {
		return result, err
	}
	s.last = frameRead
	s.frames++
	return WindowPresentation{Frame: frameRead, Memory: s.verifier.windowPublication, Drain: scanout.ReaderDrain{Generation: s.generation, Cookie: s.drainCookie}}, nil
}

func (s *WindowSession) readPublished(ctx context.Context) (result scanout.FrameRead, err error) {
	if err = s.take(ctx); err != nil {
		return result, err
	}
	defer s.active.Store(false)
	defer func() {
		if err != nil {
			s.fail(err)
			result = scanout.FrameRead{}
		}
	}()
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	result, err = s.reader.ReadFrame(ctx, s.verifier.windowPublication.CRC32)
	if err != nil {
		return result, err
	}
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	s.last = result
	s.frames++
	return result, nil
}

// Close is independent of caller cancellation. It tries the known firmware
// return once, verifies the entire arena and returns cached results thereafter.
// An uncertain bridge opening or failed return is never replayed.
func (s *WindowSession) Close() (WindowSessionReturn, error) {
	if s == nil || s.self != s || s.verifier == nil {
		return WindowSessionReturn{}, scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return WindowSessionReturn{}, scanout.ErrReaderBusy
	}
	defer s.active.Store(false)
	if s.closed {
		return s.returned, s.closeErr
	}
	s.closed = true
	defer s.verifier.readerMemoryMu.Unlock()
	if s.port != nil {
		s.port.retire()
	}
	if !s.known {
		s.closeErr = s.fault
		if s.closeErr == nil {
			s.closeErr = errors.New("native fabric: uncertain window bridge mutation")
		}
		return WindowSessionReturn{}, s.closeErr
	}
	held := s.opened.ResetMask&13 == 13
	closure, closeErr := s.bridges.CloseBridges(BridgeCloseRequest{Before: s.opened, PreserveDataContainment: held})
	closed := s.opened
	closed.Generation++
	closed.ResetMask = 15
	closed.MastersContained = true
	if closeErr == nil && (closure.After != closed || !closure.ProducersQuiescent || closure.FabricAcknowledged ||
		closure.DataRemainedContained != held || closure.HardwareDrainedMask != s.opened.Capabilities.HardwareDrainMask&^s.opened.ResetMask) {
		closeErr = errors.New("native fabric: window return lacks complete owned drain")
	}
	if closeErr == nil {
		after, verifyErr := s.verifier.Verify(s.verifier.Identity())
		if verifyErr != nil || after.Digest != s.source.Digest || after.Bridges != closed {
			closeErr = errors.New("native fabric: window return lost source or closed ownership")
		} else {
			closeErr = s.checkArena()
		}
	}
	s.closeErr = errors.Join(s.fault, closeErr)
	if s.closeErr == nil {
		s.returned = WindowSessionReturn{Source: s.source, After: closed, Memory: s.verifier.windowPublication,
			LastFrame: s.last, Frames: s.frames, ControlMapSHA256: s.controlSHA, AccessMapSHA256: s.accessSHA}
	}
	return s.returned, s.closeErr
}

// windowFrameMemory exposes only bounded, sequential visible-row writes while
// the exact reader remains drained. Hashes cover the submitted bytes before the
// platform barrier, so a silent copy/barrier corruption cannot become expected CRC.
type windowFrameMemory struct {
	session      *WindowSession
	ctx          context.Context
	written      uint64
	ready        bool
	crc          hash.Hash32
	frame, arena hash.Hash
	padding      [32]byte
}

func (m *windowFrameMemory) Size() uint64 { return m.session.verifier.windowMemory.ArenaBytes }
func (m *windowFrameMemory) reset(ctx context.Context) {
	m.ctx = ctx
	m.written = 0
	m.ready = true
	m.crc = crc32.NewIEEE()
	m.frame = sha256.New()
	m.arena = sha256.New()
	v := m.session.verifier
	m.padding = m.session.arenaHash.sum(m.session.bytes[v.windowMemory.FrameBytes:])
}
func (m *windowFrameMemory) Write(offset uint64, source []byte) error {
	s := m.session
	v := s.verifier
	if !m.ready || !s.drained || offset != m.written || len(source) == 0 || len(source) > 4*5120 || offset+uint64(len(source)) > uint64(v.windowMemory.FrameBytes) {
		return errors.New("native fabric: frame write outside stopped sequential publication")
	}
	if err := s.check(m.ctx); err != nil {
		return err
	}
	m.crc.Write(source)
	m.frame.Write(source)
	m.arena.Write(source)
	copy(s.bytes[int(offset):], source)
	m.written += uint64(len(source))
	return s.check(m.ctx)
}
func (m *windowFrameMemory) Publish() error {
	s := m.session
	v := s.verifier
	if !m.ready || m.written != uint64(v.windowMemory.FrameBytes) {
		return errors.New("native fabric: incomplete frame publication")
	}
	m.ready = false
	if err := s.check(m.ctx); err != nil {
		return err
	}
	if err := s.stopped(); err != nil {
		return err
	}
	order := s.region.OrderFrameStores
	if order == nil {
		return errors.New("native fabric: frame store-order hook disappeared")
	}
	if err := order(); err != nil {
		return err
	}
	if err := s.check(m.ctx); err != nil {
		return err
	}
	if err := s.stopped(); err != nil {
		return err
	}
	padding := m.session.bytes[v.windowMemory.FrameBytes:]
	if s.arenaHash.sum(padding) != m.padding {
		return errors.New("native fabric: frame publication changed protected padding")
	}
	s.arenaHash.write(m.arena, padding)
	var arenaSHA, frameSHA [32]byte
	copy(arenaSHA[:], m.arena.Sum(nil))
	copy(frameSHA[:], m.frame.Sum(nil))
	if s.arenaHash.sum(s.bytes) != arenaSHA {
		return errors.New("native fabric: frame publication bytes differ")
	}
	v.windowArenaSHA = arenaSHA
	v.windowPublication.FrameSHA256 = frameSHA
	v.windowPublication.CRC32 = m.crc.Sum32()
	return nil
}
