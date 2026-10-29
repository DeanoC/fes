// SPDX-License-Identifier: GPL-3.0-or-later

// Package network connects the native Go socket API to the emulated VirtIO
// network device. Its MMIO discovery addresses describe QEMU, not DE25.
package network

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

// Config selects one QEMU interface. The address is a static IPv4 prefix;
// DHCP, DNS server discovery and the Nano's MAC/PHY are separate work.
type Config struct {
	Address     string
	Gateway     string
	MMIOAddress uint32 // zero discovers a modern VirtIO net device in QEMU virt
}

func (c Config) normalized() (Config, error) {
	if c.Address == "" {
		c.Address = "10.0.2.15/24"
	}
	if c.Gateway == "" {
		c.Gateway = "10.0.2.2"
	}
	prefix, err := netip.ParsePrefix(c.Address)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 || prefix.Bits() < 1 ||
		!usableHost(prefix, prefix.Addr()) {
		return Config{}, fmt.Errorf("network: require an IPv4 host prefix")
	}
	gw, err := netip.ParseAddr(c.Gateway)
	if err != nil || !gw.Is4() || !prefix.Contains(gw) || gw == prefix.Addr() || !usableHost(prefix, gw) {
		return Config{}, fmt.Errorf("network: gateway must be another IPv4 address in the subnet")
	}
	if c.MMIOAddress != 0 && (c.MMIOAddress < 0x0a000000 || c.MMIOAddress >= 0x0a004000 || c.MMIOAddress%0x200 != 0) {
		return Config{}, fmt.Errorf("network: address is outside QEMU virt's VirtIO window")
	}
	c.Address, c.Gateway = prefix.String(), gw.String()
	return c, nil
}

func usableHost(prefix netip.Prefix, addr netip.Addr) bool {
	if addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	a := addr.As4()
	hostMask := ^uint32(0) >> prefix.Bits()
	host := binary.BigEndian.Uint32(a[:]) & hostMask
	return host != 0 && host != hostMask
}
