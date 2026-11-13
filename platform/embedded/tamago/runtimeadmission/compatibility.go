// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	g "github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission/generated"
)

// ProgrammingProfile is supplied by trusted platform integration. It describes
// compatibility only; it supplies no hardware, boot proof or programming grant.
type ProgrammingProfile struct {
	Profile, ABI   string
	Major          uint16
	DiagnosticOnly bool
}

// Compatibility checks retained package bytes and runtime contract semantics.
// It has no lifecycle or physical activation method. Its zero value rejects.
type Compatibility struct {
	target   Target
	profiles []ProgrammingProfile
}

func NewCompatibility(target Target, profiles []ProgrammingProfile) (*Compatibility, error) {
	identifier := func(s string) bool {
		return s != "" && len(s) <= 128 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n\t")
	}
	if !identifier(target.Platform) || !identifier(target.Device) || len(profiles) == 0 || len(profiles) > 64 {
		return nil, reject("backend_unavailable", "invalid platform compatibility contract")
	}
	c := &Compatibility{target: target}
	for _, p := range profiles {
		if !identifier(p.Profile) || p.DiagnosticOnly && (p.ABI != "" || p.Major != 0) ||
			!p.DiagnosticOnly && (!identifier(p.ABI) || p.Major == 0) {
			return nil, reject("backend_unavailable", "invalid programming profile pairing")
		}
		for _, prior := range c.profiles {
			if prior == p || prior.Profile == p.Profile && prior.DiagnosticOnly != p.DiagnosticOnly {
				return nil, reject("backend_unavailable", "duplicate or contradictory programming profile")
			}
		}
		c.profiles = append(c.profiles, p)
	}
	return c, nil
}

// NewDE10Compatibility uses the existing generated DE10 profile registry to
// exercise the native port against the C++ runtime. It does not support Nano
// programming or construct a DE10 physical backend.
func NewDE10Compatibility() (*Compatibility, error) {
	profiles := make([]ProgrammingProfile, 0, len(g.De10NanoProgrammingProfilePairs))
	for _, p := range g.De10NanoProgrammingProfilePairs {
		profiles = append(profiles, ProgrammingProfile{p.Profile, p.ABI, p.Major, p.DiagnosticOnly})
	}
	return NewCompatibility(Target{g.De10NanoProgrammingPlatform, g.De10NanoProgrammingDevice}, profiles)
}

// CheckArchive requires the exact immutable archive expected by the caller.
// Contract compatibility is one admission prerequisite, not permission to
// retire an owner, release a bridge or program these bytes.
func (c *Compatibility) CheckArchive(archive *corepackage.Archive, expectedID string) error {
	if archive == nil || !digestID(expectedID) {
		return reject("invalid_package", "missing retained archive or expected package identity")
	}
	i := archive.Inspection()
	if i.PackageID != expectedID {
		return reject("invalid_package", "retained package differs from expected identity")
	}
	return c.checkDescriptor(i.Descriptor)
}

func (c *Compatibility) checkDescriptor(d corepackage.Descriptor) error {
	if err := corepackage.ValidateDescriptor(d); err != nil {
		return reject("invalid_package", err.Error())
	}
	if c == nil || len(c.profiles) == 0 {
		return reject("backend_unavailable", "no platform compatibility contract")
	}
	if d.Target.Platform != c.target.Platform || d.Target.Device != c.target.Device {
		return reject("unsupported_target", "package target differs from platform")
	}
	activatable, paired := false, false
	for _, p := range c.profiles {
		if p.Profile == d.Target.ProgrammingProfile && !p.DiagnosticOnly {
			activatable = true
			paired = paired || p.ABI == d.ABI.ID && int64(p.Major) == d.ABI.Major
		}
	}
	if !activatable {
		return reject("unsupported_programming_profile", "programming profile is unavailable or diagnostic-only")
	}
	if !paired {
		return reject("unsupported_abi", "profile and ABI major are not paired")
	}
	return checkContract(d)
}

func incompatible(reason string) error { return reject("unsupported_interface", reason) }

const (
	displayPlane = "fes.fabric.video.display-plane"
	rasterSocket = "fes.fabric.video.raster-rgb888"
	nativeSocket = "fes.fabric.video.native-pixels"
	colecoBus    = "fes.expansion.coleco-bus"
	apple2Bus    = "fes.expansion.apple2-bus"
	c64Bus       = "fes.expansion.c64-bus"
	atariBus     = "fes.expansion.atari-st-bus"
	spectrumBus  = "fes.expansion.spectrum-bus"
)

func current(i corepackage.Interface) bool { return i.Major == 1 && i.Minor == 0 }
func colecoVideoShell(d corepackage.Descriptor) bool {
	return d.Core.ID == "fes.coleco" && (d.Format == 2 || d.Format == 4 && len(d.ROMs) == 2 &&
		d.ROMs[0].Role == "firmware" && d.ROMs[0].SourceSize == 8192 &&
		d.ROMs[1].Role == "cartridge" && d.ROMs[1].SourceSize == 16384)
}
func planePair(display, plane bool) error {
	if display != plane {
		return incompatible("session display and shared display plane must be declared together")
	}
	return nil
}

// This is the runtime's ABI contract policy, independent of a board's profile
// table. Platform I/O availability and live identity remain separate checks.
func checkContract(d corepackage.Descriptor) error {
	if nanoComputerProfile(d) && !nanoComputerROM(d) {
		return reject("unsupported_abi", "native Nano computer requires ABI 1.0 and one held firmware ROM")
	}
	sockets, native := 0, false
	for _, i := range d.Interfaces {
		if i.ID == rasterSocket || i.ID == nativeSocket {
			sockets++
			native = native || i.ID == nativeSocket
		}
	}
	if sockets > 1 {
		return incompatible("video fabric socket declarations must be unique")
	}
	if native && (d.ABI.ID != g.FesApplicationABIID || d.ABI.Major != 1 || !colecoVideoShell(d)) {
		return incompatible("native video socket requires a Coleco application shell")
	}
	if d.Format == 4 || d.Format == 3 && d.ROM.Role == "cartridge" {
		for _, i := range d.Interfaces {
			if current(i) && (i.ID == g.FesApplicationInterfaceMediaBlobStreamID || i.ID == g.FesApplicationInterfaceFirmwareBlobID ||
				d.ABI.ID == g.FesApplicationABIID && d.ABI.Major == 1 && i.ID == g.FesApplicationInterfaceMediaBlobID) {
				return incompatible("linked cartridge cannot declare a reset-held media or firmware mailbox")
			}
		}
	}
	switch {
	case d.ABI.ID == g.FesApplicationABIID && d.ABI.Major == int64(g.FesApplicationABIMajor):
		return applicationContract(d)
	case isComputer(d):
		return computerContract(d)
	case d.ABI.ID == g.FesSimpleComputerABIID && d.ABI.Major == int64(g.FesSimpleComputerABIMajor):
		return simpleComputerContract(d)
	case d.ABI.ID == g.FesGpABIID && d.ABI.Major == int64(g.FesGpABIMajor):
		return gameContract(d)
	default:
		return reject("unsupported_abi", "ABI driver is unavailable")
	}
}

func applicationContract(d corepackage.Descriptor) error {
	if d.ABI.Minor > int64(g.FesApplicationABIMinor) || d.Core.System != "" {
		return reject("unsupported_abi", "unsupported application ABI or system declaration")
	}
	video, blob, stream, menu, blitter, ddr := false, false, false, false, false, false
	gamepad, ports, keypad, display, plane, socket, bus := false, false, false, false, false, false, false
	for _, i := range d.Interfaces {
		if i.ID == displayPlane {
			if !i.Required || !current(i) {
				return incompatible("display plane must be required at version 1.0")
			}
			plane = true
			continue
		}
		if i.ID == rasterSocket || i.ID == nativeSocket {
			if i.Required || !current(i) {
				return incompatible("video socket must be optional at version 1.0")
			}
			socket = true
			continue
		}
		bus = bus || i.ID == colecoBus && i.Major == 2 && i.Minor == 0 && !i.Required
		firmware := i.ID == g.FesApplicationInterfaceFirmwareBlobID
		known := false
		switch i.ID {
		case g.FesApplicationInterfaceGamepadID, g.FesApplicationInterfaceGamepadPortsID,
			g.FesApplicationInterfaceKeypadPortsID, g.FesApplicationInterfaceVideoFixed720p60ID,
			g.FesApplicationInterfaceMediaBlobID, g.FesApplicationInterfaceMediaBlobStreamID,
			g.FesApplicationInterfaceAudioPcmS16Stereo48kID, g.FesApplicationInterfaceMemoryHpsDdrID,
			g.FesApplicationInterfaceVideoMenuDisplayID, g.FesApplicationInterfaceVideoMenuBlitterID,
			g.FesApplicationInterfaceVideoSessionDisplayID, g.FesApplicationInterfaceFirmwareBlobID:
			known = true
		}
		supported := known && i.Major == 1 && (i.Minor == 0 ||
			i.ID == g.FesApplicationInterfaceVideoMenuBlitterID && i.Minor <= int64(g.FesApplicationInterfaceVideoMenuBlitterMinor))
		if !supported {
			if i.Required {
				return incompatible("required application interface is unsupported")
			}
			continue
		}
		if !i.Required && !firmware {
			return incompatible("application operational interfaces must be required")
		}
		switch i.ID {
		case g.FesApplicationInterfaceVideoFixed720p60ID:
			video = i.Required
		case g.FesApplicationInterfaceMemoryHpsDdrID:
			ddr = true
		case g.FesApplicationInterfaceVideoMenuDisplayID:
			menu = true
		case g.FesApplicationInterfaceVideoMenuBlitterID:
			blitter = true
		case g.FesApplicationInterfaceVideoSessionDisplayID:
			display = true
		case g.FesApplicationInterfaceMediaBlobID:
			blob = true
		case g.FesApplicationInterfaceMediaBlobStreamID:
			stream = true
		case g.FesApplicationInterfaceGamepadID:
			gamepad = true
		case g.FesApplicationInterfaceGamepadPortsID:
			ports = true
		case g.FesApplicationInterfaceKeypadPortsID:
			keypad = true
		}
	}
	if socket && (!colecoVideoShell(d) || !bus) {
		return incompatible("video socket requires a Coleco bus-2 developer shell")
	}
	if blitter && !menu {
		return incompatible("menu blitter requires menu display")
	}
	if display && (!ddr || menu) {
		return incompatible("session display requires HPS DDR and excludes menu display")
	}
	if err := planePair(display, plane); err != nil {
		return err
	}
	want := 3
	if blitter {
		want++
	}
	if menu && (!ddr || len(d.Interfaces) != want) {
		return incompatible("menu requires exactly fixed video, DDR, menu display and optionally blitter")
	}
	if gamepad && ports || keypad && !ports {
		return incompatible("controller ports exclude single gamepad and keypad requires ports")
	}
	if !video || stream && !blob {
		return incompatible("application requires fixed video and stream requires blob media")
	}
	return nil
}
