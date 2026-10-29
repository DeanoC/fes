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

func barrier()

type queue struct {
	address       uint
	bytes         []byte
	size          uint16
	available     uint16
	used          uint16
	buffers       []byte
	bufferAddress uint
}

// NIC owns its DMA allocations for the lifetime of this boot. A failed
// exchange poisons it rather than reusing buffers whose ownership is unclear.
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
}

type Stats struct{ Received, Transmitted, Dropped uint64 }

func readMMIO(address uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(uintptr(address))))
}

func writeMMIO(address uint32, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(uintptr(address))), value)
}

func (n *NIC) read(offset uint32) uint32  { return readMMIO(n.base + offset) }
func (n *NIC) write(offset, value uint32) { writeMMIO(n.base+offset, value) }

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

func openNIC(base uint32) (*NIC, error) {
	var err error
	if base == 0 {
		base, err = discover()
		if err != nil {
			return nil, err
		}
	}
	n := &NIC{base: base}
	if n.read(0) != virtioMagic || n.read(4) != 2 || n.read(8) != 1 {
		return nil, errors.New("network: wrong VirtIO identity")
	}
	n.write(regStatus, 0)
	deadline := time.Now().Add(100 * time.Millisecond)
	for n.read(regStatus) != 0 {
		if time.Now().After(deadline) {
			return nil, errors.New("network: reset timeout")
		}
		runtime.Gosched()
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
	if n.rx, err = n.setupQueue(0, true); err != nil {
		return nil, err
	}
	if n.tx, err = n.setupQueue(1, false); err != nil {
		return nil, err
	}
	barrier()
	n.write(regStatus, 1|2|8|4)
	n.write(regNotify, 0)
	return n, nil
}

func (n *NIC) setupQueue(index uint32, receive bool) (q queue, err error) {
	n.write(regQueueSelect, index)
	maximum := n.read(regQueueMax)
	if maximum < 2 || n.read(regQueueReady) != 0 {
		return q, errors.New("network: VirtIO queue unavailable")
	}
	q.size = queueLimit
	for uint32(q.size) > maximum {
		q.size /= 2
	}
	q.address, q.bytes = dma.Reserve(4096, 4096)
	count := 1
	if receive {
		count = int(q.size)
	}
	q.bufferAddress, q.buffers = dma.Reserve(count*packetBufferSize, 16)
	if q.address == 0 || q.bufferAddress == 0 {
		return q, errors.New("network: DMA allocation failed")
	}
	// Reserve does not zero memory. In particular, stale used indices or
	// descriptor flags must not survive a firmware-retained RAM region.
	clear(q.bytes)
	clear(q.buffers)
	for i := 0; i < count; i++ {
		descriptor := q.bytes[i*16 : (i+1)*16]
		binary.LittleEndian.PutUint64(descriptor, uint64(q.bufferAddress+uint(i*packetBufferSize)))
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
	n.write(regQueueNum, uint32(q.size))
	for _, field := range []struct {
		offset  uint32
		address uint
	}{
		{regQueueDesc, q.address}, {regQueueDriver, q.address + 1024}, {regQueueDevice, q.address + 2048},
	} {
		n.write(field.offset, uint32(field.address))
		n.write(field.offset+4, uint32(uint64(field.address)>>32))
	}
	barrier()
	n.write(regQueueReady, 1)
	return q, nil
}

func usedIndex(q *queue) uint16 {
	barrier()
	return loadShared16(q.bytes[2050:])
}

// The used index is naturally aligned and written by the device. Callers
// acquire DMA contents with a barrier after observing a new index.
func loadShared16(b []byte) uint16 { return *(*uint16)(unsafe.Pointer(&b[0])) }

func (n *NIC) acknowledge() {
	if bits := n.read(regInterrupt); bits != 0 {
		n.write(regInterruptAck, bits)
	}
}

func (n *NIC) Receive(destination []byte) (int, error) {
	n.rxMu.Lock()
	defer n.rxMu.Unlock()
	if n.failed.Load() {
		return 0, errors.New("network: NIC is poisoned")
	}
	if n.read(regStatus)&(64|4) != 4 {
		n.failed.Store(true)
		return 0, errors.New("network: NIC stopped or requires reset")
	}
	q := &n.rx
	pending := uint16(usedIndex(q) - q.used)
	if pending == 0 {
		n.acknowledge()
		return 0, nil
	}
	if pending > q.size {
		n.failed.Store(true)
		return 0, errors.New("network: impossible RX completion count")
	}
	barrier()
	entry := q.bytes[2052+int(q.used%q.size)*8:]
	id := binary.LittleEndian.Uint32(entry)
	size := binary.LittleEndian.Uint32(entry[4:])
	if id >= uint32(q.size) || size > packetBufferSize {
		n.failed.Store(true)
		return 0, errors.New("network: invalid RX DMA completion")
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
	barrier()
	binary.LittleEndian.PutUint16(q.bytes[1026:], q.available)
	barrier()
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
		n.failed.Store(true)
		return errors.New("network: NIC stopped or requires reset")
	}
	if len(frame) < 14 || len(frame) > packetMax {
		return errors.New("network: invalid TX frame size")
	}
	q := &n.tx
	if usedIndex(q) != q.used {
		n.failed.Store(true)
		return errors.New("network: unexpected TX completion")
	}
	clear(q.buffers[:netHeaderSize])
	copy(q.buffers[netHeaderSize:], frame)
	binary.LittleEndian.PutUint32(q.bytes[8:], uint32(netHeaderSize+len(frame)))
	binary.LittleEndian.PutUint16(q.bytes[1028+int(q.available%q.size)*2:], 0)
	q.available++
	barrier()
	binary.LittleEndian.PutUint16(q.bytes[1026:], q.available)
	barrier()
	n.write(regNotify, 1)
	deadline := time.Now().Add(time.Second)
	for usedIndex(q) == q.used {
		if time.Now().After(deadline) {
			n.failed.Store(true)
			return errors.New("network: TX completion timeout")
		}
		runtime.Gosched()
	}
	if uint16(usedIndex(q)-q.used) != 1 {
		n.failed.Store(true)
		return errors.New("network: invalid TX completion count")
	}
	barrier()
	entry := q.bytes[2052+int(q.used%q.size)*8:]
	if binary.LittleEndian.Uint32(entry) != 0 {
		n.failed.Store(true)
		return fmt.Errorf("network: unexpected TX descriptor")
	}
	q.used++
	n.txPackets.Add(1)
	n.acknowledge()
	return nil
}

func (n *NIC) Statistics() Stats {
	return Stats{Received: n.rxPackets.Load(), Transmitted: n.txPackets.Load(), Dropped: n.dropped.Load()}
}
