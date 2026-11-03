// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"

	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

type windowMemoryMap struct {
	Format         int    `json:"format"`
	Scope          string `json:"scope"`
	PackageID      string `json:"package_id"`
	BuildID        string `json:"build_id"`
	CPUBase        uint64 `json:"cpu_base"`
	BusBase        uint64 `json:"bus_base"`
	Bytes          uint64 `json:"bytes"`
	ArenaBase      uint64 `json:"arena_base"`
	ArenaBytes     uint64 `json:"arena_bytes"`
	CPUAttributes  string `json:"cpu_attributes"`
	Lifetime       string `json:"lifetime"`
	FPGAAddressing string `json:"fpga_addressing"`
	RowBytes       uint32 `json:"row_bytes"`
	Stride         uint32 `json:"stride"`
	Rows           uint32 `json:"rows"`
	FrameBytes     uint32 `json:"frame_bytes"`
	PixelFormat    string `json:"pixel_format"`
}

// NewWindowHPSContextVerifier requires both compiled source maps explicitly.
// The ordinary fixed-reader constructor continues to reject this personality.
// This constructor performs no I/O and grants no bridge or DDR permission.
func NewWindowHPSContextVerifier(cpu FirmwareCaller, source, control, memory []byte) (*HPSContextVerifier, error) {
	v, err := newHPSContextVerifier(cpu, source, true)
	if err != nil {
		return nil, err
	}
	if v.context.ProgrammingProfile != HPSFirstWindowDiagnosticProfile {
		return nil, errors.New("native fabric: explicit window source required")
	}
	if _, err = v.checkedControlMap(control); err != nil {
		return nil, err
	}
	m, err := v.checkedWindowMemory(memory)
	if err != nil {
		return nil, err
	}
	v.windowMemory = &m
	v.windowMemorySHA = sha256.Sum256(memory)
	return v, nil
}

func (v *HPSContextVerifier) checkedWindowMemory(raw []byte) (windowMemoryMap, error) {
	var m windowMemoryMap
	if v == nil || v.context.ProgrammingProfile != HPSFirstWindowDiagnosticProfile || len(raw) == 0 || len(raw) > 4096 {
		return m, errors.New("native fabric: compiled window memory map required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return m, errors.New("native fabric: trailing window memory map")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return m, err
	}
	canonical, err := json.Marshal(fields)
	if err != nil || len(fields) != 17 || !bytes.Equal(raw, append(canonical, '\n')) ||
		m.Format != 1 || m.Scope != "sealed-cold-window-reader-memory-v1" || m.PackageID != v.context.PackageID || m.BuildID != v.context.BuildID ||
		m.CPUBase != 0x95000000 || m.BusBase != m.CPUBase || m.Bytes != 4<<20 || m.ArenaBase != m.CPUBase || m.ArenaBytes != m.Bytes ||
		m.CPUAttributes != "normal-uncached" || m.Lifetime != "until-reboot" || m.FPGAAddressing != "physical-identity-unqualified" ||
		m.RowBytes != 5120 || m.Stride != 5120 || m.Rows != 720 || m.FrameBytes != 3686400 || m.PixelFormat != "xrgb8888" {
		return m, errors.New("native fabric: window memory differs from the compiled 4 MiB source")
	}
	return m, nil
}

// WindowMemoryObservation covers CPU publication in a boot-pinned arena. The
// descriptor's bus address and live source observation do not grant FPGA access.
type WindowMemoryObservation struct {
	Source                 HPSBootObservation
	CPUBase, ArenaBytes    uint64
	Geometry               scanout.WindowGeometry
	FrameBytes             uint32
	MapSHA256, FrameSHA256 [32]byte
	CRC32                  uint32
}

// ReserveWindowMemory publishes one caller-owned, packed XRGB frame while the
// immutable boot owner is closed. The callback reserves all 4 MiB, including
// zeroed trailing padding, as Normal uncached memory excluded from the heap.
// Returned allocations remain retained through every failure and until reboot;
// this method never calls Region.Release or enables a bridge.
func (v *HPSContextVerifier) ReserveWindowMemory(frame []byte, reserve func([]byte) (*Region, error)) (WindowMemoryObservation, error) {
	if v == nil || v.windowMemory == nil || len(frame) != int(v.windowMemory.FrameBytes) || reserve == nil {
		return WindowMemoryObservation{}, errors.New("native fabric: compiled window and complete CPU frame required")
	}
	v.readerMemoryMu.Lock()
	defer v.readerMemoryMu.Unlock()
	if v.readerMemoryAttempted {
		return WindowMemoryObservation{}, errors.New("native fabric: reader memory attempt already consumed")
	}
	v.readerMemoryAttempted = true
	m := *v.windowMemory
	source, err := v.Verify(v.Identity())
	if err != nil {
		return WindowMemoryObservation{}, err
	}
	if !v.coldSource(source) {
		return WindowMemoryObservation{}, errors.New("native fabric: window reservation requires closed boot ownership")
	}
	staged := make([]byte, int(m.ArenaBytes))
	copy(staged, frame)
	region, err := reserve(bytes.Clone(staged))
	v.readerMemoryRegion = region
	if err != nil {
		return WindowMemoryObservation{}, err
	}
	if region == nil || region.Address != m.CPUBase || !bytes.Equal(region.Bytes, staged) {
		return WindowMemoryObservation{}, errors.New("native fabric: complete uncached window reservation differs")
	}
	after, err := v.Verify(v.Identity())
	if err != nil || after != source || !bytes.Equal(region.Bytes, staged) {
		return WindowMemoryObservation{}, errors.New("native fabric: window publication lost bytes or closed ownership")
	}
	v.readerMemoryReady = true
	v.windowArenaSHA = sha256.Sum256(staged)
	v.windowPublication = WindowMemoryObservation{Source: source, CPUBase: m.CPUBase, ArenaBytes: m.ArenaBytes,
		Geometry:   scanout.WindowGeometry{Base: uint32(m.BusBase), Bytes: uint32(m.Bytes), RowBytes: m.RowBytes, Stride: m.Stride, Rows: m.Rows},
		FrameBytes: m.FrameBytes, MapSHA256: v.windowMemorySHA, FrameSHA256: sha256.Sum256(staged[:m.FrameBytes]),
		CRC32: crc32.ChecksumIEEE(staged[:m.FrameBytes])}
	return v.windowPublication, nil
}
