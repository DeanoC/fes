// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"encoding/binary"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
)

type fixtureC64Picker struct {
	frame   c64PickerFrame
	length  int
	status  uint32
	commits []bool
}

func (p *fixtureC64Picker) Read32(offset uint32) (uint32, error) {
	switch offset {
	case 0x74:
		return pickerID, nil
	case pickerStatus:
		return p.status, nil
	case 0x80:
		return pickerGuardID, nil
	case 0x88:
		return 0, nil
	default:
		return 0, errors.New("unexpected picker read")
	}
}

func (p *fixtureC64Picker) Write32(offset, value uint32) error {
	switch {
	case offset == pickerData && p.length >= 0 && p.length+4 <= len(p.frame):
		binary.LittleEndian.PutUint32(p.frame[p.length:], value)
		p.length += 4
	case offset == pickerStatus && value == 1 && p.length == -1:
		p.length = 0
	case offset == pickerStatus && (value == 2 && p.length == len(p.frame) || value == 3 && p.length == -1):
		p.length = -1
		p.status = (p.status & 0xffff0000) + 0x10000
		p.commits = append(p.commits, value == 2)
		if value == 2 {
			p.status |= pickerShowing
		}
	default:
		return errors.New("unexpected picker write")
	}
	return nil
}

type c64PickerFixtureCPU struct {
	batchedSpectrumCPU
	picker *fixtureC64Picker
}

func (c c64PickerFixtureCPU) WithC64PickerBatch(base uint64, draw func(computer.IO) error) error {
	if c.picker == nil || !c.active || base != 0x20010300 {
		return errors.New("mismatched picker control aperture")
	}
	return draw(c.picker)
}
