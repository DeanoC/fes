// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

// IdentityProfile is an explicit experimental association between an admitted
// target/profile/ABI and the endpoint's 32-bit ABI personality. Nothing here
// registers that tuple as a physical programming capability.
type IdentityProfile struct {
	Platform, Device, ProgrammingProfile, ABI string
	Major, Minor                              uint16
	ControlABI                                uint32
	RequireBootHandoff                        bool
}

// BootHandoffVerifier must compare the admitted package against a trusted
// observation of the running HPS boot configuration. A package or receipt by
// itself is not evidence of what booted the board.
type BootHandoffVerifier interface {
	VerifyBootHandoff(fabricmodel.PackageIdentity) error
}

func profileValid(p IdentityProfile) bool {
	for _, v := range []string{p.Platform, p.Device, p.ProgrammingProfile, p.ABI} {
		if v == "" || len(v) > 128 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\x00\r\n\t") {
			return false
		}
	}
	return p.Major != 0 && p.ControlABI != 0
}
func sameProfile(a, b IdentityProfile) bool {
	return a.Platform == b.Platform && a.Device == b.Device && a.ProgrammingProfile == b.ProgrammingProfile && a.ABI == b.ABI && a.Major == b.Major && a.Minor == b.Minor
}
func validHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// BuildWords interprets the existing 32-hex-digit producer BUILD_ID as a
// Verilog 128-bit integer. The endpoint exposes its least significant 32-bit
// word first, unlike the left-to-right spelling of the descriptor.
func BuildWords(buildID string) ([4]uint32, error) {
	var words [4]uint32
	if !validHex(buildID, 32) {
		return words, errors.New("native fabric: invalid package build ID")
	}
	raw, err := hex.DecodeString(buildID)
	if err != nil {
		return words, err
	}
	for i := range words {
		j := 12 - 4*i
		words[i] = uint32(raw[j])<<24 | uint32(raw[j+1])<<16 | uint32(raw[j+2])<<8 | uint32(raw[j+3])
	}
	if words == [4]uint32{} {
		return words, errors.New("native fabric: empty package build ID")
	}
	return words, nil
}

func (e *Endpoint) packageIdentity(p fabricmodel.PackageIdentity) (embeddedcontrol.Identity, error) {
	if !validHex(p.PackageID, 64) || !validHex(p.ProgrammedSHA256, 64) || p.CoreID == "" ||
		(p.ActivationReceiptSHA256 != "" && !validHex(p.ActivationReceiptSHA256, 64)) {
		return embeddedcontrol.Identity{}, errors.New("native fabric: incomplete admitted package binding")
	}
	build, err := BuildWords(p.BuildID)
	if err != nil {
		return embeddedcontrol.Identity{}, err
	}
	for _, profile := range e.options.IdentityProfiles {
		if profile.Platform == p.Platform && profile.Device == p.Device && profile.ProgrammingProfile == p.ProgrammingProfile && profile.ABI == p.ABI && profile.Major == p.Major && profile.Minor == p.Minor {
			return embeddedcontrol.Identity{ABI: profile.ControlABI, Build: build}, nil
		}
	}
	return embeddedcontrol.Identity{}, fmt.Errorf("native fabric: no explicit identity profile for %s/%s/%s %s %d.%d", p.Platform, p.Device, p.ProgrammingProfile, p.ABI, p.Major, p.Minor)
}

func (e *Endpoint) verifyBootHandoff(p fabricmodel.PackageIdentity) error {
	for _, profile := range e.options.IdentityProfiles {
		if profile.Platform == p.Platform && profile.Device == p.Device && profile.ProgrammingProfile == p.ProgrammingProfile && profile.ABI == p.ABI && profile.Major == p.Major && profile.Minor == p.Minor {
			if !profile.RequireBootHandoff {
				return nil
			}
			verifier, ok := e.hooks.(BootHandoffVerifier)
			if !ok {
				return errors.New("native fabric: boot handoff verifier unavailable")
			}
			return verifier.VerifyBootHandoff(p)
		}
	}
	return errors.New("native fabric: no identity profile for boot handoff")
}

// Preflight checks an already admitted image before the controller retires the
// current owner. It does not perform I/O or establish producer authenticity.
func (e *Endpoint) Preflight(image *fabricmodel.Image) error {
	if e == nil || e.retired {
		return errors.New("native fabric: endpoint retired")
	}
	id, digest, valid := image.Binding()
	if !valid {
		return errors.New("native fabric: retained image invalid")
	}
	if id.Package.PackageID == "" {
		return errors.New("native fabric: image lacks admitted package binding")
	}
	if _, err := e.packageIdentity(id.Package); err != nil {
		return err
	}
	if err := e.verifyBootHandoff(id.Package); err != nil {
		return err
	}
	if hex.EncodeToString(digest[:]) != id.Package.ProgrammedSHA256 {
		return errors.New("native fabric: retained image differs from admitted programmed digest")
	}
	return nil
}
