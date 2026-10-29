//go:build tamago && arm64

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

	"github.com/usbarmory/tamago/dma"
)

const (
	queueSize       = 8
	maxTransfer     = 16 * SectorSize
	dmaBytes        = 16384
	availableOffset = 128
	usedOffset      = 256
	headerOffset    = 512
	dataOffset      = 1024
	statusOffset    = dataOffset + maxTransfer
	featureVersion1 = uint64(1) << 32
	featureRO       = uint64(1) << 5
	featureFlush    = uint64(1) << 9
)

// VirtIO implements one polled VirtIO 1.x MMIO block queue. Its buffers are
// owned until completion; an ambiguous timeout poisons the device. The caller
// must initialize TamaGo's global DMA arena outside the Go heap, with coherent
// or uncached mappings, before opening it. This is a QEMU backend, not SD.
type VirtIO struct {
	mu        sync.Mutex
	base      uint32
	sectors   uint64
	readonly  bool
	flush     bool
	address   uint
	buf       []byte
	available uint16
	used      uint16
	poisoned  bool
}

func regRead(base, offset uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(uintptr(base + offset))))
}
func regWrite(base, offset, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(uintptr(base+offset))), value)
}
func memoryBarrier()

// ScanVirtIO finds a block device in QEMU virt's 32-slot MMIO window.
// These are emulator addresses only and must not be used on DE25 hardware.
func ScanVirtIO() (*VirtIO, error) {
	for i := uint32(0); i < 32; i++ {
		base := uint32(0x0a000000) + i*0x200
		if regRead(base, 0) == 0x74726976 && regRead(base, 8) == 2 {
			return OpenVirtIO(base)
		}
	}
	return nil, errors.New("no QEMU VirtIO block device")
}

func OpenVirtIO(base uint32) (*VirtIO, error) {
	if base == 0 || regRead(base, 0) != 0x74726976 || regRead(base, 4) != 2 || regRead(base, 8) != 2 {
		return nil, errors.New("expected VirtIO 1.x MMIO block device")
	}
	d := &VirtIO{base: base}
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
	d.address, d.buf = dma.Reserve(dmaBytes, 4096)
	if len(d.buf) != dmaBytes {
		regWrite(base, 0x70, 128)
		return nil, errors.New("VirtIO block DMA allocation failed")
	}
	clear(d.buf)
	binary.LittleEndian.PutUint16(d.buf[availableOffset:], 1) // no interrupts
	regWrite(base, 0x38, queueSize)
	for _, pair := range [][2]uint64{{0x80, uint64(d.address)}, {0x90, uint64(d.address) + availableOffset}, {0xa0, uint64(d.address) + usedOffset}} {
		regWrite(base, uint32(pair[0]), uint32(pair[1]))
		regWrite(base, uint32(pair[0])+4, uint32(pair[1]>>32))
	}
	memoryBarrier()
	regWrite(base, 0x44, 1)
	regWrite(base, 0x70, 15)
	return d, nil
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

func (d *VirtIO) request(operation uint32, lba uint64, data []byte) error {
	if d.poisoned {
		return errors.New("VirtIO block queue is poisoned")
	}
	if regRead(d.base, 0x70)&64 != 0 {
		d.poisoned = true
		return errors.New("VirtIO block requires reset")
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
		d.descriptor(1, uint64(d.address)+dataOffset, uint32(len(data)), flags, 2)
	}
	d.descriptor(0, uint64(d.address)+headerOffset, 16, 1, next)
	d.descriptor(2, uint64(d.address)+statusOffset, 1, 2, 0)
	binary.LittleEndian.PutUint16(d.buf[availableOffset+4+int(d.available%queueSize)*2:], 0)
	memoryBarrier()
	d.available++
	binary.LittleEndian.PutUint16(d.buf[availableOffset+2:], d.available)
	memoryBarrier()
	regWrite(d.base, 0x50, 0)
	deadline := time.Now().Add(2 * time.Second)
	completed := false
	for polls := 0; polls < 2000000; polls++ {
		index := uint16(atomic.LoadUint32((*uint32)(unsafe.Pointer(&d.buf[usedOffset]))) >> 16)
		if index != d.used {
			completed = true
			break
		}
		if regRead(d.base, 0x70)&64 != 0 || time.Now().After(deadline) {
			break
		}
		runtime.Gosched()
	}
	if !completed {
		d.poisoned = true
		return errors.New("VirtIO block completion timeout; DMA remains owned")
	}
	memoryBarrier()
	index := uint16(atomic.LoadUint32((*uint32)(unsafe.Pointer(&d.buf[usedOffset]))) >> 16)
	used := d.buf[usedOffset+4+int(d.used%queueSize)*8:]
	if index != d.used+1 || binary.LittleEndian.Uint32(used) != 0 {
		d.poisoned = true
		return errors.New("VirtIO block invalid completion")
	}
	expectedWritten := uint32(1)
	if operation == 0 {
		expectedWritten += uint32(len(data))
	}
	if binary.LittleEndian.Uint32(used[4:]) != expectedWritten {
		d.poisoned = true
		return errors.New("VirtIO block completion has an invalid written length")
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
