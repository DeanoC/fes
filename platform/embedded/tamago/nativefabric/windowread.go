// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

func (v *HPSContextVerifier) checkedWindowRead(control, raw []byte) (hpsControlMap, error) {
	m, err := v.checkedControlMap(control)
	if err != nil {
		return m, err
	}
	if v.windowMemory == nil || len(raw) == 0 || len(raw) > 4096 {
		return m, errors.New("native fabric: compiled window read contract required")
	}
	var access ddrReadMap
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&access); err != nil {
		return m, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return m, errors.New("native fabric: trailing window read contract")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return m, err
	}
	canonical, err := json.Marshal(fields)
	controlSHA := sha256.Sum256(control)
	if err != nil || len(fields) != 14 || !bytes.Equal(raw, append(canonical, '\n')) ||
		access.Format != 1 || access.Scope != "sealed-cold-window-read-v1" || access.PackageID != v.context.PackageID || access.BuildID != v.context.BuildID ||
		access.ReaderMemorySHA256 != hex.EncodeToString(v.windowMemorySHA[:]) || access.ControlMapSHA256 != hex.EncodeToString(controlSHA[:]) ||
		!nonzeroDigest(access.AdapterSourceSHA256) || !nonzeroDigest(access.AdapterGeneratedSHA256) ||
		access.ARCache != 2 || access.ARProt != 3 || access.ARUser != 224 || access.ReadBytes != v.windowMemory.FrameBytes || access.OpenMask != 8 || access.CloseMask != 10 {
		return m, errors.New("native fabric: window read differs from its sealed full-frame contract")
	}
	return m, nil
}

// windowIO exposes only the selected read-only window engine. Every ordered
// CSR operation is bracketed by fresh secure bridge ownership observations.
type windowIO struct {
	mu                      sync.Mutex
	cpu                     CPUTransport
	bridges                 *FirmwareBridges
	want                    BridgeSnapshot
	base                    uint64
	generation, drainCookie uint64
	retired                 bool
}

func (p *windowIO) access(offset uint32, write bool, value uint32) (uint32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	read := offset&3 == 0 && ((offset <= 0x4c && offset != g.FesEmbeddedRegCommand) || (offset >= 0x90 && offset <= 0xac) || (offset >= 0xb0 && offset <= 0xc8))
	generation, cookie := p.generation, p.drainCookie
	if generation == 0 {
		generation = 1
	}
	if cookie == 0 {
		cookie = 1
	}
	writable := (offset == g.FesEmbeddedRegRequestGenerationLo && value == uint32(generation)) ||
		(offset == g.FesEmbeddedRegRequestGenerationHi && value == uint32(generation>>32)) ||
		(offset == g.FesEmbeddedRegRequestCookieLo && value == uint32(cookie)) ||
		(offset == g.FesEmbeddedRegRequestCookieHi && value == uint32(cookie>>32)) ||
		(offset == g.FesEmbeddedRegCommand && (value == g.FesEmbeddedCommandEnable || value == g.FesEmbeddedCommandDisable)) ||
		(offset == 0x84 && value == uint32(generation)) || (offset == 0x88 && value == uint32(generation>>32)) || (offset == 0x8c && value == 1) || (offset == 0x80 && value == 1)
	if p.retired || write && !writable || !write && !read {
		return 0, errors.New("native fabric: window aperture retired or outside contract")
	}
	before, err := p.bridges.ObserveBridges()
	if err != nil || before != p.want {
		p.retired = true
		return 0, errors.New("native fabric: window access lost bridge ownership")
	}
	var result uint32
	if write {
		err = p.cpu.WritePhysical32(p.base+uint64(offset), value)
	} else {
		result, err = p.cpu.ReadPhysical32(p.base + uint64(offset))
	}
	after, verifyErr := p.bridges.ObserveBridges()
	if err != nil || verifyErr != nil || after != p.want {
		p.retired = true
		return 0, errors.New("native fabric: uncertain window access or changed owner")
	}
	return result, nil
}
func (p *windowIO) Read32(offset uint32) (uint32, error) { return p.access(offset, false, 0) }
func (p *windowIO) Write32(offset, value uint32) error {
	_, err := p.access(offset, true, value)
	return err
}
func (p *windowIO) retire() { p.mu.Lock(); p.retired = true; p.mu.Unlock() }

// WindowReadObservation exists only after complete frame delivery and the
// independent firmware drain/closure. The arena remains pinned until reboot;
// this receipt grants no reuse, HDMI or ordinary package admission.
type WindowReadObservation struct {
	Source                            HPSBootObservation
	After                             BridgeSnapshot
	Frame                             scanout.FrameRead
	Memory                            WindowMemoryObservation
	ControlMapSHA256, AccessMapSHA256 [32]byte
}

// ProbeWindowReader retains the one-frame diagnostic interface. Its session
// always returns through independent firmware master stop and bridge closure.
func (v *HPSContextVerifier) ProbeWindowReader(ctx context.Context, control, contract []byte, transfer *ConfigurationTransfer, limits scanout.ReaderLimits) (WindowReadObservation, error) {
	s, err := v.openWindowSession(ctx, control, contract, transfer, limits, false)
	if err != nil {
		return WindowReadObservation{}, err
	}
	frame, readErr := s.readPublished(ctx)
	returned, closeErr := s.Close()
	if closeErr != nil {
		return WindowReadObservation{}, closeErr
	}
	if readErr != nil {
		return WindowReadObservation{}, readErr
	}
	return WindowReadObservation{Source: returned.Source, After: returned.After, Frame: frame, Memory: returned.Memory,
		ControlMapSHA256: returned.ControlMapSHA256, AccessMapSHA256: returned.AccessMapSHA256}, nil
}

// OpenWindowSession consumes the completed transfer once and owns its boot-pinned
// window until Close. Frame updates require a separate platform store-order hook.
func (v *HPSContextVerifier) OpenWindowSession(ctx context.Context, control, contract []byte, transfer *ConfigurationTransfer, limits scanout.ReaderLimits) (*WindowSession, error) {
	return v.openWindowSession(ctx, control, contract, transfer, limits, true)
}
func (v *HPSContextVerifier) openWindowSession(ctx context.Context, control, contract []byte, transfer *ConfigurationTransfer, limits scanout.ReaderLimits, updates bool) (_ *WindowSession, err error) {
	if v == nil || ctx == nil || limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.Timeout > time.Second || limits.MaxPolls == 0 || limits.MaxPolls > 64 {
		return nil, errors.New("native fabric: bounded window context and limits required")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	m, err := v.checkedWindowRead(control, contract)
	if err != nil {
		return nil, err
	}
	cpu, ok := v.cpu.(CPUTransport)
	if !ok {
		return nil, errors.New("native fabric: owned physical window CPU transport required")
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
	if v.ddrReaderAttempted || !v.readerMemoryReady || v.readerMemoryRegion == nil ||
		v.readerMemoryRegion.Address != v.windowMemory.CPUBase || uint64(len(v.readerMemoryRegion.Bytes)) != v.windowMemory.ArenaBytes ||
		sha256.Sum256(v.readerMemoryRegion.Bytes) != v.windowArenaSHA {
		return nil, errors.New("native fabric: fresh complete pinned window required")
	}
	if updates && v.readerMemoryRegion.OrderFrameStores == nil {
		return nil, errors.New("native fabric: ordered frame publication hook required")
	}
	if transfer == nil || transfer.binding == nil {
		return nil, errors.New("native fabric: completed window transfer required")
	}
	valid := transfer.done && transfer.err == nil && transfer.region == nil && transfer.phase == "complete" &&
		len(transfer.source) != 0 && len(transfer.pending) == 0 && transfer.offset == len(transfer.source) && transfer.accepted == len(transfer.source) &&
		transfer.binding.verifier == v && !transfer.binding.rearm && transfer.binding.program == nil
	binding := *transfer.binding
	if !valid {
		return nil, errors.New("native fabric: completed compiled-source window transfer required")
	}
	v.ddrReaderAttempted = true
	source, err := v.Verify(v.Identity())
	if err != nil {
		return nil, err
	}
	if !v.coldSource(source) || source != binding.source || source != v.windowPublication.Source || source.Bridges.Generation > math.MaxUint64-3 {
		return nil, errors.New("native fabric: window opening lost completed transfer ownership")
	}
	bridges := &FirmwareBridges{cpu: cpu}
	opened, err := bridges.OpenBridges(BridgeOpenRequest{Before: source.Bridges, Mask: 2})
	if err != nil {
		return nil, err
	}
	want := source.Bridges
	want.Generation++
	want.ResetMask &^= 2
	want.NeverEnabled = false
	if opened != want {
		return nil, errors.New("native fabric: uncertain window control opening")
	}
	s := &WindowSession{region: v.readerMemoryRegion, bytes: v.readerMemoryRegion.Bytes, verifier: v, context: ctx, source: source, opened: opened, known: true,
		bridges: bridges, limits: limits, generation: 1, nextDrain: 1,
		controlSHA: sha256.Sum256(control), accessSHA: sha256.Sum256(contract)}
	s.self = s
	handedOff = true
	defer func() {
		if err != nil {
			s.fail(err)
			_, closeErr := s.Close()
			if closeErr != nil {
				err = closeErr
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s.known = false // A failed mutation may have opened data; do not guess a held close.
	data, err := bridges.OpenBridges(BridgeOpenRequest{Before: opened, Mask: 8})
	if err != nil {
		return nil, err
	}
	want = opened
	want.Generation++
	want.ResetMask &^= 8
	want.MastersContained = false
	if data != want {
		return nil, errors.New("native fabric: uncertain window data opening")
	}
	opened = data
	s.opened = data
	s.known = true
	s.port = &windowIO{cpu: cpu, bridges: bridges, want: opened, base: m.CPUBase, generation: 1, drainCookie: 1}
	build, err := BuildWords(v.context.BuildID)
	if err != nil {
		return nil, err
	}
	identity := embeddedcontrol.Identity{ABI: scanout.WindowControlABI, Build: build}
	s.identity = identity
	endpoint := embeddedcontrol.Control{IO: s.port}
	initial, err := endpoint.Snapshot()
	if err != nil {
		return nil, err
	}
	if initial != (embeddedcontrol.Snapshot{Identity: identity}) {
		return nil, errors.New("native fabric: window reader was not fresh and idle")
	}
	if err = endpoint.Enable(identity, 1); err != nil {
		return nil, err
	}
	reader, err := scanout.NewWindowReader(ctx, s.port, identity, 1, v.windowPublication.Geometry, limits)
	if err != nil {
		return nil, err
	}
	s.reader = reader
	if updates {
		s.memory = &windowFrameMemory{session: s}
		s.surface, err = scanout.New(1280, 720, 5120, s.memory)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}
