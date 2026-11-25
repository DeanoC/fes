// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import (
	_ "embed"
	"errors"
	"io/fs"
)

//go:embed firmware/rtl8761bu_fw.bin
var builtinRTL8761BUFW []byte

//go:embed firmware/rtl8761bu_config.bin
var builtinRTL8761BUConfig []byte

func builtinFirmware(name string) ([]byte, bool) {
	switch name {
	case "rtl8761bu_fw.bin":
		return builtinRTL8761BUFW, true
	case "rtl8761bu_config.bin":
		return builtinRTL8761BUConfig, true
	default:
		return nil, false
	}
}

func loadFirmwareWithBuiltin(fw FirmwareLoader, name string, optional bool) ([]byte, error) {
	if fw != nil {
		b, err := fw.LoadFirmware(name)
		if err == nil {
			return b, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if b, ok := builtinFirmware(name); ok {
		return b, nil
	}
	if optional {
		return nil, nil
	}
	return nil, fs.ErrNotExist
}
