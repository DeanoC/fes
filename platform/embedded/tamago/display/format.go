// SPDX-License-Identifier: GPL-3.0-or-later

// Package display implements the QEMU ramfb presentation boundary. It is not
// an FPGA or DE25 display driver.
package display

import (
	"encoding/binary"
	"errors"
)

const (
	// XRGB8888 is the DRM fourcc used by QEMU ramfb. In little-endian RAM the
	// byte order is B, G, R, X.
	XRGB8888    = uint32(0x34325258)
	ConfigBytes = 28
	MaxWidth    = 4096
	MaxHeight   = 2160
)

// Config is the 28-byte, big-endian QEMU RAMFBCfg wire layout.
func Config(address uint64, width, height, stride int) ([ConfigBytes]byte, error) {
	var data [ConfigBytes]byte
	if address == 0 || width < 16 || height < 16 || width > MaxWidth || height > MaxHeight ||
		stride < width*4 || stride > MaxWidth*4 {
		return data, errors.New("invalid QEMU ramfb geometry or address")
	}
	if address > ^uint64(0)-uint64(stride*height) {
		return data, errors.New("QEMU ramfb address range wraps")
	}
	binary.BigEndian.PutUint64(data[0:8], address)
	binary.BigEndian.PutUint32(data[8:12], XRGB8888)
	binary.BigEndian.PutUint32(data[12:16], 0)
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	binary.BigEndian.PutUint32(data[24:28], uint32(stride))
	return data, nil
}
