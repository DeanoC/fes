// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	// FogCastServiceType is the DNS-SD service type advertised by FogCast targets.
	FogCastServiceType = "_fogcast._tcp"
	// FogCastProtocolVersion matches FogCast internal/discovery protocolVersion ("1").
	FogCastProtocolVersion = "1"
	// FogCastMeshProtocol matches FogCast internal/discovery MeshProtocol ("1.0").
	FogCastMeshProtocol = "1.0"
)

// FogCastInfo describes the TXT metadata advertised under _fogcast._tcp.local.
type FogCastInfo struct {
	TargetID          string
	NodeID            string
	DiscoveryProtocol string
	Mesh              string
	Cap               string
	TTLSeconds        int
}

// DirectBindable reports whether info advertises protocol version "1" and a
// valid UUID target_id matching FogCast's Phase 0 discovery contract.
func (info FogCastInfo) DirectBindable() bool {
	return info.DiscoveryProtocol == FogCastProtocolVersion && ValidID(info.TargetID)
}

// ValidID checks whether id is a canonical lowercase hyphenated UUID string
// matching FogCast internal/discovery.ValidID.
func ValidID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	for i := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// NewFogCastService constructs a validated _fogcast._tcp mDNS Service definition
// matching FogCast's DNS-SD TXT wire contract.
func NewFogCastService(instance, host string, ip netip.Addr, port uint16, info FogCastInfo) (Service, error) {
	if !ValidID(info.TargetID) {
		return Service{}, fmt.Errorf("%w: invalid FogCast target_id %q", ErrInvalidConfig, info.TargetID)
	}
	if info.NodeID != "" && !ValidID(info.NodeID) {
		return Service{}, fmt.Errorf("%w: invalid FogCast node_id %q", ErrInvalidConfig, info.NodeID)
	}
	proto := info.DiscoveryProtocol
	if proto == "" {
		proto = FogCastProtocolVersion
	}
	if proto != FogCastProtocolVersion {
		return Service{}, fmt.Errorf("%w: unsupported FogCast discovery protocol %q", ErrInvalidConfig, proto)
	}

	txt := []string{
		"v=" + proto,
		"target_id=" + info.TargetID,
	}
	if info.NodeID != "" {
		txt = append(txt, "node_id="+info.NodeID)
	}
	if info.Mesh != "" {
		txt = append(txt, "mesh="+info.Mesh)
	}
	if info.Cap != "" {
		txt = append(txt, "cap="+info.Cap)
	}
	var ttl uint32
	if info.TTLSeconds > 0 {
		txt = append(txt, "ttl="+strconv.Itoa(info.TTLSeconds))
		ttl = uint32(info.TTLSeconds)
	}

	svc := Service{
		Instance:    instance,
		ServiceType: FogCastServiceType,
		Domain:      "local",
		Host:        host,
		Port:        port,
		IP:          ip,
		TXT:         txt,
		TTL:         ttl,
	}
	norm, _, _, _, err := svc.normalized()
	if err != nil {
		return Service{}, err
	}
	return norm, nil
}

// ParseFogCastTXT decodes FogCast TXT key=value pairs into FogCastInfo.
func ParseFogCastTXT(txt []string) FogCastInfo {
	var info FogCastInfo
	for _, entry := range txt {
		key, val, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "v":
			info.DiscoveryProtocol = val
		case "target_id":
			info.TargetID = val
		case "node_id":
			info.NodeID = val
		case "mesh":
			info.Mesh = val
		case "cap":
			info.Cap = val
		case "ttl":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				info.TTLSeconds = n
			}
		}
	}
	return info
}
