// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

const hpsContextScope = "source-bound-hps-first-context-v1"

// HPSBootContext is immutable source metadata selected before the native SPL
// exists. Its digest can be compiled into both sides without a phase-one hash
// cycle. It supplies no signing, measured boot or programming permission.
type HPSBootContext struct {
	Format             int    `json:"format"`
	Scope              string `json:"scope"`
	Platform           string `json:"platform"`
	Device             string `json:"device"`
	ProgrammingProfile string `json:"programming_profile"`
	CoreID             string `json:"core_id"`
	ABI                string `json:"abi"`
	Major              uint16 `json:"abi_major"`
	Minor              uint16 `json:"abi_minor"`
	PackageID          string `json:"package_id"`
	BuildID            string `json:"build_id"`
	BuildInputsSHA256  string `json:"build_inputs_sha256"`
	CoreSHA256         string `json:"core_sha256"`
	CoreSize           int64  `json:"core_size"`
	HPSIOHash          string `json:"hps_io_hash"`
	RawSOFSHA256       string `json:"raw_sof_sha256"`
	OriginalPairSHA256 string `json:"original_pair_sha256"`
	PFGIdentity        string `json:"pfg_identity"`
	HPSInitialization  string `json:"hps_initialization"`
	OverlayRevision    string `json:"overlay_revision"`
	BoardRevision      string `json:"board_revision"`
	ControlABI         uint32 `json:"control_abi"`
	PeripheralOffset   uint32 `json:"peripheral_offset"`
	PeripheralSpan     uint32 `json:"peripheral_span"`
}

// HPSBootObservation is a read-only secure-firmware response bracketed by the
// same owned closed bridge state. It is not a runtime admission decision.
type HPSBootObservation struct {
	Digest  [32]byte
	Bridges BridgeSnapshot
}

// ReadHPSBootContext uses the private owned channel only. Every request carries
// the observed owner/generation; a changed scope or partial response rejects.
func ReadHPSBootContext(cpu FirmwareCaller) (HPSBootObservation, error) {
	if cpu == nil {
		return HPSBootObservation{}, errors.New("native fabric: HPS context transport missing")
	}
	b := &FirmwareBridges{cpu: cpu}
	before, err := b.ObserveBridges()
	if err != nil {
		return HPSBootObservation{}, err
	}
	if before.ResetMask != before.Capabilities.Mask || !before.MastersContained {
		return HPSBootObservation{}, errors.New("native fabric: HPS context requires owned closed bridges")
	}
	var digest [32]byte
	for part, operation := range []uint32{g.FesEmbeddedBridgeContextPart0, g.FesEmbeddedBridgeContextPart1} {
		r, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(operation), before.Owner,
			uint64(g.FesEmbeddedBridgeCookie), before.Generation})
		if err != nil {
			return HPSBootObservation{}, err
		}
		if r[0] != 0 || r[1] != uint64(g.FesEmbeddedBridgeContextTag)<<32|uint64(part) {
			return HPSBootObservation{}, fmt.Errorf("native fabric: HPS context part %d rejected, status %d", part, r[0])
		}
		binary.BigEndian.PutUint64(digest[part*16:], r[2])
		binary.BigEndian.PutUint64(digest[part*16+8:], r[3])
	}
	after, err := b.ObserveBridges()
	if err != nil || before != after || digest == [32]byte{} {
		return HPSBootObservation{}, errors.New("native fabric: HPS context lost ownership or returned no identity")
	}
	return HPSBootObservation{digest, after}, nil
}

// HPSContextVerifier must be constructed by the platform from its compiled source
// context. A descriptor supplied with a package is not trusted boot evidence.
// This type does not implement BootHandoffVerifier: source identity alone does
// not qualify the live HPS configuration, CPU aperture or bridge controls.
type HPSContextVerifier struct {
	cpu                    FirmwareCaller
	context                HPSBootContext
	digest                 [32]byte
	readerMemoryMu         sync.Mutex
	readerMemoryAttempted  bool
	readerMemoryReady      bool
	readerMemoryRegion     *Region
	windowMemory           *windowMemoryMap
	windowMemorySHA        [32]byte
	windowPublication      WindowMemoryObservation
	windowArenaSHA         [32]byte
	windowServiceClaimed   atomic.Bool
	ddrReaderAttempted     bool
	computerServiceClaimed bool
	computerMu             sync.Mutex
	computerGeneration     uint64
	computerClosed         bool
	computerReturn         BridgeSnapshot
	computerSDResumed      bool
}

func NewHPSContextVerifier(cpu FirmwareCaller, source []byte) (*HPSContextVerifier, error) {
	return newHPSContextVerifier(cpu, source, false)
}

func newHPSContextVerifier(cpu FirmwareCaller, source []byte, window bool) (*HPSContextVerifier, error) {
	if cpu == nil || len(source) == 0 || len(source) > 4096 {
		return nil, errors.New("native fabric: bounded compiled HPS source context required")
	}
	var c HPSBootContext
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return nil, err
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("native fabric: trailing HPS source context")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(source, &fields); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(fields)
	if err != nil || !bytes.Equal(source, append(canonical, '\n')) {
		return nil, errors.New("native fabric: HPS source context is not canonical")
	}
	profileMatchesABI := c.ProgrammingProfile == HPSFirstDiagnosticProfile && c.ControlABI == 0x10203040 ||
		c.ProgrammingProfile == HPSFirstDDRDiagnosticProfile && c.ControlABI == 0x10203041 ||
		window && c.ProgrammingProfile == HPSFirstWindowDiagnosticProfile && c.ControlABI == 0x10203042
	if c.Format != 1 || c.Scope != hpsContextScope || c.Platform != "de25_nano" || c.Device != "A5EB013BB23BE4SCS" ||
		!profileMatchesABI || c.CoreID != "fes.de25-embedded-diagnostic" || c.ABI != c.CoreID ||
		c.Major != 1 || c.Minor != 0 || c.HPSInitialization != "HPS FIRST" ||
		!nonzeroDigest(c.PackageID) || !validHex(c.BuildID, 32) || c.BuildID == strings.Repeat("0", 32) ||
		!nonzeroDigest(c.BuildInputsSHA256) || !nonzeroDigest(c.CoreSHA256) || !nonzeroDigest(c.HPSIOHash) ||
		!nonzeroDigest(c.RawSOFSHA256) || !nonzeroDigest(c.OriginalPairSHA256) || !validPFGIdentity(c.PFGIdentity) ||
		!validHex(c.OverlayRevision, 40) || !validHex(c.BoardRevision, 40) || c.CoreSize <= 0 || c.CoreSize > 64<<20 ||
		c.PeripheralOffset != 0x10400 || c.PeripheralSpan != 256 {
		return nil, errors.New("native fabric: unsupported HPS source context")
	}
	return &HPSContextVerifier{cpu: cpu, context: c, digest: sha256.Sum256(source)}, nil
}

// Identity describes the compiled diagnostic package; it grants no admission.
func (v *HPSContextVerifier) Identity() fabricmodel.PackageIdentity {
	c := v.context
	return fabricmodel.PackageIdentity{PackageID: c.PackageID, BuildID: c.BuildID, ProgrammedSHA256: c.CoreSHA256,
		CoreID: c.CoreID, Platform: c.Platform, Device: c.Device, ProgrammingProfile: c.ProgrammingProfile,
		ABI: c.ABI, Major: c.Major, Minor: c.Minor}
}

func (v *HPSContextVerifier) Verify(id fabricmodel.PackageIdentity) (HPSBootObservation, error) {
	if v == nil || id != v.Identity() {
		return HPSBootObservation{}, errors.New("native fabric: package differs from compiled HPS source context")
	}
	observed, err := ReadHPSBootContext(v.cpu)
	if err != nil {
		return HPSBootObservation{}, err
	}
	if observed.Digest != v.digest {
		return HPSBootObservation{}, errors.New("native fabric: secure HPS context differs from compiled native context")
	}
	return observed, nil
}
