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
	var reader arenaHash
	sha256sum, sha384sum, chunks := reader.sum256And384WithChunks(data, m.ChunkBytes)
	if hex.EncodeToString(sha256sum[:]) != m.CoreSHA256 || hex.EncodeToString(sha384sum[:]) != m.CoreSHA384 {
		return errors.New("native fabric: staged core differs from compiled package")
	}
	if len(chunks) != len(m.ChunkSHA384) {
		return errors.New("native fabric: staged chunk differs from compiled package")
	}
	for index, digest := range chunks {
		if hex.EncodeToString(digest[:]) != m.ChunkSHA384[index] {
			return errors.New("native fabric: staged chunk differs from compiled package")
		}
	}
	return nil
}

func populateProgramConfiguration(config *coldConfigurationMap, data []byte) error {
	if len(data) != config.CoreSize {
		return errors.New("native fabric: core size differs from compiled package")
	}
	if config.ChunkBytes <= 0 {
		return errors.New("native fabric: invalid chunk size")
	}
	chunkCount := (len(data) + config.ChunkBytes - 1) / config.ChunkBytes
	if chunkCount > maxProgramChunks {
		return errors.New("native fabric: selected program exceeds bounded chunk inventory")
	}
	h256 := sha256.New()
	h384 := sha512.New384()
	chunkHash := sha512.New384()
	config.ChunkSHA384 = make([]string, 0, chunkCount)
	for offset := 0; offset < len(data); offset += config.ChunkBytes {
		part := data[offset:min(offset+config.ChunkBytes, len(data))]
		chunkHash.Reset()
		for sub := 0; sub < len(part); sub += 32 << 10 {
			slice := part[sub:min(sub+(32<<10), len(part))]
			h256.Write(slice)
			h384.Write(slice)
			chunkHash.Write(slice)
		}
		config.ChunkSHA384 = append(config.ChunkSHA384, hex.EncodeToString(chunkHash.Sum(nil)))
	}
	if hex.EncodeToString(h256.Sum(nil)) != config.CoreSHA256 {
		return errors.New("native fabric: staged core differs from compiled package")
	}
	config.CoreSHA384 = hex.EncodeToString(h384.Sum(nil))
	return nil
}

// NewColdConfigurationTransfer takes its DMA and block parameters from the
// compiled sealed map. Preparation rechecks the staged bytes and original held
// source immediately before START. This grants no runtime admission or release.
func (v *HPSContextVerifier) NewColdConfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation) (*ConfigurationTransfer, error) {
	return v.newSourceConfigurationTransfer(control, configuration, region, source, false, nil, nil, nil)
}

// NewReconfigurationTransfer uses the same sealed bytes after an owned return.
// Its region must carry a fresh platform lease; it never reuses or releases a
// previous transfer's reservation. Secure rearming is deferred until START.
func (v *HPSContextVerifier) NewReconfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation) (*ConfigurationTransfer, error) {
	return v.newSourceConfigurationTransfer(control, configuration, region, source, true, nil, nil, nil)
}

func (v *HPSContextVerifier) newSourceConfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation, rearm bool, program *selectedProgram, payload []byte, payloadDigest *[32]byte) (*ConfigurationTransfer, error) {
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
	var usePayload []byte
	var useDigest *[32]byte
	if payloadDigest != nil && len(payload) == m.CoreSize {
		if wantDigest, ok := decodeSHA256Hex(m.CoreSHA256); ok && wantDigest == *payloadDigest {
			usePayload = payload
			useDigest = &wantDigest
		}
	}
	beforeStart := func() error {
		prepared, err := v.prepareProgramConfiguration(control, configuration, region, rearm, program, usePayload)
		if err != nil {
			return err
		}
		if prepared != source {
			return errors.New("native fabric: configuration preparation changed held source")
		}
		return nil
	}
	transfer, err := newConfigurationTransfer(v.cpu, region.Bytes, region, m.DRAMBase, m.ChunkBytes, beforeStart, useDigest)
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
	return v.prepareProgramConfiguration(control, configuration, region, rearm, nil, nil)
}

func (v *HPSContextVerifier) prepareProgramConfiguration(control, configuration []byte, region *Region, rearm bool, program *selectedProgram, payload []byte) (HPSBootObservation, error) {
	m, err := v.programConfiguration(control, configuration, program)
	if err != nil {
		return HPSBootObservation{}, err
	}
	if region == nil || region.Release == nil || region.Address != m.Base || len(region.Bytes) != m.CoreSize {
		return HPSBootObservation{}, errors.New("native fabric: configuration bytes are outside the compiled reservation")
	}
	if payload != nil {
		if len(payload) != m.CoreSize {
			return HPSBootObservation{}, errors.New("native fabric: core size differs from compiled package")
		}
		var cmp arenaComparison
		if !cmp.equal(region.Bytes, payload) {
			return HPSBootObservation{}, errors.New("native fabric: staged core differs from compiled package")
		}
	} else if err := checkConfigurationBytes(m, region.Bytes); err != nil {
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
