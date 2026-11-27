// SPDX-License-Identifier: GPL-3.0-or-later

package computer

// Native C64 extends the scanout aperture in unused transaction directions.
// Status reads the scanout command address. Data and command write to
// BUILD_ID0 and BUILD_ID1. The ordinary scanout accesses remain available.
const (
	C64PickerStatus  = 0x0c
	C64PickerData    = 0x64
	C64PickerCommand = 0x68
	C64PickerTag     = 0x4788
	C64PickerTagMask = 0xfff8
)
