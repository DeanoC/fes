// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
)

// ConfigurationTransfer runs only the contained firmware transfer protocol.
// Its caller supplies qualified firmware/bridge hooks, a reserved coherent DMA
// region and a monotonic deadline. It grants no boot, identity or activation
// authority and never opens a bridge. Keep it and its region until reboot on
// any uncertain failure, including a constructor failure after reservation.
type ConfigurationTransfer struct {
	cpu         FirmwareCaller
	region      *Region
	source      []byte
	beforeStart func() error
	phase       string
	digest      [32]byte
	chunks      [][32]byte
	chunk       int
	dramBase    uint64
	offset      int
	accepted    int
	pending     []configurationBuffer
	err         error
	done        bool

	diagnosticAttempted bool
	binding             *configurationBinding
}

// Only the compiled-source constructor can bind completion to computer access.
// A generic transfer, a copied descriptor or a source observation is insufficient.
type configurationBinding struct {
	verifier *HPSContextVerifier
	source   HPSBootObservation
	rearm    bool
}

// The pinned SiP engine has four buffer descriptors. BUSY accepts the latest
// WRITE and requires polling; it never permits replay of that buffer.
const configurationBuffers = 4

type configurationBuffer struct{ offset, amount int }

type ConfigurationTransferStatus struct {
	Phase                         string
	AcceptedBytes, CompletedBytes int
	Outstanding, RetainedBytes    int
	Done, Failed                  bool
}

// NewConfigurationTransfer takes the already reserved bytes; it cannot reserve
// memory, infer a DRAM mapping or authorize a platform. Source and Region.Bytes
// remain immutable through all asynchronous reads. The owner must not advance
// after its deadline and must check again after each synchronous call returns.
func NewConfigurationTransfer(cpu FirmwareCaller, source []byte, region *Region, dramBase uint64, chunk int, beforeStart func() error) (*ConfigurationTransfer, error) {
	if cpu == nil || len(source) == 0 || dramBase == 0 || chunk <= 0 || chunk > 0xffff0 ||
		region == nil || region.Release == nil || len(region.Bytes) != len(source) || !bytes.Equal(region.Bytes, source) ||
		region.Address%64 != 0 || region.Address <= dramBase || region.Address-dramBase > math.MaxUint32 ||
		uint64(len(source))-1 > math.MaxUint32-(region.Address-dramBase) ||
		uint64(len(source))-1 > math.MaxUint64-region.Address {
		return nil, errors.New("native fabric: invalid reserved configuration transfer")
	}
	t := &ConfigurationTransfer{cpu: cpu, region: region, source: source, beforeStart: beforeStart,
		phase: "start", digest: sha256.Sum256(source), chunk: chunk, dramBase: dramBase}
	for offset := 0; offset < len(source); offset += chunk {
		t.chunks = append(t.chunks, sha256.Sum256(source[offset:min(offset+chunk, len(source))]))
	}
	return t, nil
}

func (t *ConfigurationTransfer) Status() ConfigurationTransferStatus {
	if t == nil {
		return ConfigurationTransferStatus{Failed: true}
	}
	retained := 0
	if t.region != nil {
		retained = len(t.region.Bytes)
	}
	return ConfigurationTransferStatus{t.phase, t.accepted, t.offset, len(t.pending), retained, t.done, t.err != nil}
}

// Cancel permanently retires a pending transfer without replay or release. A
// late firmware response cannot revive it. The lifecycle owner retains it.
func (t *ConfigurationTransfer) Cancel(reason error) error {
	if t == nil || reason == nil {
		return errors.New("native fabric: configuration cancellation requires a reason")
	}
	if t.err != nil {
		return t.err
	}
	if t.done {
		return errors.New("native fabric: configuration already completed")
	}
	t.err = reason
	return reason
}

func (t *ConfigurationTransfer) stable() bool {
	return t.region != nil && sha256.Sum256(t.region.Bytes) == t.digest && sha256.Sum256(t.source) == t.digest
}

func (t *ConfigurationTransfer) stableChunk(b configurationBuffer) bool {
	if t.region == nil || b.offset < 0 || b.amount <= 0 || b.offset+b.amount > len(t.region.Bytes) || b.offset+b.amount > len(t.source) {
		return false
	}
	index := b.offset / t.chunk
	return index < len(t.chunks) && sha256.Sum256(t.region.Bytes[b.offset:b.offset+b.amount]) == t.chunks[index] &&
		sha256.Sum256(t.source[b.offset:b.offset+b.amount]) == t.chunks[index]
}

func (t *ConfigurationTransfer) stablePending() bool {
	for _, b := range t.pending {
		if !t.stableChunk(b) {
			return false
		}
	}
	return true
}

// Advance performs at most one firmware operation. busy asks the cooperative
// owner to yield before polling again. A completed transfer has consumed all
// DMA completions, checked final bytes and successfully released its region.
func (t *ConfigurationTransfer) Advance() (done, busy bool, err error) {
	if t == nil {
		return false, false, errors.New("native fabric: configuration transfer missing")
	}
	if t.err != nil {
		return false, false, t.err
	}
	if t.done {
		return true, false, nil
	}
	fail := func(err error) (bool, bool, error) { t.err = err; return false, false, err }
	switch t.phase {
	case "start":
		if !t.stable() {
			return fail(errors.New("native fabric: staged image changed"))
		}
		if t.beforeStart != nil {
			if err := t.beforeStart(); err != nil {
				return fail(err)
			}
		}
		if !t.stable() {
			return fail(errors.New("native fabric: staged image changed during boot verification"))
		}
		r, err := t.cpu.Invoke([7]uint64{start})
		if err != nil {
			return fail(err)
		}
		if r != ([4]uint64{}) {
			return fail(fmt.Errorf("native fabric: START returned %v", r))
		}
		t.phase = "write"
	case "write":
		if t.accepted == len(t.source) || len(t.pending) == configurationBuffers {
			t.phase = "completion"
			break
		}
		b := configurationBuffer{t.accepted, min(t.chunk, len(t.source)-t.accepted)}
		if !t.stablePending() || !t.stableChunk(b) {
			return fail(errors.New("native fabric: staged image changed"))
		}
		// Keep possible DMA ownership even if this call fails or returns an
		// uncertain result. Only explicit completions remove pending ranges.
		t.pending = append(t.pending, b)
		r, err := t.cpu.Invoke([7]uint64{write, t.region.Address + uint64(b.offset), uint64(b.amount)})
		if err != nil {
			return fail(err)
		}
		if !t.stablePending() {
			return fail(errors.New("native fabric: WRITE changed issued image"))
		}
		if (r[0] != 0 && r[0] != 1) || !plain(r) {
			return fail(fmt.Errorf("native fabric: WRITE returned %v", r))
		}
		t.accepted += b.amount
		if r[0] == 1 || len(t.pending) == configurationBuffers || t.accepted == len(t.source) {
			t.phase = "completion"
		}
	case "completion":
		if len(t.pending) == 0 || !t.stablePending() {
			return fail(errors.New("native fabric: asynchronous image mutation"))
		}
		r, err := t.cpu.Invoke([7]uint64{completed})
		if err != nil {
			return fail(err)
		}
		if !t.stablePending() {
			return fail(errors.New("native fabric: completion changed issued image"))
		}
		if r[0] == 1 && plain(r) {
			return false, true, nil
		}
		count := 0
		for i := 1; i < len(r) && r[i] != 0; i++ {
			if count >= len(t.pending) || r[i] != t.region.Address+uint64(t.pending[count].offset)-t.dramBase {
				return fail(fmt.Errorf("native fabric: unexpected completion %v", r))
			}
			count++
		}
		if r[0] != 0 || count == 0 {
			return fail(fmt.Errorf("native fabric: unexpected completion %v", r))
		}
		for i := count + 1; i < len(r); i++ {
			if r[i] != 0 {
				return fail(fmt.Errorf("native fabric: completion gap %v", r))
			}
		}
		for _, b := range t.pending[:count] {
			t.offset += b.amount
		}
		t.pending = t.pending[count:]
		if t.accepted < len(t.source) {
			t.phase = "write"
		} else if len(t.pending) == 0 {
			t.phase = "done"
		}
	case "done":
		if !t.stable() {
			return fail(errors.New("native fabric: staged image changed"))
		}
		r, err := t.cpu.Invoke([7]uint64{isdone})
		if err != nil {
			return fail(err)
		}
		if r[0] == 1 && plain(r) {
			return false, true, nil
		}
		if r != ([4]uint64{}) {
			return fail(fmt.Errorf("native fabric: ISDONE returned %v", r))
		}
		if !t.stable() {
			return fail(errors.New("native fabric: configured image mutation"))
		}
		t.phase = "retire"
	case "retire":
		if !t.stable() {
			return fail(errors.New("native fabric: configured image changed before release"))
		}
		if err := t.region.Release(); err != nil {
			return fail(err)
		}
		t.region = nil
		t.chunks = nil
		t.done = true
		t.phase = "complete"
		return true, false, nil
	default:
		return fail(errors.New("native fabric: invalid configuration phase"))
	}
	return false, false, nil
}
