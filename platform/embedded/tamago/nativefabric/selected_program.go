// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// These private records describe compatibility assertions, not authentication
// or observations of running hardware. The platform record is compiled by the
// board builder; a selected package may only request an exact match to it.
type programPlatform struct {
	Format                int    `json:"format"`
	Scope                 string `json:"scope"`
	Platform              string `json:"platform"`
	Device                string `json:"device"`
	ProgrammingProfile    string `json:"programming_profile"`
	ABI                   string `json:"abi"`
	Major                 uint16 `json:"abi_major"`
	Minor                 uint16 `json:"abi_minor"`
	ControlABI            uint32 `json:"control_abi"`
	HPSIOHash             string `json:"hps_io_hash"`
	PFGIdentity           string `json:"pfg_identity"`
	HPSParametersSHA256   string `json:"hps_parameters_sha256"`
	HPSHandoffSHA256      string `json:"hps_handoff_sha256"`
	MasterInventorySHA256 string `json:"master_inventory_sha256"`
}

type programPlatformSource struct {
	Format            int             `json:"format"`
	Scope             string          `json:"scope"`
	BootContextSHA256 string          `json:"boot_context_sha256"`
	Platform          programPlatform `json:"platform"`
}

type selectedProgramRecord struct {
	Format    int             `json:"format"`
	Scope     string          `json:"scope"`
	PackageID string          `json:"package_id"`
	BuildID   string          `json:"build_id"`
	Platform  programPlatform `json:"platform"`
	Control   hpsControlMap   `json:"control"`
	Computer  computerMap     `json:"computer"`
}

type selectedROMProgramRecord struct {
	selectedProgramRecord
	ROM              romControlMap `json:"rom"`
	ROMReceiptSHA256 string        `json:"rom_receipt_sha256"`
}

// decodeCompleteRecord rejects missing, duplicate, unknown and noncanonical
// fields recursively, including a missing false-valued hardware assertion.
func decodeCompleteRecord(raw []byte, limit int, value any) error {
	if len(raw) == 0 || len(raw) > limit {
		return errors.New("native fabric: bounded complete program record required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("native fabric: trailing program record")
	}
	canonical, err := canonicalProgramRecord(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errors.New("native fabric: program record is incomplete or noncanonical")
	}
	return nil
}

func canonicalProgramRecord(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&fields); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(fields)
	return append(raw, '\n'), err
}

// ProgramLoader keeps immutable boot/platform facts separate from each
// retained program. Construct it only from board-compiled source and maps.
// It permits the existing contained computer contract, not a normal profile,
// a new master, data release or BootHandoffVerifier.
type ProgramLoader struct {
	boot          *HPSContextVerifier
	platform      programPlatform
	configuration coldConfigurationMap
	control       hpsControlMap
	computer      computerMap
}

func NewProgramLoader(boot *HPSContextVerifier, compiledSource, control, configuration, computerRaw []byte) (*ProgramLoader, error) {
	if boot == nil {
		return nil, errors.New("native fabric: immutable boot verifier required")
	}
	var source programPlatformSource
	if err := decodeCompleteRecord(compiledSource, 8192, &source); err != nil {
		return nil, err
	}
	p, c := source.Platform, boot.context
	if source.Format != 1 || source.Scope != "nano-program-platform-source-v1" || source.BootContextSHA256 != hex.EncodeToString(boot.digest[:]) ||
		p.Format != 1 || p.Scope != "nano-contained-program-platform-v1" ||
		p.Platform != c.Platform || p.Device != c.Device || p.ProgrammingProfile != c.ProgrammingProfile ||
		p.ABI != c.ABI || p.Major != c.Major || p.Minor != c.Minor || p.ControlABI != c.ControlABI ||
		p.HPSIOHash != c.HPSIOHash || p.PFGIdentity != c.PFGIdentity ||
		!nonzeroDigest(p.HPSParametersSHA256) || !nonzeroDigest(p.HPSHandoffSHA256) || !nonzeroDigest(p.MasterInventorySHA256) {
		return nil, errors.New("native fabric: program platform differs from compiled boot source")
	}
	ctrl, err := boot.checkedControlMap(control)
	if err != nil {
		return nil, err
	}
	config, err := boot.configurationMap(configuration)
	if err != nil {
		return nil, err
	}
	comp, err := boot.checkedComputerMap(control, computerRaw)
	if err != nil {
		return nil, err
	}
	if boot.context.ProgrammingProfile == HPSFirstROMDiagnosticProfile {
		controlSHA, computerSHA := sha256.Sum256(control), sha256.Sum256(computerRaw)
		if boot.rom == nil || boot.rom.ControlMapSHA256 != hex.EncodeToString(controlSHA[:]) || boot.rom.ComputerMapSHA256 != hex.EncodeToString(computerSHA[:]) {
			return nil, errors.New("native fabric: loader lacks its compiled held-ROM resources")
		}
	}
	return &ProgramLoader{boot, p, config, ctrl, comp}, nil
}

// selectedProgram is private, fully retained and never modified after preflight.
// Public observations, caller maps and package metadata cannot construct it.
type selectedProgram struct {
	loader           *ProgramLoader
	identity         fabricmodel.PackageIdentity
	configuration    coldConfigurationMap
	computer         computerMap
	controlRaw       []byte
	configurationRaw []byte
	computerRaw      []byte
	rom              []byte
	romReceipt       string
}

// Preflight validates the selected package and its retained metadata while SD
// remains usable. Payload digests are derived from that archive, never a
// compiled per-core table. No firmware call or reservation occurs here.
func (l *ProgramLoader) Preflight(selection *packagestore.Selection) (*StoredConfigurationPlan, error) {
	if l == nil || l.boot == nil || selection == nil {
		return nil, errors.New("native fabric: program loader and retained selection required")
	}
	var record selectedProgramRecord
	romMode := l.boot.rom != nil
	var romRecord *romControlMap
	var romReceipt string
	if romMode {
		var rr selectedROMProgramRecord
		if err := decodeCompleteRecord(selection.Metadata(), 16<<10, &rr); err != nil {
			return nil, err
		}
		record, romRecord, romReceipt = rr.selectedProgramRecord, &rr.ROM, rr.ROMReceiptSHA256
	} else {
		if err := decodeCompleteRecord(selection.Metadata(), 16<<10, &record); err != nil {
			return nil, err
		}
	}
	i := selection.Inspection()
	d := i.Descriptor
	p := l.platform
	format, scope := int64(2), "nano-selected-program-v1"
	if romMode {
		format, scope = 3, "nano-selected-rom-program-v1"
	}
	if record.Format != 1 || record.Scope != scope || record.Platform != p ||
		record.PackageID != i.PackageID || record.BuildID != d.Build.ID ||
		d.Format != format || (!romMode && selection.Linked() != nil) || d.Target.Platform != p.Platform || d.Target.Device != p.Device ||
		d.Target.ProgrammingProfile != p.ProgrammingProfile || d.ABI.ID != p.ABI || d.ABI.Major != int64(p.Major) || d.ABI.Minor != int64(p.Minor) ||
		d.Payload.Size <= 0 || d.Payload.Size > MaxProgramBytes || d.Payload.Size%4 != 0 {
		return nil, errors.New("native fabric: selected package differs from the contained platform contract")
	}
	if _, err := BuildWords(d.Build.ID); err != nil {
		return nil, err
	}
	for _, iface := range d.Interfaces {
		if iface.Required && ((iface.ID != "fes.embedded.control" && (!romMode || iface.ID != "fes.embedded.rom")) || iface.Major != 1 || iface.Minor != 0) {
			return nil, errors.New("native fabric: selected package requires an unsupported interface")
		}
	}
	id := fabricmodel.PackageIdentity{PackageID: i.PackageID, BuildID: d.Build.ID, ProgrammedSHA256: d.Payload.SHA256,
		CoreID: d.Core.ID, Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile,
		ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor)}
	target := l.boot.context
	target.PackageID, target.BuildID = id.PackageID, id.BuildID
	control, err := canonicalProgramRecord(record.Control)
	if err != nil {
		return nil, err
	}
	ctrl, err := checkedControlMapFor(target, control)
	if err != nil {
		return nil, err
	}
	// The selected compiler handoff may differ. All addresses, stop policy and
	// bridge geometry must equal the platform's compiled maps.
	normal := ctrl
	normal.PackageID, normal.BuildID, normal.HandoffSHA256 = l.control.PackageID, l.control.BuildID, l.control.HandoffSHA256
	normalRaw, _ := canonicalProgramRecord(normal)
	compiledRaw, _ := canonicalProgramRecord(l.control)
	if !bytes.Equal(normalRaw, compiledRaw) {
		return nil, errors.New("native fabric: selected control map changes platform hardware")
	}
	computerRaw, err := canonicalProgramRecord(record.Computer)
	if err != nil {
		return nil, err
	}
	comp, err := checkedComputerMapFor(target, control, computerRaw)
	if err != nil {
		return nil, err
	}
	normalComp := comp
	normalComp.PackageID, normalComp.BuildID = l.computer.PackageID, l.computer.BuildID
	normalComp.ControlMapSHA256, normalComp.HandoffSHA256 = l.computer.ControlMapSHA256, l.computer.HandoffSHA256
	normalComp.EndpointSHA256, normalComp.WrapperSHA256 = l.computer.EndpointSHA256, l.computer.WrapperSHA256
	if normalComp != l.computer {
		return nil, errors.New("native fabric: selected computer changes platform hardware")
	}
	var romImage []byte
	if romMode {
		romRaw, err := canonicalProgramRecord(romRecord)
		if err != nil {
			return nil, err
		}
		rm, err := checkedROMMapFor(target, control, computerRaw, romRaw)
		if err != nil {
			return nil, err
		}
		normal := rm
		bootROM := l.boot.rom
		normal.PackageID, normal.BuildID = bootROM.PackageID, bootROM.BuildID
		normal.ControlMapSHA256, normal.ComputerMapSHA256, normal.HandoffSHA256 = bootROM.ControlMapSHA256, bootROM.ComputerMapSHA256, bootROM.HandoffSHA256
		normal.Bytes, normal.SourceSHA256 = bootROM.Bytes, bootROM.SourceSHA256
		normalRaw, _ := canonicalProgramRecord(normal)
		compiledRaw, _ := canonicalProgramRecord(bootROM)
		linked := selection.Linked()
		if !bytes.Equal(normalRaw, compiledRaw) || linked == nil || !nonzeroDigest(romReceipt) || linked.ReceiptSHA256() != romReceipt ||
			linked.Inspection().PackageID != i.PackageID || d.ROM == nil || d.ROM.Role != "firmware" || int64(rm.Bytes) != d.ROM.SourceSize {
			return nil, errors.New("native fabric: retained ROM or resources differ from selected program")
		}
		receipt := linked.Receipt()
		if receipt.PackageID != i.PackageID || receipt.ROMLink == nil || !receipt.ROMLink.ValidFor(d) || receipt.ROMLinks != nil || receipt.PartsComposition != nil {
			return nil, errors.New("native fabric: complete held-ROM activation receipt required")
		}
		reader, err := linked.Source(d.ROM.ID, "firmware")
		if err != nil {
			return nil, err
		}
		romImage, err = io.ReadAll(io.LimitReader(reader, d.ROM.SourceSize+1))
		sum := sha256.Sum256(romImage)
		if err != nil || len(romImage) != rm.Bytes || hex.EncodeToString(sum[:]) != receipt.ROMLink.SourceSHA256 {
			return nil, errors.New("native fabric: retained ROM bytes differ from private receipt")
		}
		id.ActivationReceiptSHA256 = romReceipt
	}
	data, err := io.ReadAll(io.LimitReader(selection.Archive().Payload(), d.Payload.Size+1))
	if err != nil {
		return nil, err
	}
	config := l.configuration
	config.PackageID, config.BuildID, config.CoreSHA256, config.CoreSize = id.PackageID, id.BuildID, id.ProgrammedSHA256, int(d.Payload.Size)
	sum := sha512.Sum384(data)
	config.CoreSHA384, config.ChunkSHA384 = hex.EncodeToString(sum[:]), nil
	for offset := 0; offset < len(data); offset += config.ChunkBytes {
		sum := sha512.Sum384(data[offset:min(offset+config.ChunkBytes, len(data))])
		config.ChunkSHA384 = append(config.ChunkSHA384, hex.EncodeToString(sum[:]))
	}
	if len(config.ChunkSHA384) > maxProgramChunks {
		return nil, errors.New("native fabric: selected program exceeds bounded chunk inventory")
	}
	if err := checkConfigurationBytes(config, data); err != nil {
		return nil, err
	}
	if romMode {
		if _, _, err := programROMExtension(config.CoreSize, romImage); err != nil {
			return nil, err
		}
	}
	configurationRaw, err := canonicalProgramRecord(config)
	if err != nil {
		return nil, err
	}
	program := &selectedProgram{loader: l, identity: id, configuration: config, computer: comp,
		controlRaw: control, configurationRaw: configurationRaw, computerRaw: computerRaw, rom: romImage, romReceipt: romReceipt}
	return &StoredConfigurationPlan{state: &storedConfigurationPlan{v: l.boot, program: program, selection: selection,
		control: bytes.Clone(control), configuration: bytes.Clone(configurationRaw), payload: data}}, nil
}

func (l *ProgramLoader) program(transfer *ConfigurationTransfer) (*selectedProgram, error) {
	if l == nil || transfer == nil || transfer.binding == nil || transfer.binding.verifier != l.boot ||
		transfer.binding.program == nil || transfer.binding.program.loader != l {
		return nil, errors.New("native fabric: transfer belongs to another program loader")
	}
	return transfer.binding.program, nil
}

func (l *ProgramLoader) OpenComputer(ctx context.Context, transfer *ConfigurationTransfer, limits computer.Limits) (*ComputerSession, error) {
	p, err := l.program(transfer)
	if err != nil {
		return nil, err
	}
	return l.boot.OpenComputer(ctx, p.controlRaw, p.computerRaw, transfer, limits)
}

func (l *ProgramLoader) RetireConfiguration(transfer *ConfigurationTransfer, source HPSBootObservation) (ConfigurationRetirement, error) {
	p, err := l.program(transfer)
	if err != nil {
		return ConfigurationRetirement{}, err
	}
	return l.boot.RetireConfiguration(transfer, source, p.configurationRaw)
}

// Identity is the retained requested program, not a live FPGA observation.
func (p *StoredConfigurationPlan) Identity() fabricmodel.PackageIdentity {
	if p == nil || p.state == nil {
		return fabricmodel.PackageIdentity{}
	}
	if p.state.program != nil {
		return p.state.program.identity
	}
	return p.state.v.Identity()
}

func (t *ConfigurationTransfer) ProgramIdentity() (fabricmodel.PackageIdentity, bool) {
	if t == nil || t.binding == nil || t.binding.verifier == nil {
		return fabricmodel.PackageIdentity{}, false
	}
	if t.binding.program != nil {
		return t.binding.program.identity, true
	}
	return t.binding.verifier.Identity(), true
}

// Kept local so descriptor assertions never become a boot verification call.
func (v *HPSContextVerifier) programConfiguration(control, raw []byte, p *selectedProgram) (coldConfigurationMap, error) {
	if v != nil && v.rom != nil && (p == nil || len(p.rom) == 0) {
		return coldConfigurationMap{}, errors.New("native fabric: ROM configuration requires its retained selected program")
	}
	if p == nil {
		if _, err := v.checkedControlMap(control); err != nil {
			return coldConfigurationMap{}, err
		}
		return v.configurationMap(raw)
	}
	if p.loader == nil || p.loader.boot != v || !bytes.Equal(control, p.controlRaw) || !bytes.Equal(raw, p.configurationRaw) {
		return coldConfigurationMap{}, errors.New("native fabric: selected program maps differ from retained binding")
	}
	return p.configuration, nil
}

// ROMReceiptSHA256 is cached retained-content evidence, not live ROM completion.
func (p *StoredConfigurationPlan) ROMReceiptSHA256() string {
	if p == nil || p.state == nil || p.state.program == nil {
		return ""
	}
	return p.state.program.romReceipt
}
