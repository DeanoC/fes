// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"testing"
)

type windowWire struct {
	t                                                            *testing.T
	region                                                       *Region
	source                                                       []byte
	chunk, accepted, acked, maxPending, batches, calls, releases int
	pending                                                      []int
	fault                                                        string
	finished                                                     bool
}

func windowFixture(t *testing.T, chunk, size int) (*windowWire, *ConfigurationTransfer) {
	t.Helper()
	w := &windowWire{t: t, chunk: chunk, source: bytes.Repeat([]byte{0x5a}, size)}
	w.region = &Region{Address: 0x94800000, Bytes: bytes.Clone(w.source)}
	w.region.Release = func() error {
		if !w.finished || w.acked != len(w.source) || len(w.pending) != 0 {
			t.Fatal("release before complete consumption")
		}
		w.releases++
		return nil
	}
	transfer, err := NewConfigurationTransfer(w, w.source, w.region, 0x80000000, chunk, nil)
	if err != nil {
		t.Fatal(err)
	}
	return w, transfer
}

func (w *windowWire) Invoke(r [7]uint64) ([4]uint64, error) {
	w.calls++
	switch r[0] {
	case start:
		if r != ([7]uint64{start}) || w.accepted != 0 {
			w.t.Fatal("invalid START")
		}
		return [4]uint64{}, nil
	case write:
		amount := min(w.chunk, len(w.source)-w.accepted)
		if len(w.pending) >= 4 || r != ([7]uint64{write, w.region.Address + uint64(w.accepted), uint64(amount)}) {
			w.t.Fatal("replayed/out-of-window WRITE", r, w.pending)
		}
		w.pending = append(w.pending, w.accepted)
		w.accepted += amount
		w.maxPending = max(w.maxPending, len(w.pending))
		if w.fault == "write-mutation" && len(w.pending) == 2 {
			w.region.Bytes[w.pending[0]] ^= 1
		}
		if len(w.pending) == 4 {
			if w.fault == "write-error" {
				return [4]uint64{}, errors.New("uncertain fourth WRITE")
			}
			return [4]uint64{1}, nil // Accepted, queue full: poll, never resend.
		}
		return [4]uint64{}, nil
	case completed:
		if r != ([7]uint64{completed}) || len(w.pending) == 0 {
			w.t.Fatal("unissued completion")
		}
		// A peer can need later data before retiring its current buffer.
		// This reproduces the serial client's stop after twelve chunks.
		if w.acked >= 12*w.chunk && len(w.pending) < 2 && w.accepted < len(w.source) {
			return [4]uint64{1}, nil
		}
		n := min(3, len(w.pending))
		result := [4]uint64{}
		for i := 0; i < n; i++ {
			result[i+1] = w.region.Address + uint64(w.pending[i]) - 0x80000000
		}
		switch w.fault {
		case "foreign":
			result[1] += uint64(w.chunk)
		case "cpu-address":
			result[1] += 0x80000000
		case "duplicate":
			result[2] = result[1]
		case "gap":
			result[2] = 0
		case "busy-address":
			result[0] = 1
		case "missing":
			result = [4]uint64{}
		case "completion-mutation":
			w.region.Bytes[w.pending[n-1]] ^= 1
		}
		if w.fault != "" {
			return result, nil
		}
		for _, offset := range w.pending[:n] {
			w.acked += min(w.chunk, len(w.source)-offset)
		}
		w.pending = w.pending[n:]
		if n > 1 {
			w.batches++
		}
		return result, nil
	case isdone:
		if r != ([7]uint64{isdone}) || w.acked != len(w.source) || len(w.pending) != 0 {
			w.t.Fatal("ISDONE before all exact completions")
		}
		w.finished = true
		return [4]uint64{}, nil
	default:
		w.t.Fatal("unexpected firmware operation", r)
	}
	return [4]uint64{}, nil
}

func TestConfigurationTransferWindowBatchesAndLookahead(t *testing.T) {
	for _, size := range []int{20*64 + 4, 2027520} {
		chunk := 64
		if size == 2027520 {
			chunk = 65536
		}
		w, transfer := windowFixture(t, chunk, size)
		for steps := 0; steps < 1000; steps++ {
			calls := w.calls
			done, _, err := transfer.Advance()
			if err != nil || w.calls-calls > 1 {
				t.Fatal("window failed or issued multiple operations", err)
			}
			if !done {
				continue
			}
			s := transfer.Status()
			if w.maxPending != 4 || w.batches == 0 || w.releases != 1 || s.AcceptedBytes != size || s.CompletedBytes != size || s.Outstanding != 0 || s.RetainedBytes != 0 {
				t.Fatal("incomplete window", s, w)
			}
			break
		}
		if !transfer.Status().Done {
			t.Fatal("window could not supply the peer's next buffer", transfer.Status())
		}
	}
}

func TestConfigurationTransferWindowFailuresRetainEveryPossibleBuffer(t *testing.T) {
	for _, fault := range []string{"foreign", "cpu-address", "duplicate", "gap", "busy-address", "missing", "completion-mutation", "write-mutation", "write-error", "cancelled", "before-poll-mutation"} {
		t.Run(fault, func(t *testing.T) {
			w, transfer := windowFixture(t, 64, 20*64+4)
			w.fault = fault
			for steps := 0; steps < 20; steps++ {
				if transfer.Status().Phase == "completion" {
					if fault == "cancelled" {
						_ = transfer.Cancel(errors.New("deadline"))
					}
					if fault == "before-poll-mutation" {
						w.region.Bytes[65] ^= 1
					}
				}
				done, _, err := transfer.Advance()
				if done {
					t.Fatal("failed window completed")
				}
				if err == nil {
					continue
				}
				s := transfer.Status()
				want := 4
				if fault == "write-mutation" {
					want = 2
				}
				if s.Outstanding != want || s.CompletedBytes != 0 || s.RetainedBytes != len(w.source) || w.releases != 0 {
					t.Fatal("window ownership lost", s)
				}
				calls := w.calls
				if _, _, again := transfer.Advance(); again != err || calls != w.calls || w.releases != 0 {
					t.Fatal("retired window revived")
				}
				return
			}
			t.Fatal("window fault was ignored")
		})
	}
}
