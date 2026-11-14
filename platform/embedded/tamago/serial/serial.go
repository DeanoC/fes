//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package serial

// Read polls one console byte without blocking.
func Read() (byte, bool)
