// SPDX-License-Identifier: GPL-3.0-or-later

package block

import (
	"errors"
	"sync/atomic"
)

// ErrNilDevice is returned when NewMetrics or a nil Metrics wrapper lacks an
// underlying block.Device.
var ErrNilDevice = errors.New("block: nil underlying device")

// MetricsStats summarizes cumulative block I/O calls, transferred sectors, and
// failed invocations recorded by a Metrics wrapper.
//
// ReadOps, WriteOps, and FlushOps increment on every call; ReadErrors,
// WriteErrors, and FlushErrors increment on failed calls; SectorsRead and
// SectorsWritten accumulate sectors from successful transfers.
type MetricsStats struct {
	ReadOps        uint64
	WriteOps       uint64
	FlushOps       uint64
	SectorsRead    uint64
	SectorsWritten uint64
	ReadErrors     uint64
	WriteErrors    uint64
	FlushErrors    uint64
}

// Metrics wraps a block.Device to record lock-free, zero-allocation I/O
// operation, sector, and error counters across ReadSectors, WriteSectors, and
// Flush.
type Metrics struct {
	dev            Device
	readOps        atomic.Uint64
	writeOps       atomic.Uint64
	flushOps       atomic.Uint64
	sectorsRead    atomic.Uint64
	sectorsWritten atomic.Uint64
	readErrors     atomic.Uint64
	writeErrors    atomic.Uint64
	flushErrors    atomic.Uint64
}

// NewMetrics constructs a Metrics wrapper forwarding block operations to dev.
func NewMetrics(dev Device) (*Metrics, error) {
	if dev == nil {
		return nil, ErrNilDevice
	}
	return &Metrics{dev: dev}, nil
}

// Underlying returns the wrapped Device.
func (m *Metrics) Underlying() Device {
	if m == nil {
		return nil
	}
	return m.dev
}

// SectorCount reports the sector capacity of the underlying device (0 if nil).
func (m *Metrics) SectorCount() uint64 {
	if m == nil || m.dev == nil {
		return 0
	}
	return m.dev.SectorCount()
}

// ReadSectors forwards the read to the underlying device and records operation,
// sector, and error telemetry.
func (m *Metrics) ReadSectors(lba uint64, dst []byte) error {
	if m == nil || m.dev == nil {
		return ErrNilDevice
	}
	m.readOps.Add(1)
	if err := m.dev.ReadSectors(lba, dst); err != nil {
		m.readErrors.Add(1)
		return err
	}
	if sectors := uint64(len(dst) / SectorSize); sectors > 0 {
		m.sectorsRead.Add(sectors)
	}
	return nil
}

// WriteSectors forwards the write to the underlying device and records
// operation, sector, and error telemetry.
func (m *Metrics) WriteSectors(lba uint64, src []byte) error {
	if m == nil || m.dev == nil {
		return ErrNilDevice
	}
	m.writeOps.Add(1)
	if err := m.dev.WriteSectors(lba, src); err != nil {
		m.writeErrors.Add(1)
		return err
	}
	if sectors := uint64(len(src) / SectorSize); sectors > 0 {
		m.sectorsWritten.Add(sectors)
	}
	return nil
}

// Flush forwards the flush to the underlying device and records operation and
// error telemetry.
func (m *Metrics) Flush() error {
	if m == nil || m.dev == nil {
		return ErrNilDevice
	}
	m.flushOps.Add(1)
	if err := m.dev.Flush(); err != nil {
		m.flushErrors.Add(1)
		return err
	}
	return nil
}

// Stats returns a point-in-time snapshot of all recorded block I/O metrics.
func (m *Metrics) Stats() MetricsStats {
	if m == nil {
		return MetricsStats{}
	}
	return MetricsStats{
		ReadOps:        m.readOps.Load(),
		WriteOps:       m.writeOps.Load(),
		FlushOps:       m.flushOps.Load(),
		SectorsRead:    m.sectorsRead.Load(),
		SectorsWritten: m.sectorsWritten.Load(),
		ReadErrors:     m.readErrors.Load(),
		WriteErrors:    m.writeErrors.Load(),
		FlushErrors:    m.flushErrors.Load(),
	}
}

// ResetStats zeroes all counters on m.
func (m *Metrics) ResetStats() {
	if m == nil {
		return
	}
	m.readOps.Store(0)
	m.writeOps.Store(0)
	m.flushOps.Store(0)
	m.sectorsRead.Store(0)
	m.sectorsWritten.Store(0)
	m.readErrors.Store(0)
	m.writeErrors.Store(0)
	m.flushErrors.Store(0)
}
