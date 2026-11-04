// SPDX-License-Identifier: GPL-3.0-or-later

// Package usb contains bounded keyboard/gamepad descriptor and report parsers
// and the QEMU host controller adapter used by the embedded experiment.
package usb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const MaxConfigurationBytes = 4096

var (
	ErrDescriptor     = errors.New("invalid USB configuration descriptor")
	ErrNoBootKeyboard = errors.New("USB configuration has no boot keyboard")
)

// Configuration selects one alternate-zero boot keyboard and its interrupt-IN
// endpoint. MaxPacketSize excludes the high-bandwidth transaction bits; this
// adapter rejects endpoints that require those transactions.
type Configuration struct {
	Value         uint8
	Interface     uint8
	Endpoint      uint8
	MaxPacketSize uint16
	Interval      uint8
}

// GamepadConfiguration selects one non-boot HID interface. Its report
// descriptor still has to prove a Joystick/Game Pad application before use.
type GamepadConfiguration struct {
	Configuration
	ReportDescriptorBytes uint16
}

var ErrNoGamepadInterface = errors.New("USB configuration has no gamepad HID candidate")

// ParseConfiguration accepts a complete standard configuration descriptor set.
// Parsing ends at wTotalLength. Composite devices are allowed, but multiple
// boot keyboards or input endpoints are rejected rather than chosen arbitrarily.
func ParseConfiguration(data []byte) (Configuration, error) {
	c, _, err := parseConfiguration(data, false)
	return c, err
}
func ParseGamepadConfiguration(data []byte) (GamepadConfiguration, error) {
	c, n, err := parseConfiguration(data, true)
	if err != nil {
		return GamepadConfiguration{}, err
	}
	return GamepadConfiguration{c, n}, nil
}

func parseConfiguration(data []byte, gamepad bool) (Configuration, uint16, error) {
	var reportBytes uint16
	var result Configuration
	invalid := func(reason string) (Configuration, uint16, error) {
		return Configuration{}, 0, fmt.Errorf("%w: %s", ErrDescriptor, reason)
	}
	if len(data) < 9 || data[0] != 9 || data[1] != 2 {
		return invalid("configuration header")
	}
	total := int(binary.LittleEndian.Uint16(data[2:4]))
	if total < 9 || total > MaxConfigurationBytes || total > len(data) {
		return invalid("total length")
	}
	if data[4] == 0 || data[5] == 0 || data[7]&0x80 == 0 || data[7]&0x1f != 0 {
		return invalid("configuration fields")
	}
	result.Value = data[5]
	var interfaceNumbers [256]bool
	interfaceCount := 0
	settings := make(map[uint16]bool)
	haveInterface, candidate := false, false
	candidateCount, inputCount := 0, 0
	declaredEndpoints, endpoints := 0, 0
	var addresses [32]bool
	hidSeen := false
	finishInterface := func() error {
		if !haveInterface {
			return nil
		}
		if endpoints != declaredEndpoints {
			return fmt.Errorf("%w: endpoint count", ErrDescriptor)
		}
		if candidate && (!hidSeen || inputCount != 1) {
			return fmt.Errorf("%w: HID descriptor or input endpoint", ErrDescriptor)
		}
		return nil
	}
	for offset := 9; offset < total; {
		if total-offset < 2 {
			return invalid("truncated descriptor header")
		}
		length := int(data[offset])
		if length < 2 || length > total-offset {
			return invalid("descriptor length")
		}
		descriptor := data[offset : offset+length]
		switch descriptor[1] {
		case 2:
			return invalid("nested configuration")
		case 4:
			if err := finishInterface(); err != nil {
				return Configuration{}, 0, err
			}
			if length != 9 || descriptor[4] > 30 {
				return invalid("interface fields")
			}
			setting := uint16(descriptor[2])<<8 | uint16(descriptor[3])
			if settings[setting] {
				return invalid("duplicate interface alternate setting")
			}
			settings[setting] = true
			if !interfaceNumbers[descriptor[2]] {
				interfaceNumbers[descriptor[2]] = true
				interfaceCount++
			}
			haveInterface = true
			candidate = descriptor[3] == 0 && descriptor[5] == 3 && ((gamepad && descriptor[6] == 0 && descriptor[7] == 0) || (!gamepad && descriptor[6] == 1 && descriptor[7] == 1))
			declaredEndpoints, endpoints, inputCount = int(descriptor[4]), 0, 0
			addresses = [32]bool{}
			hidSeen = false
			if candidate {
				candidateCount++
				if candidateCount > 1 {
					if gamepad {
						return invalid("multiple gamepad HID candidates")
					}
					return invalid("multiple boot keyboards")
				}
				result.Interface = descriptor[2]
			}
		case 5:
			if !haveInterface || length < 7 {
				return invalid("endpoint without interface or truncated endpoint")
			}
			address := descriptor[2]
			if address&0x70 != 0 || address&0x0f == 0 {
				return invalid("endpoint address")
			}
			index := int(address&0x0f) + int(address>>7)*16
			if addresses[index] {
				return invalid("duplicate endpoint address")
			}
			addresses[index] = true
			endpoints++
			if candidate && address&0x80 != 0 && descriptor[3]&3 == 3 {
				packet := binary.LittleEndian.Uint16(descriptor[4:6])
				minimum := uint16(8)
				if gamepad {
					minimum = 1
				}
				if packet < minimum || packet > 1024 || descriptor[6] == 0 {
					return invalid("unsupported HID packet size or interval")
				}
				inputCount++
				if inputCount > 1 {
					return invalid("multiple HID input endpoints")
				}
				result.Endpoint, result.MaxPacketSize, result.Interval = address, packet, descriptor[6]
			}
		case 0x21:
			if candidate {
				if hidSeen || length < 9 || descriptor[5] == 0 || length != 6+3*int(descriptor[5]) {
					return invalid("HID descriptor fields")
				}
				hidSeen = true
				reportSeen := false
				for field := 6; field < length; field += 3 {
					if descriptor[field] == 0x22 && binary.LittleEndian.Uint16(descriptor[field+1:field+3]) != 0 {
						if reportSeen {
							return invalid("duplicate HID report descriptor")
						}
						reportBytes = binary.LittleEndian.Uint16(descriptor[field+1 : field+3])
						if gamepad && reportBytes > MaxConfigurationBytes {
							return invalid("HID report descriptor size")
						}
						reportSeen = true
					}
				}
				if !reportSeen {
					return invalid("missing HID report descriptor")
				}
			}
		}
		offset += length
	}
	if err := finishInterface(); err != nil {
		return Configuration{}, 0, err
	}
	if interfaceCount != int(data[4]) {
		return invalid("interface count")
	}
	if candidateCount == 0 {
		if gamepad {
			return Configuration{}, 0, ErrNoGamepadInterface
		}
		return Configuration{}, 0, ErrNoBootKeyboard
	}
	return result, reportBytes, nil
}
