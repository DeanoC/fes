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

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

type computerMap struct {
	Format           int    `json:"format"`
	Scope            string `json:"scope"`
	PackageID        string `json:"package_id"`
	BuildID          string `json:"build_id"`
	ControlMapSHA256 string `json:"control_map_sha256"`
	HandoffSHA256    string `json:"handoff_sha256"`
	CPUBase          uint64 `json:"cpu_base"`
	Span             uint32 `json:"span"`
	Minor            uint16 `json:"abi_minor"`
	Capabilities     uint32 `json:"capabilities"`
	HPSMaster        bool   `json:"hps_master"`
	EndpointSHA256   string `json:"endpoint_sha256"`
	WrapperSHA256    string `json:"wrapper_sha256"`
}

func (v *HPSContextVerifier) checkedComputerMap(control, raw []byte) (computerMap, error) {
	m, err := v.checkedControlMap(control)
	if err != nil {
		return computerMap{}, err
	}
	if len(raw) == 0 || len(raw) > 4096 {
		return computerMap{}, errors.New("native fabric: bounded compiled computer map required")
	}
	var c computerMap
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("native fabric: trailing computer map")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return c, err
	}
	canonical, err := json.Marshal(fields)
	controlSHA := sha256.Sum256(control)
	if err != nil || len(fields) != 13 || !bytes.Equal(raw, append(canonical, '\n')) ||
		c.Format != 1 || c.Scope != "sealed-cold-computer-control-v1" || c.PackageID != v.context.PackageID || c.BuildID != v.context.BuildID ||
		c.ControlMapSHA256 != hex.EncodeToString(controlSHA[:]) || c.HandoffSHA256 != m.HandoffSHA256 ||
		c.CPUBase != m.ArchitecturalLWBase+m.BridgeBase+uint64(cg.FesDe25ComputerPeripheralOffset) || c.Span != cg.FesDe25ComputerSpan ||
		c.Minor != 0 || c.Capabilities != cg.FesComputerCapabilityVideoFixed720p60|cg.FesComputerCapabilityKeyboardHid|cg.FesComputerCapabilityMediaZx81Tape ||
		c.HPSMaster || !nonzeroDigest(c.EndpointSHA256) || !nonzeroDigest(c.WrapperSHA256) {
		return computerMap{}, errors.New("native fabric: computer map differs from compiled sealed source")
	}
	return c, nil
}

// computerIO is valid only for its exact open ownership generation. Every CSR
// access is bracketed by fresh secure snapshots. Closing retires it before the
// bridge mutation and waits for an in-flight ordered access to finish.
type computerIO struct {
	mu      sync.Mutex
	cpu     CPUTransport
	bridges *FirmwareBridges
	want    BridgeSnapshot
	base    uint64
	retired bool
	closing bool
}

func (c *computerIO) access(offset uint32, write bool, value uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	valid := offset == cg.FesDe25ComputerIdOffset || offset == cg.FesDe25ComputerVersionOffset || offset == cg.FesDe25ComputerStatusOffset ||
		offset == cg.FesDe25ComputerGpoOffset || offset == cg.FesDe25ComputerGpiOffset || offset == cg.FesDe25ComputerFaultOffset
	if c.retired || !valid || write && offset != cg.FesDe25ComputerGpoOffset {
		return 0, errors.New("native fabric: computer aperture retired or access outside contract")
	}
	if c.closing && write && value & ^cg.FesComputerRequestMask != cg.FesComputerOpcodeExecution<<24 {
		c.retired = true
		return 0, errors.New("native fabric: only Hold is permitted during computer closure")
	}
	before, err := c.bridges.ObserveBridges()
	if err != nil || before != c.want {
		c.retired = true
		return 0, errors.Join(errors.New("native fabric: computer lost control-only ownership"), err)
	}
	var result uint32
	if write {
		err = c.cpu.WritePhysical32(c.base+uint64(offset), value)
	} else {
		result, err = c.cpu.ReadPhysical32(c.base + uint64(offset))
	}
	if err != nil {
		c.retired = true
		return 0, err
	}
	after, err := c.bridges.ObserveBridges()
	if err != nil || after != c.want {
		c.retired = true
		return 0, errors.Join(errors.New("native fabric: computer ownership changed during access"), err)
	}
	return result, nil
}
func (c *computerIO) Read32(offset uint32) (uint32, error) { return c.access(offset, false, 0) }
func (c *computerIO) Write32(offset, value uint32) error {
	_, err := c.access(offset, true, value)
	return err
}
func (c *computerIO) retire()     { c.mu.Lock(); c.retired = true; c.mu.Unlock() }
func (c *computerIO) beginClose() { c.mu.Lock(); c.closing = true; c.mu.Unlock() }

type ComputerObservation struct {
	Source    HPSBootObservation
	After     BridgeSnapshot
	CPUBase   uint64
	MapSHA256 [32]byte
	// This is GP command acceptance, not independent proof of CPU execution or
	// complete stop. The board still needs its execution observation endpoint.
	HoldAcknowledged bool
}

// ComputerSession owns one native control-only scope. It grants no data-path
// release, BootHandoffVerifier or ordinary runtime profile. Close must be called
// on success and failure; retaining Control after Close confers no MMIO access.
type ComputerSession struct {
	mu      sync.Mutex
	control *computer.Computer
	port    *computerIO
	v       *HPSContextVerifier
	source  HPSBootObservation
	mapSHA  [32]byte
	closed  bool
}

func (s *ComputerSession) Computer() *computer.Computer { return s.control }

// OpenComputer uses a source-derived compiled map and the actual owned bridge
// hooks. The matched firmware must explicitly enable its one-shot computer
// mode after the complete sealed configuration. Other cold profiles reject it.
func (v *HPSContextVerifier) OpenComputer(ctx context.Context, control, raw []byte, limits computer.Limits) (_ *ComputerSession, err error) {
	m, err := v.checkedComputerMap(control, raw)
	if err != nil {
		return nil, err
	}
	cpu, ok := v.cpu.(CPUTransport)
	if !ok {
		return nil, errors.New("native fabric: computer requires the owned physical CPU transport")
	}
	if ctx == nil || limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.MaxPolls == 0 {
		return nil, errors.New("native fabric: bounded computer context and limits required")
	}
	v.computerMu.Lock()
	if v.computerAttempted {
		v.computerMu.Unlock()
		return nil, errors.New("native fabric: computer scope already attempted")
	}
	v.computerAttempted = true
	v.computerMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := v.Verify(v.Identity())
	if err != nil {
		return nil, err
	}
	if !v.coldSource(source) || source.Bridges.Generation > math.MaxUint64-2 {
		return nil, errors.New("native fabric: computer requires unchanged cold closed ownership")
	}
	b := &FirmwareBridges{cpu: cpu}
	opened, err := b.OpenBridges(BridgeOpenRequest{Before: source.Bridges, Mask: source.Bridges.Capabilities.ControlMask})
	if err != nil {
		return nil, err
	} // Uncertain open is not replayed or guessed closed.
	want := source.Bridges
	want.Generation++
	want.ResetMask &^= want.Capabilities.ControlMask
	want.NeverEnabled = false
	if opened != want {
		return nil, errors.New("native fabric: computer control opening changed unexpected ownership or paths")
	}
	s := &ComputerSession{v: v, source: source, mapSHA: sha256.Sum256(raw), port: &computerIO{cpu: cpu, bridges: b, want: want, base: m.CPUBase}}
	wantID := computer.Identity{Minor: m.Minor, Capabilities: m.Capabilities}
	build, err := hex.DecodeString(m.BuildID)
	if err != nil || len(build) != 16 {
		return nil, errors.New("native fabric: computer build encoding invalid")
	}
	copy(wantID.Build[:], build)
	s.control, err = computer.Open(ctx, s.port, limits, wantID)
	if err != nil {
		_, cleanup := s.Close(ctx)
		return nil, errors.Join(err, cleanup)
	}
	return s, nil
}

// Close asks a still-usable driver to hold once, retires the aperture, then
// atomically recloses only its expected control scope. A retired driver emits
// no fallback GPO write. The closure proves HPS containment separately from
// whether the core acknowledged Hold. Unknown closure is never retried.
func (s *ComputerSession) Close(ctx context.Context) (out ComputerObservation, err error) {
	if s == nil {
		return out, errors.New("native fabric: computer session missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return out, errors.New("native fabric: computer session already closed")
	}
	s.closed = true
	s.port.beginClose() // Prevent another caller from running after the final Hold.
	if s.control != nil {
		err = s.control.Hold(ctx)
		out.HoldAcknowledged = err == nil
	}
	s.port.retire()
	before := s.port.want
	closed, closeErr := s.port.bridges.CloseBridges(BridgeCloseRequest{Before: before, PreserveDataContainment: true})
	if closeErr != nil {
		return out, errors.Join(err, closeErr)
	}
	want := before
	want.Generation++
	want.ResetMask = want.Capabilities.Mask
	if closed.After != want || !closed.ProducersQuiescent || !closed.DataRemainedContained || closed.FabricAcknowledged || closed.HardwareDrainedMask != 0 {
		return out, errors.Join(err, errors.New("native fabric: computer closure lacks owned control-only containment"))
	}
	after, verifyErr := s.v.Verify(s.v.Identity())
	if verifyErr != nil || after.Digest != s.source.Digest || after.Bridges != want {
		return out, errors.Join(err, verifyErr, errors.New("native fabric: computer closure lost trusted source"))
	}
	out.Source = s.source
	out.After = want
	out.CPUBase = s.port.base
	out.MapSHA256 = s.mapSHA
	return out, err
}
