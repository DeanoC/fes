// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// CompiledMenuProgram is supplied by board integration, never by a selection.
// Each complete source-derived map is privately decoded before use. These are
// compatibility assertions, not live memory, master or bridge permissions.
type CompiledMenuProgram struct {
	Context, Platform, Control, Memory, Access []byte
}

type menuProgramDriver struct {
	context   HPSBootContext
	platform  programPlatform
	control   hpsControlMap
	memory    menuMemoryMap
	access    menuReadMap
	memorySHA [32]byte
	support   *runtimeadmission.DriverSupport
}

type selectedMenuProgramRecord struct {
	Format    int             `json:"format"`
	Scope     string          `json:"scope"`
	PackageID string          `json:"package_id"`
	BuildID   string          `json:"build_id"`
	Platform  programPlatform `json:"platform"`
	Control   hpsControlMap   `json:"control"`
	Memory    menuMemoryMap   `json:"memory"`
	Access    menuReadMap     `json:"access"`
}

type selectedMenuProgram struct {
	control              hpsControlMap
	memoryRaw, accessRaw []byte
}

// NewProgramLoaderWithMenu adds one compiled menu driver to the existing normal
// computer loader. The boot verifier is never replaced or relabelled. Both role
// records must agree on every shared HPS/DDR/board fingerprint, while each keeps
// its exact master inventory. The matched firmware must explicitly enable v3.
func NewProgramLoaderWithMenu(boot *HPSContextVerifier, compiledSource, control, configuration, computerRaw []byte, menu CompiledMenuProgram) (*ProgramLoader, error) {
	l, err := NewProgramLoader(boot, compiledSource, control, configuration, computerRaw)
	if err != nil {
		return nil, err
	}
	if l.platform.ProgrammingProfile != NativeComputerProfile || boot.rom == nil {
		return nil, errors.New("native fabric: menu role requires the normal compiled computer driver")
	}
	view, err := NewMenuHPSContextVerifier(boot.cpu, menu.Context, menu.Control, menu.Memory, menu.Access)
	if err != nil {
		return nil, err
	}
	var source programPlatformSource
	if err := decodeCompleteRecord(menu.Platform, 8192, &source); err != nil {
		return nil, err
	}
	p, c, b := source.Platform, view.context, l.platform
	if source.Format != 1 || source.Scope != "nano-program-platform-source-v1" || source.BootContextSHA256 != hex.EncodeToString(view.digest[:]) ||
		p.Format != 2 || p.Scope != "nano-contained-program-platform-v2" || p.Platform != c.Platform || p.Device != c.Device ||
		p.ProgrammingProfile != c.ProgrammingProfile || p.ABI != c.ABI || p.Major != c.Major || p.Minor != c.Minor || p.ControlABI != c.ControlABI ||
		p.HPSIOHash != c.HPSIOHash || p.PFGIdentity != c.PFGIdentity ||
		p.Platform != b.Platform || p.Device != b.Device || p.HPSIOHash != b.HPSIOHash || p.PFGIdentity != b.PFGIdentity ||
		p.HPSParametersSHA256 != b.HPSParametersSHA256 || p.HPSConfigurationSHA256 != b.HPSConfigurationSHA256 || !nonzeroDigest(p.MasterInventorySHA256) {
		return nil, errors.New("native fabric: compiled menu changes the shared boot platform")
	}
	ctrl, err := view.checkedMenuRead(menu.Control, menu.Access)
	if err != nil {
		return nil, err
	}
	var access menuReadMap
	if err := decodeMenuMap(menu.Access, &access, 14); err != nil {
		return nil, err
	}
	support, err := runtimeadmission.NewDriverSupport(runtimeadmission.Target{Platform: p.Platform, Device: p.Device}, []runtimeadmission.Driver{{
		Profile: p.ProgrammingProfile, ABI: runtimeadmission.Contract{ID: p.ABI, Major: int64(p.Major), Minor: int64(p.Minor)},
		Interfaces: []runtimeadmission.Contract{{ID: "fes.embedded.control", Major: 1}},
	}})
	if err != nil {
		return nil, err
	}
	l.menu = &menuProgramDriver{context: c, platform: p, control: ctrl, memory: *view.menuMemory, access: access, memorySHA: view.menuMemorySHA, support: support}
	return l, nil
}

// ReserveMenuMemory uses the original boot owner and one full pinned arena.
// A package can neither install these maps nor resize this reservation.
func (l *ProgramLoader) ReserveMenuMemory(reserve func([]byte) (*Region, error)) (MenuMemoryObservation, error) {
	if l == nil || l.boot == nil || l.menu == nil || reserve == nil {
		return MenuMemoryObservation{}, errors.New("native fabric: compiled menu loader required")
	}
	v := l.boot
	v.readerMemoryMu.Lock()
	defer v.readerMemoryMu.Unlock()
	if v.readerMemoryAttempted || v.menuMemory != nil || v.windowMemory != nil {
		return MenuMemoryObservation{}, errors.New("native fabric: reader memory already selected")
	}
	m := l.menu.memory
	v.menuMemory, v.menuMemorySHA = &m, l.menu.memorySHA
	return v.reserveMenuMemoryLocked(reserve)
}

func (l *ProgramLoader) preflightMenu(selection *packagestore.Selection) (*StoredConfigurationPlan, error) {
	if l.menu == nil {
		return nil, errors.New("native fabric: no compiled menu driver")
	}
	var r selectedMenuProgramRecord
	if err := decodeCompleteRecord(selection.Metadata(), 16<<10, &r); err != nil {
		return nil, err
	}
	i, driver := selection.Inspection(), l.menu
	d, p := i.Descriptor, driver.platform
	if r.Format != 1 || r.Scope != "nano-selected-menu-program-v1" || r.Platform != p || r.PackageID != i.PackageID || r.BuildID != d.Build.ID ||
		d.Format != 2 || selection.Linked() != nil || d.Target.Platform != p.Platform || d.Target.Device != p.Device ||
		d.Target.ProgrammingProfile != p.ProgrammingProfile || d.ABI.ID != p.ABI || d.ABI.Major != int64(p.Major) || d.ABI.Minor != int64(p.Minor) ||
		d.Payload.Size <= 0 || d.Payload.Size > MaxProgramBytes || d.Payload.Size%4 != 0 {
		return nil, errors.New("native fabric: selected menu differs from compiled driver")
	}
	if err := driver.support.CheckArchive(selection.Archive(), i.PackageID); err != nil {
		return nil, err
	}
	if _, err := BuildWords(d.Build.ID); err != nil {
		return nil, err
	}
	target := driver.context
	target.PackageID, target.BuildID = i.PackageID, d.Build.ID
	controlRaw, _ := canonicalProgramRecord(r.Control)
	ctrl, err := checkedControlMapFor(target, controlRaw)
	if err != nil {
		return nil, err
	}
	normal := ctrl
	normal.PackageID, normal.BuildID, normal.HandoffSHA256 = driver.control.PackageID, driver.control.BuildID, driver.control.HandoffSHA256
	normalRaw, _ := canonicalProgramRecord(normal)
	compiledRaw, _ := canonicalProgramRecord(driver.control)
	if !bytes.Equal(normalRaw, compiledRaw) {
		return nil, errors.New("native fabric: selected menu changes control hardware")
	}
	normalMemory := r.Memory
	normalMemory.PackageID, normalMemory.BuildID = driver.memory.PackageID, driver.memory.BuildID
	if normalMemory != driver.memory || r.Memory.PackageID != i.PackageID || r.Memory.BuildID != d.Build.ID {
		return nil, errors.New("native fabric: selected menu changes reserved geometry")
	}
	memoryRaw, _ := canonicalProgramRecord(r.Memory)
	ch, mh := sha256.Sum256(controlRaw), sha256.Sum256(memoryRaw)
	access := r.Access
	if access.PackageID != i.PackageID || access.BuildID != d.Build.ID || access.ControlMapSHA256 != hex.EncodeToString(ch[:]) || access.ReaderMemorySHA256 != hex.EncodeToString(mh[:]) {
		return nil, errors.New("native fabric: selected menu access is not bound to its maps")
	}
	access.PackageID, access.BuildID = driver.access.PackageID, driver.access.BuildID
	access.ControlMapSHA256, access.ReaderMemorySHA256 = driver.access.ControlMapSHA256, driver.access.ReaderMemorySHA256
	if access != driver.access {
		return nil, errors.New("native fabric: selected menu changes its compiled adapter")
	}
	data, err := io.ReadAll(io.LimitReader(selection.Archive().Payload(), d.Payload.Size+1))
	if err != nil {
		return nil, err
	}
	config := l.configuration
	config.PackageID, config.BuildID, config.CoreSHA256, config.CoreSize = i.PackageID, d.Build.ID, d.Payload.SHA256, int(d.Payload.Size)
	hash := sha512.Sum384(data)
	config.CoreSHA384, config.ChunkSHA384 = hex.EncodeToString(hash[:]), nil
	for offset := 0; offset < len(data); offset += config.ChunkBytes {
		hash := sha512.Sum384(data[offset:min(offset+config.ChunkBytes, len(data))])
		config.ChunkSHA384 = append(config.ChunkSHA384, hex.EncodeToString(hash[:]))
	}
	if len(config.ChunkSHA384) > maxProgramChunks {
		return nil, errors.New("native fabric: selected menu exceeds chunk inventory")
	}
	if err := checkConfigurationBytes(config, data); err != nil {
		return nil, err
	}
	configRaw, _ := canonicalProgramRecord(config)
	accessRaw, _ := canonicalProgramRecord(r.Access)
	program := &selectedProgram{role: programRoleMenu, loader: l,
		identity: fabricmodel.PackageIdentity{PackageID: i.PackageID, BuildID: d.Build.ID, ProgrammedSHA256: d.Payload.SHA256,
			CoreID: d.Core.ID, Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile,
			ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor)},
		configuration: config, controlRaw: controlRaw, configurationRaw: configRaw,
		menu: &selectedMenuProgram{control: ctrl, memoryRaw: memoryRaw, accessRaw: accessRaw}}
	return &StoredConfigurationPlan{state: &storedConfigurationPlan{v: l.boot, program: program, selection: selection,
		control: bytes.Clone(controlRaw), configuration: bytes.Clone(configRaw), payload: data}}, nil
}

func (l *ProgramLoader) OpenMenu(ctx context.Context, transfer *ConfigurationTransfer, limits scanout.MenuLimits) (*MenuSession, error) {
	p, err := l.program(transfer)
	if err != nil {
		return nil, err
	}
	if p.role != programRoleMenu || p.menu == nil || l.menu == nil {
		return nil, errors.New("native fabric: completed transfer is not a menu")
	}
	return l.boot.openMenuSession(ctx, p.controlRaw, p.menu.accessRaw, transfer, limits, p)
}
