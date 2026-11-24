// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	sntpPort           = 123
	sntpPacketLen      = 48
	sntpEpochDelta     = uint64(2208988800) // Seconds between 1900-01-01 and 1970-01-01.
	sntpModeClient     = 3
	sntpModeServer     = 4
	sntpVersion4       = 4
	sntpLeapUnsync     = 3
	defaultSNTPTimeout = 5 * time.Second
	defaultSNTPRetry   = 250 * time.Millisecond
)

var (
	// ErrSNTPInvalidConfig is returned when SNTPConfig is missing required fields or malformed.
	ErrSNTPInvalidConfig = errors.New("network: invalid SNTP configuration")
	// ErrSNTPInvalidReply is returned when an SNTP reply is malformed or fails validation.
	ErrSNTPInvalidReply = errors.New("network: invalid SNTP reply")
	// ErrSNTPKissOfDeath is returned when an SNTP server replies with stratum 0 (KoD).
	ErrSNTPKissOfDeath = errors.New("network: SNTP server returned kiss-of-death")
	// ErrSNTPUnsynchronized is returned when an SNTP server advertises LI=3 (clock unsynchronized).
	ErrSNTPUnsynchronized = errors.New("network: SNTP server clock unsynchronized")
)

// SNTPConfig configures RFC 4330 SNTPv4 UTC time queries over a RawFramePort.
type SNTPConfig struct {
	LocalMAC      string        // Unicast client MAC address
	LocalPrefix   string        // Client IPv4 CIDR prefix, e.g. "10.0.2.15/24"
	Gateway       string        // Default IPv4 gateway in LocalPrefix (used for off-subnet SNTP servers)
	Servers       []string      // Ordered list of IPv4 SNTP server addresses
	ServerPort    uint16        // Optional UDP destination port (default 123)
	NextHopMAC    string        // Optional pre-resolved next-hop MAC (skips ARP when set)
	Timeout       time.Duration // Overall deadline when ctx has none (default 5s)
	RetryInterval time.Duration // Retransmit interval per attempt (default 250ms)
	SourcePort    uint16        // Optional fixed UDP source port for deterministic tests (0 -> random ephemeral)
	Nonce         uint64        // Optional fixed 64-bit originate nonce for deterministic tests (0 -> random)
}

// SNTPReply holds the parsed header fields and UTC timestamps from one SNTPv4 server packet.
type SNTPReply struct {
	Leap         uint8
	Version      uint8
	Stratum      uint8
	ReceiveTime  time.Time
	TransmitTime time.Time
}

// SNTPResult is the validated UTC wall-clock sample returned by QuerySNTP.
type SNTPResult struct {
	Time    time.Time     // Estimated current UTC wall-clock time adjusted by half round-trip delay
	Server  string        // Answering IPv4 SNTP server address
	Stratum uint8         // Server stratum (1..15)
	RTT     time.Duration // Measured monotonic round-trip time for the matched request
}

// EncodeSNTPRequest encodes one 48-byte RFC 4330 SNTPv4 client request packet
// carrying nonce in the Transmit Timestamp field (bytes 40..47).
func EncodeSNTPRequest(nonce uint64) []byte {
	pkt := make([]byte, sntpPacketLen)
	pkt[0] = (sntpVersion4 << 3) | sntpModeClient
	binary.BigEndian.PutUint64(pkt[40:48], nonce)
	return pkt
}

// ParseSNTPResponse validates an RFC 4330 SNTPv4 server reply against expectedNonce.
// Originate Timestamp (bytes 24..31) is matched before honoring LI=3 or KoD errors
// so stray or spoofed packets cannot terminate an in-flight exchange.
func ParseSNTPResponse(pkt []byte, expectedNonce uint64) (SNTPReply, error) {
	if len(pkt) < sntpPacketLen {
		return SNTPReply{}, fmt.Errorf("%w: short packet (%d bytes)", ErrSNTPInvalidReply, len(pkt))
	}
	li := (pkt[0] >> 6) & 0x03
	vn := (pkt[0] >> 3) & 0x07
	mode := pkt[0] & 0x07
	if mode != sntpModeServer || (vn != 3 && vn != 4) {
		return SNTPReply{}, fmt.Errorf("%w: unexpected version=%d mode=%d", ErrSNTPInvalidReply, vn, mode)
	}
	originate := binary.BigEndian.Uint64(pkt[24:32])
	if expectedNonce == 0 || originate != expectedNonce {
		return SNTPReply{}, fmt.Errorf("%w: originate nonce mismatch", ErrSNTPInvalidReply)
	}
	stratum := pkt[1]
	if stratum == 0 {
		return SNTPReply{}, ErrSNTPKissOfDeath
	}
	if li == sntpLeapUnsync {
		return SNTPReply{}, ErrSNTPUnsynchronized
	}
	if stratum > 15 {
		return SNTPReply{}, fmt.Errorf("%w: invalid stratum %d", ErrSNTPInvalidReply, stratum)
	}
	txRaw := binary.BigEndian.Uint64(pkt[40:48])
	if txRaw == 0 {
		return SNTPReply{}, fmt.Errorf("%w: zero transmit timestamp", ErrSNTPInvalidReply)
	}
	var rxTime time.Time
	if rxRaw := binary.BigEndian.Uint64(pkt[32:40]); rxRaw != 0 {
		rxTime = decodeNTPTimestamp(rxRaw)
	}
	return SNTPReply{
		Leap:         li,
		Version:      vn,
		Stratum:      stratum,
		ReceiveTime:  rxTime,
		TransmitTime: decodeNTPTimestamp(txRaw),
	}, nil
}

// decodeNTPTimestamp converts a 64-bit NTP 32.32 fixed-point timestamp into UTC
// time using the RFC 4330 §3 era convention (MSB=1 -> 1968..2036, MSB=0 -> 2036..2104).
func decodeNTPTimestamp(raw uint64) time.Time {
	secs := uint32(raw >> 32)
	frac := uint32(raw)
	var unixSec int64
	if secs&0x80000000 != 0 {
		unixSec = int64(uint64(secs) - sntpEpochDelta)
	} else {
		unixSec = int64(uint64(secs) + (uint64(1)<<32 - sntpEpochDelta))
	}
	nsec := int64((uint64(frac) * 1_000_000_000) >> 32)
	return time.Unix(unixSec, nsec).UTC()
}

// encodeNTPTimestamp converts a UTC time into a 64-bit NTP 32.32 fixed-point timestamp.
func encodeNTPTimestamp(t time.Time) uint64 {
	u := t.UTC()
	secs := uint32(uint64(u.Unix()) + sntpEpochDelta)
	frac := uint32((uint64(u.Nanosecond()) << 32) / 1_000_000_000)
	return (uint64(secs) << 32) | uint64(frac)
}

func randomNonce64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		if v := binary.BigEndian.Uint64(b[:]); v != 0 {
			return v
		}
	}
	v := uint64(time.Now().UnixNano()) ^ 0x9e3779b97f4a7c15
	if v == 0 {
		v = 1
	}
	return v
}

// QuerySNTP queries the configured RFC 4330 SNTPv4 servers over port and returns
// a validated UTC wall-clock sample adjusted for round-trip network delay without
// relying on the local clock's wall-clock epoch.
func QuerySNTP(ctx context.Context, port RawFramePort, cfg SNTPConfig) (SNTPResult, error) {
	if ctx == nil || port == nil {
		return SNTPResult{}, fmt.Errorf("%w: nil context or port", ErrSNTPInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return SNTPResult{}, err
	}
	localMAC, err := parseUnicastMAC(cfg.LocalMAC)
	if err != nil {
		return SNTPResult{}, fmt.Errorf("%w: %v", ErrSNTPInvalidConfig, err)
	}
	localPrefix, err := netip.ParsePrefix(cfg.LocalPrefix)
	if err != nil || !localPrefix.Addr().Is4() || localPrefix.Bits() < 1 || localPrefix.Bits() > 30 || !usableHost(localPrefix, localPrefix.Addr()) {
		return SNTPResult{}, fmt.Errorf("%w: invalid local IPv4 prefix", ErrSNTPInvalidConfig)
	}
	var gateway netip.Addr
	if cfg.Gateway != "" {
		if _, err := staticConfig(cfg.LocalPrefix, cfg.Gateway); err != nil {
			return SNTPResult{}, fmt.Errorf("%w: %v", ErrSNTPInvalidConfig, err)
		}
		gateway = netip.MustParseAddr(cfg.Gateway)
	}
	if len(cfg.Servers) == 0 {
		return SNTPResult{}, fmt.Errorf("%w: no SNTP servers configured", ErrSNTPInvalidConfig)
	}
	servers := make([]netip.Addr, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() {
			return SNTPResult{}, fmt.Errorf("%w: invalid SNTP server %q", ErrSNTPInvalidConfig, s)
		}
		servers = append(servers, a)
	}
	var fixedNextHop [6]byte
	hasFixedNextHop := false
	if cfg.NextHopMAC != "" {
		fixedNextHop, err = parseUnicastMAC(cfg.NextHopMAC)
		if err != nil {
			return SNTPResult{}, fmt.Errorf("%w: invalid NextHopMAC: %v", ErrSNTPInvalidConfig, err)
		}
		hasFixedNextHop = true
	}
	if cfg.Timeout < 0 || cfg.RetryInterval < 0 {
		return SNTPResult{}, fmt.Errorf("%w: negative duration", ErrSNTPInvalidConfig)
	}

	serverPort := cfg.ServerPort
	if serverPort == 0 {
		serverPort = sntpPort
	}
	retryInterval := cfg.RetryInterval
	if retryInterval == 0 {
		retryInterval = defaultSNTPRetry
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSNTPTimeout)
		defer cancel()
	}

	var lastErr error
	for i, server := range servers {
		if err := ctx.Err(); err != nil {
			return SNTPResult{}, err
		}
		if err := port.Err(); err != nil {
			return SNTPResult{}, err
		}

		nextHop := server
		if !localPrefix.Contains(server) {
			if !gateway.IsValid() {
				lastErr = fmt.Errorf("%w: off-subnet SNTP server %s requires gateway", ErrSNTPInvalidConfig, server)
				continue
			}
			nextHop = gateway
		}

		var dstMAC [6]byte
		if hasFixedNextHop {
			dstMAC = fixedNextHop
		} else {
			arpCtx, arpCancel := perServerContext(ctx, len(servers)-i)
			dstMAC, err = resolveARP(arpCtx, port, localMAC, localPrefix.Addr().As4(), nextHop.As4(), retryInterval)
			arpCancel()
			if err != nil {
				lastErr = err
				if ctx.Err() != nil || port.Err() != nil {
					return SNTPResult{}, err
				}
				continue
			}
		}

		srcPort := cfg.SourcePort
		if srcPort == 0 {
			srcPort = 49152 + randomUint16()%16384
		}

		srvCtx, srvCancel := perServerContext(ctx, len(servers)-i)
		res, err := exchangeSNTP(srvCtx, port, localMAC, dstMAC, localPrefix.Addr().As4(), server.As4(), serverPort, srcPort, cfg.Nonce, retryInterval)
		srvCancel()
		if err == nil {
			res.Server = server.String()
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil || port.Err() != nil {
			return SNTPResult{}, err
		}
	}
	if lastErr != nil {
		return SNTPResult{}, lastErr
	}
	return SNTPResult{}, ErrSNTPInvalidReply
}

func exchangeSNTP(
	ctx context.Context,
	port RawFramePort,
	localMAC, dstMAC [6]byte,
	localIP, serverIP [4]byte,
	serverPort, srcPort uint16,
	fixedNonce uint64,
	retryInterval time.Duration,
) (SNTPResult, error) {
	type sntpOutcome struct {
		reply      SNTPReply
		receivedAt time.Time
		sentAt     time.Time
		err        error
	}

	var stateMu sync.Mutex
	var activeNonce uint64
	var activeSentAt time.Time

	replyCh := make(chan sntpOutcome, 1)
	remove := port.AddTap(func(frame []byte) {
		receivedAt := time.Now()
		payload, ok := parseUDPv4Reply(frame, localMAC, serverIP, localIP, serverPort, srcPort)
		if !ok {
			return
		}
		stateMu.Lock()
		expected := activeNonce
		sentAt := activeSentAt
		stateMu.Unlock()
		if expected == 0 {
			return
		}
		reply, err := ParseSNTPResponse(payload, expected)
		if err != nil && !errors.Is(err, ErrSNTPKissOfDeath) && !errors.Is(err, ErrSNTPUnsynchronized) {
			return
		}
		select {
		case replyCh <- sntpOutcome{reply: reply, receivedAt: receivedAt, sentAt: sentAt, err: err}:
		default:
		}
	})
	defer remove()

	interval := retryInterval
	for {
		if err := ctx.Err(); err != nil {
			return SNTPResult{}, err
		}
		if err := port.Err(); err != nil {
			return SNTPResult{}, err
		}
		nonce := fixedNonce
		if nonce == 0 {
			nonce = randomNonce64()
		}
		txFrame := buildUDPv4Frame(localMAC, dstMAC, localIP, serverIP, srcPort, serverPort, uint16(nonce), EncodeSNTPRequest(nonce))
		sentAt := time.Now()
		stateMu.Lock()
		activeNonce = nonce
		activeSentAt = sentAt
		stateMu.Unlock()

		paused := false
		if err := port.SendRawFrame(txFrame); err != nil {
			if !errors.Is(err, ErrPaused) {
				return SNTPResult{}, err
			}
			stateMu.Lock()
			activeNonce = 0
			stateMu.Unlock()
			paused = true
		}

		timer := time.NewTimer(interval)
		select {
		case out := <-replyCh:
			timer.Stop()
			if err := ctx.Err(); err != nil {
				return SNTPResult{}, err
			}
			if out.err != nil {
				return SNTPResult{}, out.err
			}
			rtt := out.receivedAt.Sub(out.sentAt)
			if rtt < 0 {
				rtt = 0
			}
			oneWay := rtt / 2
			if !out.reply.ReceiveTime.IsZero() && !out.reply.TransmitTime.Before(out.reply.ReceiveTime) {
				if proc := out.reply.TransmitTime.Sub(out.reply.ReceiveTime); proc <= rtt {
					oneWay = (rtt - proc) / 2
				}
			}
			return SNTPResult{
				Time:    out.reply.TransmitTime.Add(oneWay).UTC(),
				Stratum: out.reply.Stratum,
				RTT:     rtt,
			}, nil
		case <-ctx.Done():
			timer.Stop()
			return SNTPResult{}, ctx.Err()
		case <-timer.C:
		}
		if !paused && interval < 2*time.Second {
			interval *= 2
		}
	}
}
