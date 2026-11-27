// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"crypto/sha256"
	"errors"
	"net"

	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
)

// pairedStation derives a stable local unicast address from the provisioned
// FogCast target. The boot UUID and bearer never enter a link-layer frame.
func pairedStation(targetID string) (string, error) {
	if !mdns.ValidID(targetID) || targetID == "00000000-0000-0000-0000-000000000000" {
		return "", errors.New("de25-nano: paired station requires a provisioned target UUID")
	}
	sum := sha256.Sum256([]byte("FES DE25 paired station v1\x00" + targetID))
	sum[0] = (sum[0] & 0xfc) | 0x02
	return net.HardwareAddr(sum[:6]).String(), nil
}
