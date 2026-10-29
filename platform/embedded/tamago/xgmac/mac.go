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
	ringSize         = 4
	bufferSize       = 1600
	maxFrame         = 1514
	txRing           = uint32(0)
	rxRing           = uint32(64)
	txBuffers        = uint32(4096)
	rxBuffers        = txBuffers + ringSize*bufferSize
	RequiredBytes    = uint64(rxBuffers + ringSize*bufferSize)
	own              = uint32(1 << 31)
	contextDesc      = uint32(1 << 30)
	first            = uint32(1 << 29)
	last             = uint32(1 << 28)
)

type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

// Memory is exclusive, pinned Normal noncacheable memory, with ordered access.
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
type Config struct {
	MAC        string
	PHYAddress uint8
	PHYID      uint32
	CSRClockHz uint32
	// Explicit hardware contract: Linux generic XGMAC uses bits 14:1;
	// SoCFPGA U-Boot uses bits 14:4. Never autodetect or silently fall back.
	RXBufferShift uint8
}
type failure struct{ err error }
type Device struct {
	mu                             sync.Mutex
	io                             Registers
	memory                         Memory
	config                         Config
	mac                            string
	fault                          atomic.Pointer[failure]
	started                        bool
	rx, tx                         uint32
	nextLink                       time.Time
	received, transmitted, dropped atomic.Uint64
}

func Open(io Registers, memory Memory, config Config) (*Device, error) {
	mac, err := net.ParseMAC(config.MAC)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" || config.PHYAddress > 31 || config.PHYID == 0 || config.PHYID == 0xffffffff || config.CSRClockHz < 100000000 || config.CSRClockHz >= 150000000 || (config.RXBufferShift != 1 && config.RXBufferShift != 4) {
		return nil, fmt.Errorf("xgmac: invalid explicit MAC/PHY/clock/layout configuration")
	}
	if io == nil || memory == nil || memory.Base()%64 != 0 || memory.Size() < RequiredBytes || memory.Base() >= 1<<32 || memory.Size() > 1<<32-memory.Base() {
		return nil, fmt.Errorf("xgmac: invalid pinned DMA region")
	}
	d := &Device{io: io, memory: memory, config: config, mac: mac.String()}
	if err := d.initialize(mac); err != nil {
		return nil, err
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
func (d *Device) link() error {
	control, err := d.mdio(0)
	if err != nil {
		return err
	}
	if control&(1<<15|1<<14|1<<11|1<<10|1<<9) != 0 || control&(1<<12) == 0 {
		return fmt.Errorf("xgmac: PHY must have completed firmware autonegotiation")
	}
	if _, err = d.mdio(1); err != nil {
		return err
	} // BMSR link is latch-low
	status, err := d.mdio(1)
	if err != nil {
		return err
	}
	if status&(1<<5|1<<3|1<<2|1<<8) != (1<<5 | 1<<3 | 1<<2 | 1<<8) {
		return fmt.Errorf("xgmac: link/autonegotiation unavailable")
	}
	local, err := d.mdio(9)
	if err != nil {
		return err
	}
	partner, err := d.mdio(10)
	if err != nil {
		return err
	}
	if local&(1<<9) == 0 || partner&(1<<11) == 0 || partner&(1<<15) != 0 {
		return fmt.Errorf("xgmac: require negotiated 1000BASE-T full duplex")
	}
	d.nextLink = d.io.Now().Add(500 * time.Millisecond)
	return nil
}
func (d *Device) initialize(mac net.HardwareAddr) error {
	if d.io.Read32(version)&255 != 0x21 || d.io.Read32(features0)&(1<<5|1<<1) != (1<<5|1<<1) {
		return fmt.Errorf("xgmac: unsupported core version/GMII/MDIO capabilities")
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
	if err = d.link(); err != nil {
		return err
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
	d.io.Write32(packetFilter, 0) // station unicast + broadcast; no promiscuous mode
	d.io.Write32(0x070, 0)
	d.io.Write32(0x090, 0) // no unqualified pause negotiation
	d.io.Write32(0x140, 0) // full duplex
	d.io.Write32(macHigh, uint32(mac[4])|uint32(mac[5])<<8)
	d.io.Write32(macLow, uint32(mac[0])|uint32(mac[1])<<8|uint32(mac[2])<<16|uint32(mac[3])<<24)
	d.io.Write32(txConfig, 3<<29)
	d.io.Write32(rxConfig, 1<<2|1<<1) // strip CRC/padding; no jumbo/checksum offload
	d.io.Write32(0x3004, 1<<1)        // fixed 4-beat AXI bursts, one outstanding read/write
	d.io.Write32(0x3100, 0)           // 16-byte descriptors, no header split/PBLx8
	d.io.Write32(txControl, 4<<16)
	d.io.Write32(rxControl, 4<<16|bufferSize<<d.config.RXBufferShift)
	// Initialize only this driver's fixed allocation while engines are stopped.
	for offset := uint32(0); uint64(offset) < RequiredBytes; offset += 4 {
		d.memory.Write32(offset, 0)
	}
	for i := uint32(0); i < ringSize; i++ {
		d.post(i)
	}
	d.io.Write32(0x3110, 0)
	d.io.Write32(txList, uint32(d.memory.Base())+txRing)
	d.io.Write32(0x3118, 0)
	d.io.Write32(rxList, uint32(d.memory.Base())+rxRing)
	d.io.Write32(0x3130, ringSize-1)
	d.io.Write32(0x3134, ringSize-1)
	d.io.Write32(channelStatus, 0xffffffff)
	d.memory.Barrier()
	d.started = true // all failures from here retain uncertain DMA ownership
	d.io.Write32(txControl, d.io.Read32(txControl)|1)
	d.io.Write32(rxControl, d.io.Read32(rxControl)|1)
	d.io.Write32(txConfig, d.io.Read32(txConfig)|1)
	d.io.Write32(rxConfig, d.io.Read32(rxConfig)|1)
	d.io.Write32(rxTail, uint32(d.memory.Base())+rxRing+(ringSize-1)*16)
	return nil
}
func (d *Device) post(index uint32) {
	desc := rxRing + index*16
	d.memory.Write32(desc, uint32(d.memory.Base())+rxBuffers+index*bufferSize)
	d.memory.Write32(desc+4, 0)
	d.memory.Write32(desc+8, 0)
	d.memory.Barrier()
	d.memory.Write32(desc+12, own)
	d.memory.Barrier()
}
func (d *Device) fail(err error) error {
	if d.fault.CompareAndSwap(nil, &failure{err: err}) && d.started {
		// Best effort to stop new traffic. Register bits clearing are NOT a DMA
		// fence: all buffers and descriptors stay pinned until reboot, even on success.
		d.io.Write32(txConfig, d.io.Read32(txConfig)&^1)
		d.io.Write32(rxConfig, d.io.Read32(rxConfig)&^1)
		d.io.Write32(txControl, d.io.Read32(txControl)&^1)
		d.io.Write32(rxControl, d.io.Read32(rxControl)&^1)
	}
	return d.Err()
}
func (d *Device) health() error {
	if err := d.Err(); err != nil {
		return err
	}
	if d.io.Read32(channelStatus)&(1<<12|1<<1) != 0 {
		return d.fail(errors.New("xgmac: fatal DMA/stopped TX status"))
	}
	if d.io.Read32(txControl)&1 == 0 || d.io.Read32(rxControl)&1 == 0 || d.io.Read32(txConfig)&1 == 0 || d.io.Read32(rxConfig)&1 == 0 {
		return d.fail(errors.New("xgmac: device stopped unexpectedly"))
	}
	if !d.io.Now().Before(d.nextLink) {
		if err := d.link(); err != nil {
			return d.fail(err)
		}
	}
	return nil
}
func (d *Device) Receive(dst []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.health(); err != nil {
		return 0, err
	}
	desc := rxRing + d.rx*16
	state := d.memory.Read32(desc + 12)
	if state&own != 0 {
		runtime.Gosched()
		return 0, nil
	}
	d.memory.Barrier()
	if state&contextDesc != 0 || state&last == 0 {
		return 0, d.fail(errors.New("xgmac: unexpected RX descriptor/context/fragment"))
	}
	length := int(state & 0x3fff)
	if state&(1<<15) != 0 || length < 14 || length > maxFrame || length > len(dst) {
		d.dropped.Add(1)
	} else {
		d.memory.Read(rxBuffers+d.rx*bufferSize, dst[:length])
		d.received.Add(1)
	}
	dropped := state&(1<<15) != 0 || length < 14 || length > maxFrame || length > len(dst)
	d.post(d.rx)
	d.io.Write32(rxTail, uint32(d.memory.Base())+desc)
	d.rx = (d.rx + 1) % ringSize
	if dropped {
		return 0, nil
	}
	return length, nil
}
func (d *Device) Transmit(frame []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.health(); err != nil {
		return err
	}
	if len(frame) < 14 || len(frame) > maxFrame {
		return fmt.Errorf("xgmac: unsupported Ethernet frame length")
	}
	desc := txRing + d.tx*16
	if d.memory.Read32(desc+12)&own != 0 {
		return d.fail(errors.New("xgmac: TX descriptor still device-owned"))
	}
	offset := txBuffers + d.tx*bufferSize
	d.memory.Write(offset, frame)
	d.memory.Write32(desc, uint32(d.memory.Base())+offset)
	d.memory.Write32(desc+4, 0)
	d.memory.Write32(desc+8, uint32(len(frame)))
	d.memory.Barrier()
	d.memory.Write32(desc+12, own|first|last|uint32(len(frame)))
	d.memory.Barrier()
	next := (d.tx + 1) % ringSize
	d.io.Write32(txTail, uint32(d.memory.Base())+txRing+next*16)
	end := d.io.Now().Add(100 * time.Millisecond)
	for n := 0; n < 1000000; n++ {
		if err := d.health(); err != nil {
			return err
		}
		if state := d.memory.Read32(desc + 12); state&own == 0 {
			d.memory.Barrier()
			if state&contextDesc != 0 || state&last == 0 {
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
