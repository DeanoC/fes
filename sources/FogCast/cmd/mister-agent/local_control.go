package main

import (
	"context"
	"sort"

	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// durableMeshInventory publishes factory package ids plus durable packages
// from store.List. List reuses a byte verification only while the stat
// fingerprint matches. When that verification fails, this advertisement
// omits the durable library.
func durableMeshInventory(ctx context.Context, roots []string, store *kitpackages.Store) ([]meshcontent.EligibleABI, []string) {
	abis, ids := kitcontent.ReadInstalledPackages(roots)
	installed, err := store.List(ctx)
	if err != nil {
		return abis, ids
	}
	seenID := make(map[string]bool, len(ids))
	seenABI := make(map[meshcontent.EligibleABI]bool, len(abis))
	for _, id := range ids {
		seenID[id] = true
	}
	for _, abi := range abis {
		seenABI[abi] = true
	}
	for _, packageInfo := range installed {
		if !seenID[packageInfo.PackageID] {
			ids = append(ids, packageInfo.PackageID)
			seenID[packageInfo.PackageID] = true
		}
		abi := meshcontent.EligibleABI{ID: packageInfo.Descriptor.ABI.ID, Major: int(packageInfo.Descriptor.ABI.Major)}
		if !seenABI[abi] {
			abis = append(abis, abi)
			seenABI[abi] = true
		}
	}
	sort.Strings(ids)
	sort.Slice(abis, func(i, j int) bool {
		if abis[i].ID == abis[j].ID {
			return abis[i].Major < abis[j].Major
		}
		return abis[i].ID < abis[j].ID
	})
	return abis, ids
}

// localCoreInventory is the kit-local catalog source. A nil store with an
// open error means the durable library cannot be read, including when
// FESDATA3 is absent. The callback returns that error and does not open or
// scan factory packages. A nil callback leaves the factory catalog in place.
func localCoreInventory(ctx context.Context, store *kitpackages.Store, libraryErr error) func() ([]localcores.Core, error) {
	if store != nil {
		return func() ([]localcores.Core, error) { return durableLocalInventory(ctx, store) }
	}
	if libraryErr != nil {
		return func() ([]localcores.Core, error) { return nil, libraryErr }
	}
	return nil
}

func durableLocalInventory(ctx context.Context, store *kitpackages.Store) ([]localcores.Core, error) {
	// SelectedPackages reuses the last byte verification while size, mtime,
	// ctime, device, inode, mode, and directory entries match. Launch still
	// inspects the package bytes.
	selections, err := store.SelectedPackages(ctx)
	if err != nil {
		return nil, err
	}
	cores := make([]localcores.Core, 0, len(selections))
	for _, selection := range selections {
		if selection.CoreID == "fes.menu" {
			continue
		}
		cores = append(cores, localcores.FromInspection(selection.Inspection, selection.Path))
	}
	return cores, nil
}
