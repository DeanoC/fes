// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type hpsControlMap struct {
	Format              int               `json:"format"`
	Scope               string            `json:"scope"`
	ArchitecturalLWBase uint64            `json:"architectural_lw_base"`
	BridgeBase          uint64            `json:"bridge_base"`
	PeripheralOffset    uint64            `json:"peripheral_offset"`
	Span                uint64            `json:"span"`
	CPUBase             uint64            `json:"cpu_base"`
	HandoffSHA256       string            `json:"handoff_sha256"`
	PackageID           string            `json:"package_id"`
	BuildID             string            `json:"build_id"`
	FabricStop          *hpsFabricStopMap `json:"fabric_stop,omitempty"`
}

type hpsFabricStopMap struct {
	Scope        string `json:"scope"`
	ProbeCPUBase uint64 `json:"probe_cpu_base"`
	ProbeSpan    uint64 `json:"probe_span"`
	Maintenance  string `json:"maintenance"`
}

// FPGAIdentityObservation records a secure CPU read with control reclosed.
// It does not qualify programming, live HPS I/O or an active-device drain.
type FPGAIdentityObservation struct {
	Source           HPSBootObservation
	Identity         embeddedcontrol.Identity
	After            BridgeSnapshot
	CPUBase          uint64
	ControlMapSHA256 [32]byte
}

// ErrIdentityNotReady reports an unavailable probe without a partial identity.
// It does not distinguish an absent provider from unconfigured fabric. Readers
// must independently recheck closed ownership before reporting containment.
var ErrIdentityNotReady = errors.New("native fabric: secure FPGA identity probe not ready")

func (v *HPSContextVerifier) checkedControlMap(raw []byte) (hpsControlMap, error) {
	if v == nil {
		return hpsControlMap{}, errors.New("native fabric: compiled control verifier missing")
	}
	return checkedControlMapFor(v.context, raw)
}

func checkedControlMapFor(source HPSBootContext, raw []byte) (hpsControlMap, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return hpsControlMap{}, errors.New("native fabric: bounded compiled control map required")
	}
	var m hpsControlMap
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return hpsControlMap{}, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return hpsControlMap{}, errors.New("native fabric: trailing control map")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return hpsControlMap{}, err
	}
	canonical, err := json.Marshal(fields)
	count := 10
	if source.ProgrammingProfile == HPSFirstDDRDiagnosticProfile || source.ProgrammingProfile == HPSFirstWindowDiagnosticProfile || source.ProgrammingProfile == HPSFirstROMDiagnosticProfile {
		count++
		stopScope := "sealed-ddr-master-stop-v1"
		if source.ProgrammingProfile == HPSFirstWindowDiagnosticProfile {
			stopScope = "sealed-window-master-stop-v1"
		}
		if m.FabricStop == nil || *m.FabricStop != (hpsFabricStopMap{stopScope, 0x20010100, 128, "held-reset-from-configuration"}) {
			return hpsControlMap{}, errors.New("native fabric: sealed complete master stop map required")
		}
		var stopFields map[string]json.RawMessage
		if json.Unmarshal(fields["fabric_stop"], &stopFields) != nil || len(stopFields) != 4 {
			return hpsControlMap{}, errors.New("native fabric: incomplete master stop map")
		}
		encoded, err := json.Marshal(stopFields)
		if err != nil || !bytes.Equal(encoded, fields["fabric_stop"]) {
			return hpsControlMap{}, errors.New("native fabric: noncanonical master stop map")
		}
	} else if m.FabricStop != nil {
		return hpsControlMap{}, errors.New("native fabric: master stop map requires DDR source")
	}
	if err != nil || len(fields) != count || !bytes.Equal(raw, append(canonical, '\n')) ||
		m.Format != 1 || m.Scope != "sealed-lw-identity-probe-v1" || m.ArchitecturalLWBase != 0x20000000 || m.BridgeBase != 0 ||
		m.PeripheralOffset != uint64(source.PeripheralOffset) || m.Span != uint64(source.PeripheralSpan) ||
		m.CPUBase != m.ArchitecturalLWBase+m.PeripheralOffset || !nonzeroDigest(m.HandoffSHA256) ||
		m.PackageID != source.PackageID || m.BuildID != source.BuildID {
		return hpsControlMap{}, errors.New("native fabric: control map differs from compiled HPS source")
	}
	return m, nil
}

// ProbeIdentity uses the dedicated one-shot firmware operation. No physical
// address is passed through the non-secure call. The compiled control map is
// checked locally; firmware supplies its own immutable address and identity.
// HPSContextVerifier deliberately remains separate from BootHandoffVerifier.
func (v *HPSContextVerifier) ProbeIdentity(raw []byte) (FPGAIdentityObservation, error) {
	return v.probeControl(raw, g.FesEmbeddedBridgeProbeIdentity, g.FesEmbeddedBridgeIdentityTag, 2)
}

// ProbeReader verifies the same sealed identity and executes the firmware's
// one-shot on-chip reader enable/held-address/partial-burst/drain recipe before
// control reclose. It shares the identity attempt and does not open data bridges
// or provide a general DDR, display or active-master containment receipt.
func (v *HPSContextVerifier) ProbeReader(raw []byte) (FPGAIdentityObservation, error) {
	if v == nil || v.context.ProgrammingProfile != HPSFirstDiagnosticProfile {
		return FPGAIdentityObservation{}, errors.New("native fabric: on-chip reader operation requires its on-chip source")
	}
	return v.probeControl(raw, g.FesEmbeddedBridgeProbeReader, g.FesEmbeddedBridgeReaderTag, 2)
}

func (v *HPSContextVerifier) probeControl(raw []byte, operation, tag uint32, transitions uint64) (FPGAIdentityObservation, error) {
	m, err := v.checkedControlMap(raw)
	if err != nil {
		return FPGAIdentityObservation{}, err
	}

	source, err := v.Verify(v.Identity())
	if err != nil {
		return FPGAIdentityObservation{}, err
	}
	before := source.Bridges
	if before.Generation > math.MaxUint64-transitions || !v.coldSource(source) {
		return FPGAIdentityObservation{}, errors.New("native fabric: identity probe requires fresh closed cold scope")
	}
	r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(operation), before.Owner, uint64(g.FesEmbeddedBridgeCookie), before.Generation})
	if err != nil {
		return FPGAIdentityObservation{}, err
	}
	if r == ([4]uint64{uint64(g.FesEmbeddedBridgeNotReady), uint64(tag) << 32}) {
		return FPGAIdentityObservation{}, ErrIdentityNotReady
	}
	if r[0] != 0 || uint32(r[1]>>32) != tag || uint32(r[1]) != v.context.ControlABI {
		return FPGAIdentityObservation{}, fmt.Errorf("native fabric: secure FPGA identity rejected, status %d", r[0])
	}
	id := embeddedcontrol.Identity{ABI: uint32(r[1]), Build: [4]uint32{uint32(r[3]), uint32(r[3] >> 32), uint32(r[2]), uint32(r[2] >> 32)}}
	want, err := BuildWords(v.context.BuildID)
	if err != nil || id.Build != want {
		return FPGAIdentityObservation{}, errors.New("native fabric: live FPGA build differs from sealed package")
	}
	after, err := (&FirmwareBridges{cpu: v.cpu}).ObserveBridges()
	if err != nil || after.Owner != before.Owner || after.Generation != before.Generation+transitions ||
		after.Capabilities != before.Capabilities || after.ResetMask != after.Capabilities.Mask ||
		!after.MastersContained || after.NeverEnabled {
		return FPGAIdentityObservation{}, errors.New("native fabric: FPGA identity probe lost closed ownership")
	}
	return FPGAIdentityObservation{source, id, after, m.CPUBase, sha256.Sum256(raw)}, nil
}
