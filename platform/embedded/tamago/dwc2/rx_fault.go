// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// rxFault retains non-destructive register observations at the first invalid
// receive record. In particular a zero status is still an error: these samples
// help distinguish an empty/stale FIFO indication from a malformed queue head
// on hardware, without guessing which condition occurred or reading it again.
func (h *Host) rxFault(reason string, irq, peek, popped uint32) error {
	return fmt.Errorf("dwc2: %s; RX snapshot irq=%#x peek=%#x pop=%#x after=%#x channel=%#x interrupts=%#x transfer=%#x port=%#x reset=%#x",
		reason, irq, peek, popped, h.io.Read32(gint), h.io.Read32(hcchar), h.io.Read32(hcint),
		h.io.Read32(hctsiz), h.io.Read32(hprt), h.io.Read32(reset))
}
