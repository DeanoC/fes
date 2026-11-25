// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "sync/atomic"

// LatencyStats summarizes zero-allocation nanosecond timing samples from DWC2
// interrupt wakeup to completed HID report delivery.
type LatencyStats struct {
	Samples uint64
	MinNS   int64
	MaxNS   int64
	TotalNS int64
	LastNS  int64
}

// AvgNS returns the mean latency in nanoseconds, or 0 when no samples exist.
func (s LatencyStats) AvgNS() int64 {
	if s.Samples == 0 {
		return 0
	}
	return s.TotalNS / int64(s.Samples)
}

type latencyTracker struct {
	samples atomic.Uint64
	minNS   atomic.Int64
	maxNS   atomic.Int64
	totalNS atomic.Int64
	lastNS  atomic.Int64
}

func (l *latencyTracker) record(ns int64) {
	if ns < 0 {
		return
	}
	l.lastNS.Store(ns)
	l.totalNS.Add(ns)
	if l.samples.Add(1) == 1 {
		l.minNS.Store(ns)
		l.maxNS.Store(ns)
		return
	}
	for {
		cur := l.minNS.Load()
		if ns >= cur || l.minNS.CompareAndSwap(cur, ns) {
			break
		}
	}
	for {
		cur := l.maxNS.Load()
		if ns <= cur || l.maxNS.CompareAndSwap(cur, ns) {
			break
		}
	}
}

func (l *latencyTracker) snapshot() LatencyStats {
	n := l.samples.Load()
	if n == 0 {
		return LatencyStats{}
	}
	return LatencyStats{
		Samples: n,
		MinNS:   l.minNS.Load(),
		MaxNS:   l.maxNS.Load(),
		TotalNS: l.totalNS.Load(),
		LastNS:  l.lastNS.Load(),
	}
}

func (l *latencyTracker) reset() {
	l.samples.Store(0)
	l.minNS.Store(0)
	l.maxNS.Store(0)
	l.totalNS.Store(0)
	l.lastNS.Store(0)
}

// Latency returns recorded IRQ-to-report delivery latency statistics.
func (h *Host) Latency() LatencyStats {
	if h == nil {
		return LatencyStats{}
	}
	return h.latency.snapshot()
}

// ResetLatency clears recorded IRQ-to-report latency statistics.
func (h *Host) ResetLatency() {
	if h == nil {
		return
	}
	h.latency.reset()
}

// Latency returns recorded IRQ-to-report delivery latency statistics.
func (g *GamepadHost) Latency() LatencyStats {
	if g == nil || g.host == nil {
		return LatencyStats{}
	}
	return g.host.Latency()
}

// ResetLatency clears recorded IRQ-to-report latency statistics.
func (g *GamepadHost) ResetLatency() {
	if g == nil || g.host == nil {
		return
	}
	g.host.ResetLatency()
}
