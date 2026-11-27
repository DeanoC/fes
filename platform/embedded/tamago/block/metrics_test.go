// SPDX-License-Identifier: GPL-3.0-or-later

package block

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type metricsMockDevice struct {
	mu       sync.Mutex
	sectors  [][]byte
	readErr  error
	writeErr error
	flushErr error
}

func newMetricsMockDevice(count int) *metricsMockDevice {
	s := make([][]byte, count)
	for i := range s {
		s[i] = make([]byte, SectorSize)
	}
	return &metricsMockDevice{sectors: s}
}

func (d *metricsMockDevice) SectorCount() uint64 { return uint64(len(d.sectors)) }

func (d *metricsMockDevice) ReadSectors(lba uint64, dst []byte) error {
	if err := CheckRange(d.SectorCount(), lba, len(dst)); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readErr != nil {
		return d.readErr
	}
	for i := 0; i < len(dst)/SectorSize; i++ {
		copy(dst[i*SectorSize:(i+1)*SectorSize], d.sectors[int(lba)+i])
	}
	return nil
}

func (d *metricsMockDevice) WriteSectors(lba uint64, src []byte) error {
	if err := CheckRange(d.SectorCount(), lba, len(src)); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeErr != nil {
		return d.writeErr
	}
	for i := 0; i < len(src)/SectorSize; i++ {
		copy(d.sectors[int(lba)+i], src[i*SectorSize:(i+1)*SectorSize])
	}
	return nil
}

func (d *metricsMockDevice) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flushErr
}

func TestMetricsWrapperAccountingErrorsConcurrencyAndAllocs(t *testing.T) {
	if _, err := NewMetrics(nil); !errors.Is(err, ErrNilDevice) {
		t.Fatalf("NewMetrics(nil) err=%v want ErrNilDevice", err)
	}
	var nilMetrics *Metrics
	if nilMetrics.SectorCount() != 0 || nilMetrics.Underlying() != nil || nilMetrics.Stats() != (MetricsStats{}) {
		t.Fatal("nil Metrics returned non-zero state")
	}
	nilMetrics.ResetStats()
	if err := nilMetrics.ReadSectors(0, make([]byte, SectorSize)); !errors.Is(err, ErrNilDevice) {
		t.Fatalf("nil ReadSectors err=%v", err)
	}
	if err := nilMetrics.WriteSectors(0, make([]byte, SectorSize)); !errors.Is(err, ErrNilDevice) {
		t.Fatalf("nil WriteSectors err=%v", err)
	}
	if err := nilMetrics.Flush(); !errors.Is(err, ErrNilDevice) {
		t.Fatalf("nil Flush err=%v", err)
	}

	raw := newMetricsMockDevice(16)
	m, err := NewMetrics(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Underlying() != raw || m.SectorCount() != 16 {
		t.Fatalf("Underlying/SectorCount mismatch: dev=%v sectors=%d", m.Underlying(), m.SectorCount())
	}

	wbuf := make([]byte, 3*SectorSize)
	wbuf[0] = 0x5a
	rbuf := make([]byte, 2*SectorSize)
	if err := m.WriteSectors(1, wbuf); err != nil {
		t.Fatal(err)
	}
	if err := m.ReadSectors(1, rbuf); err != nil || rbuf[0] != 0x5a {
		t.Fatalf("ReadSectors err=%v rbuf[0]=%#x", err, rbuf[0])
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}

	// Inject errors and verify *Errors increment without adding sectors.
	boom := errors.New("io error")
	raw.mu.Lock()
	raw.readErr = boom
	raw.writeErr = boom
	raw.flushErr = boom
	raw.mu.Unlock()
	if err := m.ReadSectors(0, rbuf); !errors.Is(err, boom) {
		t.Fatalf("expected read error, got %v", err)
	}
	if err := m.WriteSectors(0, wbuf); !errors.Is(err, boom) {
		t.Fatalf("expected write error, got %v", err)
	}
	if err := m.Flush(); !errors.Is(err, boom) {
		t.Fatalf("expected flush error, got %v", err)
	}

	want := MetricsStats{
		ReadOps:        2,
		WriteOps:       2,
		FlushOps:       2,
		SectorsRead:    2,
		SectorsWritten: 3,
		ReadErrors:     1,
		WriteErrors:    1,
		FlushErrors:    1,
	}
	if got := m.Stats(); got != want {
		t.Fatalf("Stats=%+v want %+v", got, want)
	}

	m.ResetStats()
	if got := m.Stats(); got != (MetricsStats{}) {
		t.Fatalf("Stats after ResetStats=%+v want zero", got)
	}

	raw.mu.Lock()
	raw.readErr = nil
	raw.writeErr = nil
	raw.flushErr = nil
	raw.mu.Unlock()

	if allocs := testing.AllocsPerRun(20, func() {
		if err := m.WriteSectors(4, wbuf); err != nil {
			t.Fatal(err)
		}
		if err := m.ReadSectors(4, rbuf); err != nil {
			t.Fatal(err)
		}
		if err := m.Flush(); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("Metrics allocated %.1f times per op", allocs)
	}

	m.ResetStats()
	var wg sync.WaitGroup
	var expectedReads atomic.Uint64
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := make([]byte, SectorSize)
			for i := 0; i < 50; i++ {
				if err := m.ReadSectors(uint64(id), buf); err != nil {
					t.Errorf("concurrent ReadSectors: %v", err)
					return
				}
				expectedReads.Add(1)
				_ = m.Stats()
			}
		}(g)
	}
	wg.Wait()
	if st := m.Stats(); st.ReadOps != expectedReads.Load() || st.SectorsRead != expectedReads.Load() {
		t.Fatalf("concurrent stats=%+v want %d", st, expectedReads.Load())
	}
}
