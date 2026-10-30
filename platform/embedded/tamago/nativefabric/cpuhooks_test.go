// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"math"
	"testing"
)

type cpuTestTransport struct {
	base          uint64
	device        *fake
	reads, writes int
	err           error
}

func (c *cpuTestTransport) ReadPhysical32(address uint64) (uint32, error) {
	c.reads++
	if c.err != nil {
		return 0, c.err
	}
	return c.device.Read32(uint32(address - c.base))
}
func (c *cpuTestTransport) WritePhysical32(address uint64, value uint32) error {
	c.writes++
	if c.err != nil {
		return c.err
	}
	return c.device.Write32(uint32(address-c.base), value)
}
func (c *cpuTestTransport) Invoke(request [7]uint64) ([4]uint64, error) {
	if c.err != nil {
		return [4]uint64{}, c.err
	}
	return c.device.Invoke(request)
}

func TestCPUHooksValidateApertureAndPropagateTransportErrors(t *testing.T) {
	f := &fake{control: true}
	cpu := &cpuTestTransport{base: 0x40000000, device: f}
	for _, pair := range [][2]uint64{{0, 256}, {1, 256}, {0x40000000, 0x4c}, {0x40000000, 260}, {math.MaxUint64 - 3, 256}} {
		if _, err := NewCPUHooks(cpu, f, pair[0], uint32(pair[1])); err == nil {
			t.Fatalf("invalid aperture accepted: %x", pair)
		}
	}
	hooks, err := NewCPUHooks(cpu, f, cpu.base, 256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hooks.Read32(0); err != nil || cpu.reads != 1 {
		t.Fatal(err)
	}
	for _, offset := range []uint32{1, 0x50, 0x80, 0xfc, 0x100} {
		if _, err = hooks.Read32(offset); err == nil {
			t.Fatalf("read accepted offset %x", offset)
		}
		if err = hooks.Write32(offset, 0); err == nil {
			t.Fatalf("write accepted offset %x", offset)
		}
	}
	if cpu.reads != 1 || cpu.writes != 0 {
		t.Fatal("invalid access reached CPU transport")
	}
	ioErr := errors.New("transport failed")
	cpu.err = ioErr
	if _, err = hooks.Read32(0); !errors.Is(err, ioErr) {
		t.Fatal(err)
	}
	if err = hooks.Write32(0, 0); !errors.Is(err, ioErr) {
		t.Fatal(err)
	}
	if _, err = hooks.Invoke([7]uint64{}); !errors.Is(err, ioErr) {
		t.Fatal(err)
	}
}
