package main

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

// kitLocalProgram is set when a kit-local load may have programmed the FPGA.
// Those loads do not update the coordinator, so an idle coordinator is not
// proof that the runtime is idle.
type kitLocalProgram struct {
	mu         sync.Mutex
	programmed bool
}

func (p *kitLocalProgram) mark() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.programmed = true
	p.mu.Unlock()
}

func (p *kitLocalProgram) clear() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.programmed = false
	p.mu.Unlock()
}

func (p *kitLocalProgram) active() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.programmed
}

type nativeLocalRuntime struct {
	runtime *misterruntime.Runtime
	program *kitLocalProgram
}

func (n nativeLocalRuntime) LoadCore(admission, operation context.Context, path, packageID string) error {
	if n.runtime == nil {
		return errors.New("runtime is closed")
	}
	inspection, err := corepackage.InspectPackage(path)
	if err != nil || inspection.PackageID != packageID {
		return errors.New("the core is not installed")
	}
	archive, err := corepackage.CanonicalArchive(path)
	if err != nil {
		return errors.New("the installed core could not be read")
	}
	verified, apiErr := n.runtime.InspectCore(admission, int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil {
		return apiErr
	}
	if verified.PackageID != packageID || !verified.Compatible {
		return errors.New("installed core identity or compatibility changed")
	}
	// Mark before dispatch. load_core can program the FPGA and still return an error.
	if n.program != nil {
		n.program.mark()
	}
	// The persistent library stays immutable. The existing load path stages
	// verified bytes beneath the native runtime's trusted development root.
	activation, _, apiErr := n.runtime.LoadCoreOwned(admission, operation, operation, int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil {
		return apiErr
	}
	if activation.PackageID != packageID {
		return errors.New("installed core load failed")
	}
	return nil
}

func (n nativeLocalRuntime) LoadCartridge(admission, operation context.Context, installPath, packageID string, rom []byte) error {
	if n.runtime == nil {
		return errors.New("runtime is closed")
	}
	if len(rom) == 0 {
		return errors.New("cartridge is empty")
	}
	// The kit install is an extracted directory. Rebuild the canonical
	// archive from those members and hand it to the same ROM-link load the
	// host uses. This does not dial a host.
	inspection, err := corepackage.InspectPackage(installPath)
	if err != nil || inspection.PackageID != packageID {
		return errors.New("the core is not installed")
	}
	if inspection.Descriptor.Format != 3 || inspection.Descriptor.ROM == nil {
		return errors.New("the installed core cannot take a cartridge")
	}
	archive, err := corepackage.CanonicalArchive(installPath)
	if err != nil {
		return errors.New("the installed core could not be read")
	}
	verified, apiErr := n.runtime.InspectCore(admission, int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil {
		return apiErr
	}
	if verified.PackageID != packageID || !verified.Compatible {
		return errors.New("installed core identity or compatibility changed")
	}
	envelope, err := corepackage.WriteROMInput(corepackage.ROMInput{Package: archive, ROM: rom})
	if err != nil {
		return errors.New("this cartridge does not fit the installed core")
	}
	// Mark before dispatch. load_rom_core can program the FPGA and still return an error.
	if n.program != nil {
		n.program.mark()
	}
	activation, _, apiErr := n.runtime.LoadCoreOwned(admission, operation, operation, int64(len(envelope)), bytes.NewReader(envelope))
	if apiErr != nil {
		if apiErr.Message != "" {
			return errors.New(apiErr.Message)
		}
		return errors.New("installed core load failed")
	}
	if activation.PackageID != packageID || activation.ROMLink == nil {
		return errors.New("installed core load failed")
	}
	return nil
}

func (n nativeLocalRuntime) Stop(admission, operation context.Context) error {
	if n.runtime == nil {
		return errors.New("runtime is closed")
	}
	_, apiErr := n.runtime.StopOwned(admission, operation)
	if apiErr != nil {
		return apiErr
	}
	if n.program != nil {
		n.program.clear()
	}
	return nil
}

var _ localcores.Runtime = nativeLocalRuntime{}
var _ localcores.CartridgeRuntime = nativeLocalRuntime{}

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
	// SelectedPackages trusts install-time verification while the stat
	// fingerprint matches. Launch still inspects the package bytes.
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
