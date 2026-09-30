package main

import (
	"context"
	"errors"
	"sync"

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
