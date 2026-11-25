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
	DHCP            bool
	DHCPHostname    string
	SNTPServer      string
	SNTPPort        uint16
	Allow100Full    bool
	RecoverLinkLoss bool
}

// StartupFlags groups the raw build-time linker strings parsed into StartupSpec.
type StartupFlags struct {
	DHCP            string
	DHCPHostname    string
	SNTPServer      string
	SNTPPort        string
	Allow100Full    string
	RecoverLinkLoss string
}

// ParseStartupSpec validates build-time linker strings for opt-in DHCPv4 and
// SNTPv4 clock synchronization. When all arguments are empty or zero-disabled,
// it returns the zero StartupSpec and nil error.
func ParseStartupSpec(dhcpFlag, hostname, sntpServer, sntpPort string) (StartupSpec, error) {
	return ParseStartupFlags(StartupFlags{
		DHCP:         dhcpFlag,
		DHCPHostname: hostname,
		SNTPServer:   sntpServer,
		SNTPPort:     sntpPort,
	})
}

// ParseStartupFlags validates the full set of opt-in build-time network/clock
// and XGMAC link-recovery linker strings for DE25-Nano fpgaload.
func ParseStartupFlags(flags StartupFlags) (StartupSpec, error) {
	var spec StartupSpec
	var err error
	if spec.DHCP, err = parseBoolStartupFlag("DHCP", flags.DHCP); err != nil {
		return StartupSpec{}, err
	}
	if spec.Allow100Full, err = parseBoolStartupFlag("Allow100Full", flags.Allow100Full); err != nil {
		return StartupSpec{}, err
	}
	if spec.RecoverLinkLoss, err = parseBoolStartupFlag("RecoverLinkLoss", flags.RecoverLinkLoss); err != nil {
		return StartupSpec{}, err
	}

	hostname := strings.TrimSpace(flags.DHCPHostname)
	if hostname != "" {
		if !spec.DHCP {
			return StartupSpec{}, fmt.Errorf("%w: DHCP hostname requires DHCP enabled", ErrDHCPInvalidConfig)
		}
		if err := ValidateDHCPConfig(DHCPConfig{Hostname: hostname}); err != nil {
			return StartupSpec{}, err
		}
		spec.DHCPHostname = hostname
	}

	sntpServer := strings.TrimSpace(flags.SNTPServer)
	if sntpServer != "" {
		addr, err := netip.ParseAddr(sntpServer)
		if err != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsMulticast() {
			return StartupSpec{}, fmt.Errorf("%w: invalid SNTP server %q", ErrSNTPInvalidConfig, sntpServer)
		}
		spec.SNTPServer = addr.String()
	}

	sntpPort := strings.TrimSpace(flags.SNTPPort)
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

func parseBoolStartupFlag(name, raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("%w: invalid %s flag %q", ErrDHCPInvalidConfig, name, raw)
	}
}
