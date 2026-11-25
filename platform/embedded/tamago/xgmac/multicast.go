// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"math/bits"
	"net"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	hashTableBase = 0x010
	filterHMC     = uint32(1 << 2)
)

var (
	// ErrMulticastUnsupported is returned when SetMulticastMACs configures one or
	// more groups on an XGMAC instance whose HW_FEATURE1 reports no hash table.
	ErrMulticastUnsupported = errors.New("xgmac: multicast hash filtering unsupported by MAC features")
	// ErrInvalidMulticast is returned when an address passed to SetMulticastMACs
	// or MulticastHashIndex is not a 6-byte non-broadcast multicast MAC address.
	ErrInvalidMulticast = errors.New("xgmac: invalid multicast MAC address")
	broadcastMAC        = []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

func decodeHashTableBits(features1 uint32) int {
	switch (features1 >> 24) & 3 {
	case 1:
		return 64
	case 2:
		return 128
	case 3:
		return 256
	default:
		return 0
	}
}

// MulticastHashIndex computes the Synopsys XGMAC 2.10 multicast hash table bit
// index (bit-reversed Ethernet CRC-32 MSBs, per Linux dwxgmac2_set_filter) for a
// non-broadcast 6-byte multicast MAC address and tableBits in {64, 128, 256}.
func MulticastHashIndex(addr net.HardwareAddr, tableBits int) (uint32, error) {
	if len(addr) != 6 || addr[0]&1 == 0 || bytes.Equal(addr, broadcastMAC) {
		return 0, fmt.Errorf("%w: %v", ErrInvalidMulticast, addr)
	}
	var shift uint
	switch tableBits {
	case 64:
		shift = 26
	case 128:
		shift = 25
	case 256:
		shift = 24
	default:
		return 0, fmt.Errorf("xgmac: invalid hash table width %d", tableBits)
	}
	return bits.Reverse32(crc32.ChecksumIEEE(addr)) >> shift, nil
}

// MulticastHashBits reports the hardware multicast hash table width in bits
// decoded from MAC_HW_Feature1.HASHTBLSZ (0, 64, 128, or 256).
func (d *Device) MulticastHashBits() int {
	if d == nil {
		return 0
	}
	return int(d.hashBits.Load())
}

// SetMulticastMACs programs the Synopsys XGMAC 2.10 hash table registers and
// MAC_Packet_Filter.HMC to accept only the hashed multicast groups in addrs
// alongside unicast and broadcast frames without enabling promiscuous mode.
// Passing an empty slice clears all hash registers and disables HMC. The
// programmed filter persists across BindControl.Stop/Start cycles.
func (d *Device) SetMulticastMACs(addrs []net.HardwareAddr) error {
	if d == nil {
		return network.ErrControl
	}
	for _, a := range addrs {
		if len(a) != 6 || a[0]&1 == 0 || bytes.Equal(a, broadcastMAC) {
			return fmt.Errorf("%w: %v", ErrInvalidMulticast, a)
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.Err(); err != nil {
		return err
	}
	if d.paused {
		return network.ErrPaused
	}
	hashBits := int(d.hashBits.Load())
	if len(addrs) > 0 && hashBits == 0 {
		return ErrMulticastUnsupported
	}

	words := hashBits / 32
	var hash [8]uint32
	var filter uint32
	if len(addrs) > 0 {
		filter = filterHMC
		for _, a := range addrs {
			idx, err := MulticastHashIndex(a, hashBits)
			if err != nil {
				return err
			}
			hash[idx/32] |= uint32(1) << (idx % 32)
		}
	}

	if err := d.programMulticastLocked(words, hash, filter); err != nil {
		return d.fail(err)
	}
	d.multicastWords = words
	d.multicastHash = hash
	d.multicastFilter = filter
	return nil
}

func (d *Device) programMulticastLocked(words int, hash [8]uint32, filter uint32) error {
	for i := 0; i < words; i++ {
		reg := hashTableBase + uint32(i)*4
		d.io.Write32(reg, hash[i])
		if got := d.io.Read32(reg); got != hash[i] {
			return fmt.Errorf("xgmac: hash register %#x readback mismatch (%#x != %#x)", reg, got, hash[i])
		}
	}
	d.io.Write32(packetFilter, filter)
	if got := d.io.Read32(packetFilter); got != filter {
		return fmt.Errorf("xgmac: packet filter readback mismatch (%#x != %#x)", got, filter)
	}
	return nil
}
