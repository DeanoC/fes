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
	mu      sync.Mutex
	cpu     CPUTransport
	bridges *FirmwareBridges
	want    BridgeSnapshot
	base    uint64
	retired bool
}

func (p *windowIO) access(offset uint32, write bool, value uint32) (uint32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	read := offset&3 == 0 && ((offset <= 0x4c && offset != g.FesEmbeddedRegCommand) || (offset >= 0x90 && offset <= 0xac) || (offset >= 0xb0 && offset <= 0xc8))
	writable := (offset == g.FesEmbeddedRegRequestGenerationLo && value == 1) ||
		(offset == g.FesEmbeddedRegRequestGenerationHi && value == 0) ||
		(offset == g.FesEmbeddedRegCommand && value == g.FesEmbeddedCommandEnable) ||
		(offset == 0x84 && value == 1) || (offset == 0x88 && value == 0) || (offset == 0x8c && value == 1) || (offset == 0x80 && value == 1)
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

// ProbeWindowReader uses the actual native CSR driver after a completed source-
// bound transfer. It consumes one attempt. A known open scope is closed through
// firmware on success, cancellation and reader failure, independent of the
// caller's cancelled context. An uncertain bridge mutation is never replayed.
func (v *HPSContextVerifier) ProbeWindowReader(ctx context.Context, control, contract []byte, transfer *ConfigurationTransfer, limits scanout.ReaderLimits) (result WindowReadObservation, err error) {
	if v == nil || ctx == nil || limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.Timeout > time.Second || limits.MaxPolls == 0 || limits.MaxPolls > 64 {
		return result, errors.New("native fabric: bounded window context and limits required")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	m, err := v.checkedWindowRead(control, contract)
	if err != nil {
		return result, err
	}
	cpu, ok := v.cpu.(CPUTransport)
	if !ok {
		return result, errors.New("native fabric: owned physical window CPU transport required")
	}
	if !v.readerMemoryMu.TryLock() {
		return result, scanout.ErrReaderBusy
	}
	defer v.readerMemoryMu.Unlock()
	if v.ddrReaderAttempted || !v.readerMemoryReady || v.readerMemoryRegion == nil ||
		v.readerMemoryRegion.Address != v.windowMemory.CPUBase || uint64(len(v.readerMemoryRegion.Bytes)) != v.windowMemory.ArenaBytes ||
		sha256.Sum256(v.readerMemoryRegion.Bytes) != v.windowArenaSHA {
		return result, errors.New("native fabric: fresh complete pinned window required")
	}
	if transfer == nil || transfer.binding == nil {
		return result, errors.New("native fabric: completed window transfer required")
	}
	valid := transfer.done && transfer.err == nil && transfer.region == nil && transfer.phase == "complete" &&
		len(transfer.source) != 0 && len(transfer.pending) == 0 && transfer.offset == len(transfer.source) && transfer.accepted == len(transfer.source) &&
		transfer.binding.verifier == v && !transfer.binding.rearm && transfer.binding.program == nil
	binding := *transfer.binding
	if !valid {
		return result, errors.New("native fabric: completed compiled-source window transfer required")
	}
	v.ddrReaderAttempted = true
	source, err := v.Verify(v.Identity())
	if err != nil {
		return result, err
	}
	if !v.coldSource(source) || source != binding.source || source != v.windowPublication.Source || source.Bridges.Generation > math.MaxUint64-3 {
		return result, errors.New("native fabric: window opening lost completed transfer ownership")
	}
	bridges := &FirmwareBridges{cpu: cpu}
	opened, err := bridges.OpenBridges(BridgeOpenRequest{Before: source.Bridges, Mask: 2})
	if err != nil {
		return result, err
	}
	want := source.Bridges
	want.Generation++
	want.ResetMask &^= 2
	want.NeverEnabled = false
	if opened != want {
		return result, errors.New("native fabric: uncertain window control opening")
	}
	known := true
	var port *windowIO
	defer func() {
		if port != nil {
			port.retire()
		}
		if !known {
			result = WindowReadObservation{}
			return
		}
		held := opened.ResetMask&13 == 13
		closure, closeErr := bridges.CloseBridges(BridgeCloseRequest{Before: opened, PreserveDataContainment: held})
		closed := opened
		closed.Generation++
		closed.ResetMask = 15
		closed.MastersContained = true
		if closeErr == nil && (closure.After != closed || !closure.ProducersQuiescent || closure.FabricAcknowledged ||
			closure.DataRemainedContained != held || closure.HardwareDrainedMask != opened.Capabilities.HardwareDrainMask&^opened.ResetMask) {
			closeErr = errors.New("native fabric: window return lacks complete owned drain")
		}
		if closeErr == nil {
			after, verifyErr := v.Verify(v.Identity())
			if verifyErr != nil || after.Digest != source.Digest || after.Bridges != closed || sha256.Sum256(v.readerMemoryRegion.Bytes) != v.windowArenaSHA {
				closeErr = errors.New("native fabric: window return lost source, frame bytes or closed ownership")
			}
		}
		err = errors.Join(err, closeErr)
		if err != nil {
			result = WindowReadObservation{}
			return
		}
		result.Source = source
		result.After = closed
		result.Memory = v.windowPublication
		result.ControlMapSHA256 = sha256.Sum256(control)
		result.AccessMapSHA256 = sha256.Sum256(contract)
	}()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	known = false // A failed mutation may have opened data; do not guess a held close.
	data, err := bridges.OpenBridges(BridgeOpenRequest{Before: opened, Mask: 8})
	if err != nil {
		return result, err
	}
	want = opened
	want.Generation++
	want.ResetMask &^= 8
	want.MastersContained = false
	if data != want {
		return result, errors.New("native fabric: uncertain window data opening")
	}
	opened = data
	known = true
	port = &windowIO{cpu: cpu, bridges: bridges, want: opened, base: m.CPUBase}
	build, err := BuildWords(v.context.BuildID)
	if err != nil {
		return result, err
	}
	identity := embeddedcontrol.Identity{ABI: scanout.WindowControlABI, Build: build}
	endpoint := embeddedcontrol.Control{IO: port}
	initial, err := endpoint.Snapshot()
	if err != nil {
		return result, err
	}
	if initial != (embeddedcontrol.Snapshot{Identity: identity}) {
		return result, errors.New("native fabric: window reader was not fresh and idle")
	}
	if err = endpoint.Enable(identity, 1); err != nil {
		return result, err
	}
	reader, err := scanout.NewWindowReader(ctx, port, identity, 1, v.windowPublication.Geometry, limits)
	if err != nil {
		return result, err
	}
	result.Frame, err = reader.ReadFrame(ctx, v.windowPublication.CRC32)
	if err == nil {
		err = ctx.Err()
	}
	return result, err
}
