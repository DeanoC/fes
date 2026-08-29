//go:build fpgadev && !linux

package fpgadev

import "context"

func productionLiveProofCheck(context.Context, BootProof) error { return ErrUnsupported }

func resolveInventoryPlatform(context.Context, InventoryV1, InventoryResolutionOptions) (ResolvedInventoryV1, error) {
	return ResolvedInventoryV1{}, ErrUnsupported
}

func resolveInventoryPathPlatform(context.Context, PathExpectation, bool, InventoryResolutionOptions) (ResolvedPathV1, error) {
	return ResolvedPathV1{}, ErrUnsupported
}

func resolveInventorySourcePlatform(context.Context, SourceRecord, InventoryResolutionOptions) (ResolvedSourceV1, error) {
	return ResolvedSourceV1{}, ErrUnsupported
}
