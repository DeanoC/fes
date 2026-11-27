// SPDX-License-Identifier: GPL-3.0-or-later
package agentapi

import (
	"strings"
	"testing"
)

func TestParseProvisioning(t *testing.T) {
	const id = "12345678-2222-4333-8444-555555555555"
	const valid = `{"target_id":"` + id + `","port":8182,"token":"example.token+/="}`
	got, err := ParseProvisioning([]byte(valid))
	if err != nil || got.TargetID != id || got.Port != 8182 || got.Token != "example.token+/=" {
		t.Fatal("valid provisioning rejected")
	}
	for _, bad := range []string{
		"null", valid + "{}", strings.Replace(valid, id, "invalid", 1),
		strings.Replace(valid, "8182", "65536", 1), strings.Replace(valid, "8182", "0", 1),
		strings.Replace(valid, "example.token+/=", "bad token", 1),
		strings.Replace(valid, `"token":`, `"token":"first","token":`, 1),
		strings.Replace(valid, `"port":`, `"open":true,"port":`, 1),
		valid + strings.Repeat(" ", MaxProvisioningBytes),
	} {
		if cfg, err := ParseProvisioning([]byte(bad)); err != ErrInvalidConfig || cfg != (Provisioning{}) {
			t.Fatal("accepted invalid provisioning or returned private input")
		}
	}
}
