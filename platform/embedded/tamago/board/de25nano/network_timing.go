// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import "time"

const (
	defaultDeviceTimeout = 5 * time.Second
	dhcpStartupBudget    = 10 * time.Second
)

// The fitted CBL MAC0/KSZ9031 needs up to 25s for the secure supplier to
// qualify gigabit training. Other explicit MAC/PHY profiles keep their
// existing device-open budget.
func networkStartupTimeout(macBase uint32, phyAddress uint8, phyID uint32, dhcp bool) time.Duration {
	timeout := defaultDeviceTimeout
	if macBase == 0x10810000 && phyAddress == 1 && phyID == 0x00221622 {
		timeout += 25 * time.Second
	}
	if dhcp {
		timeout += dhcpStartupBudget
	}
	return timeout
}
