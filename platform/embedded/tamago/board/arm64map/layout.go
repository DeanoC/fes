// SPDX-License-Identifier: GPL-3.0-or-later
// Package arm64map maps explicitly reserved DMA memory using TamaGo's existing
// tables. It never allocates page tables or changes the Go heap's attributes.
package arm64map

import "fmt"

func Validate(base, size, ramBase, ramSize uint64) error {
	const alignment = uint64(2 << 20)
	if size == 0 || ramSize == 0 || base%alignment != 0 || size%alignment != 0 || ramBase%alignment != 0 || ramSize%alignment != 0 || base >= 1<<39 || ramBase >= 1<<39 || size > 1<<39-base || ramSize > 1<<39-ramBase {
		return fmt.Errorf("DMA mapping requires bounded aligned physical memory")
	}
	if base < ramBase+ramSize && ramBase < base+size {
		return fmt.Errorf("DMA overlaps Go RAM")
	}
	// Pinned TamaGo already creates an L2 table for the section containing text.
	// Refuse a new L1 section rather than allocate a table in an unknown owner.
	if base>>30 != ramBase>>30 || (base+size-1)>>30 != ramBase>>30 {
		return fmt.Errorf("DMA requires the existing Go RAM L1 section")
	}
	return nil
}
