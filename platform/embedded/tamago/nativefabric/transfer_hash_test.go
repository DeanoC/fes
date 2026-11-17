// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"fmt"
	"testing"
)

type transferHashCaller func([7]uint64) ([4]uint64, error)

func (f transferHashCaller) Invoke(r [7]uint64) ([4]uint64, error) { return f(r) }

// A full transfer spans multiple scratch buffers and several simultaneous DMA
// chunks. Mutations on either side of their boundaries must retire it at the
// same protocol points as a direct complete hash, without releasing memory.
func TestConfigurationTransferFreshHashesAcrossScratchAndDMABoundaries(t *testing.T) {
	const chunk = 64 << 10
	const size = 5*chunk + 19
	for _, where := range []string{"source", "arena"} {
		for _, phase := range []string{"start", "boot", "write", "pending", "after-write", "after-completion", "done", "after-done", "retire"} {
			for _, offset := range []int{0, (32 << 10) - 1, 32 << 10, chunk - 1} {
				t.Run(fmt.Sprintf("%s/%s/%d", where, phase, offset), func(t *testing.T) {
					w, transfer := windowFixture(t, chunk, size)
					mutated := false
					mutate := func() {
						at := offset
						if phase == "pending" || phase == "after-completion" {
							// Include the fourth pending chunk, which an initial
							// three-address completion does not yet retire.
							at += 3 * chunk
						}
						if phase == "done" || phase == "after-done" || phase == "retire" {
							// The final partial scratch chunk is covered by every
							// whole-image check, including after all DMA acks.
							at = size - 1
						}
						if where == "source" {
							w.source[at] ^= 1
						} else {
							w.region.Bytes[at] ^= 1
						}
						mutated = true
					}
					if phase == "boot" {
						transfer.beforeStart = func() error { mutate(); return nil }
					}
					if phase == "after-write" || phase == "after-completion" || phase == "after-done" {
						transfer.cpu = transferHashCaller(func(r [7]uint64) ([4]uint64, error) {
							result, err := w.Invoke(r)
							if !mutated && ((phase == "after-write" && r[0] == write) ||
								(phase == "after-completion" && r[0] == completed) ||
								(phase == "after-done" && r[0] == isdone)) {
								mutate()
							}
							return result, err
						})
					}
					for step := 0; step < 40; step++ {
						status := transfer.Status()
						if !mutated && (phase == status.Phase || (phase == "pending" && status.Phase == "completion")) {
							mutate()
						}
						calls := w.calls
						done, _, err := transfer.Advance()
						if done {
							t.Fatal("mutation supplied successful completion")
						}
						if err == nil {
							continue
						}
						status = transfer.Status()
						if !mutated || !status.Failed || status.RetainedBytes != size || w.releases != 0 {
							t.Fatal("corruption lost reservation", status, err)
						}
						if phase != "after-write" && phase != "after-completion" && phase != "after-done" && w.calls != calls {
							t.Fatal("known mutation reached firmware")
						}
						calls = w.calls
						if _, _, again := transfer.Advance(); again != err || w.calls != calls || w.releases != 0 {
							t.Fatal("retired transfer replayed or released")
						}
						return
					}
					t.Fatal("mutation was not observed")
				})
			}
		}
	}
}
