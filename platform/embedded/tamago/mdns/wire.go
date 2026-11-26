// SPDX-License-Identifier: GPL-3.0-or-later

// Package mdns implements pure-Go RFC 6762 multicast DNS and RFC 6763 DNS-SD
// over network.RawFramePort, including FogCast _fogcast._tcp discovery framing.
package mdns

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	// Port is the standard RFC 6762 mDNS UDP port.
	Port uint16 = 5353

	// DefaultTTL is the default RFC 6762 host/service record lifetime in seconds.
	DefaultTTL uint32 = 120

	// DNS record types relevant to mDNS / DNS-SD discovery.
	TypeA   uint16 = 1
	TypePTR uint16 = 12
	TypeTXT uint16 = 16
	TypeSRV uint16 = 33
	TypeANY uint16 = 255

	ClassIN         uint16 = 1
	ClassCacheFlush uint16 = 0x8001
	QUFlag          uint16 = 0x8000

	ethHeaderLen   = 14
	ethTypeIPv4    = 0x0800
	ipv4MinLen     = 20
	ipProtoUDP     = 17
	udpHeaderLen   = 8
	dnsHeaderLen   = 12
	mdnsIPv4TTL    = 255
	dnsMaxNameLen  = 253
	dnsMaxLabelLen = 63
	dnsMaxJumps    = 16
	maxTXTOctetLen = 255
)

var (
	// MulticastIP is the IPv4 link-local mDNS multicast address (224.0.0.251).
	MulticastIP = netip.AddrFrom4([4]byte{224, 0, 0, 251})

	multicastIPv4Bytes = [4]byte{224, 0, 0, 251}
	multicastMACBytes  = [6]byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0xfb}

	// ErrInvalidConfig is returned when mDNS responder or browser configuration is invalid.
	ErrInvalidConfig = errors.New("mdns: invalid configuration")
)

// MulticastMAC returns the Ethernet multicast MAC address for 224.0.0.251.
func MulticastMAC() [6]byte {
	return multicastMACBytes
}

type dnsQuestion struct {
	Name   string
	Type   uint16
	Class  uint16
	WantQU bool
}

type dnsRecord struct {
	Name       string
	Type       uint16
	Class      uint16
	CacheFlush bool
	TTL        uint32
	A          netip.Addr
	PTR        string
	SRVPort    uint16
	SRVTarget  string
	TXT        []string
	RawRData   []byte
}

type dnsMessage struct {
	ID          uint16
	Response    bool
	Questions   []dnsQuestion
	Answers     []dnsRecord
	Authorities []dnsRecord
	Additional  []dnsRecord
}

func normalizeHostName(name string) (string, error) {
	s := strings.TrimSpace(name)
	s = strings.TrimSuffix(s, ".")
	if len(s) == 0 || len(s) > dnsMaxNameLen {
		return "", fmt.Errorf("%w: invalid DNS hostname length", ErrInvalidConfig)
	}
	labels := strings.Split(s, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > dnsMaxLabelLen {
			return "", fmt.Errorf("%w: invalid DNS label %q", ErrInvalidConfig, label)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
			if !valid {
				return "", fmt.Errorf("%w: invalid character in DNS label %q", ErrInvalidConfig, label)
			}
		}
	}
	return strings.ToLower(s), nil
}

func validateServiceType(serviceType string) (string, error) {
	norm, err := normalizeHostName(serviceType)
	if err != nil {
		return "", err
	}
	parts := strings.Split(norm, ".")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "_") || (parts[1] != "_tcp" && parts[1] != "_udp") {
		return "", fmt.Errorf("%w: invalid DNS-SD service type %q", ErrInvalidConfig, serviceType)
	}
	return norm, nil
}

func validateInstanceLabel(instance string) (string, error) {
	if len(instance) == 0 || len(instance) > dnsMaxLabelLen {
		return "", fmt.Errorf("%w: invalid DNS-SD instance length", ErrInvalidConfig)
	}
	if strings.IndexByte(instance, 0) >= 0 {
		return "", fmt.Errorf("%w: NUL byte in DNS-SD instance", ErrInvalidConfig)
	}
	return instance, nil
}

func encodeLabels(labels []string) ([]byte, error) {
	total := 1
	for _, label := range labels {
		if len(label) == 0 || len(label) > dnsMaxLabelLen {
			return nil, fmt.Errorf("%w: invalid label length %d", ErrInvalidConfig, len(label))
		}
		total += 1 + len(label)
	}
	if total-1 > dnsMaxNameLen {
		return nil, fmt.Errorf("%w: DNS name exceeds %d bytes", ErrInvalidConfig, dnsMaxNameLen)
	}
	out := make([]byte, 0, total)
	for _, label := range labels {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0)
	return out, nil
}

func encodeDNSName(name string) ([]byte, error) {
	s := strings.TrimSpace(name)
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return nil, fmt.Errorf("%w: empty DNS name", ErrInvalidConfig)
	}
	return encodeLabels(strings.Split(s, "."))
}

func decodeDNSName(msg []byte, offset int) (string, int, error) {
	if offset < 0 || offset >= len(msg) {
		return "", 0, errors.New("mdns: DNS name offset out of range")
	}
	var labels []string
	cur := offset
	endOffset := 0
	jumped := false
	jumps := 0
	totalLen := 0

	for {
		if cur >= len(msg) {
			return "", 0, errors.New("mdns: truncated DNS name")
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
				return "", 0, errors.New("mdns: truncated DNS compression pointer")
			}
			ptr := int(binary.BigEndian.Uint16(msg[cur:cur+2]) & 0x3fff)
			if ptr >= len(msg) {
				return "", 0, errors.New("mdns: DNS compression pointer out of range")
			}
			if !jumped {
				endOffset = cur + 2
				jumped = true
			}
			jumps++
			if jumps > dnsMaxJumps {
				return "", 0, errors.New("mdns: DNS compression pointer loop")
			}
			cur = ptr
		case 0x00:
			cur++
			if cur+length > len(msg) {
				return "", 0, errors.New("mdns: truncated DNS label")
			}
			totalLen += length + 1
			if totalLen > dnsMaxNameLen+1 {
				return "", 0, errors.New("mdns: DNS name exceeds max length")
			}
			labels = append(labels, string(msg[cur:cur+length]))
			cur += length
		default:
			return "", 0, errors.New("mdns: invalid DNS label encoding")
		}
	}
	return strings.Join(labels, "."), endOffset, nil
}

func encodeMDNSMessage(msg dnsMessage) ([]byte, error) {
	var buf []byte
	buf = make([]byte, dnsHeaderLen)
	binary.BigEndian.PutUint16(buf[0:2], msg.ID)
	var flags uint16
	if msg.Response {
		flags = 0x8400 // QR=1, AA=1
	}
	binary.BigEndian.PutUint16(buf[2:4], flags)
	binary.BigEndian.PutUint16(buf[4:6], uint16(len(msg.Questions)))
	binary.BigEndian.PutUint16(buf[6:8], uint16(len(msg.Answers)))
	binary.BigEndian.PutUint16(buf[8:10], uint16(len(msg.Authorities)))
	binary.BigEndian.PutUint16(buf[10:12], uint16(len(msg.Additional)))

	for _, q := range msg.Questions {
		wireName, err := encodeDNSName(q.Name)
		if err != nil {
			return nil, err
		}
		class := q.Class
		if class == 0 {
			class = ClassIN
		}
		if q.WantQU {
			class |= QUFlag
		}
		buf = append(buf, wireName...)
		var tail [4]byte
		binary.BigEndian.PutUint16(tail[0:2], q.Type)
		binary.BigEndian.PutUint16(tail[2:4], class)
		buf = append(buf, tail[:]...)
	}

	appendRecord := func(rr dnsRecord) error {
		wireName, err := encodeDNSName(rr.Name)
		if err != nil {
			return err
		}
		var rdata []byte
		switch rr.Type {
		case TypeA:
			if !rr.A.Is4() {
				return fmt.Errorf("%w: A record requires IPv4 address", ErrInvalidConfig)
			}
			a4 := rr.A.As4()
			rdata = a4[:]
		case TypePTR:
			rdata, err = encodeDNSName(rr.PTR)
			if err != nil {
				return err
			}
		case TypeSRV:
			target, err := encodeDNSName(rr.SRVTarget)
			if err != nil {
				return err
			}
			rdata = make([]byte, 6+len(target))
			binary.BigEndian.PutUint16(rdata[4:6], rr.SRVPort)
			copy(rdata[6:], target)
		case TypeTXT:
			if len(rr.TXT) == 0 {
				rdata = []byte{0}
			} else {
				for _, entry := range rr.TXT {
					if len(entry) > maxTXTOctetLen {
						return fmt.Errorf("%w: TXT entry exceeds %d bytes", ErrInvalidConfig, maxTXTOctetLen)
					}
					rdata = append(rdata, byte(len(entry)))
					rdata = append(rdata, entry...)
				}
			}
		default:
			if len(rr.RawRData) == 0 {
				return fmt.Errorf("%w: unsupported mDNS record type %d", ErrInvalidConfig, rr.Type)
			}
			rdata = append([]byte(nil), rr.RawRData...)
		}

		class := rr.Class
		if class == 0 {
			class = ClassIN
		}
		if rr.CacheFlush {
			class |= QUFlag
		}
		buf = append(buf, wireName...)
		var hdr [10]byte
		binary.BigEndian.PutUint16(hdr[0:2], rr.Type)
		binary.BigEndian.PutUint16(hdr[2:4], class)
		binary.BigEndian.PutUint32(hdr[4:8], rr.TTL)
		binary.BigEndian.PutUint16(hdr[8:10], uint16(len(rdata)))
		buf = append(buf, hdr[:]...)
		buf = append(buf, rdata...)
		return nil
	}

	for _, rr := range msg.Answers {
		if err := appendRecord(rr); err != nil {
			return nil, err
		}
	}
	for _, rr := range msg.Authorities {
		if err := appendRecord(rr); err != nil {
			return nil, err
		}
	}
	for _, rr := range msg.Additional {
		if err := appendRecord(rr); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func decodeMDNSMessage(msg []byte) (dnsMessage, error) {
	if len(msg) < dnsHeaderLen {
		return dnsMessage{}, errors.New("mdns: short DNS message")
	}
	id := binary.BigEndian.Uint16(msg[0:2])
	flags := binary.BigEndian.Uint16(msg[2:4])
	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))
	nsCount := int(binary.BigEndian.Uint16(msg[8:10]))
	arCount := int(binary.BigEndian.Uint16(msg[10:12]))

	out := dnsMessage{
		ID:       id,
		Response: flags&0x8000 != 0,
	}
	off := dnsHeaderLen

	for i := 0; i < qdCount; i++ {
		qname, next, err := decodeDNSName(msg, off)
		if err != nil {
			return dnsMessage{}, err
		}
		if next+4 > len(msg) {
			return dnsMessage{}, errors.New("mdns: truncated DNS question")
		}
		qtype := binary.BigEndian.Uint16(msg[next : next+2])
		rawClass := binary.BigEndian.Uint16(msg[next+2 : next+4])
		off = next + 4
		out.Questions = append(out.Questions, dnsQuestion{
			Name:   qname,
			Type:   qtype,
			Class:  rawClass & 0x7fff,
			WantQU: rawClass&QUFlag != 0,
		})
	}

	parseSection := func(count int, collect bool) ([]dnsRecord, error) {
		var list []dnsRecord
		for i := 0; i < count; i++ {
			name, next, err := decodeDNSName(msg, off)
			if err != nil {
				return nil, err
			}
			if next+10 > len(msg) {
				return nil, errors.New("mdns: truncated DNS record header")
			}
			rtype := binary.BigEndian.Uint16(msg[next : next+2])
			rawClass := binary.BigEndian.Uint16(msg[next+2 : next+4])
			ttl := binary.BigEndian.Uint32(msg[next+4 : next+8])
			rdLen := int(binary.BigEndian.Uint16(msg[next+8 : next+10]))
			rdataOff := next + 10
			if rdataOff+rdLen > len(msg) {
				return nil, errors.New("mdns: truncated DNS record rdata")
			}
			off = rdataOff + rdLen
			if !collect || rawClass&0x7fff != ClassIN {
				continue
			}
			rr := dnsRecord{
				Name:       name,
				Type:       rtype,
				Class:      rawClass & 0x7fff,
				CacheFlush: rawClass&QUFlag != 0,
				TTL:        ttl,
			}
			switch rtype {
			case TypeA:
				if rdLen != 4 {
					return nil, errors.New("mdns: invalid A record length")
				}
				var ip4 [4]byte
				copy(ip4[:], msg[rdataOff:rdataOff+4])
				rr.A = netip.AddrFrom4(ip4)
				list = append(list, rr)
			case TypePTR:
				target, end, err := decodeDNSName(msg, rdataOff)
				if err != nil || end != off {
					return nil, errors.New("mdns: invalid PTR record rdata")
				}
				rr.PTR = target
				list = append(list, rr)
			case TypeSRV:
				if rdLen < 7 {
					return nil, errors.New("mdns: invalid SRV record length")
				}
				rr.SRVPort = binary.BigEndian.Uint16(msg[rdataOff+4 : rdataOff+6])
				target, end, err := decodeDNSName(msg, rdataOff+6)
				if err != nil || end != off {
					return nil, errors.New("mdns: invalid SRV target rdata")
				}
				rr.SRVTarget = target
				list = append(list, rr)
			case TypeTXT:
				cur := rdataOff
				for cur < off {
					txtLen := int(msg[cur])
					cur++
					if cur+txtLen > off {
						return nil, errors.New("mdns: truncated TXT string")
					}
					if txtLen > 0 {
						rr.TXT = append(rr.TXT, string(msg[cur:cur+txtLen]))
					}
					cur += txtLen
				}
				list = append(list, rr)
			default:
				rr.RawRData = append([]byte(nil), msg[rdataOff:off]...)
				list = append(list, rr)
			}
		}
		return list, nil
	}

	var err error
	if out.Answers, err = parseSection(anCount, true); err != nil {
		return dnsMessage{}, err
	}
	if out.Authorities, err = parseSection(nsCount, true); err != nil {
		return dnsMessage{}, err
	}
	if out.Additional, err = parseSection(arCount, true); err != nil {
		return dnsMessage{}, err
	}
	return out, nil
}

type parsedMDNSFrame struct {
	srcMAC  [6]byte
	srcIP   [4]byte
	srcPort uint16
	msg     dnsMessage
}

func buildMDNSFrame(srcMAC, dstMAC [6]byte, srcIP, dstIP [4]byte, srcPort, dstPort, ipID uint16, payload []byte) []byte {
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
	ip[8] = mdnsIPv4TTL
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

func parseMDNSFrame(frame []byte, localMAC [6]byte, localIP [4]byte, acceptAnyUnicast bool) (parsedMDNSFrame, bool) {
	if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dnsHeaderLen {
		return parsedMDNSFrame{}, false
	}
	dstMAC := frame[0:6]
	if !bytes.Equal(dstMAC, multicastMACBytes[:]) && !bytes.Equal(dstMAC, localMAC[:]) && !bytes.Equal(dstMAC, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		return parsedMDNSFrame{}, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIPv4 {
		return parsedMDNSFrame{}, false
	}
	ip := frame[ethHeaderLen:]
	if ip[0]>>4 != 4 {
		return parsedMDNSFrame{}, false
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < ipv4MinLen || ihl > len(ip) {
		return parsedMDNSFrame{}, false
	}
	totalLen := int(binary.BigEndian.Uint16(ip[2:4]))
	if totalLen < ihl+udpHeaderLen+dnsHeaderLen || totalLen > len(ip) {
		return parsedMDNSFrame{}, false
	}
	if binary.BigEndian.Uint16(ip[6:8])&0x3fff != 0 || ip[8] != mdnsIPv4TTL || ip[9] != ipProtoUDP {
		return parsedMDNSFrame{}, false
	}
	if !bytes.Equal(ip[16:20], multicastIPv4Bytes[:]) && !bytes.Equal(ip[16:20], localIP[:]) {
		if !acceptAnyUnicast {
			return parsedMDNSFrame{}, false
		}
	}
	if internetChecksum(ip[:ihl]) != 0 {
		return parsedMDNSFrame{}, false
	}
	udp := ip[ihl:totalLen]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if dstPort != Port && (!acceptAnyUnicast || srcPort != Port) {
		return parsedMDNSFrame{}, false
	}
	if udpLen < udpHeaderLen+dnsHeaderLen || udpLen > len(udp) {
		return parsedMDNSFrame{}, false
	}
	udp = udp[:udpLen]
	if binary.BigEndian.Uint16(udp[6:8]) != 0 && udpChecksum(ip[12:16], ip[16:20], udp) != 0 {
		return parsedMDNSFrame{}, false
	}
	dnsMsg, err := decodeMDNSMessage(udp[udpHeaderLen:])
	if err != nil {
		return parsedMDNSFrame{}, false
	}
	var out parsedMDNSFrame
	copy(out.srcMAC[:], frame[6:12])
	copy(out.srcIP[:], ip[12:16])
	out.srcPort = srcPort
	out.msg = dnsMsg
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
