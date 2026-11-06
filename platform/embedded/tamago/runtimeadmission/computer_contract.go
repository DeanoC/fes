// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"github.com/DeanoC/FogCast/corepackage"
	c "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission/generated"
)

func isComputer(d corepackage.Descriptor) bool {
	return d.ABI.ID == c.FesComputerABIID && d.ABI.Major == int64(c.FesComputerABIMajor)
}

func computerContract(d corepackage.Descriptor) error {
	// Match the implemented C++ driver, which reads identity words 0..15.
	// The shared ABI already describes 1.1; that does not implement its driver.
	if d.ABI.Minor > 0 || d.Core.System != "" {
		return reject("unsupported_abi", "unsupported computer ABI or system declaration")
	}
	if d.Format == 4 || d.Format == 3 && d.ROM.Role != "firmware" {
		return reject("unsupported_abi", "computer may link only a firmware ROM")
	}
	present := make(map[string]bool)
	mediaUnits, buses := 0, 0
	for _, i := range d.Interfaces {
		if i.ID == "fes.embedded.rom" && nanoComputerROM(d) {
			if !i.Required || !current(i) {
				return incompatible("held ROM requires the native Nano firmware contract at version 1.0")
			}
			continue
		}
		bus := i.ID == apple2Bus || i.ID == c64Bus || i.ID == atariBus || i.ID == spectrumBus
		media := i.ID == c.FesComputerInterfaceMediaApple2FloppyID || i.ID == c.FesComputerInterfaceMediaC64DiskID ||
			i.ID == c.FesComputerInterfaceMediaAtariStFloppyID || i.ID == c.FesComputerInterfaceMediaSpectrumTapeID ||
			i.ID == c.FesComputerInterfaceMediaZx81TapeID
		known := media || bus
		switch i.ID {
		case c.FesComputerInterfaceVideoFixed720p60ID, c.FesComputerInterfaceKeyboardHidID,
			c.FesComputerInterfaceMouseRelativeID, c.FesComputerInterfaceMediaAtariStFloppyWriteID,
			c.FesComputerInterfaceMediaZx81TapeWriteID, c.FesComputerInterfaceMediaSpectrumDiskID,
			c.FesComputerInterfaceMediaAtariStFloppyGeometryID, c.FesComputerInterfaceGamepadPortsID,
			c.FesComputerInterfaceAudioPcmS16Stereo48kID, c.FesComputerInterfaceMemoryHpsDdrID,
			c.FesComputerInterfaceVideoSessionDisplayID, displayPlane:
			known = true
		}
		version := current(i)
		if i.ID == c.FesComputerInterfaceMediaSpectrumTapeID {
			version = i.Major == int64(c.FesComputerInterfaceMediaSpectrumTapeMajor) &&
				i.Minor == int64(c.FesComputerInterfaceMediaSpectrumTapeMinor)
		}
		if !known || !version {
			if i.Required {
				return incompatible("required computer interface is unsupported")
			}
			continue
		}
		if bus {
			if i.Required {
				return incompatible("computer expansion bus must be optional")
			}
			buses++
			continue
		}
		if !i.Required {
			return incompatible("computer operational interfaces must be required")
		}
		present[i.ID] = true
		if media {
			mediaUnits++
		}
	}
	display, ddr := present[c.FesComputerInterfaceVideoSessionDisplayID], present[c.FesComputerInterfaceMemoryHpsDdrID]
	if display && !ddr {
		return incompatible("session display requires HPS DDR")
	}
	if err := planePair(display, present[displayPlane]); err != nil {
		return err
	}
	if (present[c.FesComputerInterfaceMediaAtariStFloppyWriteID] || present[c.FesComputerInterfaceMediaAtariStFloppyGeometryID]) &&
		!present[c.FesComputerInterfaceMediaAtariStFloppyID] {
		return incompatible("Atari ST media extensions require the base floppy interface")
	}
	if present[c.FesComputerInterfaceMediaZx81TapeWriteID] && !present[c.FesComputerInterfaceMediaZx81TapeID] {
		return incompatible("ZX81 tape write requires the base tape interface")
	}
	if present[c.FesComputerInterfaceMediaSpectrumDiskID] && (!present[c.FesComputerInterfaceMediaSpectrumTapeID] || !ddr) {
		return incompatible("Spectrum disk requires Spectrum tape and HPS DDR")
	}
	if mediaUnits > 1 || buses > 1 {
		return incompatible("computer declares more than one unit-0 medium or expansion bus")
	}
	if !present[c.FesComputerInterfaceVideoFixed720p60ID] {
		return incompatible("computer requires fixed video")
	}
	return nil
}

func simpleComputerContract(d corepackage.Descriptor) error {
	if d.ABI.Minor > int64(g.FesSimpleComputerABIMinor) || d.Core.System != "" {
		return reject("unsupported_abi", "unsupported simple-computer ABI or system declaration")
	}
	keyboard, video, media, ddr, display, plane := false, false, false, false, false, false
	for _, i := range d.Interfaces {
		switch i.ID {
		case g.FesSimpleComputerInterfaceKeyboardID, g.FesSimpleComputerInterfaceVideoFixed720p60ID, g.FesSimpleComputerInterfaceMediaBlobID:
			major, minor := g.FesSimpleComputerInterfaceKeyboardMajor, g.FesSimpleComputerInterfaceKeyboardMinor
			if i.ID == g.FesSimpleComputerInterfaceVideoFixed720p60ID {
				major, minor = g.FesSimpleComputerInterfaceVideoFixed720p60Major, g.FesSimpleComputerInterfaceVideoFixed720p60Minor
			} else if i.ID == g.FesSimpleComputerInterfaceMediaBlobID {
				major, minor = g.FesSimpleComputerInterfaceMediaBlobMajor, g.FesSimpleComputerInterfaceMediaBlobMinor
			}
			supported := i.Major == int64(major) && i.Minor <= int64(minor)
			if !supported && i.Required {
				return incompatible("required simple-computer interface is unsupported")
			}
			active := supported && i.Required
			switch i.ID {
			case g.FesSimpleComputerInterfaceKeyboardID:
				keyboard = active
			case g.FesSimpleComputerInterfaceVideoFixed720p60ID:
				video = active
			case g.FesSimpleComputerInterfaceMediaBlobID:
				media = active
			}
		case g.FesSimpleComputerInterfaceMediaBlobStreamID:
			if i.Required && (i.Major != int64(g.FesSimpleComputerInterfaceMediaBlobStreamMajor) || i.Minor != int64(g.FesSimpleComputerInterfaceMediaBlobStreamMinor)) {
				return incompatible("required media stream interface is unsupported")
			}
		case g.FesSimpleComputerInterfaceAudioPcmS16Stereo48kID:
			if !i.Required || i.Major != int64(g.FesSimpleComputerInterfaceAudioPcmS16Stereo48kMajor) || i.Minor != int64(g.FesSimpleComputerInterfaceAudioPcmS16Stereo48kMinor) {
				return incompatible("simple-computer audio must be required at version 1.0")
			}
		case g.FesSimpleComputerInterfaceMemoryHpsDdrID, g.FesSimpleComputerInterfaceVideoSessionDisplayID, displayPlane:
			if !i.Required || !current(i) {
				return incompatible("simple-computer DDR, display and plane must be required at version 1.0")
			}
			switch i.ID {
			case g.FesSimpleComputerInterfaceMemoryHpsDdrID:
				ddr = true
			case displayPlane:
				plane = true
			default:
				display = true
			}
		default:
			if i.Required {
				return incompatible("required simple-computer interface is unsupported")
			}
		}
	}
	if display && !ddr {
		return incompatible("session display requires HPS DDR")
	}
	if err := planePair(display, plane); err != nil {
		return err
	}
	linkedCartridge := d.Format == 3 && d.ROM.Role == "cartridge"
	if !keyboard || !video || !media && !linkedCartridge {
		return incompatible("required simple-computer interfaces are missing")
	}
	return nil
}

func gameContract(d corepackage.Descriptor) error {
	if d.ABI.Minor > int64(g.FesGpABIMinor) || d.Core.System != "" {
		return reject("unsupported_abi", "unsupported simple-game ABI or system declaration")
	}
	gamepad, video, words, layout := false, false, false, false
	for _, i := range d.Interfaces {
		switch i.ID {
		case g.FesGpInterfaceGamepadID, g.FesGpInterfaceVideoFixed720p60ID:
			major, minor := g.FesGpInterfaceGamepadMajor, g.FesGpInterfaceGamepadMinor
			if i.ID == g.FesGpInterfaceVideoFixed720p60ID {
				major, minor = g.FesGpInterfaceVideoFixed720p60Major, g.FesGpInterfaceVideoFixed720p60Minor
			}
			supported := i.Major == int64(major) && i.Minor <= int64(minor)
			if !supported && i.Required {
				return incompatible("required game interface is unsupported")
			}
			if i.ID == g.FesGpInterfaceGamepadID {
				gamepad = supported && i.Required
			} else {
				video = supported && i.Required
			}
		case g.FesGpInterfacePersistenceWordsID, g.FesGpInterfacePongProgressID:
			if !i.Required || !current(i) {
				return incompatible("unsupported persistence version or requirement")
			}
			if i.ID == g.FesGpInterfacePersistenceWordsID {
				words = true
			} else {
				layout = true
			}
		default:
			if i.Required {
				return incompatible("required game interface is unsupported")
			}
		}
	}
	if words != layout {
		return incompatible("persistence transport requires exactly one registered layout")
	}
	if !gamepad || !video {
		return incompatible("required simple-game interfaces are missing")
	}
	return nil
}
