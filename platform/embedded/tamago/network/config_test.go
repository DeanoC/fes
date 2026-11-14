// SPDX-License-Identifier: GPL-3.0-or-later
package network

import "testing"

func TestStaticConfiguration(t *testing.T) {
	c, err := (Config{}).normalized()
	if err != nil || c.Address != "10.0.2.15/24" || c.Gateway != "10.0.2.2" {
		t.Fatalf("default: %+v, %v", c, err)
	}
	for _, c := range []Config{
		{Address: "bad"}, {Address: "::1/64"}, {Address: "0.0.0.0/24"},
		{Address: "10.0.2.15/32"}, {Address: "10.0.2.0/24"},
		{Address: "10.0.2.255/24"}, {Address: "224.0.2.15/24"},
		{Gateway: "10.0.3.2"}, {Gateway: "10.0.2.0"}, {Gateway: "10.0.2.255"},
		{Gateway: "10.0.2.15"}, {MMIOAddress: 0x09000000},
		{MMIOAddress: 0x0a000001},
	} {
		if _, err := c.normalized(); err == nil {
			t.Errorf("invalid configuration accepted: %+v", c)
		}
	}
}
