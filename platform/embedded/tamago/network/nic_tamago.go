//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
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
	virtioMagic       = 0x74726976
	regFeatures       = 0x010
	regFeatureSelect  = 0x014
	regDriverFeatures = 0x020
	regDriverSelect   = 0x024
	regQueueSelect    = 0x030
	regQueueMax       = 0x034
	regQueueNum       = 0x038
	regQueueReady     = 0x044
	regNotify         = 0x050
	regInterrupt      = 0x060
	regInterruptAck   = 0x064
	regStatus         = 0x070
	regQueueDesc      = 0x080
	regQueueDriver    = 0x090
	regQueueDevice    = 0x0a0
	regGeneration     = 0x0fc
	regConfig         = 0x100
	featureVersion1   = uint64(1) << 32
	featureMAC        = uint64(1) << 5
	featureStatus     = uint64(1) << 16
	queueLimit        = 64
	packetBufferSize  = 2048
	netHeaderSize     = 12 // modern VirtIO version 1 header, without offloading
	packetMax         = 1518
)

type queue struct {
	ringBuf   *dmaops.Buffer
	dataBuf   *dmaops.Buffer
	bytes     []byte
	buffers   []byte
	size      uint16
	available uint16
	used      uint16
}

func (q *queue) poison(err error) {
	if q.ringBuf != nil {
		q.ringBuf.Poison(err)
	}
	if q.dataBuf != nil {
		q.dataBuf.Poison(err)
	}
}

func (q *queue) free() {
	if q.ringBuf != nil {
		_ = q.ringBuf.Free()
		q.ringBuf = nil
	}
	if q.dataBuf != nil {
		_ = q.dataBuf.Free()
		q.dataBuf = nil
	}
}

// NIC owns its dmaops allocations for the lifetime of this boot. A failed
// exchange poisons both the NIC and its underlying dmaops buffers rather than
// reusing buffers whose ownership is unclear.
type NIC struct {
	base      uint32
	rx        queue
	tx        queue
	rxMu      sync.Mutex
	txMu      sync.Mutex
	mac       net.HardwareAddr
	failed    atomic.Bool
	rxPackets atomic.Uint64
	txPackets atomic.Uint64
	dropped   atomic.Uint64
	rxReady   chan struct{}
	irqActive atomic.Bool
	irqCount  atomic.Uint64
}

func (n *NIC) poison(err error) {
	n.failed.Store(true)
	n.rx.poison(err)
	n.tx.poison(err)
}

func readMMIO(address uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(uintptr(address))))
}

func writeMMIO(address uint32, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(uintptr(address))), value)
}

func (n *NIC) read(offset uint32) uint32  { return readMMIO(n.base + offset) }
func (n *NIC) write(offset, value uint32) { writeMMIO(n.base+offset, value) }

func (n *NIC) resetDevice() bool {
	n.write(regStatus, 0)
	deadline := time.Now().Add(100 * time.Millisecond)
	for n.read(regStatus) != 0 {
		if time.Now().After(deadline) {
			return false
		}
		runtime.Gosched()
	}
	return true
}

func discover() (uint32, error) {
	var found uint32
	for base := uint32(0x0a000000); base < 0x0a004000; base += 0x200 {
		if readMMIO(base) == virtioMagic && readMMIO(base+4) == 2 && readMMIO(base+8) == 1 {
			if found != 0 {
				return 0, errors.New("network: multiple VirtIO NICs; select an MMIO address")
			}
			found = base
		}
	}
	if found == 0 {
		return 0, errors.New("network: no modern QEMU VirtIO NIC")
	}
	return found, nil
}

func openNIC(base uint32, pool *dmaops.Pool) (*NIC, error) {
	if pool != nil && pool.Mode() != dmaops.Uncached {
		return nil, errors.New("network: VirtIO NIC requires an uncached DMA pool")
	}
	var err error
	if base == 0 {
		base, err = discover()
		if err != nil {
			return nil, err
		}
	}
	n := &NIC{base: base, rxReady: make(chan struct{}, 1)}
	if n.read(0) != virtioMagic || n.read(4) != 2 || n.read(8) != 1 {
		return nil, errors.New("network: wrong VirtIO identity")
	}
	if !n.resetDevice() {
		return nil, errors.New("network: reset timeout")
	}
	n.write(regStatus, 1|2)
	n.write(regFeatureSelect, 0)
	features := uint64(n.read(regFeatures))
	n.write(regFeatureSelect, 1)
	features |= uint64(n.read(regFeatures)) << 32
	if features&(featureVersion1|featureMAC) != featureVersion1|featureMAC {
		n.write(regStatus, 128)
		return nil, errors.New("network: require modern VirtIO and device MAC")
	}
	features &= featureVersion1 | featureMAC | featureStatus
	n.write(regDriverSelect, 0)
	n.write(regDriverFeatures, uint32(features))
	n.write(regDriverSelect, 1)
	n.write(regDriverFeatures, uint32(features>>32))
	n.write(regStatus, 1|2|8)
	if n.read(regStatus)&8 == 0 {
		return nil, errors.New("network: feature negotiation rejected")
	}
	for tries := 0; tries < 16; tries++ {
		before := n.read(regGeneration)
		lo, hi := n.read(regConfig), n.read(regConfig+4)
		if before != n.read(regGeneration) {
			continue
		}
		n.mac = net.HardwareAddr{byte(lo), byte(lo >> 8), byte(lo >> 16), byte(lo >> 24), byte(hi), byte(hi >> 8)}
		if features&featureStatus != 0 && hi>>16&1 == 0 {
			return nil, errors.New("network: link is down")
		}
		break
	}
	if len(n.mac) != 6 || n.mac[0]&1 != 0 || (n.mac[0]|n.mac[1]|n.mac[2]|n.mac[3]|n.mac[4]|n.mac[5]) == 0 {
		return nil, errors.New("network: invalid or unstable MAC configuration")
	}
	if n.rx, err = n.setupQueue(0, true, pool); err != nil {
		n.write(regStatus, 128)
		return nil, err
	}
	if n.tx, err = n.setupQueue(1, false, pool); err != nil {
		// Reset device before reclaiming RX buffers; if reset stalls, poison
		// RX buffers so memory is never recycled while device may access it.
		if n.resetDevice() {
			n.rx.free()
		} else {
			n.rx.poison(err)
		}
		return nil, err
	}
	dmaops.Native().DataBarrier()
	n.write(regStatus, 1|2|8|4)
	n.write(regNotify, 0)
	return n, nil
}

func allocQueuePool(bytes int) (*dmaops.Pool, error) {
	addr, raw := dma.Reserve(bytes, 4096)
	if len(raw) != bytes {
		return nil, errors.New("network: DMA allocation failed")
	}
	return dmaops.NewPool(uint64(addr), raw, dmaops.Uncached, dmaops.Native())
}

func (n *NIC) setupQueue(index uint32, receive bool, pool *dmaops.Pool) (q queue, err error) {
	n.write(regQueueSelect, index)
	maximum := n.read(regQueueMax)
	if maximum < 2 || n.read(regQueueReady) != 0 {
		return q, errors.New("network: VirtIO queue unavailable")
	}
	q.size = queueLimit
	for uint32(q.size) > maximum {
		q.size /= 2
	}
	count := 1
	if receive {
		count = int(q.size)
	}
	dataBytes := count * packetBufferSize
	qPool := pool
	if qPool == nil {
		qPool, err = allocQueuePool(4096 + dataBytes)
		if err != nil {
			return q, err
		}
	}
	q.ringBuf, err = qPool.Alloc(4096, 4096)
	if err != nil {
		return q, fmt.Errorf("network: ring DMA allocation failed: %w", err)
	}
	q.dataBuf, err = qPool.Alloc(uint64(dataBytes), 64)
	if err != nil {
		_ = q.ringBuf.Free()
		q.ringBuf = nil
		return q, fmt.Errorf("network: buffer DMA allocation failed: %w", err)
	}
	q.bytes = q.ringBuf.Bytes()
	q.buffers = q.dataBuf.Bytes()
	// Zero explicitly so stale used indices or descriptor flags cannot survive.
	clear(q.bytes)
	clear(q.buffers)
	bufBase := q.dataBuf.PhysAddr()
	for i := 0; i < count; i++ {
		descriptor := q.bytes[i*16 : (i+1)*16]
		binary.LittleEndian.PutUint64(descriptor, bufBase+uint64(i*packetBufferSize))
		binary.LittleEndian.PutUint32(descriptor[8:], packetBufferSize)
		if receive {
			binary.LittleEndian.PutUint16(descriptor[12:], 2)
		}
		binary.LittleEndian.PutUint16(q.bytes[1028+i*2:], uint16(i))
	}
	binary.LittleEndian.PutUint16(q.bytes[1024:], 1) // suppress interrupts; polling owns completion
	if receive {
		q.available = q.size
		binary.LittleEndian.PutUint16(q.bytes[1026:], q.available)
	}
	ringAddr := q.ringBuf.PhysAddr()
	n.write(regQueueNum, uint32(q.size))
	for _, field := range []struct {
		offset  uint32
		address uint64
	}{
		{regQueueDesc, ringAddr}, {regQueueDriver, ringAddr + 1024}, {regQueueDevice, ringAddr + 2048},
	} {
		n.write(field.offset, uint32(field.address))
		n.write(field.offset+4, uint32(field.address>>32))
	}
	if err := q.ringBuf.SyncForDevice(dmaops.ToDevice); err != nil {
		q.free()
		return queue{}, err
	}
	n.write(regQueueReady, 1)
	return q, nil
}

func usedIndex(q *queue) uint16 {
	dmaops.Native().DataBarrier()
	return loadShared16(q.bytes[2050:])
}

// The used index is naturally aligned and written by the device. Callers
// acquire DMA contents with a barrier after observing a new index.
func loadShared16(b []byte) uint16 { return *(*uint16)(unsafe.Pointer(&b[0])) }

func (n *NIC) acknowledge() {
	if bits := n.read(regInterrupt); bits != 0 {
		n.write(regInterruptAck, bits)
		if n.irqActive.Load() {
			select {
			case n.rxReady <- struct{}{}:
			default:
			}
		}
	}
}

func (n *NIC) irqID() (uint32, bool) {
	if n.base < 0x0a000000 || n.base >= 0x0a004000 || (n.base-0x0a000000)%0x200 != 0 {
		return 0, false
	}
	// QEMU virt wires VirtIO-MMIO slots 0..31 to SPI 16..47 (INTID 48..79).
	return 48 + (n.base-0x0a000000)/0x200, true
}

// RXReady returns the RX interrupt wakeup channel while IRQ mode is active.
func (n *NIC) RXReady() <-chan struct{} {
	if !n.irqActive.Load() {
		return nil
	}
	return n.rxReady
}

// IRQCount returns the number of VirtIO-net interrupts handled since EnableIRQ.
func (n *NIC) IRQCount() uint64 { return n.irqCount.Load() }

// EnableIRQ routes this VirtIO-net slot's GICv3 SPI to wake Link.Start on RX.
// Call after g.Init(); g.Serve must run for interrupts to be dispatched.
func (n *NIC) EnableIRQ(g *gic.Controller) error {
	if g == nil {
		return errors.New("network: nil GIC controller")
	}
	id, ok := n.irqID()
	if !ok || id >= g.Lines() {
		return gic.ErrInvalidConfig
	}
	n.rxMu.Lock()
	defer n.rxMu.Unlock()
	g.Disable(id)
	n.irqCount.Store(0)
	// Clear AVAIL_F_NO_INTERRUPT on RX queue; keep TX queue suppressed.
	binary.LittleEndian.PutUint16(n.rx.bytes[1024:], 0)
	dmaops.Native().DataBarrier()
	n.acknowledge()
	g.Register(id, func() {
		n.irqCount.Add(1)
		n.acknowledge()
	})
	n.irqActive.Store(true)
	g.Enable(id)
	select {
	case n.rxReady <- struct{}{}:
	default:
	}
	return nil
}

// DisableIRQ re-suppresses VirtIO RX interrupts and masks the GICv3 SPI.
func (n *NIC) DisableIRQ(g *gic.Controller) {
	n.rxMu.Lock()
	defer n.rxMu.Unlock()
	binary.LittleEndian.PutUint16(n.rx.bytes[1024:], 1)
	dmaops.Native().DataBarrier()
	if g != nil {
		if id, ok := n.irqID(); ok {
			g.Disable(id)
			g.Register(id, nil)
		}
	}
	n.irqActive.Store(false)
	n.acknowledge()
}

func (n *NIC) Receive(destination []byte) (int, error) {
	n.rxMu.Lock()
	defer n.rxMu.Unlock()
	if n.failed.Load() {
		return 0, errors.New("network: NIC is poisoned")
	}
	if n.read(regStatus)&(64|4) != 4 {
		err := errors.New("network: NIC stopped or requires reset")
		n.poison(err)
		return 0, err
	}
	q := &n.rx
	pending := uint16(usedIndex(q) - q.used)
	if pending == 0 {
		n.acknowledge()
		return 0, nil
	}
	if pending > q.size {
		err := errors.New("network: impossible RX completion count")
		n.poison(err)
		return 0, err
	}
	dmaops.Native().DataBarrier()
	entry := q.bytes[2052+int(q.used%q.size)*8:]
	id := binary.LittleEndian.Uint32(entry)
	size := binary.LittleEndian.Uint32(entry[4:])
	if id >= uint32(q.size) || size > packetBufferSize {
		err := errors.New("network: invalid RX DMA completion")
		n.poison(err)
		return 0, err
	}
	packet := q.buffers[int(id)*packetBufferSize : int(id)*packetBufferSize+int(size)]
	var err error
	length := int(size) - netHeaderSize
	if size < netHeaderSize+14 || length > len(destination) || length > packetMax {
		err = errors.New("network: malformed or oversized RX frame")
	} else if packet[0] != 0 || packet[1] != 0 {
		err = errors.New("network: unexpected offloaded RX frame")
	} else {
		copy(destination, packet[netHeaderSize:])
		n.rxPackets.Add(1)
	}
	q.used++
	binary.LittleEndian.PutUint16(q.bytes[1028+int(q.available%q.size)*2:], uint16(id))
	q.available++
	dmaops.Native().DataBarrier()
	binary.LittleEndian.PutUint16(q.bytes[1026:], q.available)
	dmaops.Native().DataBarrier()
	n.write(regNotify, 0)
	n.acknowledge()
	if err != nil {
		n.dropped.Add(1)
		return 0, err
	}
	return length, nil
}

func (n *NIC) Transmit(frame []byte) error {
	n.txMu.Lock()
	defer n.txMu.Unlock()
	if n.failed.Load() {
		return errors.New("network: NIC is poisoned")
	}
	if n.read(regStatus)&(64|4) != 4 {
		err := errors.New("network: NIC stopped or requires reset")
		n.poison(err)
		return err
	}
	if len(frame) < 14 || len(frame) > packetMax {
		return errors.New("network: invalid TX frame size")
	}
	q := &n.tx
	if usedIndex(q) != q.used {
		err := errors.New("network: unexpected TX completion")
		n.poison(err)
		return err
	}
	clear(q.buffers[:netHeaderSize])
	copy(q.buffers[netHeaderSize:], frame)
	binary.LittleEndian.PutUint32(q.bytes[8:], uint32(netHeaderSize+len(frame)))
	binary.LittleEndian.PutUint16(q.bytes[1028+int(q.available%q.size)*2:], 0)
	q.available++
	dmaops.Native().DataBarrier()
	binary.LittleEndian.PutUint16(q.bytes[1026:], q.available)
	dmaops.Native().DataBarrier()
	n.write(regNotify, 1)
	deadline := time.Now().Add(time.Second)
	for usedIndex(q) == q.used {
		if time.Now().After(deadline) {
			err := errors.New("network: TX completion timeout")
			n.poison(err)
			return err
		}
		runtime.Gosched()
	}
	if uint16(usedIndex(q)-q.used) != 1 {
		err := errors.New("network: invalid TX completion count")
		n.poison(err)
		return err
	}
	dmaops.Native().DataBarrier()
	entry := q.bytes[2052+int(q.used%q.size)*8:]
	if binary.LittleEndian.Uint32(entry) != 0 {
		err := fmt.Errorf("network: unexpected TX descriptor")
		n.poison(err)
		return err
	}
	q.used++
	n.txPackets.Add(1)
	n.acknowledge()
	return nil
}

func (n *NIC) Statistics() Stats {
	return Stats{Received: n.rxPackets.Load(), Transmitted: n.txPackets.Load(), Dropped: n.dropped.Load()}
}

func (n *NIC) MAC() string { return n.mac.String() }
func (n *NIC) Err() error {
	if n.failed.Load() {
		return errors.New("network: VirtIO NIC failed")
	}
	return nil
}
