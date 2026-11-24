// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

const spectrum48Profile = "de25-native-spectrum48-v1"
const spectrum48ROMBytes = 16 << 10
const spectrum48Capabilities = cg.FesComputerCapabilityVideoFixed720p60 |
	cg.FesComputerCapabilityKeyboardHid | cg.FesComputerCapabilityGamepadPorts |
	cg.FesComputerCapabilityAudioPcmS16Stereo48k | cg.FesComputerCapabilityMediaSpectrumTape |
	cg.FesComputerCapabilityVideoSessionDisplay

// WithSpectrum48 binds one explicit Spectrum companion to a matched native
// MENU/computer boot. Call from trusted startup before constructing the
// ProgramService. Ordinary loaders retain their existing profile checks.
func (l *ProgramLoader) WithSpectrum48(compiled []byte) (*ProgramLoader, error) {
	if l == nil || l.boot == nil || l.menu == nil || l.spectrum48 != nil || l.sharedCoreShell || l.platform.ProgrammingProfile != NativeComputerProfile {
		return nil, errors.New("native fabric: Spectrum requires the matched mixed-role platform")
	}
	shell, err := l.decodeUploadShell(compiled)
	if err != nil {
		return nil, err
	}
	paired := *l
	paired.spectrum48 = shell
	return &paired, nil
}

// WithSharedCoreShell enables package selection against the fixed upload and
// scanout shell. Package and ROM identity are retained per SD selection.
func (l *ProgramLoader) WithSharedCoreShell() (*ProgramLoader, error) {
	if l == nil || l.boot == nil || l.menu == nil || l.spectrum48 != nil || l.sharedCoreShell ||
		l.platform.ProgrammingProfile != NativeComputerProfile {
		return nil, errors.New("native fabric: shared core shell requires the matched mixed-role platform")
	}
	paired := *l
	paired.sharedCoreShell = true
	return &paired, nil
}

func (l *ProgramLoader) preflightUpload(selection *packagestore.Selection, s *uploadShellProgram) (*StoredConfigurationPlan, error) {
	if l.sharedCoreShell {
		var err error
		s, err = l.decodeUploadShell(selection.Metadata())
		if err != nil {
			return nil, err
		}
	}
	if s == nil {
		return nil, errors.New("native fabric: Spectrum package has no matching compiled companion")
	}
	if err := l.uploadSupport.CheckArchive(selection.Archive(), s.record.PackageID); err != nil {
		return nil, err
	}
	d := selection.Inspection().Descriptor
	wantBytes, capabilities := int64(spectrum48ROMBytes), spectrum48Capabilities
	c64 := d.Target.ProgrammingProfile == c64Profile
	if c64 {
		wantBytes, capabilities = c64FirmwareBytes, c64Capabilities
	}
	if !l.sharedCoreShell && d.Core.ID != "fes.spectrum" || c64 && d.Core.ID != "fes.c64" ||
		d.ROM == nil || d.ROM.SourceSize != wantBytes {
		return nil, errors.New("native fabric: firmware or core differs from the selected upload endpoint")
	}
	pkg, err := l.admitUploadPackage(selection, s)
	if err != nil {
		return nil, err
	}
	var tape, disk []byte
	image := selection.Tape()
	if c64 && len(image) != 0 {
		if selection.TapeFormat() != packagestore.C64DiskFormat || len(image) != int(cg.FesComputerC64DiskBytes) || !declaresC64Disk(d) {
			return nil, errors.New("native fabric: C64 initial media requires a declared 35-track D64 unit")
		}
		disk = image
	} else if len(image) != 0 {
		if selection.TapeFormat() != packagestore.SpectrumTapeFormat || !protocol.DeclaresSpectrumTape(d) {
			return nil, errors.New("native fabric: Spectrum tape requires a declared Spectrum cassette unit")
		}
		var err error
		tape, err = protocol.ConvertSpectrumTape(image, computer.MaxInitialSpectrumTapeBytes)
		if err != nil {
			return nil, errors.Join(errors.New("native fabric: Spectrum tape conversion"), err)
		}
	}
	// The one compiled record owns both private map references for this role.
	p := &selectedProgram{role: programRoleUploadShell, loader: l, identity: pkg.identity,
		configuration: s.configuration, configurationRaw: s.configRaw,
		computer: computerMap{CPUBase: s.record.ComputerCPUBase, Span: cg.FesDe25ComputerSpan,
			Capabilities: capabilities, BuildID: s.record.BuildID},
		controlRaw: s.raw, computerRaw: s.raw, firmware: pkg.firmware, tape: tape, disk: disk, romReceipt: s.record.ROMReceiptSHA256}
	return &StoredConfigurationPlan{state: &storedConfigurationPlan{v: l.boot, program: p,
		selection: selection, control: s.raw, configuration: s.configRaw, payload: pkg.payload}}, nil
}

// This is the host's current upload endpoint inventory. A package may
// require a subset of these interfaces but cannot register new operations.
func spectrum48UploadDriver() runtimeadmission.Driver {
	return runtimeadmission.Driver{
		Profile: spectrum48Profile,
		ABI:     runtimeadmission.Contract{ID: cg.FesComputerABIID, Major: 1},
		Interfaces: []runtimeadmission.Contract{
			{ID: cg.FesComputerInterfaceVideoFixed720p60ID, Major: 1},
			{ID: cg.FesComputerInterfaceKeyboardHidID, Major: 1},
			{ID: cg.FesComputerInterfaceGamepadPortsID, Major: 1},
			{ID: cg.FesComputerInterfaceAudioPcmS16Stereo48kID, Major: 1},
			{ID: cg.FesComputerInterfaceVideoSessionDisplayID, Major: 1},
			{ID: cg.FesComputerInterfaceMediaSpectrumTapeID, Major: int64(cg.FesComputerInterfaceMediaSpectrumTapeMajor), Minor: int64(cg.FesComputerInterfaceMediaSpectrumTapeMinor)},
			{ID: "fes.fabric.firmware-upload", Major: 1},
			{ID: "fes.fabric.video.display-plane", Major: 1},
			{ID: "fes.fabric.de25-scanout", Major: 1, Minor: 3},
		},
	}
}
