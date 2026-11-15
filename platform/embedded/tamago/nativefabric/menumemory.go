// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// A separate scope and complete two-slot geometry; a 4 MiB window cannot grant it.
type menuMemoryMap struct {
	windowMemoryMap
	Slots     uint32 `json:"slots"`
	SlotBytes uint64 `json:"slot_bytes"`
}
type menuReadMap struct {
	Format                 int    `json:"format"`
	Scope                  string `json:"scope"`
	PackageID              string `json:"package_id"`
	BuildID                string `json:"build_id"`
	ReaderMemorySHA256     string `json:"reader_memory_sha256"`
	ControlMapSHA256       string `json:"control_map_sha256"`
	AdapterSourceSHA256    string `json:"adapter_source_sha256"`
	AdapterGeneratedSHA256 string `json:"adapter_generated_sha256"`
	ARCache                uint32 `json:"arcache"`
	ARProt                 uint32 `json:"arprot"`
	ARUser                 uint32 `json:"aruser"`
	ReadBytes              uint32 `json:"read_bytes"`
	OpenMask               uint32 `json:"open_mask"`
	CloseMask              uint32 `json:"close_mask"`
}

func decodeMenuMap(raw []byte, target any, fields int) error {
	if len(raw) == 0 || len(raw) > 4096 {
		return errors.New("native menu: bounded compiled map required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("native menu: trailing map data")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return err
	}
	canonical, err := json.Marshal(members)
	if err != nil || len(members) != fields || !bytes.Equal(raw, append(canonical, '\n')) {
		return errors.New("native menu: incomplete or noncanonical compiled map")
	}
	return nil
}

// NewMenuHPSContextVerifier requires the explicitly compiled menu personality.
// It performs no I/O, grants no boot admission and cannot relabel an older reader.
func NewMenuHPSContextVerifier(cpu FirmwareCaller, source, control, memory, access []byte) (*HPSContextVerifier, error) {
	v, err := newHPSContextVerifier(cpu, source, HPSFirstMenuProfile)
	if err != nil {
		return nil, err
	}
	if v.context.ProgrammingProfile != HPSFirstMenuProfile {
		return nil, errors.New("native menu: explicit menu source required")
	}
	if _, err = v.checkedControlMap(control); err != nil {
		return nil, err
	}
	var m menuMemoryMap
	if err = decodeMenuMap(memory, &m, 19); err != nil {
		return nil, err
	}
	if m.Format != 1 || m.Scope != "sealed-cold-menu-reader-memory-v1" || m.PackageID != v.context.PackageID || m.BuildID != v.context.BuildID ||
		m.CPUBase != 0x95000000 || m.BusBase != m.CPUBase || m.Bytes != scanout.MenuArenaBytes || m.ArenaBase != m.CPUBase || m.ArenaBytes != m.Bytes ||
		m.CPUAttributes != "normal-uncached" || m.Lifetime != "until-reboot" || m.FPGAAddressing != "physical-identity-unqualified" ||
		m.RowBytes != 5120 || m.Stride != 5120 || m.Rows != 720 || m.FrameBytes != 3686400 || m.PixelFormat != "xrgb8888" || m.Slots != 2 || m.SlotBytes != 4<<20 {
		return nil, errors.New("native menu: memory differs from compiled two-slot arena")
	}
	v.menuMemory = &m
	v.menuMemorySHA = sha256.Sum256(memory)
	if _, err = v.checkedMenuRead(control, access); err != nil {
		return nil, err
	}
	return v, nil
}
func (v *HPSContextVerifier) checkedMenuRead(control, raw []byte) (hpsControlMap, error) {
	if v == nil || v.menuMemory == nil || v.context.ProgrammingProfile != HPSFirstMenuProfile {
		return hpsControlMap{}, errors.New("native menu: compiled memory missing")
	}
	c, err := v.checkedControlMap(control)
	if err != nil {
		return c, err
	}
	var access menuReadMap
	if err = decodeMenuMap(raw, &access, 14); err != nil {
		return c, err
	}
	h := sha256.Sum256(control)
	if access.Format != 1 || access.Scope != "sealed-cold-menu-read-v1" || access.PackageID != v.context.PackageID || access.BuildID != v.context.BuildID ||
		access.ReaderMemorySHA256 != hex.EncodeToString(v.menuMemorySHA[:]) || access.ControlMapSHA256 != hex.EncodeToString(h[:]) ||
		!nonzeroDigest(access.AdapterSourceSHA256) || !nonzeroDigest(access.AdapterGeneratedSHA256) ||
		access.ARCache != 2 || access.ARProt != 3 || access.ARUser != 224 || access.ReadBytes != v.menuMemory.FrameBytes || access.OpenMask != 8 || access.CloseMask != 10 {
		return c, errors.New("native menu: read access differs from complete compiled contract")
	}
	return c, nil
}

// MenuMemoryObservation records reservation under closed secure ownership.
// Both the actual arena and a private expected image remain rooted until reboot.
// It supplies no FPGA read permission or allocation-release authority.
type MenuMemoryObservation struct {
	Source                         HPSBootObservation
	CPUBase, ArenaBytes, SlotBytes uint64
	Slots                          uint32
	MapSHA256, InitialSHA256       [32]byte
}

func (v *HPSContextVerifier) ReserveMenuMemory(reserve func([]byte) (*Region, error)) (MenuMemoryObservation, error) {
	if v == nil || v.menuMemory == nil || reserve == nil {
		return MenuMemoryObservation{}, errors.New("native menu: compiled reservation required")
	}
	v.readerMemoryMu.Lock()
	defer v.readerMemoryMu.Unlock()
	return v.reserveMenuMemoryLocked(reserve)
}

func (v *HPSContextVerifier) reserveMenuMemoryLocked(reserve func([]byte) (*Region, error)) (MenuMemoryObservation, error) {
	if v.readerMemoryAttempted {
		return MenuMemoryObservation{}, errors.New("native menu: memory attempt consumed")
	}
	v.readerMemoryAttempted = true
	source, err := v.Verify(v.Identity())
	if err != nil {
		return MenuMemoryObservation{}, err
	}
	if !v.coldSource(source) {
		return MenuMemoryObservation{}, errors.New("native menu: closed cold owner required")
	}
	expected := make([]byte, int(v.menuMemory.ArenaBytes))
	v.menuExpected = expected
	region, err := reserve(bytes.Clone(expected))
	v.readerMemoryRegion = region
	if err != nil {
		return MenuMemoryObservation{}, err
	}
	if region == nil || region.Address != v.menuMemory.CPUBase || region.OrderFrameStores == nil || !bytes.Equal(region.Bytes, expected) {
		return MenuMemoryObservation{}, errors.New("native menu: full ordered boot-pinned arena required")
	}
	after, err := v.Verify(v.Identity())
	if err != nil || after != source || !bytes.Equal(region.Bytes, expected) {
		return MenuMemoryObservation{}, errors.New("native menu: reservation lost bytes or closed owner")
	}
	v.readerMemoryReady = true
	v.menuPublication = MenuMemoryObservation{source, region.Address, v.menuMemory.ArenaBytes, v.menuMemory.SlotBytes, v.menuMemory.Slots, v.menuMemorySHA, sha256.Sum256(expected)}
	return v.menuPublication, nil
}
