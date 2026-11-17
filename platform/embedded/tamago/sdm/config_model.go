// SPDX-License-Identifier: GPL-3.0-or-later
package sdm

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"time"
)

type ConfigFirmware string

const (
	ReferenceTFAV212 ConfigFirmware = "reference-tfa-v2.12"
	// HeldBridgeModel is a hypothetical software endpoint, not a firmware pin,
	// board capability or permission to invoke configuration services.
	HeldBridgeModel ConfigFirmware = "model-held-bridges"
)

var (
	ErrUnsafeFirmware = errors.New("SDM model: firmware cannot preserve containment and DMA mapping")
	ErrConfigStale    = errors.New("SDM model: stale configuration completion")
	ErrConfigDeadline = errors.New("SDM model: configuration deadline expired")
)

type ConfigOperation string

const (
	StartConfig    ConfigOperation = "start"
	WriteConfig    ConfigOperation = "write"
	PollWrites     ConfigOperation = "completed_write"
	PollConfigured ConfigOperation = "isdone"
)

type ConfigToken struct {
	owner    *ConfigModel
	sequence uint64
}
type ConfigCommand struct {
	Token     ConfigToken
	Operation ConfigOperation
	Registers [7]uint64
	// Bytes represents a reserved buffer at the synthetic wire address. It is
	// retained through asynchronous completion and checked for mutation.
	Bytes []byte
}
type ConfigReply struct {
	Registers [4]uint64
	Err       error
}
type ConfigStatus struct {
	Done, Failed                                              bool
	AcceptedBytes, CompletedBytes, Outstanding, RetainedBytes int
}
type configBuffer struct {
	command *ConfigCommand
	digest  [32]byte
}

// ConfigModel models TF-A's dedicated configuration protocol below the single
// fabric lifecycle. It has no Invoke, MMIO, bridge-release or identity method.
// It must never be connected to the board's SMC entry. Reference TF-A is
// rejected before issuing START; only the hypothetical held-bridge model runs.
type ConfigModel struct {
	image               []byte
	base                uint64
	chunk               int
	lastTime, deadline  time.Duration
	sequence            uint64
	phase               ConfigOperation
	issued              *ConfigCommand
	held                []*ConfigCommand
	pending             map[uint64]configBuffer
	accepted, completed int
	pause, done         bool
	err                 error
}

func NewConfigModel(firmware ConfigFirmware, image []byte, base uint64, chunk int, now, deadline time.Duration) (*ConfigModel, error) {
	if firmware != HeldBridgeModel {
		return nil, ErrUnsafeFirmware
	}
	if len(image) == 0 || len(image) > 8<<20 || chunk < 1 || chunk > 0xffff0 || base < 0x82000000 || base%64 != 0 || base > 0xc0000000-uint64(len(image)) || now < 0 || deadline <= now {
		return nil, errors.New("SDM model: invalid image, synthetic DDR range, chunk or deadline")
	}
	return &ConfigModel{image: bytes.Clone(image), base: base, chunk: chunk, lastTime: now, deadline: deadline, phase: StartConfig, pending: make(map[uint64]configBuffer)}, nil
}

func (m *ConfigModel) Status() ConfigStatus {
	if m == nil {
		return ConfigStatus{Failed: true}
	}
	s := ConfigStatus{Done: m.done, Failed: m.err != nil, AcceptedBytes: m.accepted, CompletedBytes: m.completed, Outstanding: len(m.pending), RetainedBytes: len(m.image)}
	for _, c := range m.held {
		s.RetainedBytes += len(c.Bytes)
	}
	return s
}
func (m *ConfigModel) fail(err error) error { m.err = err; return err }
func (m *ConfigModel) clock(now time.Duration) error {
	if now < m.lastTime {
		return m.fail(errors.New("SDM model: monotonic clock regressed"))
	}
	m.lastTime = now
	if now >= m.deadline {
		return m.fail(ErrConfigDeadline)
	}
	return nil
}

// Next issues once; polling while a command is outstanding cannot replay it.
// Busy polling remains caller-driven and bounded by the transition deadline.
func (m *ConfigModel) Next(now time.Duration) (*ConfigCommand, error) {
	if m == nil || len(m.image) == 0 {
		return nil, errors.New("SDM model: uninitialized transfer")
	}
	if m.err != nil {
		return nil, m.err
	}
	if m.done {
		return nil, nil
	}
	if err := m.clock(now); err != nil {
		return nil, err
	}
	if m.issued != nil {
		return nil, nil
	}
	if m.sequence == math.MaxUint64 {
		return nil, m.fail(errors.New("SDM model: sequence exhausted"))
	}
	m.sequence++
	c := &ConfigCommand{Token: ConfigToken{m, m.sequence}, Operation: m.phase}
	switch m.phase {
	case StartConfig:
		// Full configuration: do not disguise it as partial/authentication to
		// bypass the reference firmware's automatic bridge release.
		c.Registers[0] = 0xc2000001
	case WriteConfig:
		if m.accepted == len(m.image) && len(m.pending) == 0 {
			c.Operation = PollConfigured
			c.Registers[0] = 0xc2000004
		} else if m.pause || len(m.pending) == 4 || m.accepted == len(m.image) {
			c.Operation = PollWrites
			c.Registers[0] = 0xc2000003
		} else {
			n := min(m.chunk, len(m.image)-m.accepted)
			c.Registers = [7]uint64{0x42000002, m.base + uint64(m.accepted), uint64(n)}
			c.Bytes = bytes.Clone(m.image[m.accepted : m.accepted+n])
			m.held = append(m.held, c)
		}
	default:
		return nil, m.fail(errors.New("SDM model: invalid phase"))
	}
	m.issued = c
	out := *c
	return &out, nil
}

func (m *ConfigModel) Complete(token ConfigToken, reply ConfigReply, now time.Duration) error {
	if m == nil || m.err != nil || m.issued == nil || token != m.issued.Token {
		return ErrConfigStale
	}
	if err := m.clock(now); err != nil {
		return err
	}
	if reply.Err != nil {
		return m.fail(reply.Err)
	}
	c, r := m.issued, reply.Registers
	status := r[0]
	if c.Operation != PollWrites && (r[1] != 0 || r[2] != 0 || r[3] != 0) {
		return m.fail(errors.New("SDM model: unexpected response registers"))
	}
	switch c.Operation {
	case StartConfig:
		if status != 0 {
			return m.fail(errors.New("SDM model: START failed; state uncertain"))
		}
		m.phase = WriteConfig
	case WriteConfig:
		// Both OK and BUSY accept this buffer. REJECTED does not; no write
		// result is automatically replayed into an existing configuration.
		if status != 0 && status != 1 {
			return m.fail(errors.New("SDM model: WRITE rejected or uncertain"))
		}
		start := m.accepted
		if !bytes.Equal(c.Bytes, m.image[start:start+len(c.Bytes)]) {
			return m.fail(errors.New("SDM model: issued write buffer mutated"))
		}
		// The reviewed Agilex 5 WRITE stores mem-DRAM_BASE in TF-A's
		// bookkeeping, and COMPLETED_WRITE returns that SDM-relative value.
		// Keep the namespaces distinct; a CPU address is a foreign completion.
		m.pending[c.Registers[1]-0x80000000] = configBuffer{c, sha256.Sum256(c.Bytes)}
		m.accepted += len(c.Bytes)
		m.pause = status == 1
	case PollWrites:
		if status == 1 {
			if r[1] != 0 || r[2] != 0 || r[3] != 0 {
				return m.fail(errors.New("SDM model: BUSY carried completion addresses"))
			}
		} else if status == 0 {
			// Validate the complete batch before retiring any buffer. In
			// particular OK/zero is not proof that our outstanding writes drained.
			seen := make(map[uint64]bool)
			zero := false
			for _, addr := range r[1:] {
				if addr == 0 {
					zero = true
					continue
				}
				buffer, known := m.pending[addr]
				if zero || seen[addr] || !known {
					return m.fail(errors.New("SDM model: duplicate, foreign or noncontiguous completion"))
				}
				if sha256.Sum256(buffer.command.Bytes) != buffer.digest {
					return m.fail(errors.New("SDM model: asynchronous buffer mutated"))
				}
				seen[addr] = true
			}
			if len(seen) == 0 {
				return m.fail(errors.New("SDM model: completion addresses missing"))
			}
			for addr := range seen {
				m.completed += len(m.pending[addr].command.Bytes)
				delete(m.pending, addr)
			}
			m.pause = false
		} else {
			return m.fail(errors.New("SDM model: completion failed; ownership uncertain"))
		}
	case PollConfigured:
		if status == 0 {
			m.done = true
		} else if status != 1 {
			return m.fail(errors.New("SDM model: configuration failed"))
		}
	default:
		return m.fail(errors.New("SDM model: unknown command"))
	}
	m.issued = nil
	return nil
}
