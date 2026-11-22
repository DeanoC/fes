// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "testing"

func TestDeviceBatchSerializesMappingAndRetiresOnChange(t *testing.T) {
	model := tables()
	m, err := NewMapper(model, 0x84000000, 0x10000000)
	if err != nil {
		t.Fatal(err)
	}
	device := Range{Base: 0x20010300, Bytes: 32, Kind: Device}
	calls := 0
	err = m.withDeviceMapping(device, func() error {
		calls++
		if m.mu.TryLock() {
			m.mu.Unlock()
			t.Fatal("managed mappings could change during a device batch")
		}
		model.cpu.SCTLR ^= 1
		return nil
	})
	if err == nil || calls != 1 {
		t.Fatal("changed mapping was accepted", err, calls)
	}
	if err := m.withDeviceMapping(device, func() error {
		t.Fatal("retired mapper issued another batch")
		return nil
	}); err == nil {
		t.Fatal("retired mapper accepted another batch")
	}
}
