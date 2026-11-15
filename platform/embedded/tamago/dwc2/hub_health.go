// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"encoding/binary"
	"fmt"
	"time"
)

// A GET_PORT_STATUS progresses one transaction step per invocation. The caller
// applies a fixed per-poll budget; it never overlaps a keyboard packet.
func (h *Host) pollHubHealth() (bool, error) {
	port := h.path[h.healthIndex]
	if h.healthStage == 0 {
		h.selectTarget(port.hub)
		h.healthDeadline = h.io.Now().Add(2 * time.Second)
		h.healthStage = 1
		h.healthPolls = 0
		setup := []byte{0xa3, 0, 0, 0, byte(port.port), byte(port.port >> 8), 4, 0}
		return true, h.begin(0, h.ep0, 0, false, 3, setup, 0, h.healthDeadline)
	}
	h.healthPolls++
	if h.healthPolls > 4096 || !h.io.Now().Before(h.healthDeadline) {
		return true, fmt.Errorf("dwc2: hub health poll budget exhausted")
	}
	done, data, nak, err := h.pollStep()
	if err != nil {
		return true, err
	}
	if !done {
		return true, nil
	}
	if nak {
		switch h.healthStage {
		case 1:
			return true, fmt.Errorf("dwc2: hub rejected status setup")
		case 2:
			return true, h.begin(0, h.ep0, 0, true, 2, nil, 4, h.healthDeadline)
		case 3:
			return true, h.begin(0, h.ep0, 0, false, 2, nil, 0, h.healthDeadline)
		}
	}
	switch h.healthStage {
	case 1:
		h.healthStage = 2
		return true, h.begin(0, h.ep0, 0, true, 2, nil, 4, h.healthDeadline)
	case 2:
		if len(data) != 4 {
			return true, fmt.Errorf("dwc2: short hub health status")
		}
		h.healthStatus = binary.LittleEndian.Uint32(data)
		h.healthStage = 3
		return true, h.begin(0, h.ep0, 0, false, 2, nil, 0, h.healthDeadline)
	case 3:
		if !validPort(h.healthStatus, port.speed) {
			return true, fmt.Errorf("dwc2: selected hub path disconnected/reset/changed")
		}
		h.selectTarget(h.keyboard)
		h.healthStage = 0
		h.healthIndex = (h.healthIndex + 1) % len(h.path)
		h.healthNext = h.io.Now().Add(100 * time.Millisecond)
		return true, nil
	}
	return true, fmt.Errorf("dwc2: invalid hub health phase")
}
