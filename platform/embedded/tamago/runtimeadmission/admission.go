// SPDX-License-Identifier: GPL-3.0-or-later

// Package runtimeadmission binds retained FES archives to a restricted software
// backend. Policy is a trusted test input produced by the existing C++ runtime
// compatibility checker, not a signature, hardware capability or DE25 driver.
package runtimeadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

type Contract struct {
	ID    string `json:"id"`
	Major int64  `json:"major"`
	Minor int64  `json:"minor"`
}
type Driver struct {
	Profile    string     `json:"profile"`
	ABI        Contract   `json:"abi"`
	Interfaces []Contract `json:"interfaces"`
}
type Target struct {
	Platform string `json:"platform"`
	Device   string `json:"device"`
}
type Decision struct {
	PackageID string `json:"package_id"`
	Code      string `json:"code"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
}
type Policy struct {
	Scope     string     `json:"scope"`
	RuntimeID string     `json:"runtime_id"`
	Target    Target     `json:"target"`
	Drivers   []Driver   `json:"drivers"`
	Decisions []Decision `json:"decisions"`
}

type Error struct{ Code, Reason string }

func (e *Error) Error() string         { return e.Code + ": " + e.Reason }
func reject(code, reason string) error { return &Error{code, reason} }
func ErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var admission *Error
	if errors.As(err, &admission) {
		return admission.Code
	}
	return "model_error"
}
func digestID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func valid(c Contract) bool {
	return c.ID != "" && c.Major >= 0 && c.Major <= 65535 && c.Minor >= 0 && c.Minor <= 65535
}

// Gate has no physical activation method. Its zero value fails closed.
// The caller owns the controller and serializes LoadModel with all other work.
type Gate struct {
	runtimeID string
	target    Target
	drivers   []Driver
	decisions map[string]Decision
}

func NewModel(policy Policy) (*Gate, error) {
	if policy.Scope != "qemu-model-only" || !digestID(policy.RuntimeID) || policy.Target.Platform == "" || policy.Target.Device == "" || len(policy.Drivers) == 0 || len(policy.Drivers) > 16 || len(policy.Decisions) > 512 {
		return nil, reject("backend_unavailable", "invalid model backend policy")
	}
	g := &Gate{runtimeID: policy.RuntimeID, target: policy.Target, decisions: make(map[string]Decision)}
	for _, d := range policy.Drivers {
		if d.Profile == "" || !valid(d.ABI) || len(d.Interfaces) > 64 {
			return nil, reject("backend_unavailable", "invalid model driver")
		}
		for _, prior := range g.drivers {
			if prior.Profile == d.Profile && prior.ABI == d.ABI {
				return nil, reject("backend_unavailable", "duplicate model driver")
			}
		}
		seen := make(map[string]bool)
		for _, i := range d.Interfaces {
			if !valid(i) || seen[i.ID] {
				return nil, reject("backend_unavailable", "invalid model interface")
			}
			seen[i.ID] = true
		}
		d.Interfaces = append([]Contract(nil), d.Interfaces...)
		g.drivers = append(g.drivers, d)
	}
	for _, d := range policy.Decisions {
		if !digestID(d.PackageID) || d.Code == "" || len(d.Code) > 64 {
			return nil, reject("backend_unavailable", "invalid runtime decision")
		}
		if _, found := g.decisions[d.PackageID]; found {
			return nil, reject("backend_unavailable", "duplicate runtime decision")
		}
		g.decisions[d.PackageID] = d
	}
	return g, nil
}

func (g *Gate) check(inspection corepackage.Inspection, expectedID string, linked bool) error {
	if !digestID(expectedID) || inspection.PackageID != expectedID {
		return reject("package_mismatch", "retained package differs from requested identity")
	}
	if g == nil || g.runtimeID == "" {
		return reject("backend_unavailable", "no qualified model policy")
	}
	d := inspection.Descriptor
	if d.Target.Platform != g.target.Platform || d.Target.Device != g.target.Device {
		return reject("unsupported_target", "backend target mismatch")
	}
	profile := false
	var driver *Driver
	for i := range g.drivers {
		row := &g.drivers[i]
		if row.Profile != d.Target.ProgrammingProfile {
			continue
		}
		profile = true
		if row.ABI == (Contract{d.ABI.ID, d.ABI.Major, d.ABI.Minor}) {
			driver = row
			break
		}
	}
	if !profile {
		return reject("unsupported_programming_profile", "model has no driver for profile")
	}
	if driver == nil {
		return reject("unsupported_abi", "model has no driver for ABI version")
	}
	if d.Format != 2 && !linked {
		return reject("unsupported_abi", "raw ROM packages require a retained activation receipt")
	}
	for _, required := range d.Interfaces {
		if !required.Required {
			continue
		}
		found := false
		for _, supported := range driver.Interfaces {
			if supported == (Contract{required.ID, required.Major, required.Minor}) {
				found = true
				break
			}
		}
		if !found {
			return reject("unsupported_interface", "model driver lacks required interface")
		}
	}
	decision, found := g.decisions[expectedID]
	if !found {
		return reject("backend_unavailable", "no runtime compatibility decision for exact package")
	}
	if decision.Code != "none" {
		return reject(decision.Code, decision.Phase+": "+decision.Message)
	}
	return nil
}

// Admit performs every check before a lifecycle transition can retire input,
// save the current owner, drain display or issue a model program command.
// PrepareModel admits a retained format-2 package without touching the
// current owner. It is limited to a trusted qemu-model-only policy; callers of
// an injected endpoint must also perform that endpoint's preflight.
func (g *Gate) PrepareModel(archive *corepackage.Archive, expectedID string) (*fabricmodel.Image, error) {
	if archive == nil || !digestID(expectedID) {
		return nil, reject("package_mismatch", "missing retained package or expected identity")
	}
	inspection := archive.Inspection()
	if inspection.PackageID != expectedID {
		return nil, reject("package_mismatch", "retained package differs from requested identity")
	}
	if err := g.check(inspection, expectedID, false); err != nil {
		return nil, err
	}
	d := inspection.Descriptor
	// A deliberately small model bound, not the supported production RBF size.
	if d.Payload.Size < 1 || d.Payload.Size > fabricmodel.MaxImageBytes {
		return nil, reject("model_limit", "payload exceeds software model bound")
	}
	payload, err := io.ReadAll(io.LimitReader(archive.Payload(), d.Payload.Size+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if int64(len(payload)) != d.Payload.Size || hex.EncodeToString(sum[:]) != d.Payload.SHA256 {
		return nil, reject("package_mismatch", "retained payload mismatch")
	}
	binding := fabricmodel.PackageIdentity{PackageID: expectedID, CoreID: d.Core.ID, ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), BuildID: d.Build.ID,
		Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile, ProgrammedSHA256: d.Payload.SHA256}
	return fabricmodel.Prepare("model.package", payload, sum, fabricmodel.Identity{ABI: 1, Core: 1, Build: 1, Package: binding}, false)
}

func (g *Gate) LoadModel(controller *fabricmodel.Controller, archive *corepackage.Archive, expectedID string, now, deadline time.Duration) error {
	if controller == nil {
		return reject("backend_unavailable", "missing model controller")
	}
	image, err := g.PrepareModel(archive, expectedID)
	if err != nil {
		return err
	}
	return controller.Load(image, now, deadline)
}

// LoadLinkedModel requires an opaque locally linked object and the exact receipt
// selected by the caller. A raw package or reconstructed JSON cannot replace it.
func (g *Gate) LoadLinkedModel(controller *fabricmodel.Controller, linked *corepackage.LinkedArchive, expectedID, expectedReceipt string, now, deadline time.Duration) error {
	if controller == nil {
		return reject("backend_unavailable", "missing model controller")
	}
	image, err := g.PrepareLinkedModel(linked, expectedID, expectedReceipt)
	if err != nil {
		return err
	}
	return controller.Load(image, now, deadline)
}

// PrepareLinkedModel completes admission without retiring the current owner.
// An injected endpoint can reject an unsupported hardware identity before Load.
func (g *Gate) PrepareLinkedModel(linked *corepackage.LinkedArchive, expectedID, expectedReceipt string) (*fabricmodel.Image, error) {
	if !digestID(expectedReceipt) || linked.ReceiptSHA256() != expectedReceipt {
		return nil, reject("receipt_mismatch", "linked ROM activation receipt differs from request")
	}
	inspection := linked.Inspection()
	if err := g.check(inspection, expectedID, true); err != nil {
		return nil, err
	}
	r := linked.Receipt()
	var size int64
	var digest string
	switch {
	case inspection.Descriptor.Format == 3 && r.ROMLink != nil && r.ROMLinks == nil && r.ROMLink.ValidFor(inspection.Descriptor):
		size, digest = r.ROMLink.ProgrammedSize, r.ROMLink.ProgrammedSHA256
	case inspection.Descriptor.Format == 4 && r.ROMLinks != nil && r.ROMLink == nil && r.ROMLinks.ValidFor(inspection.Descriptor):
		size, digest = r.ROMLinks.ProgrammedSize, r.ROMLinks.ProgrammedSHA256
	default:
		return nil, reject("receipt_mismatch", "missing or invalid linked ROM identity")
	}
	if r.PackageID != expectedID {
		return nil, reject("receipt_mismatch", "receipt package identity mismatch")
	}
	if size < 1 || size > fabricmodel.MaxImageBytes {
		return nil, reject("model_limit", "linked output exceeds software model bound")
	}
	payload, err := io.ReadAll(io.LimitReader(linked.Programmed(), size+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if int64(len(payload)) != size || hex.EncodeToString(sum[:]) != digest {
		return nil, reject("receipt_mismatch", "linked programming bytes differ from receipt")
	}
	d := inspection.Descriptor
	binding := fabricmodel.PackageIdentity{PackageID: expectedID, CoreID: d.Core.ID, ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), BuildID: d.Build.ID,
		Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile, ActivationReceiptSHA256: expectedReceipt, ProgrammedSHA256: digest}
	image, err := fabricmodel.Prepare("model.linked", payload, sum, fabricmodel.Identity{ABI: 1, Core: 1, Build: 1, Package: binding}, false)
	if err != nil {
		return nil, err
	}
	return image, nil
}
