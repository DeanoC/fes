// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const (
	ethTypeARP     = 0x0806
	arpHeaderLen   = 28
	arpOpRequest   = 1
	arpOpReply     = 2
	dnsPort        = 53
	dnsHeaderLen   = 12
	dnsTypeA       = 1
	dnsTypeCNAME   = 5
	dnsClassIN     = 1
	dnsMaxNameLen  = 253
	dnsMaxLabelLen = 63
	dnsMaxJumps    = 16

	defaultDNSTimeout       = 5 * time.Second
	defaultDNSRetryInterval = 250 * time.Millisecond
)

var (
	// ErrDNSInvalidConfig is returned when DNSConfig or the queried hostname is invalid.
	ErrDNSInvalidConfig = errors.New("network: invalid DNS configuration or name")
	// ErrDNSNotFound is returned when the DNS server replies with NXDOMAIN (RCODE 3).
	ErrDNSNotFound = errors.New("network: DNS name not found")
	// ErrDNSNoRecords is returned when a DNS query succeeds with no matching A records.
	ErrDNSNoRecords = errors.New("network: DNS response contained no A records")
	// ErrDNSTruncated is returned when a DNS response sets the TC (truncated) bit.
	ErrDNSTruncated = errors.New("network: DNS response truncated")
	// ErrDNSServerError is returned when the DNS server returns a non-zero error RCODE.
	ErrDNSServerError = errors.New("network: DNS server error")
)

// RawFramePort is the minimal raw Ethernet frame interface needed for ARP/DNS/DHCP
// exchanges over an active Link (satisfied by *Link).
type RawFramePort interface {
	SendRawFrame(frame []byte) error
	AddTap(fn func(frame []byte)) (remove func())
	Err() error
}

// DNSConfig configures RFC 1035 DNSv4 A-record resolution over a RawFramePort.
type DNSConfig struct {
	LocalMAC      string        // Unicast client MAC address
	LocalPrefix   string        // Client IPv4 CIDR prefix, e.g. "10.0.2.15/24"
	Gateway       string        // Default IPv4 gateway in LocalPrefix (used for off-subnet DNS servers)
	Servers       []string      // Ordered list of IPv4 DNS server addresses
	NextHopMAC    string        // Optional pre-resolved next-hop MAC (skips ARP when set)
	Timeout       time.Duration // Overall deadline when ctx has none (default 5s)
	RetryInterval time.Duration // Retransmit interval per attempt (default 250ms)
	QueryID       uint16        // Optional fixed DNS transaction ID for deterministic tests (0 -> random)
	SourcePort    uint16        // Optional fixed UDP source port for deterministic tests (0 -> random ephemeral)
}

// EncodeDNSQuery encodes an RFC 1035 recursive IN/A query for name.
func EncodeDNSQuery(id uint16, name string) ([]byte, error) {
	wireName, err := encodeDNSName(name)
	if err != nil {
		return nil, err
	}
	msg := make([]byte, dnsHeaderLen+len(wireName)+4)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // Standard query, RD=1
	binary.BigEndian.PutUint16(msg[4:6], 1)      // QDCOUNT=1
	copy(msg[dnsHeaderLen:], wireName)
	off := dnsHeaderLen + len(wireName)
	binary.BigEndian.PutUint16(msg[off:off+2], dnsTypeA)
	binary.BigEndian.PutUint16(msg[off+2:off+4], dnsClassIN)
	return msg, nil
}

// ParseDNSResponse parses an RFC 1035 DNSv4 response for expectedID and queryName,
// following any CNAME aliases in the Answer section and returning all matching IPv4 A records.
func ParseDNSResponse(msg []byte, expectedID uint16, queryName string) ([]netip.Addr, error) {
	canonicalQuery, err := normalizeDNSName(queryName)
	if err != nil {
		return nil, err
	}
	if len(msg) < dnsHeaderLen {
		return nil, errors.New("network: short DNS response")
	}
	if binary.BigEndian.Uint16(msg[0:2]) != expectedID {
		return nil, errors.New("network: DNS response ID mismatch")
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	if flags&0x8000 == 0 {
		return nil, errors.New("network: DNS packet is not a response")
	}

	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))
	off := dnsHeaderLen

	// Validate the question before honoring TC or error RCODEs so a delayed or
	// unrelated response sharing the same ID cannot falsely fail this lookup.
	matchedQuestion := false
	for i := 0; i < qdCount; i++ {
		qname, next, err := decodeDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		if next+4 > len(msg) {
			return nil, errors.New("network: truncated DNS question")
		}
		qtype := binary.BigEndian.Uint16(msg[next : next+2])
		qclass := binary.BigEndian.Uint16(msg[next+2 : next+4])
		off = next + 4
		if strings.EqualFold(qname, canonicalQuery) && qtype == dnsTypeA && qclass == dnsClassIN {
			matchedQuestion = true
		}
	}
	if !matchedQuestion {
		return nil, errors.New("network: DNS question mismatch")
	}
	if flags&0x0200 != 0 {
		return nil, ErrDNSTruncated
	}
	rcode := flags & 0x000f
	if rcode == 3 {
		return nil, ErrDNSNotFound
	}
	if rcode != 0 {
		return nil, fmt.Errorf("%w: rcode=%d", ErrDNSServerError, rcode)
	}

	type aRec struct {
		name string
		addr netip.Addr
	}
	cnames := make(map[string]string)
	var aRecords []aRec

	for i := 0; i < anCount; i++ {
		rname, next, err := decodeDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		if next+10 > len(msg) {
			return nil, errors.New("network: truncated DNS answer header")
		}
		rtype := binary.BigEndian.Uint16(msg[next : next+2])
		rclass := binary.BigEndian.Uint16(msg[next+2 : next+4])
		rdLen := int(binary.BigEndian.Uint16(msg[next+8 : next+10]))
		rdataOff := next + 10
		if rdataOff+rdLen > len(msg) {
			return nil, errors.New("network: truncated DNS answer rdata")
		}
		off = rdataOff + rdLen
		if rclass != dnsClassIN {
			continue
		}
		lowerName := strings.ToLower(rname)
		switch rtype {
		case dnsTypeA:
			if rdLen != 4 {
				return nil, errors.New("network: invalid DNS A record length")
			}
			var ip4 [4]byte
			copy(ip4[:], msg[rdataOff:rdataOff+4])
			aRecords = append(aRecords, aRec{name: lowerName, addr: netip.AddrFrom4(ip4)})
		case dnsTypeCNAME:
			target, targetEnd, err := decodeDNSName(msg, rdataOff)
			if err != nil || targetEnd != off {
				return nil, errors.New("network: invalid DNS CNAME rdata")
			}
			cnames[lowerName] = strings.ToLower(target)
		}
	}

	// Follow any CNAME chain from canonicalQuery with cycle guard.
	target := strings.ToLower(canonicalQuery)
	visited := make(map[string]bool)
	for !visited[target] {
		visited[target] = true
		next, ok := cnames[target]
		if !ok {
			break
		}
		target = next
	}

	var result []netip.Addr
	for _, rec := range aRecords {
		if rec.name == target {
			result = append(result, rec.addr)
		}
	}
	if len(result) == 0 {
		return nil, ErrDNSNoRecords
	}
	return result, nil
}

// ResolveA resolves name to one or more IPv4 addresses using the configured
// DNSv4 servers over port. Numeric IPv4 literals return immediately.
func ResolveA(ctx context.Context, port RawFramePort, cfg DNSConfig, name string) ([]netip.Addr, error) {
	if ctx == nil || port == nil {
		return nil, fmt.Errorf("%w: nil context or port", ErrDNSInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(name)
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		if !addr.Is4() {
			return nil, fmt.Errorf("%w: IPv6 literal unsupported", ErrDNSInvalidConfig)
		}
		return []netip.Addr{addr}, nil
	}
	if _, err := normalizeDNSName(trimmed); err != nil {
		return nil, err
	}
	localMAC, err := parseUnicastMAC(cfg.LocalMAC)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDNSInvalidConfig, err)
	}
	localPrefix, err := netip.ParsePrefix(cfg.LocalPrefix)
	if err != nil || !localPrefix.Addr().Is4() || localPrefix.Bits() < 1 || localPrefix.Bits() > 30 || !usableHost(localPrefix, localPrefix.Addr()) {
		return nil, fmt.Errorf("%w: invalid local IPv4 prefix", ErrDNSInvalidConfig)
	}
	var gateway netip.Addr
	if cfg.Gateway != "" {
		if _, err := staticConfig(cfg.LocalPrefix, cfg.Gateway); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDNSInvalidConfig, err)
		}
		gateway = netip.MustParseAddr(cfg.Gateway)
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("%w: no DNS servers configured", ErrDNSInvalidConfig)
	}
	servers := make([]netip.Addr, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() {
			return nil, fmt.Errorf("%w: invalid DNS server %q", ErrDNSInvalidConfig, s)
		}
		servers = append(servers, a)
	}
	var fixedNextHop [6]byte
	hasFixedNextHop := false
	if cfg.NextHopMAC != "" {
		fixedNextHop, err = parseUnicastMAC(cfg.NextHopMAC)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid NextHopMAC: %v", ErrDNSInvalidConfig, err)
		}
		hasFixedNextHop = true
	}
	if cfg.Timeout < 0 || cfg.RetryInterval < 0 {
		return nil, fmt.Errorf("%w: negative duration", ErrDNSInvalidConfig)
	}

	retryInterval := cfg.RetryInterval
	if retryInterval == 0 {
		retryInterval = defaultDNSRetryInterval
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDNSTimeout)
		defer cancel()
	}

	var lastErr error
	for i, server := range servers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := port.Err(); err != nil {
			return nil, err
		}

		nextHop := server
		if !localPrefix.Contains(server) {
			if !gateway.IsValid() {
				lastErr = fmt.Errorf("%w: off-subnet DNS server %s requires gateway", ErrDNSInvalidConfig, server)
				continue
			}
			nextHop = gateway
		}

		var dstMAC [6]byte
		if hasFixedNextHop {
			dstMAC = fixedNextHop
		} else {
			// Share remaining context budget across remaining servers so an unreachable
			// primary server fails over to secondary servers before overall expiry.
			arpCtx, arpCancel := perServerContext(ctx, len(servers)-i)
			dstMAC, err = resolveARP(arpCtx, port, localMAC, localPrefix.Addr().As4(), nextHop.As4(), retryInterval)
			arpCancel()
			if err != nil {
				lastErr = err
				if ctx.Err() != nil || port.Err() != nil {
					return nil, err
				}
				continue
			}
		}

		queryID := cfg.QueryID
		if queryID == 0 {
			queryID = randomUint16()
		}
		srcPort := cfg.SourcePort
		if srcPort == 0 {
			srcPort = 49152 + randomUint16()%16384
		}

		queryPayload, err := EncodeDNSQuery(queryID, trimmed)
		if err != nil {
			return nil, err
		}
		txFrame := buildUDPv4Frame(localMAC, dstMAC, localPrefix.Addr().As4(), server.As4(), srcPort, dnsPort, queryID, queryPayload)

		srvCtx, srvCancel := perServerContext(ctx, len(servers)-i)
		addrs, err := exchangeDNS(srvCtx, port, txFrame, localMAC, localPrefix.Addr().As4(), server.As4(), srcPort, queryID, trimmed, retryInterval)
		srvCancel()
		if err == nil {
			return addrs, nil
		}
		lastErr = err
		if errors.Is(err, ErrDNSNotFound) || errors.Is(err, ErrDNSNoRecords) || ctx.Err() != nil || port.Err() != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrDNSNoRecords
}

func perServerContext(parent context.Context, remainingServers int) (context.Context, context.CancelFunc) {
	if remainingServers <= 1 {
		return context.WithCancel(parent)
	}
	deadline, ok := parent.Deadline()
	if !ok {
		return context.WithCancel(parent)
	}
	rem := time.Until(deadline)
	if rem <= 0 {
		return context.WithCancel(parent)
	}
	slice := rem / time.Duration(remainingServers)
	if slice < time.Millisecond {
		slice = time.Millisecond
	}
	return context.WithTimeout(parent, slice)
}

func resolveARP(ctx context.Context, port RawFramePort, localMAC [6]byte, localIP, targetIP [4]byte, retryInterval time.Duration) ([6]byte, error) {
	replyCh := make(chan [6]byte, 1)
	remove := port.AddTap(func(frame []byte) {
		if mac, ok := parseARPReply(frame, localMAC, targetIP); ok {
			select {
			case replyCh <- mac:
			default:
			}
		}
	})
	defer remove()

	req := buildARPRequest(localMAC, localIP, targetIP)
	interval := retryInterval
	for {
		if err := ctx.Err(); err != nil {
			return [6]byte{}, err
		}
		if err := port.Err(); err != nil {
			return [6]byte{}, err
		}
		if err := port.SendRawFrame(req); err != nil {
			return [6]byte{}, err
		}
		timer := time.NewTimer(interval)
		select {
		case mac := <-replyCh:
			timer.Stop()
			return mac, nil
		case <-ctx.Done():
			timer.Stop()
			return [6]byte{}, ctx.Err()
		case <-timer.C:
		}
		if interval < time.Second {
			interval *= 2
		}
	}
}

func exchangeDNS(
	ctx context.Context,
	port RawFramePort,
	txFrame []byte,
	localMAC [6]byte,
	localIP [4]byte,
	serverIP [4]byte,
	srcPort uint16,
	queryID uint16,
	queryName string,
	retryInterval time.Duration,
) ([]netip.Addr, error) {
	type dnsOutcome struct {
		addrs []netip.Addr
		err   error
	}
	replyCh := make(chan dnsOutcome, 1)
	remove := port.AddTap(func(frame []byte) {
		payload, ok := parseUDPv4Reply(frame, localMAC, serverIP, localIP, dnsPort, srcPort)
		if !ok {
			return
		}
		addrs, err := ParseDNSResponse(payload, queryID, queryName)
		// Ignore packets that don't match our query ID / question (e.g., stray DNS traffic).
		if err != nil && !errors.Is(err, ErrDNSNotFound) && !errors.Is(err, ErrDNSNoRecords) &&
			!errors.Is(err, ErrDNSTruncated) && !errors.Is(err, ErrDNSServerError) {
			return
		}
		select {
		case replyCh <- dnsOutcome{addrs: addrs, err: err}:
		default:
		}
	})
	defer remove()

	interval := retryInterval
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := port.Err(); err != nil {
			return nil, err
		}
		if err := port.SendRawFrame(txFrame); err != nil {
			return nil, err
		}
		timer := time.NewTimer(interval)
		select {
		case out := <-replyCh:
			timer.Stop()
			return out.addrs, out.err
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if interval < 2*time.Second {
			interval *= 2
		}
	}
}

func buildARPRequest(localMAC [6]byte, localIP, targetIP [4]byte) []byte {
	// Pad to 60-byte minimum Ethernet frame payload (excluding FCS).
	frame := make([]byte, 60)
	copy(frame[0:6], broadcastMAC[:])
	copy(frame[6:12], localMAC[:])
	binary.BigEndian.PutUint16(frame[12:14], ethTypeARP)

	arp := frame[ethHeaderLen : ethHeaderLen+arpHeaderLen]
	binary.BigEndian.PutUint16(arp[0:2], 1)           // Ethernet
	binary.BigEndian.PutUint16(arp[2:4], ethTypeIPv4) // IPv4
	arp[4] = 6
	arp[5] = 4
	binary.BigEndian.PutUint16(arp[6:8], arpOpRequest)
	copy(arp[8:14], localMAC[:])
	copy(arp[14:18], localIP[:])
	copy(arp[24:28], targetIP[:])
	return frame
}

func parseARPReply(frame []byte, localMAC [6]byte, targetIP [4]byte) ([6]byte, bool) {
	if len(frame) < ethHeaderLen+arpHeaderLen {
		return [6]byte{}, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != ethTypeARP {
		return [6]byte{}, false
	}
	if !bytes.Equal(frame[0:6], localMAC[:]) && !bytes.Equal(frame[0:6], broadcastMAC[:]) {
		return [6]byte{}, false
	}
	arp := frame[ethHeaderLen : ethHeaderLen+arpHeaderLen]
	if binary.BigEndian.Uint16(arp[0:2]) != 1 || binary.BigEndian.Uint16(arp[2:4]) != ethTypeIPv4 ||
		arp[4] != 6 || arp[5] != 4 || binary.BigEndian.Uint16(arp[6:8]) != arpOpReply {
		return [6]byte{}, false
	}
	if !bytes.Equal(arp[14:18], targetIP[:]) {
		return [6]byte{}, false
	}
	var senderMAC [6]byte
	copy(senderMAC[:], arp[8:14])
	if senderMAC == ([6]byte{}) || senderMAC[0]&1 != 0 {
		return [6]byte{}, false
	}
	return senderMAC, true
}

func buildUDPv4Frame(srcMAC, dstMAC [6]byte, srcIP, dstIP [4]byte, srcPort, dstPort, ipID uint16, payload []byte) []byte {
	udpLen := udpHeaderLen + len(payload)
	ipLen := ipv4MinLen + udpLen
	frame := make([]byte, ethHeaderLen+ipLen)

	copy(frame[0:6], dstMAC[:])
	copy(frame[6:12], srcMAC[:])
	binary.BigEndian.PutUint16(frame[12:14], ethTypeIPv4)

	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	binary.BigEndian.PutUint16(ip[4:6], ipID)
	ip[8] = 64
	ip[9] = ipProtoUDP
	copy(ip[12:16], srcIP[:])
	copy(ip[16:20], dstIP[:])
	binary.BigEndian.PutUint16(ip[10:12], internetChecksum(ip))

	udp := frame[ethHeaderLen+ipv4MinLen:]
	binary.BigEndian.PutUint16(udp[0:2], srcPort)
	binary.BigEndian.PutUint16(udp[2:4], dstPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	copy(udp[udpHeaderLen:], payload)

	csum := udpChecksum(ip[12:16], ip[16:20], udp)
	if csum == 0 {
		csum = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:8], csum)
	return frame
}

func parseUDPv4Reply(frame []byte, localMAC [6]byte, expectedSrcIP, expectedDstIP [4]byte, expectedSrcPort, expectedDstPort uint16) ([]byte, bool) {
	if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen {
		return nil, false
	}
	if !bytes.Equal(frame[0:6], localMAC[:]) && !bytes.Equal(frame[0:6], broadcastMAC[:]) {
		return nil, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIPv4 {
		return nil, false
	}
	ip := frame[ethHeaderLen:]
	if ip[0]>>4 != 4 {
		return nil, false
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < ipv4MinLen || ihl > len(ip) {
		return nil, false
	}
	totalLen := int(binary.BigEndian.Uint16(ip[2:4]))
	if totalLen < ihl+udpHeaderLen || totalLen > len(ip) {
		return nil, false
	}
	if binary.BigEndian.Uint16(ip[6:8])&0x3fff != 0 || ip[9] != ipProtoUDP {
		return nil, false
	}
	if !bytes.Equal(ip[12:16], expectedSrcIP[:]) || !bytes.Equal(ip[16:20], expectedDstIP[:]) {
		return nil, false
	}
	if internetChecksum(ip[:ihl]) != 0 {
		return nil, false
	}
	udp := ip[ihl:totalLen]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if srcPort != expectedSrcPort || dstPort != expectedDstPort || udpLen < udpHeaderLen || udpLen > len(udp) {
		return nil, false
	}
	udp = udp[:udpLen]
	if binary.BigEndian.Uint16(udp[6:8]) != 0 {
		if udpChecksum(ip[12:16], ip[16:20], udp) != 0 {
			return nil, false
		}
	}
	return udp[udpHeaderLen:], true
}

func normalizeDNSName(name string) (string, error) {
	s := strings.TrimSpace(name)
	s = strings.TrimSuffix(s, ".")
	if len(s) == 0 || len(s) > dnsMaxNameLen {
		return "", fmt.Errorf("%w: invalid hostname length", ErrDNSInvalidConfig)
	}
	labels := strings.Split(s, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > dnsMaxLabelLen {
			return "", fmt.Errorf("%w: invalid DNS label %q", ErrDNSInvalidConfig, label)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
			if !valid {
				return "", fmt.Errorf("%w: invalid character in DNS label %q", ErrDNSInvalidConfig, label)
			}
		}
	}
	return strings.ToLower(s), nil
}

func encodeDNSName(name string) ([]byte, error) {
	norm, err := normalizeDNSName(name)
	if err != nil {
		return nil, err
	}
	labels := strings.Split(norm, ".")
	out := make([]byte, 0, len(norm)+2)
	for _, label := range labels {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0)
	return out, nil
}

func decodeDNSName(msg []byte, offset int) (string, int, error) {
	if offset < 0 || offset >= len(msg) {
		return "", 0, errors.New("network: DNS name offset out of range")
	}
	var labels []string
	cur := offset
	endOffset := 0
	jumped := false
	jumps := 0
	totalLen := 0

	for {
		if cur >= len(msg) {
			return "", 0, errors.New("network: truncated DNS name")
		}
		length := int(msg[cur])
		if length == 0 {
			if !jumped {
				endOffset = cur + 1
			}
			break
		}
		switch length & 0xc0 {
		case 0xc0:
			if cur+1 >= len(msg) {
				return "", 0, errors.New("network: truncated DNS compression pointer")
			}
			ptr := int(binary.BigEndian.Uint16(msg[cur:cur+2]) & 0x3fff)
			if ptr >= len(msg) {
				return "", 0, errors.New("network: DNS compression pointer out of range")
			}
			if !jumped {
				endOffset = cur + 2
				jumped = true
			}
			jumps++
			if jumps > dnsMaxJumps {
				return "", 0, errors.New("network: DNS compression pointer loop")
			}
			cur = ptr
		case 0x00:
			cur++
			if cur+length > len(msg) {
				return "", 0, errors.New("network: truncated DNS label")
			}
			totalLen += length + 1
			if totalLen > dnsMaxNameLen+1 {
				return "", 0, errors.New("network: DNS name exceeds maximum length")
			}
			labels = append(labels, string(msg[cur:cur+length]))
			cur += length
		default:
			return "", 0, errors.New("network: invalid DNS label encoding")
		}
	}
	return strings.Join(labels, "."), endOffset, nil
}

func randomUint16() uint16 {
	var b [2]byte
	if _, err := rand.Read(b[:]); err == nil {
		if v := binary.BigEndian.Uint16(b[:]); v != 0 {
			return v
		}
	}
	return uint16(time.Now().UnixNano()) | 1
}
