// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
)

type romBatchFunc func(uint64, func(computer.IO) error) error

func (f romBatchFunc) WithSpectrumROMBatch(base uint64, send func(computer.IO) error) error {
	return f(base, send)
}

func TestComputerFirmwareBatchRejectsOwnershipChanges(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			v, cpu, control, mapping := computerFixture(t, "")
			s, err := v.OpenComputer(t.Context(), control, mapping, computerCompletionFixture(v, cpu), computerLimits())
			if err != nil {
				t.Fatal(err)
			}
			issued := 0
			s.port.romBatch = romBatchFunc(func(base uint64, send func(computer.IO) error) error {
				if !s.port.mu.TryLock() {
					issued++
					return send(nil)
				}
				s.port.mu.Unlock()
				t.Fatal("ROM batch ran outside the owned session lock")
				return nil
			})
			if stage == "before" {
				cpu.state.Generation++
			}
			send := func(computer.IO) error {
				cpu.state.Generation++
				return nil
			}
			before := cpu.calls
			if err := s.port.WithFirmwareDataBatch(send); err == nil || !s.port.retired {
				t.Fatal("changed ownership was accepted", err)
			}
			if stage == "before" && (issued != 0 || cpu.calls-before != 1) ||
				stage == "after" && (issued != 1 || cpu.calls-before != 2) {
				t.Fatal("ownership did not bracket the batch", issued, cpu.calls-before)
			}
			before = cpu.calls
			if err := s.port.WithFirmwareDataBatch(send); err == nil || cpu.calls != before {
				t.Fatal("retired batch performed IO again", err)
			}
		})
	}
}
