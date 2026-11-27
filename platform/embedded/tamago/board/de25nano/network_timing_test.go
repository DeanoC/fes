// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"testing"
	"time"
)

func TestFittedMAC0PHYTrainingBudget(t *testing.T) {
	for _, test := range []struct {
		base    uint32
		address uint8
		phy     uint32
		want    time.Duration
	}{
		{0x10810000, 1, 0x00221622, 30 * time.Second},
		{0x10820000, 1, 0x00221622, 5 * time.Second},
		{0x10810000, 2, 0x00221622, 5 * time.Second},
		{0x10810000, 1, 0x001cc916, 5 * time.Second},
	} {
		got := networkStartupTimeout(test.base, test.address, test.phy, false)
		withDHCP := networkStartupTimeout(test.base, test.address, test.phy, true)
		if got != test.want || withDHCP != test.want+10*time.Second {
			t.Fatalf("MAC %#x PHY %d/%#x: startup %s, with DHCP %s", test.base, test.address, test.phy, got, withDHCP)
		}
	}
}
