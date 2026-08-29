//go:build !fpgadev

package fpgadev

// InventoryV1 is opaque on builds that do not carry the target-only
// fpgadev capability. This keeps the public Task 7 seam linkable while
// ensuring the untagged command binary cannot retain install/proof grammar.
type InventoryV1 struct{ Identity string }

func (i InventoryV1) Validate() error {
	if i.Identity == "" {
		return ErrRunnerConfiguration
	}
	return nil
}

func (i InventoryV1) Equal(other InventoryV1) bool {
	return i.Identity != "" && i.Identity == other.Identity
}
