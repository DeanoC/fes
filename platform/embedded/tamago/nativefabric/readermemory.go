// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

type readerMemoryMap struct {
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
	Pattern        string `json:"pattern"`
	PatternSHA256  string `json:"pattern_sha256"`
}

func readerMemoryPattern() []byte {
	data := make([]byte, 8192)
	for i := 0; i < len(data)/4; i++ {
		binary.LittleEndian.PutUint32(data[i*4:], 0x4e414e4f^uint32(i)*0x9e3779b9)
	}
	return data
}

func (v *HPSContextVerifier) readerMemoryMap(raw []byte) (readerMemoryMap, error) {
	if v == nil || (v.context.ProgrammingProfile != HPSFirstDDRDiagnosticProfile && !heldROMProfile(v.context.ProgrammingProfile)) || len(raw) == 0 || len(raw) > 4096 {
		return readerMemoryMap{}, errors.New("native fabric: compiled DDR reader source and memory map required")
	}
	var m readerMemoryMap
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return readerMemoryMap{}, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return readerMemoryMap{}, errors.New("native fabric: trailing reader memory map")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return readerMemoryMap{}, err
	}
	canonical, err := json.Marshal(fields)
	patternHash := sha256.Sum256(readerMemoryPattern())
	if err != nil || len(fields) != 14 || !bytes.Equal(raw, append(canonical, '\n')) ||
		m.Format != 1 || m.Scope != "sealed-cold-ddr-reader-memory-v1" || m.PackageID != v.context.PackageID || m.BuildID != v.context.BuildID ||
		m.CPUBase != 0x95000000 || m.BusBase != m.CPUBase || m.Bytes != 8192 || m.ArenaBase != m.CPUBase || m.ArenaBytes != 2<<20 ||
		m.CPUAttributes != "normal-uncached" || m.Lifetime != "until-reboot" || m.FPGAAddressing != "physical-identity-unqualified" ||
		m.Pattern != "le32-xor-nano-golden-v1" || m.PatternSHA256 != hex.EncodeToString(patternHash[:]) {
		return readerMemoryMap{}, errors.New("native fabric: reader memory differs from the compiled cold source")
	}
	return m, nil
}

// ReaderMemoryObservation describes a boot-pinned CPU reservation. BusBase is
// the intended FPGA address; it is not a live translation or access grant.
type ReaderMemoryObservation struct {
	Source                                         HPSBootObservation
	CPUBase, BusBase, Bytes, ArenaBase, ArenaBytes uint64
	MapSHA256, PatternSHA256                       [32]byte
}

// ReserveReaderMemory stages one fixed pattern only while the original cold
// ownership is closed. The platform must reserve the entire uncached arena
// until reboot. Neither success nor any error calls Region.Release. Even an
// allocation returned alongside an error remains retained by this verifier.
// This performs no bridge mutation, FPGA access or runtime admission.
func (v *HPSContextVerifier) ReserveReaderMemory(raw []byte, reserve func([]byte) (*Region, error)) (ReaderMemoryObservation, error) {
	m, err := v.readerMemoryMap(raw)
	if err != nil {
		return ReaderMemoryObservation{}, err
	}
	if reserve == nil {
		return ReaderMemoryObservation{}, errors.New("native fabric: boot-pinned reader reservation missing")
	}
	v.readerMemoryMu.Lock()
	defer v.readerMemoryMu.Unlock()
	if v.readerMemoryAttempted {
		return ReaderMemoryObservation{}, errors.New("native fabric: reader memory attempt already consumed")
	}
	v.readerMemoryAttempted = true
	source, err := v.Verify(v.Identity())
	if err != nil {
		return ReaderMemoryObservation{}, err
	}
	if !v.coldSource(source) {
		return ReaderMemoryObservation{}, errors.New("native fabric: reader memory requires closed cold ownership")
	}
	pattern := readerMemoryPattern()
	region, err := reserve(bytes.Clone(pattern))
	v.readerMemoryRegion = region
	if err != nil {
		return ReaderMemoryObservation{}, err
	}
	if region == nil || region.Address != m.CPUBase || !bytes.Equal(region.Bytes, pattern) {
		return ReaderMemoryObservation{}, errors.New("native fabric: reader reservation differs from compiled memory")
	}
	after, err := v.Verify(v.Identity())
	if err != nil || after != source || !bytes.Equal(region.Bytes, pattern) {
		return ReaderMemoryObservation{}, errors.New("native fabric: reader reservation lost bytes or closed ownership")
	}
	v.readerMemoryReady = true
	return ReaderMemoryObservation{source, m.CPUBase, m.BusBase, m.Bytes, m.ArenaBase, m.ArenaBytes, sha256.Sum256(raw), sha256.Sum256(pattern)}, nil
}
