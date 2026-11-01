// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

type transferWire struct {
	t          *testing.T
	region     *Region
	source     []byte
	calls      [][7]uint64
	received   []byte
	pending    uint64
	releases   int
	pollBusy   bool
	statusBusy bool
	fault      string
}

func transferFixture(t *testing.T) (*transferWire, *ConfigurationTransfer) {
	t.Helper()
	w := &transferWire{t: t, source: bytes.Repeat([]byte{0x5a}, 130)}
	w.region = &Region{Address: 0x94800000, Bytes: bytes.Clone(w.source)}
	w.region.Release = func() error {
		w.releases++
		if w.fault == "release" {
			return errors.New("unresolved release")
		}
		return nil
	}
	transfer, err := NewConfigurationTransfer(w, w.source, w.region, 0x80000000, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	return w, transfer
}

func (w *transferWire) Invoke(r [7]uint64) ([4]uint64, error) {
	w.calls = append(w.calls, r)
	if w.fault == "transport" {
		return [4]uint64{}, errors.New("unknown firmware outcome")
	}
	switch r[0] {
	case start:
		if r != ([7]uint64{start}) || len(w.received) != 0 || w.pending != 0 {
			w.t.Fatal("invalid/replayed START", r)
		}
		if w.fault == "start" {
			return [4]uint64{4}, nil
		}
		return [4]uint64{}, nil
	case write:
		if w.pending != 0 || r[1] != w.region.Address+uint64(len(w.received)) || r[2] == 0 || r[2] > 64 || !plain([4]uint64{0, r[3], r[4], r[5]}) || r[6] != 0 {
			w.t.Fatal("overlap or replay of a submitted buffer", r)
		}
		w.pending = r[1] - 0x80000000
		end := len(w.received) + int(r[2])
		if end > len(w.source) {
			w.t.Fatal("write outside reservation")
		}
		w.received = append(w.received, w.region.Bytes[len(w.received):end]...)
		w.pollBusy = false
		if w.fault == "write" {
			return [4]uint64{2}, nil
		}
		if w.fault == "write-mutation" {
			w.region.Bytes[len(w.received)-1] ^= 1
		}
		return [4]uint64{1}, nil // Accepted BUSY, not a rejected write.
	case completed:
		if r != ([7]uint64{completed}) || w.pending == 0 {
			w.t.Fatal("completion without an issued buffer", r)
		}
		switch w.fault {
		case "cpu-address":
			return [4]uint64{0, w.pending + 0x80000000}, nil
		case "foreign":
			return [4]uint64{0, w.pending + 64}, nil
		case "duplicate":
			return [4]uint64{0, w.pending, w.pending}, nil
		case "missing":
			return [4]uint64{}, nil
		case "busy-address":
			return [4]uint64{1, w.pending}, nil
		case "completion-mutation":
			w.region.Bytes[0] ^= 1
		case "source-mutation":
			w.source[0] ^= 1
		}
		if !w.pollBusy {
			w.pollBusy = true
			return [4]uint64{1}, nil
		}
		address := w.pending
		w.pending = 0
		return [4]uint64{0, address}, nil
	case isdone:
		if w.pending != 0 || !bytes.Equal(w.received, w.source) || r != ([7]uint64{isdone}) {
			w.t.Fatal("ISDONE before all exact bytes completed")
		}
		if w.fault == "done" {
			return [4]uint64{4}, nil
		}
		if w.fault == "done-mutation" {
			w.region.Bytes[0] ^= 1
			return [4]uint64{}, nil
		}
		if !w.statusBusy {
			w.statusBusy = true
			return [4]uint64{1}, nil
		}
		return [4]uint64{}, nil
	default:
		w.t.Fatal("unexpected firmware operation", r)
		return [4]uint64{}, nil
	}
}

func TestConfigurationTransferAcceptedBusyAndExactRelativeCompletions(t *testing.T) {
	w, transfer := transferFixture(t)
	busyCount := 0
	for steps := 0; steps < 40; steps++ {
		done, busy, err := transfer.Advance()
		if err != nil {
			t.Fatal(err)
		}
		if busy {
			busyCount++
		}
		if !done {
			continue
		}
		s := transfer.Status()
		if !s.Done || s.Failed || s.AcceptedBytes != len(w.source) || s.CompletedBytes != len(w.source) || s.Outstanding != 0 || s.RetainedBytes != 0 || busyCount != 4 || w.releases != 1 || !bytes.Equal(w.received, w.source) {
			t.Fatal("incomplete or replayed transfer", s, busyCount, w.releases)
		}
		writes := 0
		for _, call := range w.calls {
			if call[0] == write {
				writes++
			}
		}
		if writes != 3 {
			t.Fatal("accepted BUSY was replayed", writes)
		}
		calls := len(w.calls)
		if done, busy, err = transfer.Advance(); !done || busy || err != nil || len(w.calls) != calls || w.releases != 1 {
			t.Fatal("completed transfer replayed or released again")
		}
		return
	}
	t.Fatal("transfer did not complete")
}

func TestConfigurationTransferFailuresRetainAndNeverReplay(t *testing.T) {
	for _, fault := range []string{"transport", "start", "write", "write-mutation", "cpu-address", "foreign", "duplicate", "missing", "busy-address", "completion-mutation", "source-mutation", "done", "done-mutation", "release"} {
		t.Run(fault, func(t *testing.T) {
			w, transfer := transferFixture(t)
			w.fault = fault
			for steps := 0; steps < 40; steps++ {
				done, _, err := transfer.Advance()
				if done {
					t.Fatal("failed transfer completed")
				}
				if err == nil {
					continue
				}
				s := transfer.Status()
				if !s.Failed || s.Done || s.RetainedBytes != len(w.source) || transfer.region != w.region {
					t.Fatal("failure lost reservation", s)
				}
				if fault == "write" && s.Outstanding != 1 {
					t.Fatal("ambiguous write lost possible DMA ownership", s)
				}
				calls, releases := len(w.calls), w.releases
				if _, _, late := transfer.Advance(); late != err || len(w.calls) != calls || w.releases != releases {
					t.Fatal("failed transfer revived")
				}
				if fault != "release" && releases != 0 {
					t.Fatal("failed transfer released memory")
				}
				return
			}
			t.Fatal("fault did not retire transfer")
		})
	}
}

func TestConfigurationTransferBootGateMutationAndCancellation(t *testing.T) {
	for _, kind := range []string{"rejected", "mutated", "cancelled", "retire-mutation"} {
		t.Run(kind, func(t *testing.T) {
			w, transfer := transferFixture(t)
			if kind == "rejected" {
				transfer.beforeStart = func() error { return errors.New("boot owner changed") }
			} else if kind == "mutated" {
				transfer.beforeStart = func() error { w.region.Bytes[0] ^= 1; return nil }
			} else if kind == "cancelled" {
				for i := 0; i < 2; i++ {
					if _, _, err := transfer.Advance(); err != nil {
						t.Fatal(err)
					}
				}
				if transfer.Status().Outstanding != 1 {
					t.Fatal("pending DMA not tracked")
				}
				_ = transfer.Cancel(errors.New("owner deadline expired"))
			} else {
				for i := 0; transfer.Status().Phase != "retire" && i < 40; i++ {
					if _, _, err := transfer.Advance(); err != nil {
						t.Fatal(err)
					}
				}
				w.region.Bytes[0] ^= 1
			}
			calls := len(w.calls)
			if _, _, err := transfer.Advance(); err == nil || !transfer.Status().Failed || w.releases != 0 || len(w.calls) != calls {
				t.Fatal("gate/cancellation/late mutation touched firmware or lost memory")
			}
		})
	}
}

func TestConfigurationTransferRejectsInvalidReservations(t *testing.T) {
	for _, kind := range []string{"cpu", "empty", "base", "chunk", "large-chunk", "region", "release", "length", "bytes", "alignment", "below-dram", "relative-overflow", "range-overflow"} {
		t.Run(kind, func(t *testing.T) {
			w, _ := transferFixture(t)
			var cpu FirmwareCaller = w
			source, region, base, chunk := w.source, w.region, uint64(0x80000000), 64
			switch kind {
			case "cpu":
				cpu = nil
			case "empty":
				source = nil
			case "base":
				base = 0
			case "chunk":
				chunk = 0
			case "large-chunk":
				chunk = 0xffff1
			case "region":
				region = nil
			case "release":
				region.Release = nil
			case "length":
				region.Bytes = region.Bytes[:1]
			case "bytes":
				region.Bytes[0] ^= 1
			case "alignment":
				region.Address++
			case "below-dram":
				region.Address = base
			case "relative-overflow":
				region.Address = base + math.MaxUint32 + 1
			case "range-overflow":
				region.Address = base + math.MaxUint32 - 63
			}
			if transfer, err := NewConfigurationTransfer(cpu, source, region, base, chunk, nil); err == nil || transfer != nil || len(w.calls) != 0 || w.releases != 0 {
				t.Fatal("invalid reservation accepted or consumed")
			}
		})
	}
}
