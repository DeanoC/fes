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

// RAMFBConfig describes the decoded wire parameters of a QEMU ramfb configuration.
type RAMFBConfig struct {
	Address uint64
	FourCC  uint32
	Flags   uint32
	Width   int
	Height  int
	Stride  int
}

func validateGeometry(address uint64, width, height, stride int64) error {
	if address == 0 || width < 16 || height < 16 || width > MaxWidth || height > MaxHeight ||
		stride < width*4 || stride > MaxWidth*4 {
		return errors.New("invalid QEMU ramfb geometry or address")
	}
	if address > ^uint64(0)-uint64(stride)*uint64(height) {
		return errors.New("QEMU ramfb address range wraps")
	}
	return nil
}

// Config is the 28-byte, big-endian QEMU RAMFBCfg wire layout.
func Config(address uint64, width, height, stride int) ([ConfigBytes]byte, error) {
	var data [ConfigBytes]byte
	if err := validateGeometry(address, int64(width), int64(height), int64(stride)); err != nil {
		return data, err
	}
	binary.BigEndian.PutUint64(data[0:8], address)
	binary.BigEndian.PutUint32(data[8:12], XRGB8888)
	binary.BigEndian.PutUint32(data[12:16], 0)
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	binary.BigEndian.PutUint32(data[24:28], uint32(stride))
	return data, nil
}

// DecodeConfig parses and validates a 28-byte big-endian QEMU RAMFBCfg payload.
func DecodeConfig(data [ConfigBytes]byte) (RAMFBConfig, error) {
	addr := binary.BigEndian.Uint64(data[0:8])
	fourcc := binary.BigEndian.Uint32(data[8:12])
	flags := binary.BigEndian.Uint32(data[12:16])
	width := binary.BigEndian.Uint32(data[16:20])
	height := binary.BigEndian.Uint32(data[20:24])
	stride := binary.BigEndian.Uint32(data[24:28])
	if fourcc != XRGB8888 {
		return RAMFBConfig{}, errors.New("unsupported QEMU ramfb pixel format")
	}
	if flags != 0 {
		return RAMFBConfig{}, errors.New("non-zero QEMU ramfb flags unsupported")
	}
	if err := validateGeometry(addr, int64(width), int64(height), int64(stride)); err != nil {
		return RAMFBConfig{}, err
	}
	return RAMFBConfig{
		Address: addr,
		FourCC:  fourcc,
		Flags:   flags,
		Width:   int(width),
		Height:  int(height),
		Stride:  int(stride),
	}, nil
}
