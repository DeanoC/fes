// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// This envelope asserts the package, configuration bytes, firmware receipt
// and fixed shell geometry. Addresses are compared to the compiled platform.
// Format 1 retains the original Spectrum wire scope for existing SD media.
type uploadShellRecord struct {
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

type uploadShellProgram struct {
	record        uploadShellRecord
	raw           []byte
	configuration coldConfigurationMap
	configRaw     []byte
}

func (l *ProgramLoader) decodeUploadShell(compiled []byte) (*uploadShellProgram, error) {
	var record uploadShellRecord
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
		return nil, errors.New("native fabric: upload shell record differs from the owned boot platform")
	}
	if _, err := BuildWords(record.BuildID); err != nil {
		return nil, err
	}
	hashes := append([]string{record.CoreSHA384}, record.ChunkSHA384...)
	for _, digest := range hashes {
		if !validHex(digest, 96) || digest == strings.Repeat("0", 96) {
			return nil, errors.New("native fabric: upload shell payload hashes incomplete")
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
	return &uploadShellProgram{record, bytes.Clone(compiled), config, configRaw}, nil
}

type admittedUploadPackage struct {
	identity          fabricmodel.PackageIdentity
	firmware, payload []byte
}

// admitUploadPackage verifies retained inputs independently of the machine
// name and its firmware size, within the upload transport's envelope. It is
// independent of driver availability and supplies no activation authority.
func (l *ProgramLoader) admitUploadPackage(selection *packagestore.Selection, shell *uploadShellProgram) (*admittedUploadPackage, error) {
	i := selection.Inspection()
	d, r := i.Descriptor, shell.record
	if !bytes.Equal(selection.Metadata(), shell.raw) || i.PackageID != r.PackageID || d.Build.ID != r.BuildID ||
		d.Target.Platform != l.platform.Platform || d.Target.Device != l.platform.Device ||
		d.ABI != (corepackage.Contract{ID: cg.FesComputerABIID, Major: 1}) ||
		d.Payload.Size != int64(r.CoreSize) || d.Payload.SHA256 != r.CoreSHA256 ||
		d.Format != 3 || !corepackage.FirmwareUpload(d) || d.ROM == nil ||
		d.ROM.Role != "firmware" || !computer.ValidFirmwareUploadSize(d.ROM.SourceSize) {
		return nil, errors.New("native fabric: selection differs from the upload shell record")
	}
	linked := selection.Linked()
	if linked == nil || linked.Inspection().PackageID != i.PackageID || linked.ReceiptSHA256() != r.ROMReceiptSHA256 {
		return nil, errors.New("native fabric: upload package requires its retained firmware receipt")
	}
	receipt := linked.Receipt()
	if receipt.PackageID != i.PackageID || receipt.ROMLink == nil || !receipt.ROMLink.ValidFor(d) ||
		receipt.ROMLinks != nil || receipt.PartsComposition != nil ||
		receipt.ROMLink.SourceSHA256 != r.ROMSourceSHA256 || receipt.ROMLink.ProgrammedSHA256 != r.CoreSHA256 {
		return nil, errors.New("native fabric: upload receipt differs from retained firmware or payload")
	}
	reader, err := linked.Source(d.ROM.ID, d.ROM.Role)
	if err != nil {
		return nil, err
	}
	firmware, err := io.ReadAll(io.LimitReader(reader, d.ROM.SourceSize+1))
	digest := sha256.Sum256(firmware)
	if err != nil || int64(len(firmware)) != d.ROM.SourceSize || hex.EncodeToString(digest[:]) != r.ROMSourceSHA256 {
		return nil, errors.New("native fabric: uploaded firmware differs from the retained receipt")
	}
	key := preflightCacheKey{
		packageID:  i.PackageID,
		buildID:    d.Build.ID,
		coreSHA256: shell.configuration.CoreSHA256,
		coreSize:   shell.configuration.CoreSize,
		chunkBytes: shell.configuration.ChunkBytes,
	}
	var payload []byte
	if cached, coreSHA384, chunkSHA384, ok := l.planCache.get(key); ok {
		if coreSHA384 != shell.configuration.CoreSHA384 || !slices.Equal(chunkSHA384, shell.configuration.ChunkSHA384) {
			return nil, errors.New("native fabric: staged core differs from compiled package")
		}
		payload = cached
	} else {
		l.planCache.recordExtract()
		payload, err = io.ReadAll(io.LimitReader(selection.Archive().Payload(), d.Payload.Size+1))
		if err != nil {
			return nil, err
		}
		if err := checkConfigurationBytes(shell.configuration, payload); err != nil {
			return nil, err
		}
		l.planCache.put(key, payload, shell.configuration.CoreSHA384, shell.configuration.ChunkSHA384)
	}
	id := fabricmodel.PackageIdentity{PackageID: i.PackageID, BuildID: d.Build.ID, CoreID: d.Core.ID,
		ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), Platform: d.Target.Platform, Device: d.Target.Device,
		ProgrammingProfile: d.Target.ProgrammingProfile, ActivationReceiptSHA256: r.ROMReceiptSHA256, ProgrammedSHA256: r.CoreSHA256}
	return &admittedUploadPackage{identity: id, firmware: firmware, payload: payload}, nil
}
