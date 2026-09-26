package main

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

type observationControl struct {
	misterruntime.Control
	response misterruntime.Protocol2Response
}

func (c observationControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return c.response, nil
}

func TestObserveRuntimeInputBindsComputerKeyboardHID(t *testing.T) {
	generation := uint64(4)
	response := func(abi string, interfaces ...string) misterruntime.Protocol2Response {
		r := misterruntime.Protocol2Response{OK: true, State: "running_development", Generation: &generation,
			ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: strings.Repeat("a", 64), Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: abi, Major: 1}}}}
		for _, id := range interfaces {
			r.Capabilities.ActiveInterfaces = append(r.Capabilities.ActiveInterfaces, misterruntime.Protocol2Interface{ID: id, Major: 1})
		}
		return r
	}
	obs, err := observeRuntimeInput(context.Background(), misterruntime.NewRuntime(observationControl{response: response("fes.computer", "fes.gamepad.ports", "fes.keyboard.hid")}, "", 0, 0))
	if err != nil || obs.KeyboardHID == nil || obs.KeyboardHID.Generation != 4 || obs.Keyboard || obs.Binding == nil || obs.Binding.Keypad {
		t.Fatalf("computer observation %+v %v", obs, err)
	}
	obs, err = observeRuntimeInput(context.Background(), misterruntime.NewRuntime(observationControl{response: response("fes.computer", "fes.keyboard.hid")}, "", 0, 0))
	if err != nil || obs.KeyboardHID == nil || obs.Binding != nil || !obs.Active {
		t.Fatalf("keyboard-only observation %+v %v", obs, err)
	}
	obs, err = observeRuntimeInput(context.Background(), misterruntime.NewRuntime(observationControl{response: response("fes.simple-computer", "fes.keyboard.hid")}, "", 0, 0))
	if err != nil || obs.KeyboardHID != nil {
		t.Fatalf("HID outside fes.computer bound: %+v %v", obs, err)
	}
}
