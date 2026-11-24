// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"net/netip"
	"time"
)

const (
	ethHeaderLen   = 14
	ipv4MinLen     = 20
	udpHeaderLen   = 8
	bootpFixedLen  = 236
	dhcpCookieLen  = 4
	dhcpMinPayload = bootpFixedLen + dhcpCookieLen

	ethTypeIPv4 = 0x0800
	ipProtoUDP  = 17

	dhcpServerPort = 67
	dhcpClientPort = 68

	bootpRequest = 1
	bootpReply   = 2

	dhcpMsgDiscover = 1
	dhcpMsgOffer    = 2
	dhcpMsgRequest  = 3
	dhcpMsgAck      = 5
	dhcpMsgNak      = 6

	dhcpOptPad         = 0
	dhcpOptSubnetMask  = 1
	dhcpOptRouter      = 3
	dhcpOptDNS         = 6
	dhcpOptHostname    = 12
	dhcpOptRequestedIP = 50
	dhcpOptLeaseTime   = 51
	dhcpOptMsgType     = 53
	dhcpOptServerID    = 54
	dhcpOptParamList   = 55
	dhcpOptEnd         = 255

	dhcpFlagBroadcast = 0x8000

	defaultDHCPInitialInterval = 250 * time.Millisecond
	defaultDHCPMaxInterval     = 2 * time.Second
	defaultDHCPTimeout         = 10 * time.Second
	maxDHCPRequestRetries      = 3
)

var (
	dhcpMagicCookie = [4]byte{99, 130, 83, 99}
	broadcastMAC    = [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

	// ErrDHCPInvalidConfig is returned when DHCPConfig or device MAC is invalid.
	ErrDHCPInvalidConfig = errors.New("network: invalid DHCP configuration")
)

// DHCPLease describes an acquired and validated IPv4 DHCPv4 lease.
type DHCPLease struct {
	Address       string        // IPv4 host CIDR prefix, e.g. "10.0.2.15/24"
	Gateway       string        // IPv4 router address within the leased subnet
	Server        string        // DHCP server identifier (option 54)
	DNS           []string      // Optional DNS server IPv4 addresses (option 6)
	LeaseDuration time.Duration // Optional IP address lease duration (option 51)
}

// DHCPConfig tunes RFC 2131 DHCPv4 lease acquisition. Zero values select safe defaults.
type DHCPConfig struct {
	Hostname        string        // Optional DHCP option 12 hostname (default "fes-native")
	InitialInterval time.Duration // Initial retransmit interval (default 250ms)
	MaxInterval     time.Duration // Maximum retransmit interval (default 2s)
	Timeout         time.Duration // Upper bound when ctx has no deadline (default 10s; <0 invalid)
	XID             uint32        // Fixed transaction ID for deterministic tests (0 -> random)
}

type dhcpParsedReply struct {
	msgType   byte
	yiaddr    [4]byte
	mask      [4]byte
	hasMask   bool
	routers   [][4]byte
	dns       [][4]byte
	serverID  [4]byte
	hasServer bool
	leaseTime time.Duration
}

// ValidateDHCPConfig validates DHCPConfig without touching any network device.
func ValidateDHCPConfig(cfg DHCPConfig) error {
	return validateDHCPConfig(cfg)
}

func validateDHCPConfig(cfg DHCPConfig) error {
	if cfg.InitialInterval < 0 || cfg.MaxInterval < 0 || cfg.Timeout < 0 {
		return fmt.Errorf("%w: negative duration", ErrDHCPInvalidConfig)
	}
	if len(cfg.Hostname) > 255 {
		return fmt.Errorf("%w: hostname exceeds 255 bytes", ErrDHCPInvalidConfig)
	}
	for i := 0; i < len(cfg.Hostname); i++ {
		if cfg.Hostname[i] < 0x20 || cfg.Hostname[i] > 0x7e {
			return fmt.Errorf("%w: non-ASCII hostname", ErrDHCPInvalidConfig)
		}
	}
	return nil
}

// AcquireDHCP runs an RFC 2131 DHCPv4 handshake (DISCOVER -> OFFER -> REQUEST -> ACK)
// directly over dev before an IP stack claims its receive path.
func AcquireDHCP(ctx context.Context, dev Device, cfg DHCPConfig) (DHCPLease, error) {
	if ctx == nil || dev == nil {
		return DHCPLease{}, fmt.Errorf("%w: nil context or device", ErrDHCPInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return DHCPLease{}, err
	}
	mac, err := parseUnicastMAC(dev.MAC())
	if err != nil {
		return DHCPLease{}, fmt.Errorf("%w: %v", ErrDHCPInvalidConfig, err)
	}
	if err := validateDHCPConfig(cfg); err != nil {
		return DHCPLease{}, err
	}

	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "fes-native"
	}
	initialInterval := cfg.InitialInterval
	if initialInterval == 0 {
		initialInterval = defaultDHCPInitialInterval
	}
	maxInterval := cfg.MaxInterval
	if maxInterval == 0 {
		maxInterval = defaultDHCPMaxInterval
	}
	if maxInterval < initialInterval {
		maxInterval = initialInterval
	}

	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDHCPTimeout)
		defer cancel()
	}

	fixedXID := cfg.XID != 0
	xid := cfg.XID
	if !fixedXID {
		xid = randomXID()
	}

	rxBuf := make([]byte, 1518)
	for {
		if err := ctx.Err(); err != nil {
			return DHCPLease{}, err
		}
		if err := dev.Err(); err != nil {
			return DHCPLease{}, err
		}

		// Phase 1: DISCOVER -> OFFER.
		offer, err := dhcpExchange(ctx, dev, rxBuf, mac, xid, initialInterval, maxInterval, 0,
			buildDHCPFrame(mac, xid, dhcpMsgDiscover, hostname, [4]byte{}, [4]byte{}),
			func(r dhcpParsedReply) (bool, bool) {
				if r.msgType != dhcpMsgOffer {
					return false, false
				}
				if _, ok := buildValidatedLease(r); !ok {
					return false, false
				}
				return true, false
			})
		if err != nil {
			return DHCPLease{}, err
		}

		// Phase 2: REQUEST -> ACK (or NAK / retry exhaustion -> restart DISCOVER).
		reqFrame := buildDHCPFrame(mac, xid, dhcpMsgRequest, hostname, offer.yiaddr, offer.serverID)
		restarted := false
		ack, err := dhcpExchange(ctx, dev, rxBuf, mac, xid, initialInterval, maxInterval, maxDHCPRequestRetries,
			reqFrame,
			func(r dhcpParsedReply) (bool, bool) {
				if r.msgType == dhcpMsgNak {
					if !r.hasServer || r.serverID == offer.serverID {
						restarted = true
						return false, true
					}
					return false, false
				}
				if r.msgType != dhcpMsgAck {
					return false, false
				}
				if r.hasServer && r.serverID != offer.serverID {
					return false, false
				}
				if r.yiaddr != offer.yiaddr {
					return false, false
				}
				if _, ok := buildValidatedLease(r); !ok {
					return false, false
				}
				return true, false
			})
		if err != nil {
			if errors.Is(err, errDHCPRestart) {
				if !fixedXID {
					xid = randomXID()
				}
				continue
			}
			return DHCPLease{}, err
		}
		if restarted {
			if !fixedXID {
				xid = randomXID()
			}
			continue
		}
		lease, ok := buildValidatedLease(ack)
		if !ok {
			if !fixedXID {
				xid = randomXID()
			}
			continue
		}
		return lease, nil
	}
}

var errDHCPRestart = errors.New("network: restart DHCP discovery")

func dhcpExchange(
	ctx context.Context,
	dev Device,
	rxBuf []byte,
	mac [6]byte,
	xid uint32,
	initialInterval, maxInterval time.Duration,
	maxAttempts int,
	txFrame []byte,
	match func(dhcpParsedReply) (accepted bool, abort bool),
) (dhcpParsedReply, error) {
	var waiter <-chan struct{}
	if w, ok := dev.(rxWaiter); ok {
		waiter = w.RXReady()
	}

	interval := initialInterval
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return dhcpParsedReply{}, err
		}
		if err := dev.Err(); err != nil {
			return dhcpParsedReply{}, err
		}
		if maxAttempts > 0 && attempts >= maxAttempts {
			return dhcpParsedReply{}, errDHCPRestart
		}
		if err := dev.Transmit(txFrame); err != nil {
			return dhcpParsedReply{}, err
		}
		attempts++

		nextRetry := time.Now().Add(interval)
		for {
			if err := ctx.Err(); err != nil {
				return dhcpParsedReply{}, err
			}
			if err := dev.Err(); err != nil {
				return dhcpParsedReply{}, err
			}
			n, err := dev.Receive(rxBuf)
			if err != nil {
				return dhcpParsedReply{}, err
			}
			if n > 0 && n <= len(rxBuf) {
				if reply, ok := parseDHCPFrame(rxBuf[:n], mac, xid); ok {
					accepted, abort := match(reply)
					if abort {
						return dhcpParsedReply{}, errDHCPRestart
					}
					if accepted {
						return reply, nil
					}
				}
				if time.Until(nextRetry) <= 0 {
					break
				}
				continue
			}

			rem := time.Until(nextRetry)
			if rem <= 0 {
				break
			}
			if waiter != nil {
				timer := time.NewTimer(rem)
				select {
				case <-ctx.Done():
					timer.Stop()
					return dhcpParsedReply{}, ctx.Err()
				case <-waiter:
					timer.Stop()
				case <-timer.C:
				}
			} else {
				step := rem
				if step > 2*time.Millisecond {
					step = 2 * time.Millisecond
				}
				timer := time.NewTimer(step)
				select {
				case <-ctx.Done():
					timer.Stop()
					return dhcpParsedReply{}, ctx.Err()
				case <-timer.C:
				}
			}
		}

		interval *= 2
		if interval > maxInterval {
			interval = maxInterval
		}
	}
}

func parseUnicastMAC(s string) ([6]byte, error) {
	var out [6]byte
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 || hw[0]&1 != 0 {
		return out, errors.New("invalid unicast MAC")
	}
	copy(out[:], hw)
	if out == ([6]byte{}) {
		return out, errors.New("zero MAC")
	}
	return out, nil
}

func randomXID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		if x := binary.BigEndian.Uint32(b[:]); x != 0 {
			return x
		}
	}
	return uint32(time.Now().UnixNano()) | 1
}

func maskPrefixBits(mask [4]byte) (int, bool) {
	m := binary.BigEndian.Uint32(mask[:])
	ones := bits.LeadingZeros32(^m)
	if ones < 1 || ones > 30 {
		return 0, false
	}
	expected := ^uint32(0) << (32 - ones)
	if m != expected {
		return 0, false
	}
	return ones, true
}

func buildValidatedLease(r dhcpParsedReply) (DHCPLease, bool) {
	if !r.hasServer || !r.hasMask {
		return DHCPLease{}, false
	}
	prefixBits, ok := maskPrefixBits(r.mask)
	if !ok {
		return DHCPLease{}, false
	}
	addr := netip.AddrFrom4(r.yiaddr)
	prefix := netip.PrefixFrom(addr, prefixBits)
	server := netip.AddrFrom4(r.serverID)
	if server.IsUnspecified() || server.IsMulticast() {
		return DHCPLease{}, false
	}

	var gw string
	for _, candidate := range r.routers {
		g := netip.AddrFrom4(candidate)
		if _, err := staticConfig(prefix.String(), g.String()); err == nil {
			gw = g.String()
			break
		}
	}
	if gw == "" {
		return DHCPLease{}, false
	}
	cfg, err := staticConfig(prefix.String(), gw)
	if err != nil {
		return DHCPLease{}, false
	}

	var dns []string
	for _, d := range r.dns {
		a := netip.AddrFrom4(d)
		if !a.IsUnspecified() && !a.IsMulticast() {
			dns = append(dns, a.String())
		}
	}
	return DHCPLease{
		Address:       cfg.Address,
		Gateway:       cfg.Gateway,
		Server:        server.String(),
		DNS:           dns,
		LeaseDuration: r.leaseTime,
	}, true
}

func buildDHCPFrame(mac [6]byte, xid uint32, msgType byte, hostname string, requestedIP, serverID [4]byte) []byte {
	var opts bytes.Buffer
	opts.Write(dhcpMagicCookie[:])
	opts.Write([]byte{dhcpOptMsgType, 1, msgType})
	if msgType == dhcpMsgRequest {
		opts.Write([]byte{dhcpOptRequestedIP, 4})
		opts.Write(requestedIP[:])
		opts.Write([]byte{dhcpOptServerID, 4})
		opts.Write(serverID[:])
	}
	if hostname != "" {
		opts.Write([]byte{dhcpOptHostname, byte(len(hostname))})
		opts.WriteString(hostname)
	}
	opts.Write([]byte{dhcpOptParamList, 5, dhcpOptSubnetMask, dhcpOptRouter, dhcpOptDNS, dhcpOptLeaseTime, dhcpOptServerID})
	opts.WriteByte(dhcpOptEnd)
	// Pad DHCP options to at least 64 bytes so BOOTP payload meets RFC 2131 minimum 300 bytes.
	for opts.Len() < 64 {
		opts.WriteByte(dhcpOptPad)
	}

	dhcpLen := bootpFixedLen + opts.Len()
	udpLen := udpHeaderLen + dhcpLen
	ipLen := ipv4MinLen + udpLen
	frame := make([]byte, ethHeaderLen+ipLen)

	// Ethernet header.
	copy(frame[0:6], broadcastMAC[:])
	copy(frame[6:12], mac[:])
	binary.BigEndian.PutUint16(frame[12:14], ethTypeIPv4)

	// IPv4 header (0.0.0.0 -> 255.255.255.255).
	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	binary.BigEndian.PutUint16(ip[4:6], uint16(xid))
	ip[8] = 64 // TTL
	ip[9] = ipProtoUDP
	copy(ip[16:20], []byte{255, 255, 255, 255})
	binary.BigEndian.PutUint16(ip[10:12], internetChecksum(ip))

	// UDP header (68 -> 67).
	udp := frame[ethHeaderLen+ipv4MinLen:]
	binary.BigEndian.PutUint16(udp[0:2], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))

	// BOOTP header + DHCP options.
	bootp := udp[udpHeaderLen:]
	bootp[0] = bootpRequest
	bootp[1] = 1 // Ethernet
	bootp[2] = 6 // MAC len
	binary.BigEndian.PutUint32(bootp[4:8], xid)
	binary.BigEndian.PutUint16(bootp[10:12], dhcpFlagBroadcast)
	copy(bootp[28:34], mac[:])
	copy(bootp[bootpFixedLen:], opts.Bytes())

	// UDP checksum with IPv4 pseudo-header.
	csum := udpChecksum(ip[12:16], ip[16:20], udp)
	if csum == 0 {
		csum = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:8], csum)
	return frame
}

func parseDHCPFrame(frame []byte, mac [6]byte, xid uint32) (dhcpParsedReply, bool) {
	if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dhcpMinPayload {
		return dhcpParsedReply{}, false
	}
	if !bytes.Equal(frame[0:6], broadcastMAC[:]) && !bytes.Equal(frame[0:6], mac[:]) {
		return dhcpParsedReply{}, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIPv4 {
		return dhcpParsedReply{}, false
	}

	ip := frame[ethHeaderLen:]
	if ip[0]>>4 != 4 {
		return dhcpParsedReply{}, false
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < ipv4MinLen || ihl > len(ip) {
		return dhcpParsedReply{}, false
	}
	totalLen := int(binary.BigEndian.Uint16(ip[2:4]))
	if totalLen < ihl+udpHeaderLen+dhcpMinPayload || totalLen > len(ip) {
		return dhcpParsedReply{}, false
	}
	// Reject fragmented IPv4 datagrams (MF bit or non-zero fragment offset).
	if binary.BigEndian.Uint16(ip[6:8])&0x3fff != 0 {
		return dhcpParsedReply{}, false
	}
	if ip[9] != ipProtoUDP {
		return dhcpParsedReply{}, false
	}
	if internetChecksum(ip[:ihl]) != 0 {
		return dhcpParsedReply{}, false
	}

	udp := ip[ihl:totalLen]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if srcPort != dhcpServerPort || dstPort != dhcpClientPort || udpLen < udpHeaderLen+dhcpMinPayload || udpLen > len(udp) {
		return dhcpParsedReply{}, false
	}
	udp = udp[:udpLen]
	if binary.BigEndian.Uint16(udp[6:8]) != 0 {
		if udpChecksum(ip[12:16], ip[16:20], udp) != 0 {
			return dhcpParsedReply{}, false
		}
	}

	bootp := udp[udpHeaderLen:]
	if bootp[0] != bootpReply || bootp[1] != 1 || bootp[2] != 6 {
		return dhcpParsedReply{}, false
	}
	if binary.BigEndian.Uint32(bootp[4:8]) != xid {
		return dhcpParsedReply{}, false
	}
	if !bytes.Equal(bootp[28:34], mac[:]) {
		return dhcpParsedReply{}, false
	}
	if !bytes.Equal(bootp[bootpFixedLen:dhcpMinPayload], dhcpMagicCookie[:]) {
		return dhcpParsedReply{}, false
	}

	var out dhcpParsedReply
	copy(out.yiaddr[:], bootp[16:20])

	opts := bootp[dhcpMinPayload:]
	for i := 0; i < len(opts); {
		code := opts[i]
		if code == dhcpOptPad {
			i++
			continue
		}
		if code == dhcpOptEnd {
			break
		}
		if i+2 > len(opts) {
			return dhcpParsedReply{}, false
		}
		optLen := int(opts[i+1])
		if i+2+optLen > len(opts) {
			return dhcpParsedReply{}, false
		}
		val := opts[i+2 : i+2+optLen]
		switch code {
		case dhcpOptMsgType:
			if optLen != 1 {
				return dhcpParsedReply{}, false
			}
			out.msgType = val[0]
		case dhcpOptSubnetMask:
			if optLen != 4 {
				return dhcpParsedReply{}, false
			}
			copy(out.mask[:], val)
			out.hasMask = true
		case dhcpOptRouter:
			if optLen == 0 || optLen%4 != 0 {
				return dhcpParsedReply{}, false
			}
			for j := 0; j < optLen; j += 4 {
				var r [4]byte
				copy(r[:], val[j:j+4])
				out.routers = append(out.routers, r)
			}
		case dhcpOptDNS:
			if optLen == 0 || optLen%4 != 0 {
				return dhcpParsedReply{}, false
			}
			for j := 0; j < optLen; j += 4 {
				var d [4]byte
				copy(d[:], val[j:j+4])
				out.dns = append(out.dns, d)
			}
		case dhcpOptLeaseTime:
			if optLen != 4 {
				return dhcpParsedReply{}, false
			}
			out.leaseTime = time.Duration(binary.BigEndian.Uint32(val)) * time.Second
		case dhcpOptServerID:
			if optLen != 4 {
				return dhcpParsedReply{}, false
			}
			copy(out.serverID[:], val)
			out.hasServer = true
		}
		i += 2 + optLen
	}
	if out.msgType == 0 {
		return dhcpParsedReply{}, false
	}
	return out, true
}

func internetChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func udpChecksum(srcIP, dstIP, udp []byte) uint16 {
	var sum uint32
	sum += uint32(binary.BigEndian.Uint16(srcIP[0:2]))
	sum += uint32(binary.BigEndian.Uint16(srcIP[2:4]))
	sum += uint32(binary.BigEndian.Uint16(dstIP[0:2]))
	sum += uint32(binary.BigEndian.Uint16(dstIP[2:4]))
	sum += uint32(ipProtoUDP)
	sum += uint32(len(udp))
	for i := 0; i+1 < len(udp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(udp[i : i+2]))
	}
	if len(udp)%2 == 1 {
		sum += uint32(udp[len(udp)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
