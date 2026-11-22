//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package block

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/usbarmory/tamago/dma"
)

const (
	queueSize          = 8
	maxTransfer        = 128 * SectorSize
	availableOffset    = 128
	usedOffset         = 256
	headerOffset       = 512
	dataOffset         = 1024
	statusOffset       = dataOffset + maxTransfer
	dmaBytes           = statusOffset + 64
	regInterrupt       = 0x60
	regInterruptAck    = 0x64
	featureVersion1    = uint64(1) << 32
	featureRO          = uint64(1) << 5
	featureFlush       = uint64(1) << 9
	virtioMMIOBase     = uint32(0x0a000000)
	virtioMMIOStride   = uint32(0x200)
	virtioMMIOSlots    = uint32(32)
	virtioMMIOFirstIRQ = uint32(48)
)

// VirtIO implements one VirtIO 1.x MMIO block queue backed by dmaops.Buffer.
// Its DMA span stays owned until completion; an ambiguous timeout poisons both
// the queue and the underlying dmaops.Buffer so memory is never recycled.
// Call EnableIRQ to wake transfers from WFI via the GICv3 SPI instead of polling.
type VirtIO struct {
	mu        sync.Mutex
	base      uint32
	sectors   uint64
	readonly  bool
	flush     bool
	dmaBuf    *dmaops.Buffer
	address   uint64
	buf       []byte
	available uint16
	used      uint16
	poisoned  bool
	irqReady  chan struct{}
	irqActive atomic.Bool
	irqCount  atomic.Uint64
}

func regRead(base, offset uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(uintptr(base + offset))))
}
func regWrite(base, offset, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(uintptr(base+offset))), value)
}

// ScanVirtIO finds a block device in QEMU virt's 32-slot MMIO window.
// These are emulator addresses only and must not be used on DE25 hardware.
func ScanVirtIO() (*VirtIO, error) {
	return ScanVirtIOWithPool(nil)
}

// ScanVirtIOWithPool finds a QEMU virt VirtIO block device and allocates its
// queue and transfer staging buffer from pool (or reserves from tamago/dma when nil).
func ScanVirtIOWithPool(pool *dmaops.Pool) (*VirtIO, error) {
	for i := uint32(0); i < virtioMMIOSlots; i++ {
		base := virtioMMIOBase + i*virtioMMIOStride
		if regRead(base, 0) == 0x74726976 && regRead(base, 8) == 2 {
			return OpenVirtIOWithPool(base, pool)
		}
	}
	return nil, errors.New("no QEMU VirtIO block device")
}

func OpenVirtIO(base uint32) (*VirtIO, error) {
	return OpenVirtIOWithPool(base, nil)
}

func OpenVirtIOWithPool(base uint32, pool *dmaops.Pool) (*VirtIO, error) {
	if base == 0 || regRead(base, 0) != 0x74726976 || regRead(base, 4) != 2 || regRead(base, 8) != 2 {
		return nil, errors.New("expected VirtIO 1.x MMIO block device")
	}
	d := &VirtIO{base: base, irqReady: make(chan struct{}, 1)}
	regWrite(base, 0x70, 0)
	deadline := time.Now().Add(time.Second)
	for polls := 0; regRead(base, 0x70) != 0; polls++ {
		if polls >= 2000000 || time.Now().After(deadline) {
			return nil, errors.New("VirtIO block reset timeout")
		}
		runtime.Gosched()
	}
	regWrite(base, 0x70, 3)
	regWrite(base, 0x14, 0)
	features := uint64(regRead(base, 0x10))
	regWrite(base, 0x14, 1)
	features |= uint64(regRead(base, 0x10)) << 32
	if features&featureVersion1 == 0 {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block lacks VERSION_1")
	}
	d.readonly = features&featureRO != 0
	d.flush = features&featureFlush != 0
	// FLUSH is required: a filesystem must not claim persistence based only on
	// completion of an OUT request against a potentially cached host backend.
	if !d.flush {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block lacks FLUSH")
	}
	selected := featureVersion1 | featureFlush
	if d.readonly {
		selected |= featureRO
	}
	regWrite(base, 0x24, 0)
	regWrite(base, 0x20, uint32(selected))
	regWrite(base, 0x24, 1)
	regWrite(base, 0x20, uint32(selected>>32))
	regWrite(base, 0x70, 11)
	if regRead(base, 0x70)&8 == 0 {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block feature negotiation rejected")
	}
	regWrite(base, 0x30, 0)
	if regRead(base, 0x34) < queueSize || regRead(base, 0x44) != 0 {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block queue unavailable")
	}
	for attempt := 0; attempt < 16; attempt++ {
		generation := regRead(base, 0xfc)
		low, high := regRead(base, 0x100), regRead(base, 0x104)
		if generation == regRead(base, 0xfc) {
			d.sectors = uint64(low) | uint64(high)<<32
			break
		}
	}
	if d.sectors == 0 {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block capacity unavailable")
	}
	if pool == nil {
		addr, raw := dma.Reserve(dmaBytes, 4096)
		if len(raw) != dmaBytes {
			regWrite(base, 0x70, 128)
			return nil, errors.New("VirtIO block DMA allocation failed")
		}
		var err error
		pool, err = dmaops.NewPool(uint64(addr), raw, dmaops.Uncached, dmaops.Native())
		if err != nil {
			regWrite(base, 0x70, 128)
			return nil, err
		}
	}
	var err error
	d.dmaBuf, err = pool.Alloc(dmaBytes, 4096)
	if err != nil {
		regWrite(base, 0x70, 128)
		return nil, fmt.Errorf("VirtIO block DMA allocation failed: %w", err)
	}
	d.address = d.dmaBuf.PhysAddr()
	d.buf = d.dmaBuf.Bytes()
	binary.LittleEndian.PutUint16(d.buf[availableOffset:], 1) // suppress interrupts until EnableIRQ
	regWrite(base, 0x38, queueSize)
	for _, pair := range [][2]uint64{{0x80, d.address}, {0x90, d.address + availableOffset}, {0xa0, d.address + usedOffset}} {
		regWrite(base, uint32(pair[0]), uint32(pair[1]))
		regWrite(base, uint32(pair[0])+4, uint32(pair[1]>>32))
	}
	if err := d.dmaBuf.SyncForDevice(dmaops.ToDevice); err != nil {
		d.dmaBuf.Poison(err)
		regWrite(base, 0x70, 128)
		return nil, err
	}
	regWrite(base, 0x44, 1)
	regWrite(base, 0x70, 15)
	return d, nil
}

func (d *VirtIO) irqID() (uint32, bool) {
	if d.base < virtioMMIOBase || (d.base-virtioMMIOBase)%virtioMMIOStride != 0 {
		return 0, false
	}
	slot := (d.base - virtioMMIOBase) / virtioMMIOStride
	if slot >= virtioMMIOSlots {
		return 0, false
	}
	return virtioMMIOFirstIRQ + slot, true
}

func (d *VirtIO) acknowledge() {
	if status := regRead(d.base, regInterrupt); status != 0 {
		regWrite(d.base, regInterruptAck, status)
	}
}

// IRQCount returns the number of VirtIO-block interrupts handled since EnableIRQ.
func (d *VirtIO) IRQCount() uint64 { return d.irqCount.Load() }

// EnableIRQ routes this VirtIO-block slot's GICv3 SPI to wake pending requests
// from WFI instead of busy-polling. Call after g.Init() with g.Serve running.
func (d *VirtIO) EnableIRQ(g *gic.Controller) error {
	if g == nil {
		return errors.New("block: nil GIC controller")
	}
	id, ok := d.irqID()
	if !ok || id >= g.Lines() {
		return gic.ErrInvalidConfig
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.poisoned {
		return errors.New("VirtIO block queue is poisoned")
	}
	g.Disable(id)
	d.irqCount.Store(0)
	binary.LittleEndian.PutUint16(d.buf[availableOffset:], 0) // clear AVAIL_F_NO_INTERRUPT
	if err := d.dmaBuf.SyncForDevice(dmaops.ToDevice); err != nil {
		return err
	}
	d.acknowledge()
	g.Register(id, func() {
		d.irqCount.Add(1)
		d.acknowledge()
		select {
		case d.irqReady <- struct{}{}:
		default:
		}
	})
	d.irqActive.Store(true)
	g.Enable(id)
	return nil
}

// DisableIRQ re-suppresses VirtIO block interrupts and masks the GICv3 SPI.
func (d *VirtIO) DisableIRQ(g *gic.Controller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.poisoned && d.dmaBuf != nil {
		binary.LittleEndian.PutUint16(d.buf[availableOffset:], 1)
		_ = d.dmaBuf.SyncForDevice(dmaops.ToDevice)
	}
	if g != nil {
		if id, ok := d.irqID(); ok {
			g.Disable(id)
			g.Register(id, nil)
		}
	}
	d.irqActive.Store(false)
	d.acknowledge()
}

func (d *VirtIO) SectorCount() uint64 { return d.sectors }

func (d *VirtIO) ReadSectors(lba uint64, dst []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := CheckRange(d.sectors, lba, len(dst)); err != nil {
		return err
	}
	for len(dst) > 0 {
		n := min(len(dst), maxTransfer)
		if err := d.request(0, lba, dst[:n]); err != nil {
			return err
		}
		lba += uint64(n / SectorSize)
		dst = dst[n:]
	}
	return nil
}

func (d *VirtIO) WriteSectors(lba uint64, src []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readonly {
		return ErrReadOnly
	}
	if err := CheckRange(d.sectors, lba, len(src)); err != nil {
		return err
	}
	for len(src) > 0 {
		n := min(len(src), maxTransfer)
		if err := d.request(1, lba, src[:n]); err != nil {
			return err
		}
		lba += uint64(n / SectorSize)
		src = src[n:]
	}
	return nil
}

func (d *VirtIO) Flush() error { d.mu.Lock(); defer d.mu.Unlock(); return d.request(4, 0, nil) }

func (d *VirtIO) descriptor(index int, address uint64, length uint32, flags, next uint16) {
	b := d.buf[index*16 : index*16+16]
	binary.LittleEndian.PutUint64(b, address)
	binary.LittleEndian.PutUint32(b[8:], length)
	binary.LittleEndian.PutUint16(b[12:], flags)
	binary.LittleEndian.PutUint16(b[14:], next)
}

func (d *VirtIO) poison(err error) error {
	d.poisoned = true
	if d.dmaBuf != nil {
		d.dmaBuf.Poison(err)
	}
	return err
}

func (d *VirtIO) usedIndex() uint16 {
	return uint16(atomic.LoadUint32((*uint32)(unsafe.Pointer(&d.buf[usedOffset]))) >> 16)
}

func (d *VirtIO) request(operation uint32, lba uint64, data []byte) error {
	if d.poisoned {
		return errors.New("VirtIO block queue is poisoned")
	}
	if regRead(d.base, 0x70)&64 != 0 {
		return d.poison(errors.New("VirtIO block requires reset"))
	}
	header := d.buf[headerOffset : headerOffset+16]
	clear(header)
	binary.LittleEndian.PutUint32(header, operation)
	binary.LittleEndian.PutUint64(header[8:], lba)
	d.buf[statusOffset] = 0xff
	next := uint16(2)
	if len(data) > 0 {
		next = 1
		flags := uint16(1)
		if operation == 0 {
			flags |= 2
		} else {
			copy(d.buf[dataOffset:], data)
		}
		d.descriptor(1, d.address+dataOffset, uint32(len(data)), flags, 2)
	}
	d.descriptor(0, d.address+headerOffset, 16, 1, next)
	d.descriptor(2, d.address+statusOffset, 1, 2, 0)
	binary.LittleEndian.PutUint16(d.buf[availableOffset+4+int(d.available%queueSize)*2:], 0)
	if err := d.dmaBuf.SyncForDevice(dmaops.Bidirectional); err != nil {
		return d.poison(err)
	}
	d.available++
	binary.LittleEndian.PutUint16(d.buf[availableOffset+2:], d.available)
	if err := d.dmaBuf.SyncForDevice(dmaops.ToDevice); err != nil {
		return d.poison(err)
	}
	if d.irqActive.Load() {
		select {
		case <-d.irqReady:
		default:
		}
	}
	regWrite(d.base, 0x50, 0)

	completed := false
	if d.irqActive.Load() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			if d.usedIndex() != d.used {
				completed = true
				break
			}
			if regRead(d.base, 0x70)&64 != 0 {
				break
			}
			select {
			case <-d.irqReady:
			case <-timer.C:
				if d.usedIndex() != d.used {
					completed = true
				}
				goto doneWait
			}
		}
	} else {
		deadline := time.Now().Add(2 * time.Second)
		for polls := 0; polls < 2000000; polls++ {
			if d.usedIndex() != d.used {
				completed = true
				break
			}
			if regRead(d.base, 0x70)&64 != 0 || time.Now().After(deadline) {
				break
			}
			runtime.Gosched()
		}
	}
doneWait:
	if !completed {
		return d.poison(errors.New("VirtIO block completion timeout; DMA remains owned"))
	}
	if err := d.dmaBuf.SyncForCPU(dmaops.FromDevice); err != nil {
		return d.poison(err)
	}
	if !d.irqActive.Load() {
		d.acknowledge()
	}
	index := d.usedIndex()
	used := d.buf[usedOffset+4+int(d.used%queueSize)*8:]
	if index != d.used+1 || binary.LittleEndian.Uint32(used) != 0 {
		return d.poison(errors.New("VirtIO block invalid completion"))
	}
	expectedWritten := uint32(1)
	if operation == 0 {
		expectedWritten += uint32(len(data))
	}
	if binary.LittleEndian.Uint32(used[4:]) != expectedWritten {
		return d.poison(errors.New("VirtIO block completion has an invalid written length"))
	}
	d.used = index
	if status := d.buf[statusOffset]; status != 0 {
		return fmt.Errorf("VirtIO block request status %d", status)
	}
	if operation == 0 {
		copy(data, d.buf[dataOffset:dataOffset+len(data)])
	}
	return nil
}
