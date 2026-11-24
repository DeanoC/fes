// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"github.com/DeanoC/FogCast/corepackage"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

const c64Profile = "de25-native-c64-v1"
const c64FirmwareBytes = 36 << 10
const c64Capabilities = spectrum48Capabilities&^cg.FesComputerCapabilityMediaSpectrumTape | cg.FesComputerCapabilityMediaC64Disk

var c64DiskInterface = runtimeadmission.Contract{ID: cg.FesComputerInterfaceMediaC64DiskID, Major: 1}

func c64UploadDriver() runtimeadmission.Driver {
	driver := spectrum48UploadDriver()
	driver.Profile = c64Profile
	for i, supported := range driver.Interfaces {
		if supported.ID == cg.FesComputerInterfaceMediaSpectrumTapeID {
			driver.Interfaces[i] = c64DiskInterface
		}
	}
	return driver
}

func declaresC64Disk(d corepackage.Descriptor) bool {
	for _, media := range d.Interfaces {
		if media.Required && media.ID == c64DiskInterface.ID &&
			media.Major == c64DiskInterface.Major && media.Minor == c64DiskInterface.Minor {
			return true
		}
	}
	return false
}
