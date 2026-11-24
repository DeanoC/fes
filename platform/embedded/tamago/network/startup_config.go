// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// StartupSpec holds validated build-time network/clock startup options for the
// DE25-Nano fpgaload binary (supplied via -X linker flags).
type StartupSpec struct {
	DHCP         bool
	DHCPHostname string
	SNTPServer   string
	SNTPPort     uint16
}

// ParseStartupSpec validates build-time linker strings for opt-in DHCPv4 and
// SNTPv4 clock synchronization. When all arguments are empty or zero-disabled,
// it returns the zero StartupSpec and nil error.
func ParseStartupSpec(dhcpFlag, hostname, sntpServer, sntpPort string) (StartupSpec, error) {
	var spec StartupSpec
	switch strings.TrimSpace(dhcpFlag) {
	case "", "0":
	case "1":
		spec.DHCP = true
	default:
		return StartupSpec{}, fmt.Errorf("%w: invalid DHCP flag %q", ErrDHCPInvalidConfig, dhcpFlag)
	}

	hostname = strings.TrimSpace(hostname)
	if hostname != "" {
		if !spec.DHCP {
			return StartupSpec{}, fmt.Errorf("%w: DHCP hostname requires DHCP enabled", ErrDHCPInvalidConfig)
		}
		if err := ValidateDHCPConfig(DHCPConfig{Hostname: hostname}); err != nil {
			return StartupSpec{}, err
		}
		spec.DHCPHostname = hostname
	}

	sntpServer = strings.TrimSpace(sntpServer)
	if sntpServer != "" {
		addr, err := netip.ParseAddr(sntpServer)
		if err != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsMulticast() {
			return StartupSpec{}, fmt.Errorf("%w: invalid SNTP server %q", ErrSNTPInvalidConfig, sntpServer)
		}
		spec.SNTPServer = addr.String()
	}

	sntpPort = strings.TrimSpace(sntpPort)
	if sntpPort != "" {
		if spec.SNTPServer == "" {
			return StartupSpec{}, fmt.Errorf("%w: SNTP port requires SNTP server", ErrSNTPInvalidConfig)
		}
		p, err := strconv.Atoi(sntpPort)
		if err != nil || p < 1 || p > 65535 {
			return StartupSpec{}, fmt.Errorf("%w: invalid SNTP port %q", ErrSNTPInvalidConfig, sntpPort)
		}
		spec.SNTPPort = uint16(p)
	}

	return spec, nil
}
