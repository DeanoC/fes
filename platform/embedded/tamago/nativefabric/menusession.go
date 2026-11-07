// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"errors"
	stdimage "image"
	"math"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// MenuSession binds one completed compiled-source transfer and the entire
// boot-pinned arena to the existing owned firmware bridge service. It does not
// implement general package admission or infer boot authority from metadata.
type MenuSession struct {
	hasher                    arenaHash
	self                      *MenuSession
	verifier                  *HPSContextVerifier
	region                    *Region
	bytes                     []byte
	source                    HPSBootObservation
	opened                    BridgeSnapshot
	bridges                   *FirmwareBridges
	port                      *menuIO
	memory                    *menuFrameMemory
	presenter                 *scanout.MenuPresenter
	context, operationContext context.Context
	active                    atomic.Bool
	known, closed             bool
	fault, closeErr           error
	frames                    uint64
	last                      scanout.MenuFrame
	controlSHA, accessSHA     [32]byte
	returned                  MenuSessionReturn
}

// MenuSessionReturn exists only after independent secure master-stop, exact
// bridge closure and source/memory revalidation. It grants no arena release,
// physical HDMI acceptance or ordinary programming profile.
type MenuSessionReturn struct {
	Source                            HPSBootObservation
	After                             BridgeSnapshot
	Memory                            MenuMemoryObservation
	LastFrame                         scanout.MenuFrame
	Frames                            uint64
	ControlMapSHA256, AccessMapSHA256 [32]byte
}

func (v *HPSContextVerifier) OpenMenuSession(ctx context.Context, control, access []byte, transfer *ConfigurationTransfer, limits scanout.MenuLimits) (session *MenuSession, err error) {
	if v == nil || ctx == nil || limits.Validate() != nil {
		return nil, scanout.ErrReaderArgument
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	mapping, err := v.checkedMenuRead(control, access)
	if err != nil {
		return nil, err
	}
	cpu, ok := v.cpu.(CPUTransport)
	if !ok {
		return nil, errors.New("native menu: owned physical transport required")
	}
	if !v.readerMemoryMu.TryLock() {
		return nil, scanout.ErrReaderBusy
	}
	handedOff := false
	defer func() {
		if !handedOff {
			v.readerMemoryMu.Unlock()
		}
	}()
	if !v.readerMemoryReady || v.readerMemoryRegion == nil ||
		v.readerMemoryRegion.Address != v.menuMemory.CPUBase || uint64(len(v.readerMemoryRegion.Bytes)) != v.menuMemory.ArenaBytes ||
		v.readerMemoryRegion.OrderFrameStores == nil || (new(arenaHash)).sum(v.readerMemoryRegion.Bytes) != sha256.Sum256(v.menuExpected) {
		return nil, errors.New("native menu: fresh full pinned arena required")
	}
	if transfer == nil || transfer.binding == nil || v.ddrReaderAttempted {
		return nil, errors.New("native menu: fresh completed transfer required")
	}
	b := transfer.binding
	if !transfer.done || transfer.err != nil || transfer.region != nil || transfer.phase != "complete" || len(transfer.source) == 0 || len(transfer.pending) != 0 ||
		transfer.offset != len(transfer.source) || transfer.accepted != len(transfer.source) || b.verifier != v || b.program != nil || b.rearm || b.windowOpened || b.menuOpened {
		return nil, errors.New("native menu: completion does not belong to this cold scope")
	}
	v.ddrReaderAttempted = true
	b.menuOpened = true
	source, err := v.Verify(v.Identity())
	if err != nil {
		return nil, err
	}
	if !v.coldSource(source) || source != v.menuPublication.Source || source != b.source || source.Bridges.Generation > math.MaxUint64-3 {
		return nil, errors.New("native menu: opening lost closed publication/transfer ownership")
	}
	s := &MenuSession{verifier: v, region: v.readerMemoryRegion, bytes: v.readerMemoryRegion.Bytes, source: source, opened: source.Bridges,
		bridges: &FirmwareBridges{cpu: cpu}, context: ctx, operationContext: ctx, controlSHA: sha256.Sum256(control), accessSHA: sha256.Sum256(access)}
	s.self = s
	session = s
	v.menuSession = s
	handedOff = true
	defer func() {
		if err != nil {
			s.fail(err)
			_, closeErr := s.Close()
			err = errors.Join(err, closeErr)
		}
	}()
	for _, mask := range []uint32{2, 8} {
		if err = ctx.Err(); err != nil {
			return session, err
		}
		s.known = false
		var opened BridgeSnapshot
		opened, err = s.bridges.OpenBridges(BridgeOpenRequest{Before: s.opened, Mask: mask})
		if err != nil {
			return session, err
		}
		want := s.opened
		want.Generation++
		want.ResetMask &^= mask
		want.NeverEnabled = false
		if mask == 8 {
			want.MastersContained = false
		}
		if opened != want {
			return session, errors.New("native menu: uncertain bridge opening")
		}
		s.opened = opened
		s.known = true
	}
	build, err := BuildWords(v.context.BuildID)
	if err != nil {
		return session, err
	}
	identity := embeddedcontrol.Identity{ABI: scanout.MenuControlABI, Build: build}
	s.port = &menuIO{cpu: cpu, bridges: s.bridges, want: s.opened, base: mapping.CPUBase, command: g.FesEmbeddedCommandEnable}
	endpoint := embeddedcontrol.Control{IO: s.port}
	snapshot, err := endpoint.Snapshot()
	if err != nil {
		return session, err
	}
	if snapshot != (embeddedcontrol.Snapshot{Identity: identity}) {
		return session, errors.New("native menu: endpoint is not fresh and disabled")
	}
	if err = endpoint.Enable(identity, 1); err != nil {
		return session, err
	}
	s.port.set(0, 0, 0)
	s.memory = &menuFrameMemory{session: s}
	hooks := scanout.MenuHooks{IO: s.port, Memory: s.memory, Owns: func() error { return s.check(s.operationContext) }}
	// The session enforces caller cancellation during frames; independent Close
	// uses a new bounded operation even when that caller context has expired.
	s.presenter, err = scanout.NewMenuPresenter(context.Background(), hooks, identity, 1, uint32(v.menuMemory.BusBase), limits)
	return session, err
}
func (s *MenuSession) fail(err error) {
	if s.fault == nil {
		s.fault = err
	}
}
func (s *MenuSession) checkRegion() error {
	v := s.verifier
	r := v.readerMemoryRegion
	if r == nil || r != s.region || r.Address != v.menuMemory.CPUBase || uint64(len(r.Bytes)) != v.menuMemory.ArenaBytes ||
		len(r.Bytes) != len(s.bytes) || &r.Bytes[0] != &s.bytes[0] || r.OrderFrameStores == nil || len(v.menuExpected) != len(s.bytes) {
		return errors.New("native menu: pinned region changed")
	}
	return nil
}
func (s *MenuSession) check(ctx context.Context) error {
	if ctx == nil {
		return scanout.ErrReaderArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkRegion(); err != nil {
		return err
	}
	before, err := s.bridges.ObserveBridges()
	if err != nil || before != s.opened {
		return errors.New("native menu: lost secure bridge owner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.checkRegion()
}
func (s *MenuSession) checkArena() error {
	if err := s.checkRegion(); err != nil {
		return err
	}
	if s.hasher.sum(s.bytes) != sha256.Sum256(s.verifier.menuExpected) {
		return errors.New("native menu: arena bytes differ from private publication")
	}
	return nil
}
func (s *MenuSession) Present(frame *stdimage.RGBA) error {
	if s == nil {
		return scanout.ErrReaderArgument
	}
	_, err := s.PresentFrame(s.context, frame)
	return err
}
func (s *MenuSession) PresentFrame(ctx context.Context, frame *stdimage.RGBA) (result scanout.MenuFrame, err error) {
	if s == nil || s.self != s || ctx == nil {
		return result, scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return result, scanout.ErrReaderBusy
	}
	defer s.active.Store(false)
	if s.closed || s.fault != nil || s.presenter == nil {
		return result, scanout.ErrReaderRetired
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = s.context.Err(); err != nil {
		return result, err
	}
	if frame == nil || frame.Rect.Dx() != 1280 || frame.Rect.Dy() != 720 || frame.Stride < 5120 || frame.Stride > 1<<30 ||
		uint64(len(frame.Pix)) < uint64(719)*uint64(frame.Stride)+5120 {
		return result, scanout.ErrReaderArgument
	}
	defer func() {
		if err != nil {
			s.fail(err)
			result = scanout.MenuFrame{}
		}
	}()
	s.operationContext = ctx
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	if s.frames >= math.MaxUint32 {
		return result, scanout.ErrReaderLimit
	}
	slot := uint32(s.frames % 2)
	sequence := uint32(s.frames + 1)
	command := uint32(2)
	if s.frames == 0 {
		command = 1
	}
	s.memory.slot = slot
	s.memory.written = 0
	s.memory.ready = true
	s.port.set(command, sequence, slot)
	result, err = s.presenter.PresentFrame(ctx, frame)
	if err != nil {
		return result, err
	}
	if result.Generation != 1 || result.Sequence != sequence || result.Submitted != sequence || result.Slot != slot ||
		s.memory.ready || s.memory.written != 3686400 {
		return result, errors.New("native menu: incomplete or foreign frame result")
	}
	if err = s.check(ctx); err != nil {
		return result, err
	}
	if err = s.checkArena(); err != nil {
		return result, err
	}
	s.frames++
	s.last = result
	return result, nil
}

// Close is cached and independent of caller cancellation. Even after a frame
// failure it attempts the known firmware containment once; no failed frame can
// turn that containment into a normal return or allocation-release receipt.
func (s *MenuSession) Close() (MenuSessionReturn, error) {
	if s == nil || s.self != s {
		return MenuSessionReturn{}, scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return MenuSessionReturn{}, scanout.ErrReaderBusy
	}
	defer s.active.Store(false)
	if s.closed {
		return s.returned, s.closeErr
	}
	s.closed = true
	defer s.verifier.readerMemoryMu.Unlock()
	if !s.known {
		s.closeErr = s.fault
		if s.closeErr == nil {
			s.closeErr = errors.New("native menu: bridge mutation unknown")
		}
		return MenuSessionReturn{}, s.closeErr
	}
	s.operationContext = context.Background()
	if s.presenter != nil {
		s.port.set(3, uint32(s.frames), uint32(s.frames%2))
		drain, err := s.presenter.Stop(s.operationContext, 1)
		if err != nil {
			s.fail(err)
		} else if drain != (scanout.ReaderDrain{Generation: 1, Cookie: 1}) {
			s.fail(errors.New("native menu: foreign local drain"))
		}
	}
	if s.port != nil {
		s.port.retire()
	}
	held := s.opened.ResetMask&13 == 13
	closure, err := s.bridges.CloseBridges(BridgeCloseRequest{Before: s.opened, PreserveDataContainment: held})
	expected := s.opened
	expected.Generation++
	expected.ResetMask = 15
	expected.MastersContained = true
	if err == nil && (closure.After != expected || !closure.ProducersQuiescent || closure.FabricAcknowledged || closure.DataRemainedContained != held ||
		closure.HardwareDrainedMask != s.opened.Capabilities.HardwareDrainMask&^s.opened.ResetMask) {
		err = errors.New("native menu: incomplete independent firmware closure")
	}
	if err == nil {
		after, e := s.verifier.Verify(s.verifier.Identity())
		if e != nil || after.Digest != s.source.Digest || after.Bridges != expected {
			err = errors.New("native menu: closed source changed")
		} else {
			err = s.checkArena()
		}
	}
	s.closeErr = errors.Join(s.fault, err)
	if s.closeErr == nil {
		s.returned = MenuSessionReturn{s.source, expected, s.verifier.menuPublication, s.last, s.frames, s.controlSHA, s.accessSHA}
	}
	return s.returned, s.closeErr
}

type menuFrameMemory struct {
	session *MenuSession
	slot    uint32
	written uint64
	ready   bool
}

func (m *menuFrameMemory) Size() uint64 { return scanout.MenuArenaBytes }
func (m *menuFrameMemory) Write(offset uint64, data []byte) error {
	s := m.session
	base := uint64(m.slot) * (4 << 20)
	if !m.ready || len(data) == 0 || len(data) > 4*5120 || offset != base+m.written || m.written+uint64(len(data)) > 3686400 {
		return errors.New("native menu: write outside sequential spare slot")
	}
	if err := s.check(s.operationContext); err != nil {
		return err
	}
	copy(s.verifier.menuExpected[offset:offset+uint64(len(data))], data)
	copy(s.bytes[offset:offset+uint64(len(data))], data)
	m.written += uint64(len(data))
	return s.check(s.operationContext)
}
func (m *menuFrameMemory) Publish() error {
	s := m.session
	if !m.ready || m.written != 3686400 {
		return errors.New("native menu: incomplete CPU publication")
	}
	m.ready = false
	if err := s.check(s.operationContext); err != nil {
		return err
	}
	if err := s.region.OrderFrameStores(); err != nil {
		return err
	}
	if err := s.check(s.operationContext); err != nil {
		return err
	}
	return s.checkArena()
}

type menuIO struct {
	mu                      sync.Mutex
	cpu                     CPUTransport
	bridges                 *FirmwareBridges
	want                    BridgeSnapshot
	base                    uint64
	command, sequence, slot uint32
	retired                 bool
}

func (p *menuIO) set(command, sequence, slot uint32) {
	p.mu.Lock()
	p.command = command
	p.sequence = sequence
	p.slot = slot
	p.mu.Unlock()
}
func (p *menuIO) retire()                              { p.mu.Lock(); p.retired = true; p.mu.Unlock() }
func (p *menuIO) Read32(offset uint32) (uint32, error) { return p.access(offset, false, 0) }
func (p *menuIO) Write32(offset, value uint32) error {
	_, err := p.access(offset, true, value)
	return err
}
func (p *menuIO) access(offset uint32, write bool, value uint32) (uint32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	readable := offset&3 == 0 && ((offset <= 0x4c && offset != g.FesEmbeddedRegCommand) || offset >= 0x94 && offset <= 0xc8)
	writable := offset == g.FesEmbeddedRegRequestGenerationLo && value == 1 || offset == g.FesEmbeddedRegRequestGenerationHi && value == 0 ||
		offset == g.FesEmbeddedRegRequestCookieLo && value == 1 || offset == g.FesEmbeddedRegRequestCookieHi && value == 0 ||
		offset == g.FesEmbeddedRegCommand && (value == g.FesEmbeddedCommandDisable || p.command == g.FesEmbeddedCommandEnable && value == g.FesEmbeddedCommandEnable) ||
		offset == 0x84 && value == 1 || offset == 0x88 && value == 0 || offset == 0x8c && value == p.sequence || offset == 0x90 && value == p.slot ||
		offset == 0x80 && p.command > 0 && value == p.command
	if p.retired || write && !writable || !write && !readable {
		return 0, errors.New("native menu: retired/outside owned CSR contract")
	}
	before, err := p.bridges.ObserveBridges()
	if err != nil || before != p.want {
		p.retired = true
		return 0, errors.New("native menu: CSR lost secure owner")
	}
	var result uint32
	if write {
		err = p.cpu.WritePhysical32(p.base+uint64(offset), value)
	} else {
		result, err = p.cpu.ReadPhysical32(p.base + uint64(offset))
	}
	after, e := p.bridges.ObserveBridges()
	if err != nil || e != nil || after != p.want {
		p.retired = true
		return 0, errors.New("native menu: uncertain CSR or changed owner")
	}
	return result, nil
}
