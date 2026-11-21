// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

const spectrum48Profile = "de25-native-spectrum48-v1"
const spectrum48ROMBytes = 16 << 10
const spectrum48Capabilities = cg.FesComputerCapabilityVideoFixed720p60 |
	cg.FesComputerCapabilityKeyboardHid | cg.FesComputerCapabilityGamepadPorts |
	cg.FesComputerCapabilityAudioPcmS16Stereo48k | cg.FesComputerCapabilityMediaSpectrumTape |
	cg.FesComputerCapabilityVideoSessionDisplay

// This record is compiled from the matched platform and its selected companion.
// The SD copy proves the selection agrees with that record; it grants no MMIO
// addresses, firmware roles, or bridge release permissions.
type spectrum48Record struct {
	Format            int      `json:"format"`
	Scope             string   `json:"scope"`
	BootContextSHA256 string   `json:"boot_context_sha256"`
	HandoffSHA256     string   `json:"handoff_sha256"`
	PackageID         string   `json:"package_id"`
	BuildID           string   `json:"build_id"`
	CoreSize          int      `json:"core_size"`
	CoreSHA256        string   `json:"core_sha256"`
	CoreSHA384        string   `json:"core_sha384"`
	ChunkSHA384       []string `json:"chunk_sha384"`
	ROMSourceSHA256   string   `json:"rom_source_sha256"`
	ROMReceiptSHA256  string   `json:"rom_receipt_sha256"`
	ComputerCPUBase   uint64   `json:"computer_cpu_base"`
	ScanoutCPUBase    uint64   `json:"scanout_cpu_base"`
	ProbeCPUBase      uint64   `json:"probe_cpu_base"`
}

type spectrum48Program struct {
	record        spectrum48Record
	raw           []byte
	configuration coldConfigurationMap
	configRaw     []byte
}

// WithSpectrum48 binds one explicit Spectrum companion to a matched native
// MENU/computer boot. Call from trusted startup before constructing the
// ProgramService. Ordinary loaders retain their existing profile checks.
func (l *ProgramLoader) WithSpectrum48(compiled []byte) (*ProgramLoader, error) {
	if l == nil || l.boot == nil || l.menu == nil || l.spectrum48 != nil || l.platform.ProgrammingProfile != NativeComputerProfile {
		return nil, errors.New("native fabric: Spectrum requires the matched mixed-role platform")
	}
	var record spectrum48Record
	if err := decodeCompleteRecord(compiled, 8192, &record); err != nil {
		return nil, err
	}
	base := l.control.ArchitecturalLWBase + l.control.BridgeBase
	if record.Format != 1 || record.Scope != "nano-compiled-spectrum48-program-v1" ||
		record.BootContextSHA256 != hex.EncodeToString(l.boot.digest[:]) || !nonzeroDigest(record.HandoffSHA256) ||
		!nonzeroDigest(record.PackageID) || !nonzeroDigest(record.CoreSHA256) ||
		!nonzeroDigest(record.ROMSourceSHA256) || !nonzeroDigest(record.ROMReceiptSHA256) ||
		record.ComputerCPUBase != base+uint64(cg.FesDe25ComputerPeripheralOffset) ||
		record.ScanoutCPUBase != base+uint64(cg.FesDe25ScanoutPeripheralOffset) ||
		l.control.FabricStop == nil || record.ProbeCPUBase != l.control.FabricStop.ProbeCPUBase ||
		record.CoreSize <= 0 || record.CoreSize > MaxProgramBytes || record.CoreSize%4 != 0 ||
		l.configuration.ChunkBytes <= 0 || len(record.ChunkSHA384) != (record.CoreSize-1)/l.configuration.ChunkBytes+1 ||
		len(record.ChunkSHA384) > maxProgramChunks {
		return nil, errors.New("native fabric: compiled Spectrum companion differs from the owned boot platform")
	}
	if _, err := BuildWords(record.BuildID); err != nil {
		return nil, err
	}
	hashes := append([]string{record.CoreSHA384}, record.ChunkSHA384...)
	for _, digest := range hashes {
		if !validHex(digest, 96) || digest == strings.Repeat("0", 96) {
			return nil, errors.New("native fabric: compiled Spectrum payload hashes incomplete")
		}
	}
	config := l.configuration
	config.PackageID, config.BuildID = record.PackageID, record.BuildID
	config.CoreSize, config.CoreSHA256, config.CoreSHA384 = record.CoreSize, record.CoreSHA256, record.CoreSHA384
	config.ChunkSHA384 = record.ChunkSHA384
	configRaw, err := canonicalProgramRecord(config)
	if err != nil {
		return nil, err
	}
	paired := *l
	paired.spectrum48 = &spectrum48Program{record, bytes.Clone(compiled), config, configRaw}
	return &paired, nil
}

func (l *ProgramLoader) preflightSpectrum48(selection *packagestore.Selection) (*StoredConfigurationPlan, error) {
	s := l.spectrum48
	if s == nil {
		return nil, errors.New("native fabric: Spectrum package has no matching compiled companion")
	}
	i := selection.Inspection()
	d, r := i.Descriptor, s.record
	if !bytes.Equal(selection.Metadata(), s.raw) || i.PackageID != r.PackageID || d.Build.ID != r.BuildID ||
		d.Core.ID != "fes.spectrum" || d.Target.Platform != l.platform.Platform || d.Target.Device != l.platform.Device ||
		d.ABI.ID != "fes.computer" || d.ABI.Major != 1 || d.ABI.Minor != 0 ||
		d.Payload.Size != int64(r.CoreSize) || d.Payload.SHA256 != r.CoreSHA256 ||
		d.Format != 3 || !corepackage.FirmwareUpload(d) || d.ROM == nil ||
		d.ROM.Role != "firmware" || d.ROM.SourceSize != spectrum48ROMBytes {
		return nil, errors.New("native fabric: Spectrum selection differs from the compiled companion")
	}
	var tape []byte
	if image := selection.Tape(); len(image) != 0 {
		if selection.TapeFormat() != packagestore.SpectrumTapeFormat || !protocol.DeclaresSpectrumTape(d) {
			return nil, errors.New("native fabric: Spectrum tape requires a declared Spectrum cassette unit")
		}
		var err error
		tape, err = protocol.ConvertSpectrumTape(image, computer.MaxInitialSpectrumTapeBytes)
		if err != nil {
			return nil, errors.Join(errors.New("native fabric: Spectrum tape conversion"), err)
		}
	}
	linked := selection.Linked()
	if linked == nil || linked.Inspection().PackageID != i.PackageID || linked.ReceiptSHA256() != r.ROMReceiptSHA256 {
		return nil, errors.New("native fabric: Spectrum requires its retained firmware receipt")
	}
	receipt := linked.Receipt()
	if receipt.PackageID != i.PackageID || receipt.ROMLink == nil || !receipt.ROMLink.ValidFor(d) ||
		receipt.ROMLinks != nil || receipt.PartsComposition != nil ||
		receipt.ROMLink.SourceSHA256 != r.ROMSourceSHA256 || receipt.ROMLink.ProgrammedSHA256 != r.CoreSHA256 {
		return nil, errors.New("native fabric: Spectrum firmware receipt changes the retained payload or ROM")
	}
	reader, err := linked.Source(d.ROM.ID, "firmware")
	if err != nil {
		return nil, err
	}
	firmware, err := io.ReadAll(io.LimitReader(reader, spectrum48ROMBytes+1))
	digest := sha256.Sum256(firmware)
	if err != nil || len(firmware) != spectrum48ROMBytes || hex.EncodeToString(digest[:]) != r.ROMSourceSHA256 {
		return nil, errors.New("native fabric: Spectrum firmware differs from the retained receipt")
	}
	payload, err := io.ReadAll(io.LimitReader(selection.Archive().Payload(), d.Payload.Size+1))
	if err != nil {
		return nil, err
	}
	if err := checkConfigurationBytes(s.configuration, payload); err != nil {
		return nil, err
	}
	id := fabricmodel.PackageIdentity{PackageID: i.PackageID, BuildID: d.Build.ID, CoreID: d.Core.ID,
		ABI: d.ABI.ID, Major: 1, Platform: d.Target.Platform, Device: d.Target.Device,
		ProgrammingProfile: spectrum48Profile, ActivationReceiptSHA256: r.ROMReceiptSHA256, ProgrammedSHA256: r.CoreSHA256}
	// The one compiled record owns both private map references for this role.
	p := &selectedProgram{role: programRoleSpectrum48, loader: l, identity: id,
		configuration: s.configuration, configurationRaw: s.configRaw,
		computer: computerMap{CPUBase: r.ComputerCPUBase, Span: cg.FesDe25ComputerSpan,
			Capabilities: spectrum48Capabilities, BuildID: r.BuildID},
		controlRaw: s.raw, computerRaw: s.raw, firmware: firmware, tape: tape, romReceipt: r.ROMReceiptSHA256}
	return &StoredConfigurationPlan{state: &storedConfigurationPlan{v: l.boot, program: p,
		selection: selection, control: s.raw, configuration: s.configRaw, payload: payload}}, nil
}
