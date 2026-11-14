// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// The transport only admits the selected fixed protocol. Firmware still owns
// boot, generation, device/window and independent drain validation.
func invokeMAC(request [7]uint64, selected bool, invoke func([7]uint64) ([4]uint64, error)) ([4]uint64, error) {
	if !selected || invoke == nil || request[0] != uint64(g.FesEmbeddedMacCall) ||
		request[1] > uint64(g.FesEmbeddedMacStop) || request[3] != uint64(g.FesEmbeddedMacCookie) ||
		request[5] != 0 || request[6] != 0 {
		return [4]uint64{}, errors.New("DE25: owned MAC call differs from selected fixed service")
	}
	return invoke(request)
}
