// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// periodicController is a wire-clock oracle, not a copy of the driver's frame
// selector. HCCHAR parity selects this or the next frame; queued work consumes
// wire time and a transfer that cannot fit reports FRMOVRUN. Device/RX state is
// supplied by the existing independent descriptor/FIFO model only when its
// scheduled event occurs. No driver flags or desired wire frame enter this model.
// The optional toggle-disable event injects the captured observation; it does
// not explain why that observation occurred on the physical Nano.
type periodicController struct {
	*controller
	origin                                          time.Time
	elapsed, quantum, frameTime, wireTime, haltTime time.Duration
	channelDelay                                    time.Duration
	frameBase                                       uint16
	epoch                                           uint64
	events                                          []periodicEvent
	wireAvailable                                   time.Duration
	finished                                        bool
	toggles                                         int
	stalledHalt                                     bool
	queueReserved                                   int
	history                                         []periodicWireRecord
	writes                                          []periodicWrite
}
type periodicEvent struct {
	at    time.Duration
	epoch uint64
	kind  string
	tx    transaction
}
type periodicWireRecord struct {
	at    time.Duration
	frame uint16
	kind  string
	pid   uint32
}
type periodicWrite struct {
	at         time.Duration
	reg, value uint32
}

func newPeriodicController() *periodicController {
	c := newController()
	c.address, c.configured, c.protocol, c.reportPID = 1, true, true, 2
	c.regs[hprt] = 0x4180f
	c.regs[hfir] = 59999
	return &periodicController{controller: c, origin: c.tick, quantum: time.Microsecond,
		frameTime: time.Millisecond, wireTime: 100 * time.Microsecond, haltTime: 20 * time.Microsecond, frameBase: 0x372}
}
func (c *periodicController) frame() uint16 { return c.frameBase + uint16(c.elapsed/c.frameTime) }
func (c *periodicController) advance(delta time.Duration) {
	if delta < 0 {
		panic("model: clock moved backwards")
	}
	target := c.elapsed + delta
	for {
		selected := -1
		for i, e := range c.events {
			if e.at <= target && (selected < 0 || e.at < c.events[selected].at) {
				selected = i
			}
		}
		if selected < 0 {
			break
		}
		e := c.events[selected]
		c.events = append(c.events[:selected], c.events[selected+1:]...)
		c.elapsed = e.at
		c.tick = c.origin.Add(c.elapsed)
		if e.epoch != c.epoch {
			continue
		}
		if e.kind == "halt" {
			c.regs[hcchar] &^= channelEnable | channelDisable
			c.regs[hcint] |= 2
			c.events = nil
			c.wireAvailable = c.elapsed
			c.history = append(c.history, periodicWireRecord{c.elapsed, c.frame(), "halt", e.tx.pid})
			continue
		}
		if c.finished {
			continue
		}
		if e.kind == "overrun" {
			c.regs[hcint] |= 1 << 9
			c.regs[hcchar] |= channelEnable | channelDisable
			c.history = append(c.history, periodicWireRecord{c.elapsed, c.frame(), "overrun", e.tx.pid})
			continue
		}
		if c.toggles > 0 {
			c.toggles--
			c.regs[hcint] |= dataToggle
			c.regs[hcchar] |= channelEnable | channelDisable
			c.rx = append(c.rx, 5<<17)
			c.events = nil
			c.wireAvailable = c.elapsed
			c.history = append(c.history, periodicWireRecord{c.elapsed, c.frame(), "toggle", e.tx.pid})
			continue
		}
		c.active = e.tx
		empty := len(c.reports) == 0
		c.controller.receive()
		kind := "NAK"
		if !empty {
			kind = "data"
			c.finished = true
			c.events = nil
			c.regs[hctsiz] = c.reportPID << 29 // no bytes or packets remain after real data
		}
		c.history = append(c.history, periodicWireRecord{c.elapsed, c.frame(), kind, e.tx.pid})
	}
	c.elapsed = target
	c.tick = c.origin.Add(target)
}
func (c *periodicController) Now() time.Time { c.advance(c.quantum); return c.tick }
func (c *periodicController) Read32(reg uint32) uint32 {
	c.advance(c.quantum)
	if reg == hfnum {
		c.accesses++
		remaining := (c.frameTime - c.elapsed%c.frameTime) / time.Nanosecond * 60 / 1000
		if remaining > 0 {
			remaining--
		}
		return uint32(remaining)<<16 | uint32(c.frame())
	}
	if reg == ptxstatus {
		c.accesses++
		occupied := c.queueReserved
		for _, e := range c.events {
			if e.kind != "halt" {
				occupied++
			}
		}
		free := 16 - occupied
		if free < 0 {
			free = 0
		}
		return uint32(free)<<16 | 64
	}
	return c.controller.Read32(reg)
}
func (c *periodicController) Write32(reg, value uint32) {
	c.advance(c.quantum)
	if reg == hcchar {
		c.advance(c.channelDelay)
	}
	c.writes = append(c.writes, periodicWrite{c.elapsed, reg, value})
	if reg == hctsiz {
		if c.regs[hcchar]&channelEnable != 0 {
			panic("model: live transfer reinitialized")
		}
		c.epoch++
		c.finished = false
	}
	if reg != hcchar || (value>>18)&3 != 3 {
		c.controller.Write32(reg, value)
		return
	}
	c.regs[reg] = value
	if value&channelDisable != 0 {
		if !c.stalledHalt && c.queueReserved+len(c.events) < 16 {
			c.events = append(c.events, periodicEvent{at: c.elapsed + c.haltTime, epoch: c.epoch, kind: "halt", tx: c.active})
		}
		return
	}
	if value&channelEnable == 0 {
		return
	}
	if c.queueReserved+len(c.events) >= 16 {
		panic("model: request queued without capacity")
	}
	size := c.regs[hctsiz]
	if (size>>19)&1023 != 1 {
		panic("model: completed transfer requeued")
	}
	tx := transaction{in: value&(1<<15) != 0, ep: (value >> 11) & 15, pid: size >> 29 & 3, addr: value >> 22 & 127, size: size & 0x7ffff}
	if !tx.in || tx.ep == 0 || tx.addr != c.address || c.regs[hcsplit] != 0 {
		panic("model: unsupported periodic target")
	}
	c.active = tx
	c.starts = append(c.starts, tx)
	start := c.elapsed
	if uint16((value>>29)&1) != c.frame()&1 {
		start = (c.elapsed/c.frameTime + 1) * c.frameTime
	}
	boundary := (start/c.frameTime + 1) * c.frameTime
	if start < c.wireAvailable {
		start = c.wireAvailable
	}
	end, kind := start+c.wireTime, "packet"
	if end > boundary {
		end, kind = boundary, "overrun"
	}
	c.wireAvailable = end
	c.events = append(c.events, periodicEvent{at: end, epoch: c.epoch, kind: kind, tx: tx})
}

func periodicHost(t *testing.T, c *periodicController) *Host {
	t.Helper()
	return &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2, interval: 10 * time.Millisecond,
		toggle: 2, config: usb.Configuration{Endpoint: 1, MaxPacketSize: 8}}
}

// Check the oracle independently of the driver. Matching parity starts now;
// opposite parity waits for the frame tick, data costs wire time, and halt is
// an independent later event rather than a software flag change.
func TestPeriodicModelDefersWireDataAndRetirement(t *testing.T) {
	c := newPeriodicController()
	c.quantum = 0
	c.elapsed = 200 * time.Microsecond
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	c.Write32(hctsiz, 8|1<<19|2<<29)
	flags := uint32(8|1<<11|3<<18|1<<15|1<<17|1<<20|1<<22) | channelEnable | 1<<29
	c.Write32(hcchar, flags)
	if len(c.rx) != 0 || c.regs[hcint] != 0 {
		t.Fatal("queued request delivered synchronously")
	}
	c.advance(899 * time.Microsecond)
	if len(c.rx) != 0 {
		t.Fatal("data arrived before selected frame plus wire time")
	}
	c.advance(time.Microsecond)
	if len(c.rx) != 3 || c.regs[hcint]&1 == 0 || c.regs[hcchar]&channelEnable == 0 || len(c.history) != 1 || c.history[0].kind != "data" || c.history[0].frame != 0x373 {
		t.Fatal("wire event missing", c.history, c.regs)
	}
	c.Write32(hcchar, flags|channelDisable)
	if c.regs[hcchar]&channelEnable == 0 {
		t.Fatal("halt completed synchronously")
	}
	c.advance(c.haltTime)
	if c.regs[hcchar]&channelEnable != 0 || c.regs[hcint]&2 == 0 || c.history[1].kind != "halt" {
		t.Fatal("retirement event missing", c.history)
	}
}

func TestPeriodicModelOverrunRequiresWireTimePastSelectedFrame(t *testing.T) {
	c := newPeriodicController()
	c.quantum = 0
	c.elapsed = 950 * time.Microsecond
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	c.Write32(hctsiz, 8|1<<19|2<<29)
	flags := uint32(8|1<<11|3<<18|1<<15|1<<17|1<<20|1<<22) | channelEnable // current even frame
	c.Write32(hcchar, flags)
	c.advance(50 * time.Microsecond)
	if len(c.rx) != 0 || c.regs[hcint] != 1<<9 || len(c.history) != 1 || c.history[0].kind != "overrun" || len(c.reports) != 1 || c.reportPID != 2 {
		t.Fatal("overrun fabricated data/toggle", c.history, c.regs)
	}
}

func TestPeriodicModelQueuedWireWorkConsumesFrameBudget(t *testing.T) {
	c := newPeriodicController()
	c.quantum = 0
	c.elapsed = 200 * time.Microsecond
	c.wireTime = 600 * time.Microsecond
	c.Write32(hctsiz, 8|1<<19|2<<29)
	flags := uint32(8|1<<11|3<<18|1<<15|1<<17|1<<20|1<<22) | channelEnable | 1<<29
	c.Write32(hcchar, flags)
	c.Write32(hcchar, flags)
	if c.Read32(ptxstatus)>>16 != 14 || len(c.rx) != 0 {
		t.Fatal("queue capacity or deferred execution missing")
	}
	c.advance(1800 * time.Microsecond)
	if len(c.history) != 2 || c.history[0].kind != "NAK" || c.history[0].at != 1600*time.Microsecond || c.history[1].kind != "overrun" || c.history[1].at != 2*time.Millisecond || c.regs[hcint]&(1<<9) == 0 || len(c.rx) != 0 || c.reportPID != 2 {
		t.Fatal("queued bus work did not exhaust the selected frame", c.history)
	}
}

func TestDriverWithIndependentPeriodicClockAndFrontendGaps(t *testing.T) {
	for _, toggles := range []int{0, 1} {
		for _, gap := range []time.Duration{50 * time.Microsecond, 500 * time.Microsecond, time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond} {
			for _, frame := range []uint16{0x372, 0xffff} {
				t.Run(fmt.Sprintf("toggle%d-gap%s-frame%x", toggles, gap, frame), func(t *testing.T) {
					c := newPeriodicController()
					c.toggles = toggles
					c.frameBase = frame
					want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
					c.reports = [][]byte{want}
					h := periodicHost(t, c)
					var got []byte
					var err error
					for n := 0; n < 12000; n++ {
						got, err = h.pollInput()
						if got != nil || err != nil {
							break
						}
						c.advance(gap)
					}
					// Long caller gaps can expire the existing deadline even after hardware
					// delivered bytes. They must fail rather than extend the packet lifetime.
					if gap == 200*time.Millisecond && toggles == 1 {
						if err == nil || got != nil || h.fault == nil {
							t.Fatal("late completion extended deadline", got, err, c.history)
						}
					} else if err != nil || !bytes.Equal(got, want) || h.pending.active || h.toggle != 0 {
						t.Fatal("valid delayed transfer did not complete", got, err, c.history, h.pending)
					}
					for _, record := range c.history {
						if record.kind == "overrun" {
							t.Fatal("caller gap alone invented frame overrun", c.history)
						}
					}
					if got != nil && c.regs[hcchar]&channelEnable != 0 {
						t.Fatal("report escaped before retirement")
					}
				})
			}
		}
	}
}

func TestPeriodicModelLateRearmCommitOverrunRemainsTerminal(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		t.Run(fmt.Sprintf("halt-stalled-%t", stalled), func(t *testing.T) {
			c := newPeriodicController()
			c.toggles = 1
			c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
			h := periodicHost(t, c)
			if data, err := h.pollInput(); err != nil || data != nil {
				t.Fatal(data, err)
			}
			deadline, pid := h.pending.deadline, h.pending.pid
			// Retire the first service before a new attempt. Delay only that
			// fresh arm, after the real halt and original advertised interval.
			c.advance(2 * time.Millisecond)
			if _, err := h.pollInput(); err != nil {
				t.Fatal(err)
			}
			c.advance(100 * time.Microsecond)
			if _, err := h.pollInput(); err != nil || !h.pending.toggleRetry {
				t.Fatal(err, h.pending)
			}
			c.advance(10100*time.Microsecond - c.elapsed)
			c.channelDelay = 1800 * time.Microsecond
			if data, err := h.pollInput(); err != nil || data != nil || h.pending.toggleRetirements != 1 || h.pending.continuations != 0 {
				t.Fatal(data, err, h.pending)
			}
			c.stalledHalt = stalled
			c.advance(300 * time.Microsecond)
			data, err := h.pollInput()
			if err == nil || data != nil || !strings.Contains(err.Error(), "channel error 0x200") || h.fault == nil || h.pending.received || h.pending.done || h.pending.deadline != deadline || h.pending.pid != pid || h.toggle != 2 {
				t.Fatal("frame overrun was hidden or packet lifetime changed", data, err, h.pending, c.history)
			}
			if stalled && !strings.Contains(err.Error(), "containment:") {
				t.Fatal("unobserved halt lost", err)
			}
			if !stalled && c.regs[hcchar]&channelEnable != 0 {
				t.Fatal("modeled halt did not finish")
			}
			accesses, writes := c.accesses, len(c.writes)
			if _, err := h.pollInput(); err == nil || c.accesses != accesses || len(c.writes) != writes {
				t.Fatal("retired model instance touched hardware")
			}
		})
	}
}

func TestPeriodicModelQueueAndNAKCannotProduceReports(t *testing.T) {
	for _, mode := range []string{"full", "NAK", "stuck-halt"} {
		t.Run(mode, func(t *testing.T) {
			c := newPeriodicController()
			h := periodicHost(t, c)
			if mode == "full" {
				c.queueReserved = 16
			}
			if mode == "stuck-halt" {
				c.stalledHalt = true
			}
			_, err := h.pollInput()
			if mode == "full" {
				if err == nil || len(c.starts) != 0 || h.fault == nil {
					t.Fatal("full queue admitted a request", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c.advance(2 * time.Millisecond)
			if got, err := h.pollInput(); err != nil || got != nil || !h.pending.nak || !h.pending.active {
				t.Fatal("NAK became data", got, err, h.pending)
			}
			if mode == "NAK" {
				c.advance(c.haltTime)
				if got, err := h.pollInput(); err != nil || got != nil || h.pending.active || h.toggle != 2 {
					t.Fatal("NAK did not retire without advancing PID", got, err, h.pending)
				}
			} else {
				deadline := h.pending.deadline
				c.advance(600 * time.Millisecond)
				if got, err := h.pollInput(); err == nil || got != nil || h.pending.deadline != deadline || h.toggle != 2 || h.fault == nil {
					t.Fatal("stuck halt became reusable", got, err, h.pending)
				}
			}
		})
	}
}

// Log the full sweep as model evidence rather than asserting that a selected
// hardware symptom must be its outcome. A software caller gap and a delayed
// MMIO commit are different inputs; these scenarios are not measured Nano times.
func TestPeriodicModelPhaseAndCommitDelaySweep(t *testing.T) {
	type result struct {
		PhaseNS, CommitDelayNS, WireNS int64
		Delivered                      bool
		Error                          string
		Overruns, DataEvents           int
	}
	delivered, overruns := 0, 0
	for _, phase := range []time.Duration{0, 100 * time.Microsecond, 500 * time.Microsecond, 900 * time.Microsecond} {
		for _, delay := range []time.Duration{0, 100 * time.Microsecond, 900 * time.Microsecond, 1800 * time.Microsecond} {
			for _, wire := range []time.Duration{100 * time.Microsecond, 300 * time.Microsecond} {
				c := newPeriodicController()
				c.elapsed = phase
				c.channelDelay = delay
				c.wireTime = wire
				c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
				h := periodicHost(t, c)
				var got []byte
				var err error
				for n := 0; n < 100; n++ {
					got, err = h.pollInput()
					if got != nil || err != nil {
						break
					}
					c.advance(100 * time.Microsecond)
				}
				r := result{PhaseNS: int64(phase), CommitDelayNS: int64(delay), WireNS: int64(wire), Delivered: got != nil}
				if err != nil {
					r.Error = err.Error()
				}
				for _, e := range c.history {
					if e.kind == "overrun" {
						r.Overruns++
					}
					if e.kind == "data" {
						r.DataEvents++
					}
				}
				if r.Overruns > 0 {
					overruns++
					if err == nil || got != nil || h.fault == nil {
						t.Fatal("timing overrun admitted a report", r)
					}
				} else {
					delivered++
					if err != nil || !bytes.Equal(got, []byte{0, 0, 4, 0, 0, 0, 0, 0}) || h.pending.active {
						t.Fatal("feasible timed report failed", r, c.history)
					}
				}
				b, _ := json.Marshal(r)
				t.Log("FES_USB_FRAME_MODEL " + string(b))
			}
		}
	}
	if delivered == 0 || overruns == 0 {
		t.Fatal("sweep did not distinguish successful scheduling and overrun", delivered, overruns)
	}
	t.Logf("FES_USB_FRAME_MODEL_TOTAL delivered=%d overruns=%d", delivered, overruns)
}
