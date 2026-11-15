// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type coldConfigurationMap struct {
	Format      int      `json:"format"`
	Scope       string   `json:"scope"`
	PackageID   string   `json:"package_id"`
	BuildID     string   `json:"build_id"`
	CoreSHA256  string   `json:"core_sha256"`
	CoreSHA384  string   `json:"core_sha384"`
	CoreSize    int      `json:"core_size"`
	Base        uint64   `json:"base"`
	Size        uint64   `json:"size"`
	DRAMBase    uint64   `json:"dram_base"`
	ChunkBytes  int      `json:"chunk_bytes"`
	ChunkSHA384 []string `json:"chunk_sha384"`
}

func (v *HPSContextVerifier) configurationMap(raw []byte) (coldConfigurationMap, error) {
	if v == nil || len(raw) == 0 || len(raw) > 32768 {
		return coldConfigurationMap{}, errors.New("native fabric: bounded compiled configuration map required")
	}
	var m coldConfigurationMap
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return coldConfigurationMap{}, err
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return coldConfigurationMap{}, errors.New("native fabric: trailing configuration map")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return coldConfigurationMap{}, err
	}
	canonical, err := json.Marshal(fields)
	if err != nil || len(fields) != 12 || !bytes.Equal(raw, append(canonical, '\n')) ||
		m.Format != 1 || m.Scope != "sealed-cold-configuration-v1" || m.PackageID != v.context.PackageID ||
		m.BuildID != v.context.BuildID || m.CoreSHA256 != v.context.CoreSHA256 || int64(m.CoreSize) != v.context.CoreSize ||
		m.CoreSize <= 0 || m.CoreSize > MaxProgramBytes || m.CoreSize%4 != 0 || !validHex(m.CoreSHA384, 96) ||
		m.Base != 0x94800000 || m.Size != 8<<20 || m.DRAMBase != 0x80000000 ||
		m.ChunkBytes <= 0 || m.ChunkBytes > 0xffff0 || m.ChunkBytes%64 != 0 ||
		len(m.ChunkSHA384) > maxProgramChunks || len(m.ChunkSHA384) != (m.CoreSize+m.ChunkBytes-1)/m.ChunkBytes {
		return coldConfigurationMap{}, errors.New("native fabric: configuration map differs from compiled cold source")
	}
	for _, digest := range m.ChunkSHA384 {
		if !validHex(digest, 96) {
			return coldConfigurationMap{}, errors.New("native fabric: malformed compiled chunk digest")
		}
	}
	return m, nil
}

func (v *HPSContextVerifier) coldSource(source HPSBootObservation) bool {
	b := source.Bridges
	return v != nil && source.Digest == v.digest && validBridgeSnapshot(b) && b.NeverEnabled &&
		b.ResetMask == b.Capabilities.Mask && b.MastersContained && b.Capabilities.FabricHandshake == FabricHandshakeAbsent
}

func (v *HPSContextVerifier) reconfigurationSource(source HPSBootObservation) bool {
	b := source.Bridges
	return v != nil && source.Digest == v.digest && validBridgeSnapshot(b) && !b.NeverEnabled &&
		b.Generation > 1 && b.Generation <= ^uint64(0)-2 && b.ResetMask == b.Capabilities.Mask &&
		b.MastersContained && b.Capabilities.FabricHandshake == FabricHandshakeAbsent
}

func checkConfigurationBytes(m coldConfigurationMap, data []byte) error {
	if len(data) != m.CoreSize {
		return errors.New("native fabric: core size differs from compiled package")
	}
	sha256sum, sha384sum := sha256.Sum256(data), sha512.Sum384(data)
	if hex.EncodeToString(sha256sum[:]) != m.CoreSHA256 || hex.EncodeToString(sha384sum[:]) != m.CoreSHA384 {
		return errors.New("native fabric: staged core differs from compiled package")
	}
	for offset, index := 0, 0; offset < len(data); offset, index = offset+m.ChunkBytes, index+1 {
		digest := sha512.Sum384(data[offset:min(offset+m.ChunkBytes, len(data))])
		if hex.EncodeToString(digest[:]) != m.ChunkSHA384[index] {
			return errors.New("native fabric: staged chunk differs from compiled package")
		}
	}
	return nil
}

// NewColdConfigurationTransfer takes its DMA and block parameters from the
// compiled sealed map. Preparation rechecks the staged bytes and original held
// source immediately before START. This grants no runtime admission or release.
func (v *HPSContextVerifier) NewColdConfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation) (*ConfigurationTransfer, error) {
	return v.newSourceConfigurationTransfer(control, configuration, region, source, false, nil)
}

// NewReconfigurationTransfer uses the same sealed bytes after an owned return.
// Its region must carry a fresh platform lease; it never reuses or releases a
// previous transfer's reservation. Secure rearming is deferred until START.
func (v *HPSContextVerifier) NewReconfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation) (*ConfigurationTransfer, error) {
	return v.newSourceConfigurationTransfer(control, configuration, region, source, true, nil)
}

func (v *HPSContextVerifier) newSourceConfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation, rearm bool, program *selectedProgram) (*ConfigurationTransfer, error) {
	m, err := v.programConfiguration(control, configuration, program)
	if err != nil {
		return nil, err
	}
	held := v.coldSource(source)
	if rearm {
		held = v.reconfigurationSource(source)
	}
	if !held ||
		region == nil || region.Address != m.Base || len(region.Bytes) != m.CoreSize {
		return nil, errors.New("native fabric: held source and compiled reservation required")
	}
	control, configuration = bytes.Clone(control), bytes.Clone(configuration)
	beforeStart := func() error {
		prepared, err := v.prepareProgramConfiguration(control, configuration, region, rearm, program)
		if err != nil {
			return err
		}
		if prepared != source {
			return errors.New("native fabric: configuration preparation changed held source")
		}
		return nil
	}
	transfer, err := NewConfigurationTransfer(v.cpu, region.Bytes, region, m.DRAMBase, m.ChunkBytes, beforeStart)
	if err != nil {
		return nil, err
	}
	transfer.binding = &configurationBinding{verifier: v, source: source, rearm: rearm, program: program}
	return transfer, nil
}

// PrepareConfiguration checks the compiled map, staged bytes and fresh closed
// source ownership, then asks the source-bound secure provider to arm its fixed
// transfer. It sends no address or digest. The caller owns the region throughout
// this call, including on error; it must retain uncertain storage until reboot.
// This is not a BootHandoffVerifier or a runtime compatibility decision.
func (v *HPSContextVerifier) PrepareConfiguration(control, configuration []byte, region *Region) (HPSBootObservation, error) {
	return v.prepareConfiguration(control, configuration, region, false)
}

// PrepareReconfiguration asks the secure provider to rearm the same compiled
// source after a successful transfer and a newer owned all-master closure.
// Failed or uncertain transfers cannot be rearmed. The caller retains the
// region, including on error; this grants no runtime admission or bridge access.
func (v *HPSContextVerifier) PrepareReconfiguration(control, configuration []byte, region *Region) (HPSBootObservation, error) {
	return v.prepareConfiguration(control, configuration, region, true)
}

func (v *HPSContextVerifier) prepareConfiguration(control, configuration []byte, region *Region, rearm bool) (HPSBootObservation, error) {
	return v.prepareProgramConfiguration(control, configuration, region, rearm, nil)
}

func (v *HPSContextVerifier) prepareProgramConfiguration(control, configuration []byte, region *Region, rearm bool, program *selectedProgram) (HPSBootObservation, error) {
	m, err := v.programConfiguration(control, configuration, program)
	if err != nil {
		return HPSBootObservation{}, err
	}
	if region == nil || region.Release == nil || region.Address != m.Base || len(region.Bytes) != m.CoreSize {
		return HPSBootObservation{}, errors.New("native fabric: configuration bytes are outside the compiled reservation")
	}
	if err := checkConfigurationBytes(m, region.Bytes); err != nil {
		return HPSBootObservation{}, err
	}
	source, err := v.Verify(v.Identity())
	if err != nil {
		return HPSBootObservation{}, err
	}
	before := source.Bridges
	operation, tag := uint64(g.FesEmbeddedBridgePrepareConfiguration), uint64(g.FesEmbeddedBridgePreparationTag)
	if rearm {
		if !v.reconfigurationSource(source) {
			return HPSBootObservation{}, errors.New("native fabric: reconfiguration requires a newer owned all-master closure")
		}
		operation, tag = uint64(g.FesEmbeddedBridgeRearmConfiguration), uint64(g.FesEmbeddedBridgeRearmTag)
	} else if !v.coldSource(source) {
		return HPSBootObservation{}, errors.New("native fabric: cold preparation requires fresh closed source ownership")
	}
	if region.PublishConfiguration == nil {
		return HPSBootObservation{}, errors.New("native fabric: per-load configuration publication unavailable")
	}
	header, err := encodeProgramBinding(source, m)
	if err != nil {
		return HPSBootObservation{}, err
	}
	if program != nil && program.role != 0 {
		header, _, err = encodeRoleProgramBinding(source, m, program.role, program.rom)
		if err != nil {
			return HPSBootObservation{}, err
		}
	}
	if program != nil && len(program.rom) != 0 {
		if v.rom == nil || region.StageROM == nil {
			return HPSBootObservation{}, errors.New("native fabric: held ROM staging unavailable")
		}
		if program.role == 0 {
			header, _, err = encodeROMProgramBinding(source, m, program.rom)
			if err != nil {
				return HPSBootObservation{}, err
			}
		}
		if err = region.StageROM(program.rom); err != nil {
			return HPSBootObservation{}, err
		}
	}
	if err := region.PublishConfiguration(header); err != nil {
		return HPSBootObservation{}, err
	}
	r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), operation, before.Owner,
		uint64(g.FesEmbeddedBridgeCookie), before.Generation})
	if err != nil {
		return HPSBootObservation{}, err
	}
	if r != ([4]uint64{0, tag<<32 | programBoundReply | uint64(m.CoreSize), before.Owner, before.Generation}) {
		return HPSBootObservation{}, errors.New("native fabric: secure configuration preparation rejected")
	}
	after, err := (&FirmwareBridges{cpu: v.cpu}).ObserveBridges()
	if err != nil || after != before {
		return HPSBootObservation{}, errors.New("native fabric: configuration preparation lost closed ownership")
	}
	return source, nil
}
