package main

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/localcores"
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
	// Mark before dispatch. load_core can program the FPGA and still return an error.
	if n.program != nil {
		n.program.mark()
	}
	response, err := n.runtime.LoadInstalledCoreOwned(admission, operation, path, packageID)
	if err != nil {
		return err
	}
	if !response.OK {
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
