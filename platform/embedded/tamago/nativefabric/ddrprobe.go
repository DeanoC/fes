// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type ddrReadMap struct {
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

// ProbeDDRReader invokes only the firmware's fixed, read-only two-beat recipe.
// Both maps must come from the compiled sealed build, never an incoming package.
// Firmware independently checks the live translation/firewall, pinned pattern,
// ownership and all master/drain receipts. The reservation stays pinned through
// every outcome; this diagnostic supplies no general runtime admission.
func (v *HPSContextVerifier) ProbeDDRReader(control, memory, contract []byte) (FPGAIdentityObservation, error) {
	if v == nil || v.context.ProgrammingProfile != HPSFirstDDRDiagnosticProfile || len(contract) == 0 || len(contract) > 4096 {
		return FPGAIdentityObservation{}, errors.New("native fabric: compiled DDR read contract required")
	}
	if _, err := v.checkedControlMap(control); err != nil {
		return FPGAIdentityObservation{}, err
	}
	if _, err := v.readerMemoryMap(memory); err != nil {
		return FPGAIdentityObservation{}, err
	}
	var m ddrReadMap
	d := json.NewDecoder(bytes.NewReader(contract))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return FPGAIdentityObservation{}, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return FPGAIdentityObservation{}, errors.New("native fabric: trailing DDR read contract")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contract, &fields); err != nil {
		return FPGAIdentityObservation{}, err
	}
	canonical, err := json.Marshal(fields)
	memoryHash, controlHash := sha256.Sum256(memory), sha256.Sum256(control)
	if err != nil || len(fields) != 14 || !bytes.Equal(contract, append(canonical, '\n')) ||
		m.Format != 1 || m.Scope != "sealed-cold-ddr-read-v1" || m.PackageID != v.context.PackageID || m.BuildID != v.context.BuildID ||
		m.ReaderMemorySHA256 != hex.EncodeToString(memoryHash[:]) || m.ControlMapSHA256 != hex.EncodeToString(controlHash[:]) ||
		!nonzeroDigest(m.AdapterSourceSHA256) || !nonzeroDigest(m.AdapterGeneratedSHA256) ||
		m.ARCache != 2 || m.ARProt != 3 || m.ARUser != 224 || m.ReadBytes != 32 || m.OpenMask != 8 || m.CloseMask != 10 {
		return FPGAIdentityObservation{}, errors.New("native fabric: DDR read differs from its compiled sealed contract")
	}
	v.readerMemoryMu.Lock()
	defer v.readerMemoryMu.Unlock()
	if v.ddrReaderAttempted || !v.readerMemoryReady || v.readerMemoryRegion == nil ||
		v.readerMemoryRegion.Address != 0x95000000 || !bytes.Equal(v.readerMemoryRegion.Bytes, readerMemoryPattern()) {
		return FPGAIdentityObservation{}, errors.New("native fabric: fresh pinned DDR pattern required")
	}
	v.ddrReaderAttempted = true
	observed, err := v.probeControl(control, g.FesEmbeddedBridgeProbeReader, g.FesEmbeddedBridgeReaderTag, 3)
	if err != nil {
		return FPGAIdentityObservation{}, err
	}
	if !bytes.Equal(v.readerMemoryRegion.Bytes, readerMemoryPattern()) {
		return FPGAIdentityObservation{}, errors.New("native fabric: pinned DDR pattern changed during read")
	}
	return observed, nil
}
