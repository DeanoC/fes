// SPDX-License-Identifier: GPL-3.0-or-later

// Package xgmac provides an exploratory polling XGMAC 2.10 DMA transport.
// Firmware owns RGMII routing/delays, PHY reset/autonegotiation, clocks, SMMU
// reachability and the exclusive noncacheable DMA region. No Go heap is DMA.
package xgmac

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	txConfig         = 0x000
	rxConfig         = 0x004
	packetFilter     = 0x008
	macInterrupt     = 0x0b4
	version          = 0x110
	features0        = 0x11c
	features1        = 0x120
	features2        = 0x124
	mdioAddress      = 0x200
	mdioData         = 0x204
	mdioClause       = 0x220
	macHigh          = 0x300
	macLow           = 0x304
	dmaMode          = 0x3000
	txControl        = 0x3104
	rxControl        = 0x3108
	txList           = 0x3114
	rxList           = 0x311c
	txTail           = 0x3124
	rxTail           = 0x312c
	channelInterrupt = 0x3138
	channelStatus    = 0x3160
	txStopped        = uint32(1 << 1)
	rxStopped        = uint32(1 << 8)
	dmaFaults        = uint32(1<<9 | 1<<12 | 1<<13)
	txSpeedMask      = uint32(7 << 29)
	txSpeed1000GMII  = uint32(3 << 29) // DW XGMAC XGMAC_CONFIG_SS_1000_GMII
	txSpeed100MII    = uint32(4 << 29) // DW XGMAC XGMAC_CONFIG_SS_100_MII
	linkPollUp       = 500 * time.Millisecond
	linkPollDown     = 250 * time.Millisecond
	bufferCount      = 4
	agilexRingSize   = 32 // Agilex 5 can prefetch up to 32 descriptors per channel.
	bufferSize       = 1600
	maxFrame         = 1514
	// Keep descriptor writeback, TX reads and RX writes on separate SMMU pages.
	dmaPageSize      = 4096
	txRing           = uint32(0)
	txBuffers        = uint32(dmaPageSize)
	bufferPagesBytes = (bufferCount*bufferSize + dmaPageSize - 1) &^ (dmaPageSize - 1)
	rxBuffers        = txBuffers + bufferPagesBytes
	RequiredBytes    = uint64(rxBuffers + bufferPagesBytes)
	own              = uint32(1 << 31)
	contextDesc      = uint32(1 << 30)
	first            = uint32(1 << 29)
	last             = uint32(1 << 28)
	txDescError      = uint32(1 << 27)
)

type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

// Memory is page-aligned, exclusive, pinned Normal noncacheable memory, with ordered access.
// Barrier publishes/acquires descriptor ownership. It has no free operation.
type Memory interface {
	Base() uint64
	Size() uint64
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Read(uint32, []byte)
	Write(uint32, []byte)
	Barrier()
}

// LinkSpeed reports the active negotiated full-duplex Ethernet link speed.
type LinkSpeed uint32

const (
	LinkSpeedDown     LinkSpeed = 0
	LinkSpeed100Full  LinkSpeed = 100
	LinkSpeed1000Full LinkSpeed = 1000
)

// LinkStats reports non-fatal PHY carrier transitions observed when
// Config.RecoverLinkLoss is enabled, including one BindControl.Start finds
// relative to the link before Stop. Flaps counts losses of an up link;
// Recoveries counts the link-ups ending them, so it never exceeds Flaps. The
// first link-up after Open found the carrier down is reported through
// OnLinkChange but is not a recovery.
type LinkStats struct {
	Flaps      uint64
	Recoveries uint64
}

type Config struct {
	ExpectedVersion uint16 // Optional exact MAC device and Synopsys revision (MAC_Version bits 15:0).
	MAC             string
	PHYAddress      uint8
	PHYID           uint32
	CSRClockHz      uint32
	// Explicit hardware contract: Linux generic XGMAC uses bits 14:1;
	// SoCFPGA U-Boot uses bits 14:4. Never autodetect or silently fall back.
	RXBufferShift uint8
	// IRQ optionally specifies the GICv3 SPI INTID for this MAC (e.g. Agilex 5
	// IRQEMAC0=222, IRQEMAC1=239, IRQEMAC2=256). When non-zero, posted RX
	// descriptors set IOC and EnableIRQ can bind GIC delivery; zero keeps
	// polling-only mode.
	IRQ uint32
	// Allow100Full optionally admits firmware-negotiated 100BASE-TX full duplex
	// in addition to 1000BASE-T full duplex, selecting XGMAC_CONFIG_SS_100_MII.
	// Half duplex and 10 Mbps links remain rejected. Default false preserves
	// strict 1000BASE-T-only admission.
	Allow100Full bool
	// RecoverLinkLoss optionally treats transient PHY carrier loss or ongoing
	// autonegotiation as a recoverable link-down state instead of permanently
	// retiring the device and poisoning DMA memory. Hardware/DMA/MDIO faults
	// still fail closed. Default false preserves strict fail-closed link checks.
	RecoverLinkLoss bool
	// OnSpeedChange optionally adjusts platform clock/PHY glue (e.g. RGMII TX
	// clock divider) before applying a negotiated speed on initialize or
	// post-flap speed change. Returning an error fails the device closed.
	OnSpeedChange func(LinkSpeed) error
	// OnLinkChange optionally notifies callers (outside the device mutex) when
	// carrier state or negotiated link speed changes under RecoverLinkLoss. A
	// BindControl pause is no change by itself: Start reports the re-probed link
	// when it differs from the link before Stop.
	OnLinkChange func(up bool, speed LinkSpeed)
	// ResetMMCOnRead enables clear-on-read MMC hardware counters during
	// initialization (masking MMC rollover IRQs and enabling ROR). When true,
	// HardwareStats accumulates cleared deltas across polls and Stop/Start.
	// Default false leaves MMC control registers untouched.
	ResetMMCOnRead bool
}
type failure struct{ err error }
type linkEvent struct {
	changed bool
	up      bool
	speed   LinkSpeed
}
type Device struct {
	mu                             sync.Mutex
	irqMu                          sync.Mutex
	io                             Registers
	memory                         Memory
	config                         Config
	lease                          *maccontrol.Client // retained by controlled startup, even after failure
	mac                            string
	fault                          atomic.Pointer[failure]
	started                        bool
	paused, controlBound           bool
	linkDown                       bool
	configuredSpeed                LinkSpeed
	dmaBase, dmaBytes              uint64
	rx, tx, ringEntries            uint32
	nextLink                       time.Time
	received, transmitted, dropped atomic.Uint64
	linkSpeed                      atomic.Uint32
	linkFlaps, linkRecoveries      atomic.Uint64
	irqGIC                         *gic.Controller
	rxReady                        chan struct{}
	irqActive                      atomic.Bool
	irqCount                       atomic.Uint64
	hashBits                       atomic.Int32
	multicastWords                 int
	multicastHash                  [8]uint32
	multicastFilter                uint32
	allMulticast                   bool
	hwStats                        HardwareStats
}

func Open(io Registers, memory Memory, config Config) (*Device, error) {
	mac, err := net.ParseMAC(config.MAC)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" || config.PHYAddress > 31 || config.PHYID == 0 || config.PHYID == 0xffffffff || config.CSRClockHz < 100000000 || config.CSRClockHz >= 150000000 || (config.RXBufferShift != 1 && config.RXBufferShift != 4) {
		return nil, fmt.Errorf("xgmac: invalid explicit MAC/PHY/clock/layout configuration")
	}
	if config.IRQ != 0 && (config.IRQ < gic.FirstSPI || config.IRQ >= gic.MaxINTID) {
		return nil, fmt.Errorf("xgmac: invalid SPI interrupt ID %d", config.IRQ)
	}
	if io == nil || memory == nil || memory.Base()%dmaPageSize != 0 || memory.Size() < RequiredBytes || memory.Base() >= 1<<32 || memory.Size() > 1<<32-memory.Base() {
		return nil, fmt.Errorf("xgmac: invalid pinned DMA region")
	}
	if adopter, ok := memory.(interface{ adopt() error }); ok {
		if err := adopter.adopt(); err != nil {
			return nil, err
		}
	}
	d := &Device{io: io, memory: memory, config: config, mac: mac.String(), dmaBase: memory.Base(), dmaBytes: memory.Size(), rxReady: make(chan struct{}, 1)}
	if err := d.initialize(mac); err != nil {
		return d, d.retireControl(err)
	}
	return d, nil
}
func (d *Device) MAC() string { return d.mac }
func (d *Device) Err() error {
	if fault := d.fault.Load(); fault != nil {
		return fault.err
	}
	return nil
}
func (d *Device) Statistics() network.Stats {
	return network.Stats{Received: d.received.Load(), Transmitted: d.transmitted.Load(), Dropped: d.dropped.Load()}
}

// LinkStatus reports whether the PHY carrier is currently up and its negotiated speed.
func (d *Device) LinkStatus() (bool, LinkSpeed) {
	if d == nil {
		return false, LinkSpeedDown
	}
	speed := LinkSpeed(d.linkSpeed.Load())
	return speed != LinkSpeedDown, speed
}

// LinkCounters returns counts of transient carrier losses and recoveries.
func (d *Device) LinkCounters() LinkStats {
	if d == nil {
		return LinkStats{}
	}
	return LinkStats{Flaps: d.linkFlaps.Load(), Recoveries: d.linkRecoveries.Load()}
}

func (d *Device) notifyLink(ev linkEvent) {
	if ev.changed && d.config.OnLinkChange != nil {
		d.config.OnLinkChange(ev.up, ev.speed)
	}
}

// reportLinkLocked publishes speed (LinkSpeedDown while the carrier is down) and
// returns the OnLinkChange event for a change from prev, the link consumers last
// observed. Losing an up link counts a flap; a link-up counts a recovery only
// while a counted loss is outstanding (Flaps > Recoveries), so Recoveries never
// exceeds Flaps. Caller must hold d.mu.
func (d *Device) reportLinkLocked(prev, speed LinkSpeed) linkEvent {
	d.linkSpeed.Store(uint32(speed))
	if speed == prev {
		return linkEvent{}
	}
	if speed == LinkSpeedDown {
		d.linkFlaps.Add(1)
	} else if prev == LinkSpeedDown && d.linkFlaps.Load() > d.linkRecoveries.Load() {
		d.linkRecoveries.Add(1)
	}
	return linkEvent{changed: true, up: speed != LinkSpeedDown, speed: speed}
}

func txSpeedBits(speed LinkSpeed) uint32 {
	if speed == LinkSpeed100Full {
		return txSpeed100MII
	}
	return txSpeed1000GMII
}
func (d *Device) wait(reg, mask, want uint32) error {
	end := d.io.Now().Add(100 * time.Millisecond)
	for n := 0; n < 1000000; n++ {
		if d.io.Read32(reg)&mask == want {
			return nil
		}
		if !d.io.Now().Before(end) {
			break
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("xgmac: timeout at register %#x", reg)
}
func (d *Device) mdio(reg uint32) (uint16, error) {
	if err := d.wait(mdioData, 1<<22, 0); err != nil {
		return 0, err
	}
	d.io.Write32(mdioClause, 1<<d.config.PHYAddress)
	d.io.Write32(mdioAddress, uint32(d.config.PHYAddress)<<16|reg)
	// CSR 100..150 MHz uses /62, MDC <2.5 MHz; single Clause-22 read.
	d.io.Write32(mdioData, 1<<22|1<<18|3<<16)
	if err := d.wait(mdioData, 1<<22, 0); err != nil {
		return 0, err
	}
	return uint16(d.io.Read32(mdioData)), nil
}
func (d *Device) probeLink() (LinkSpeed, bool, error) {
	control, err := d.mdio(0)
	if err != nil {
		return LinkSpeedDown, false, err
	}
	if control&(1<<15|1<<14|1<<11|1<<10) != 0 || control&(1<<12) == 0 {
		return LinkSpeedDown, false, fmt.Errorf("xgmac: PHY must have completed firmware autonegotiation")
	}
	if control&(1<<9) != 0 {
		return LinkSpeedDown, true, fmt.Errorf("xgmac: PHY must have completed firmware autonegotiation")
	}
	if _, err = d.mdio(1); err != nil {
		return LinkSpeedDown, false, err
	} // BMSR link is latch-low
	status, err := d.mdio(1)
	if err != nil {
		return LinkSpeedDown, false, err
	}
	if status&(1<<3) == 0 {
		return LinkSpeedDown, false, fmt.Errorf("xgmac: link/autonegotiation unavailable")
	}
	if status&(1<<5|1<<2) != (1<<5 | 1<<2) {
		return LinkSpeedDown, true, fmt.Errorf("xgmac: link/autonegotiation unavailable")
	}
	hasExt := status&(1<<8) != 0
	if hasExt {
		local1000, err := d.mdio(9)
		if err != nil {
			return LinkSpeedDown, false, err
		}
		partner1000, err := d.mdio(10)
		if err != nil {
			return LinkSpeedDown, false, err
		}
		if partner1000&(1<<15) != 0 {
			return LinkSpeedDown, false, fmt.Errorf("xgmac: 1000BASE-T master/slave configuration fault")
		}
		// IEEE 802.3 Annex 28B.3 priority: 1000BASE-T FD > 1000BASE-T HD > 100BASE-TX FD.
		if local1000&(1<<9) != 0 && partner1000&(1<<11) != 0 {
			// On CBL the KSZ9031 can finish autonegotiation while the
			// remote receiver is still training. Its low byte counts idle
			// errors and can saturate during that transition.
			if d.config.PHYID&0xfffffff0 == 0x00221620 &&
				(partner1000&(3<<12) != 3<<12 || partner1000&0xff == 0xff) {
				return LinkSpeedDown, true, fmt.Errorf("xgmac: KSZ9031 gigabit receivers are not ready")
			}
			return LinkSpeed1000Full, false, nil
		}
		if local1000&(1<<8) != 0 && partner1000&(1<<10) != 0 {
			return LinkSpeedDown, false, fmt.Errorf("xgmac: half-duplex 1000BASE-T unsupported")
		}
	} else if !d.config.Allow100Full {
		return LinkSpeedDown, false, fmt.Errorf("xgmac: link/autonegotiation unavailable")
	}
	if d.config.Allow100Full {
		anar, err := d.mdio(4)
		if err != nil {
			return LinkSpeedDown, false, err
		}
		anlpar, err := d.mdio(5)
		if err != nil {
			return LinkSpeedDown, false, err
		}
		if anar&(1<<8) != 0 && anlpar&(1<<8) != 0 {
			return LinkSpeed100Full, false, nil
		}
		return LinkSpeedDown, false, fmt.Errorf("xgmac: require negotiated 1000BASE-T or 100BASE-TX full duplex")
	}
	return LinkSpeedDown, false, fmt.Errorf("xgmac: require negotiated 1000BASE-T full duplex")
}

func (d *Device) applySpeedLocked(speed LinkSpeed) error {
	if d.config.OnSpeedChange != nil {
		if err := d.config.OnSpeedChange(speed); err != nil {
			return err
		}
	}
	cur := d.io.Read32(txConfig)
	d.io.Write32(txConfig, cur&^1)
	updated := (cur &^ (txSpeedMask | 1)) | txSpeedBits(speed)
	d.io.Write32(txConfig, updated)
	d.io.Write32(txConfig, updated|1)
	d.configuredSpeed = speed
	return nil
}

func (d *Device) initialize(mac net.HardwareAddr) error {
	id := d.io.Read32(version)
	if d.config.ExpectedVersion != 0 && uint16(id) != d.config.ExpectedVersion {
		return fmt.Errorf("xgmac: expected MAC version %#x, read %#x", d.config.ExpectedVersion, id)
	}
	switch {
	case id&0xff == 0x21:
		d.ringEntries = bufferCount
	case id&0xffff == 0x7631: // Agilex 5 XGMAC2 DEVID 0x76, SNPSVER 0x31
		if d.config.RXBufferShift != 1 {
			return fmt.Errorf("xgmac: Agilex 5 XGMAC2 requires RX buffer byte count at bit 1")
		}
		d.ringEntries = agilexRingSize
	default:
		return fmt.Errorf("xgmac: unsupported core version %#x", id)
	}
	if d.io.Read32(features0)&(1<<5|1<<1) != (1<<5 | 1<<1) {
		return fmt.Errorf("xgmac: missing GMII/MDIO capabilities")
	}
	count := d.io.Read32(features2)
	if (count>>18)&15 > 7 || (count>>12)&15 > 7 {
		return fmt.Errorf("xgmac: channel count exceeds SoC register window")
	}
	for i := uint32(0); i <= max((count>>18)&15, (count>>12)&15); i++ {
		if d.io.Read32(txControl+i*0x80)&1 != 0 || d.io.Read32(rxControl+i*0x80)&1 != 0 {
			return fmt.Errorf("xgmac: firmware DMA channel still active")
		}
	}
	if d.io.Read32(txConfig)&1 != 0 || d.io.Read32(rxConfig)&1 != 0 {
		return fmt.Errorf("xgmac: firmware MAC still active")
	}
	if d.io.Read32(dmaMode)&1 != 0 {
		return fmt.Errorf("xgmac: firmware reset incomplete")
	}
	d.io.Write32(dmaMode, 1)
	if err := d.wait(dmaMode, 1, 0); err != nil {
		return err
	}
	idHigh, err := d.mdio(2)
	if err != nil {
		return err
	}
	idLow, err := d.mdio(3)
	if err != nil {
		return err
	}
	if uint32(idHigh)<<16|uint32(idLow) != d.config.PHYID {
		return fmt.Errorf("xgmac: PHY identity mismatch")
	}
	speed, transientDown, err := d.probeLink()
	initialSpeed := LinkSpeed1000Full
	if err != nil {
		if !d.config.RecoverLinkLoss || !transientDown {
			return err
		}
		d.linkDown = true
		d.configuredSpeed = LinkSpeedDown
		d.linkSpeed.Store(uint32(LinkSpeedDown))
		d.nextLink = d.io.Now().Add(linkPollDown)
	} else {
		if d.config.OnSpeedChange != nil {
			if err := d.config.OnSpeedChange(speed); err != nil {
				return err
			}
		}
		initialSpeed = speed
		d.linkDown = false
		d.configuredSpeed = speed
		d.linkSpeed.Store(uint32(speed))
		d.nextLink = d.io.Now().Add(linkPollUp)
	}
	fifo := d.io.Read32(features1)
	txPower, rxPower := (fifo>>6)&31, fifo&31
	if txPower < 4 || txPower > 10 || rxPower < 4 || rxPower > 11 {
		return fmt.Errorf("xgmac: unsupported FIFO sizes")
	}
	d.io.Write32(macInterrupt, 0)
	d.io.Write32(channelInterrupt, 0)
	d.io.Write32(0x1100, ((uint32(128)<<txPower)/256-1)<<16|2<<2|1<<1)
	d.io.Write32(0x1118, 0x10)
	d.io.Write32(0x1140, ((uint32(128)<<rxPower)/256-1)<<16|1<<5)
	d.io.Write32(0x1030, 0)    // RX queue 0 -> DMA channel 0, static mapping
	d.io.Write32(0x0a0, 2)     // queue 0 DCB; other RX queues disabled
	d.io.Write32(0x0a4, 1<<15) // multicast/broadcast queue 0
	d.io.Write32(0x0a8, 0)
	d.hashBits.Store(int32(decodeHashTableBits(fifo)))
	if d.multicastWords > 0 {
		if err := d.programMulticastLocked(d.multicastWords, d.multicastHash, d.effectivePacketFilterLocked(d.multicastFilter)); err != nil {
			return err
		}
	} else if d.allMulticast {
		if err := d.programPacketFilterLocked(d.effectivePacketFilterLocked(0)); err != nil {
			return err
		}
	} else {
		d.io.Write32(packetFilter, 0) // station unicast + broadcast; no promiscuous mode
	}
	if d.config.ResetMMCOnRead {
		d.initMMCLocked()
	}
	d.io.Write32(0x070, 0)
	d.io.Write32(0x090, 0) // no unqualified pause negotiation
	d.io.Write32(0x140, 0) // full duplex
	d.io.Write32(macHigh, uint32(mac[4])|uint32(mac[5])<<8)
	d.io.Write32(macLow, uint32(mac[0])|uint32(mac[1])<<8|uint32(mac[2])<<16|uint32(mac[3])<<24)
	d.io.Write32(txConfig, txSpeedBits(initialSpeed))
	d.io.Write32(rxConfig, 1<<2|1<<1) // strip CRC/padding; no jumbo/checksum offload
	d.io.Write32(0x3004, 1<<1)        // fixed 4-beat AXI bursts, one outstanding read/write
	d.io.Write32(0x3100, 0)           // 16-byte descriptors, no header split/PBLx8
	d.io.Write32(txControl, 4<<16)
	d.io.Write32(rxControl, 4<<16|bufferSize<<d.config.RXBufferShift)
	// Initialize only this driver's fixed allocation while engines are stopped.
	for offset := uint32(0); uint64(offset) < RequiredBytes; offset += 4 {
		d.memory.Write32(offset, 0)
	}
	for i := uint32(0); i < bufferCount; i++ {
		d.post(i)
	}
	d.io.Write32(0x3110, 0)
	d.io.Write32(txList, uint32(d.memory.Base())+txRing)
	d.io.Write32(0x3118, 0)
	d.io.Write32(rxList, uint32(d.memory.Base())+d.rxDescriptor(0))
	d.io.Write32(0x3130, d.ringEntries-1)
	d.io.Write32(0x3134, d.ringEntries-1)
	d.io.Write32(channelStatus, 0xffffffff)
	d.memory.Barrier()
	d.started = true // all failures from here retain uncertain DMA ownership
	d.io.Write32(txControl, d.io.Read32(txControl)|1)
	d.io.Write32(rxControl, d.io.Read32(rxControl)|1)
	d.io.Write32(txConfig, d.io.Read32(txConfig)|1)
	d.io.Write32(rxConfig, d.io.Read32(rxConfig)|1)
	d.publishRXTail(bufferCount - 1)
	return nil
}
func (d *Device) rxDescriptor(index uint32) uint32 { return (d.ringEntries + index) * 16 }

func (d *Device) publishRXTail(lastPosted uint32) {
	if d.ringEntries == agilexRingSize {
		// The Agilex tail is the descriptor just beyond the available buffers.
		lastPosted = (lastPosted + 1) % d.ringEntries
	}
	d.io.Write32(rxTail, uint32(d.memory.Base())+d.rxDescriptor(lastPosted))
}

func (d *Device) post(index uint32) {
	desc := d.rxDescriptor(index)
	d.memory.Write32(desc, uint32(d.memory.Base())+rxBuffers+index%bufferCount*bufferSize)
	d.memory.Write32(desc+4, 0)
	d.memory.Write32(desc+8, 0)
	d.memory.Barrier()
	flags := own
	if d.config.IRQ != 0 {
		flags |= rxIOC
	}
	d.memory.Write32(desc+12, flags)
	d.memory.Barrier()
}
func (d *Device) poisonMemory(err error) {
	if pm, ok := d.memory.(interface{ Poison(error) }); ok {
		pm.Poison(err)
	}
}
func (d *Device) fail(err error) error {
	if d.fault.CompareAndSwap(nil, &failure{err: err}) {
		d.linkSpeed.Store(uint32(LinkSpeedDown))
		d.retireIRQLocked(d.started)
		d.poisonMemory(err)
		if d.started {
			// Best effort to stop new traffic. Register bits clearing are NOT a DMA
			// fence: all buffers and descriptors stay pinned until reboot, even on success.
			d.io.Write32(txConfig, d.io.Read32(txConfig)&^1)
			d.io.Write32(rxConfig, d.io.Read32(rxConfig)&^1)
			d.io.Write32(txControl, d.io.Read32(txControl)&^1)
			d.io.Write32(rxControl, d.io.Read32(rxControl)&^1)
		}
	}
	return d.Err()
}
func (d *Device) health(allowLinkRecovery bool) (linkEvent, error) {
	if err := d.Err(); err != nil {
		return linkEvent{}, err
	}
	if d.paused {
		return linkEvent{}, network.ErrPaused
	}
	if d.io.Read32(channelStatus)&(dmaFaults|txStopped|rxStopped) != 0 {
		return linkEvent{}, d.fail(errors.New("xgmac: DMA error or unexpected process stop"))
	}
	if d.io.Read32(txControl)&1 == 0 || d.io.Read32(rxControl)&1 == 0 || d.io.Read32(txConfig)&1 == 0 || d.io.Read32(rxConfig)&1 == 0 {
		return linkEvent{}, d.fail(errors.New("xgmac: device stopped unexpectedly"))
	}
	if !d.io.Now().Before(d.nextLink) {
		speed, transientDown, err := d.probeLink()
		if err != nil {
			if !d.config.RecoverLinkLoss || !transientDown {
				return linkEvent{}, d.fail(err)
			}
			d.nextLink = d.io.Now().Add(linkPollDown)
			d.linkDown = true
			return d.reportLinkLocked(LinkSpeed(d.linkSpeed.Load()), LinkSpeedDown), nil
		}
		if !allowLinkRecovery && (d.linkDown || speed != d.configuredSpeed) {
			// Defer link-up recovery and TX MAC speed reprogramming while a TX
			// descriptor is in flight or while BindControl.Stop is containing the MAC.
			return linkEvent{}, nil
		}
		if speed != d.configuredSpeed {
			if err := d.applySpeedLocked(speed); err != nil {
				return linkEvent{}, d.fail(err)
			}
		}
		d.nextLink = d.io.Now().Add(linkPollUp)
		d.linkDown = false
		return d.reportLinkLocked(LinkSpeed(d.linkSpeed.Load()), speed), nil
	}
	return linkEvent{}, nil
}
func (d *Device) Receive(dst []byte) (int, error) {
	var linkEv linkEvent
	defer func() { d.notifyLink(linkEv) }()
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	linkEv, err = d.health(true)
	if err != nil {
		return 0, err
	}
	if d.linkDown {
		runtime.Gosched()
		return 0, nil
	}
	desc := d.rxDescriptor(d.rx)
	state := d.memory.Read32(desc + 12)
	if state&own != 0 {
		if d.irqActive.Load() {
			if recheck := d.rearmRXIRQLocked(desc); recheck&own == 0 {
				state = recheck
			} else {
				return 0, nil
			}
		} else {
			runtime.Gosched()
			return 0, nil
		}
	}
	d.memory.Barrier()
	if state&(contextDesc|first|last) != first|last {
		return 0, d.fail(errors.New("xgmac: unexpected RX descriptor/context/fragment"))
	}
	length := int(state & 0x3fff)
	if state&(1<<15) != 0 || length < 14 || length > maxFrame || length > len(dst) {
		d.dropped.Add(1)
	} else {
		d.memory.Read(rxBuffers+d.rx%bufferCount*bufferSize, dst[:length])
		d.received.Add(1)
	}
	dropped := state&(1<<15) != 0 || length < 14 || length > maxFrame || length > len(dst)
	// Only these four buffers can be owned at once. Move the one just
	// consumed to the next free descriptor before advancing the tail.
	repost := (d.rx + bufferCount) % d.ringEntries
	d.post(repost)
	d.publishRXTail(repost)
	d.rx = (d.rx + 1) % d.ringEntries
	if dropped {
		return 0, nil
	}
	return length, nil
}
func (d *Device) Transmit(frame []byte) error {
	var linkEv linkEvent
	defer func() { d.notifyLink(linkEv) }()
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	linkEv, err = d.health(true)
	if err != nil {
		return err
	}
	if len(frame) < 14 || len(frame) > maxFrame {
		return fmt.Errorf("xgmac: unsupported Ethernet frame length")
	}
	if d.linkDown {
		d.dropped.Add(1)
		return nil
	}
	desc := txRing + d.tx*16
	if d.memory.Read32(desc+12)&own != 0 {
		return d.fail(errors.New("xgmac: TX descriptor still device-owned"))
	}
	offset := txBuffers + d.tx%bufferCount*bufferSize
	d.memory.Write(offset, frame)
	d.memory.Write32(desc, uint32(d.memory.Base())+offset)
	d.memory.Write32(desc+4, 0)
	d.memory.Write32(desc+8, uint32(len(frame)))
	d.memory.Barrier()
	d.memory.Write32(desc+12, own|first|last|uint32(len(frame)))
	d.memory.Barrier()
	next := (d.tx + 1) % d.ringEntries
	d.io.Write32(txTail, uint32(d.memory.Base())+txRing+next*16)
	end := d.io.Now().Add(100 * time.Millisecond)
	for n := 0; n < 1000000; n++ {
		ev, err := d.health(false)
		if ev.changed {
			linkEv = ev
		}
		if err != nil {
			return err
		}
		if state := d.memory.Read32(desc + 12); state&own == 0 {
			d.memory.Barrier()
			if state&(contextDesc|first|last|txDescError) != first|last {
				return d.fail(errors.New("xgmac: malformed TX completion"))
			}
			d.tx = next
			d.transmitted.Add(1)
			return nil
		}
		if !d.io.Now().Before(end) {
			break
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return d.fail(errors.New("xgmac: TX completion timeout; DMA memory retained"))
}
