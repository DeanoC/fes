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
		m.CoreSize <= 0 || m.CoreSize > 8<<20 || m.CoreSize%4 != 0 || !validHex(m.CoreSHA384, 96) ||
		m.Base != 0x94800000 || m.Size != 8<<20 || m.DRAMBase != 0x80000000 ||
		m.ChunkBytes <= 0 || m.ChunkBytes > 0xffff0 || m.ChunkBytes%64 != 0 ||
		len(m.ChunkSHA384) != (m.CoreSize+m.ChunkBytes-1)/m.ChunkBytes {
		return coldConfigurationMap{}, errors.New("native fabric: configuration map differs from compiled cold source")
	}
	for _, digest := range m.ChunkSHA384 {
		if !validHex(digest, 96) {
			return coldConfigurationMap{}, errors.New("native fabric: malformed compiled chunk digest")
		}
	}
	return m, nil
}

// NewColdConfigurationTransfer takes its DMA and block parameters from the
// compiled sealed map. Preparation rechecks the staged bytes and original held
// source immediately before START. This grants no runtime admission or release.
func (v *HPSContextVerifier) NewColdConfigurationTransfer(control, configuration []byte, region *Region, source HPSBootObservation) (*ConfigurationTransfer, error) {
	if _, err := v.checkedControlMap(control); err != nil {
		return nil, err
	}
	m, err := v.configurationMap(configuration)
	if err != nil {
		return nil, err
	}
	if source.Digest != v.digest || !validBridgeSnapshot(source.Bridges) ||
		source.Bridges.Generation != 1 || !source.Bridges.NeverEnabled ||
		source.Bridges.ResetMask != source.Bridges.Capabilities.Mask || !source.Bridges.MastersContained ||
		source.Bridges.Capabilities.FabricHandshake != FabricHandshakeAbsent ||
		region == nil || region.Address != m.Base || len(region.Bytes) != m.CoreSize {
		return nil, errors.New("native fabric: original held source and compiled reservation required")
	}
	control, configuration = bytes.Clone(control), bytes.Clone(configuration)
	beforeStart := func() error {
		prepared, err := v.PrepareConfiguration(control, configuration, region)
		if err != nil {
			return err
		}
		if prepared != source {
			return errors.New("native fabric: configuration preparation changed cold source")
		}
		return nil
	}
	return NewConfigurationTransfer(v.cpu, region.Bytes, region, m.DRAMBase, m.ChunkBytes, beforeStart)
}

// PrepareConfiguration checks the compiled map, staged bytes and fresh closed
// source ownership, then asks the source-bound secure provider to arm its fixed
// transfer. It sends no address or digest. The caller owns the region throughout
// this call, including on error; it must retain uncertain storage until reboot.
// This is not a BootHandoffVerifier or a runtime compatibility decision.
func (v *HPSContextVerifier) PrepareConfiguration(control, configuration []byte, region *Region) (HPSBootObservation, error) {
	if _, err := v.checkedControlMap(control); err != nil {
		return HPSBootObservation{}, err
	}
	m, err := v.configurationMap(configuration)
	if err != nil {
		return HPSBootObservation{}, err
	}
	if region == nil || region.Release == nil || region.Address != m.Base || len(region.Bytes) != m.CoreSize {
		return HPSBootObservation{}, errors.New("native fabric: configuration bytes are outside the compiled reservation")
	}
	sha256sum, sha384sum := sha256.Sum256(region.Bytes), sha512.Sum384(region.Bytes)
	if hex.EncodeToString(sha256sum[:]) != m.CoreSHA256 || hex.EncodeToString(sha384sum[:]) != m.CoreSHA384 {
		return HPSBootObservation{}, errors.New("native fabric: staged core differs from compiled package")
	}
	for offset, index := 0, 0; offset < len(region.Bytes); offset, index = offset+m.ChunkBytes, index+1 {
		digest := sha512.Sum384(region.Bytes[offset:min(offset+m.ChunkBytes, len(region.Bytes))])
		if hex.EncodeToString(digest[:]) != m.ChunkSHA384[index] {
			return HPSBootObservation{}, errors.New("native fabric: staged chunk differs from compiled package")
		}
	}
	source, err := v.Verify(v.Identity())
	if err != nil {
		return HPSBootObservation{}, err
	}
	before := source.Bridges
	if before.Generation != 1 || !before.NeverEnabled || before.Capabilities.FabricHandshake != FabricHandshakeAbsent {
		return HPSBootObservation{}, errors.New("native fabric: cold preparation requires fresh closed source ownership")
	}
	r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgePrepareConfiguration), before.Owner,
		uint64(g.FesEmbeddedBridgeCookie), before.Generation})
	if err != nil {
		return HPSBootObservation{}, err
	}
	if r != ([4]uint64{0, uint64(g.FesEmbeddedBridgePreparationTag)<<32 | uint64(m.CoreSize), before.Owner, before.Generation}) {
		return HPSBootObservation{}, errors.New("native fabric: secure cold configuration preparation rejected")
	}
	after, err := (&FirmwareBridges{cpu: v.cpu}).ObserveBridges()
	if err != nil || after != before {
		return HPSBootObservation{}, errors.New("native fabric: configuration preparation lost closed ownership")
	}
	return source, nil
}
