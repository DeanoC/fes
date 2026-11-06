// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const HPSFirstROMDiagnosticProfile = "de25-embedded-hps-first-rom-diagnostic-v1"
const NativeComputerProfile = "de25-native-computer-v1"

// This source constructor selects only the reviewed boot producer. Runtime
// selections are independently checked against the shared ABI/profile registry.
func nativeComputerSource(c HPSBootContext) bool {
	return c.ProgrammingProfile == NativeComputerProfile && c.ABI == "fes.computer" &&
		c.Major == 1 && c.Minor == 0 && c.ControlABI == 0x10203041 &&
		(c.CoreID == "fes.zx80" || c.CoreID == "fes.zx81")
}

func heldROMProfile(profile string) bool {
	return profile == HPSFirstROMDiagnosticProfile || profile == NativeComputerProfile
}

type romControlMap struct {
	Format            int               `json:"format"`
	Scope             string            `json:"scope"`
	PackageID         string            `json:"package_id"`
	BuildID           string            `json:"build_id"`
	ControlMapSHA256  string            `json:"control_map_sha256"`
	ComputerMapSHA256 string            `json:"computer_map_sha256"`
	HandoffSHA256     string            `json:"handoff_sha256"`
	CPUBase           uint64            `json:"cpu_base"`
	Span              uint32            `json:"span"`
	Bytes             int               `json:"bytes"`
	ClockHz           uint64            `json:"clock_hz"`
	Transport         string            `json:"transport"`
	Role              string            `json:"role"`
	HeldConduit       string            `json:"held_conduit"`
	HPSMaster         bool              `json:"hps_master"`
	SourceSHA256      map[string]string `json:"source_sha256"`
}

// NewROMHPSContextVerifier requires all compiled ROM/computer maps explicitly.
// It performs no I/O, maps no CSR, and grants no bridge or programming authority.
func NewROMHPSContextVerifier(cpu FirmwareCaller, source, control, computer, rom []byte) (*HPSContextVerifier, error) {
	return newHeldROMHPSContextVerifier(cpu, source, control, computer, rom, HPSFirstROMDiagnosticProfile)
}

// NewComputerHPSContextVerifier explicitly selects the native fes.computer boot
// source and its held-ROM resources. It supplies no live boot qualification or
// physical activation permission; ProgramLoader additionally checks semantics
// and its compiled driver inventory before storage containment.
func NewComputerHPSContextVerifier(cpu FirmwareCaller, source, control, computer, rom []byte) (*HPSContextVerifier, error) {
	return newHeldROMHPSContextVerifier(cpu, source, control, computer, rom, NativeComputerProfile)
}

func newHeldROMHPSContextVerifier(cpu FirmwareCaller, source, control, computer, rom []byte, profile string) (*HPSContextVerifier, error) {
	v, err := newHPSContextVerifier(cpu, source, profile)
	if err != nil {
		return nil, err
	}
	if v.context.ProgrammingProfile != profile {
		return nil, errors.New("native fabric: boot source differs from explicitly selected ROM profile")
	}
	m, err := checkedROMMapFor(v.context, control, computer, rom)
	if err != nil {
		return nil, err
	}
	v.rom = &m
	return v, nil
}

func checkedROMMapFor(source HPSBootContext, control, computer, raw []byte) (romControlMap, error) {
	var m romControlMap
	if !heldROMProfile(source.ProgrammingProfile) || source.ControlABI != 0x10203041 {
		return m, errors.New("native fabric: explicit held-ROM source required")
	}
	c, err := checkedControlMapFor(source, control)
	if err != nil {
		return m, err
	}
	if _, err = checkedComputerMapFor(source, control, computer); err != nil {
		return m, err
	}
	if err = decodeCompleteRecord(raw, 8192, &m); err != nil {
		return m, err
	}
	controlSHA, computerSHA := sha256.Sum256(control), sha256.Sum256(computer)
	if m.Format != 1 || m.Scope != "sealed-cold-held-rom-v1" || m.PackageID != source.PackageID || m.BuildID != source.BuildID ||
		m.ControlMapSHA256 != hex.EncodeToString(controlSHA[:]) || m.ComputerMapSHA256 != hex.EncodeToString(computerSHA[:]) ||
		m.HandoffSHA256 != c.HandoffSHA256 || m.CPUBase != 0x20010500 || m.Span != 128 ||
		(m.Bytes != 4096 && m.Bytes != 8192) || m.ClockHz != 100000000 || m.Transport != "cpu-csr-v1" || m.Role != "firmware" ||
		m.HeldConduit != "fes_computer.rom/fes_rom.rom" || m.HPSMaster || len(m.SourceSHA256) != 5 {
		return romControlMap{}, errors.New("native fabric: held-ROM map differs from compiled contract")
	}
	for _, name := range []string{"fes_de25_rom.sv", "fes_de25_rom_hw.tcl", "fes_embedded_rom.sv", "fes_embedded_rom_csr.sv", "generated/fes_embedded_rom.vh"} {
		if !nonzeroDigest(m.SourceSHA256[name]) {
			return romControlMap{}, errors.New("native fabric: incomplete ROM source inventory")
		}
	}
	return m, nil
}
