// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"errors"
	"strings"
	"testing"
)

func TestParseStartupSpec(t *testing.T) {
	spec, err := ParseStartupSpec("", "", "", "")
	if err != nil {
		t.Fatalf("empty spec: %v", err)
	}
	if spec != (StartupSpec{}) {
		t.Fatalf("empty spec=%+v, want zero", spec)
	}

	spec, err = ParseStartupSpec("0", "", "", "")
	if err != nil || spec.DHCP {
		t.Fatalf("disabled flag=(%+v, %v)", spec, err)
	}

	spec, err = ParseStartupSpec("1", "fes-de25", "10.0.2.2", "12345")
	if err != nil {
		t.Fatalf("valid full spec: %v", err)
	}
	if !spec.DHCP || spec.DHCPHostname != "fes-de25" || spec.SNTPServer != "10.0.2.2" || spec.SNTPPort != 12345 {
		t.Fatalf("got=%+v", spec)
	}

	spec, err = ParseStartupSpec("1", "", "192.168.1.1", "")
	if err != nil {
		t.Fatalf("valid default-port spec: %v", err)
	}
	if !spec.DHCP || spec.SNTPServer != "192.168.1.1" || spec.SNTPPort != 0 {
		t.Fatalf("got=%+v", spec)
	}

	dhcpBad := []struct {
		name     string
		flag     string
		hostname string
	}{
		{name: "bad-flag", flag: "true"},
		{name: "hostname-without-dhcp", flag: "", hostname: "fes"},
		{name: "non-ascii-hostname", flag: "1", hostname: "fes-\x80"},
		{name: "oversized-hostname", flag: "1", hostname: strings.Repeat("a", 256)},
	}
	for _, tc := range dhcpBad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseStartupSpec(tc.flag, tc.hostname, "", ""); !errors.Is(err, ErrDHCPInvalidConfig) {
				t.Fatalf("%s err=%v, want ErrDHCPInvalidConfig", tc.name, err)
			}
		})
	}

	sntpBad := []struct {
		name   string
		server string
		port   string
	}{
		{name: "ipv6-server", server: "::1"},
		{name: "unspecified-server", server: "0.0.0.0"},
		{name: "multicast-server", server: "224.0.0.1"},
		{name: "bad-ip", server: "not-an-ip"},
		{name: "port-without-server", server: "", port: "123"},
		{name: "port-zero", server: "10.0.2.2", port: "0"},
		{name: "port-too-high", server: "10.0.2.2", port: "65536"},
		{name: "port-nonnumeric", server: "10.0.2.2", port: "ntp"},
	}
	for _, tc := range sntpBad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseStartupSpec("", "", tc.server, tc.port); !errors.Is(err, ErrSNTPInvalidConfig) {
				t.Fatalf("%s err=%v, want ErrSNTPInvalidConfig", tc.name, err)
			}
		})
	}
}
