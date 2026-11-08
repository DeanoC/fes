// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func TestDiscoveryContextPreservesNoCandidateError(t *testing.T) {
	hw := newController() // Only a modeled boot keyboard; no non-boot HID.
	host, err := OpenGamepad(hw, Config{PHYClockHz: 60000000})
	if !errors.Is(err, usb.ErrNoGamepadInterface) || host == nil || hw.configured || hw.reportFetched ||
		!strings.Contains(err.Error(), "candidate={none}") || !strings.Contains(err.Error(), "addr=1 id=") {
		t.Fatal("no-candidate error lost type or discovery state", err)
	}
	accesses := hw.accesses
	if _, err := host.PollGamepad(); err == nil || hw.accesses != accesses {
		t.Fatal("missing-input host reused")
	}
}

func TestCapturedSystemControlRejectionRetainsCandidateAndNeverPolls(t *testing.T) {
	hw := gamepadController(t)
	d, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/system-control-report.bin")
	if err != nil {
		t.Fatal(err)
	}
	hw.reportDescriptor = d
	hw.configuration[25], hw.configuration[26] = byte(len(d)), byte(len(d)>>8)
	fw := &ownedFirmware{t: t, controller: hw, gamepad: true}
	client, err := usbcontrol.New(context.Background(), fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var host *GamepadHost
	source, err := usbcontrol.OpenGamepad(context.Background(), client, func() (input.USBGamepadReports, error) {
		var err error
		host, err = OpenGamepad(hw, Config{PHYClockHz: 60000000})
		return host, err
	})
	if err == nil || source == nil || host == nil || fw.confirms != 0 || fw.stops != 1 || !bytes.Equal(host.ReportDescriptor(), d) {
		t.Fatal("rejected descriptor lost containment or capture", err, fw)
	}
	// This VID/PID is a model identity, not an observation of the board's device.
	for _, want := range []string{"gamepad application required", "addr=1 id=081f:e401", "candidate={address=1 interface=0 endpoint=0x81 report_bytes=53}"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatal("missing captured discovery context", want, err)
		}
	}
	accesses := hw.accesses
	if _, err := source.PollGamepad(); err == nil || hw.accesses != accesses {
		t.Fatal("rejected source touched controller")
	}
	if _, err := host.PollGamepad(); err == nil || hw.accesses != accesses {
		t.Fatal("retired host touched controller")
	}
}

func TestOwnedGamepadCapturedSchemaUsesEightByteInput(t *testing.T) {
	hw := gamepadController(t)
	descriptor, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/joystick-report.bin")
	if err != nil {
		t.Fatal(err)
	}
	hw.reportDescriptor = descriptor
	hw.configuration[25], hw.configuration[26] = byte(len(descriptor)), byte(len(descriptor)>>8)
	// Constructed endpoint payloads; only the descriptor is a physical capture.
	hw.reports = [][]byte{{128, 128, 0, 0, 0, 0, 0, 0}, {255, 0, 255, 170, 85, 31, 224, 255}}
	fw := &ownedFirmware{t: t, controller: hw, gamepad: true}
	client, err := usbcontrol.New(context.Background(), fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var host *GamepadHost
	source, err := usbcontrol.OpenGamepad(context.Background(), client, func() (input.USBGamepadReports, error) {
		var err error
		host, err = OpenGamepad(hw, Config{PHYClockHz: 60000000})
		return host, err
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := input.NewUSBGamepad(source, host.ReportDescriptor(), input.GamepadButtons{Confirm: 1, Back: 2})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for n := 0; n < 150; n++ {
		events, err := adapter.Poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			keys = append(keys, event.Key)
		}
		if len(keys) >= 3 {
			break
		}
	}
	if len(keys) != 3 || keys[0] != "up" || keys[1] != "right" || keys[2] != "return" || adapter.State().Buttons != 0x201 {
		t.Fatal(keys, adapter.State())
	}
	if err := source.Close(context.Background()); err != nil || fw.confirms != 1 || fw.stops != 1 {
		t.Fatal(err, fw)
	}
	accesses := hw.accesses
	if _, err := source.PollGamepad(); err == nil || hw.accesses != accesses {
		t.Fatal("closed captured-schema source polled", err)
	}
}

func gamepadController(t *testing.T) *controller {
	t.Helper()
	c := newController()
	data, err := hex.DecodeString("05010905a101150026ff007508950209300931810205091901290c150025017501950c8102750495018103c0")
	if err != nil {
		t.Fatal(err)
	}
	c.reportDescriptor = data
	c.configuration[15], c.configuration[16] = 0, 0
	c.configuration[25], c.configuration[26] = byte(len(data)), byte(len(data)>>8)
	c.device[8], c.device[9], c.device[10], c.device[11] = 0x1f, 0x08, 0x01, 0xe4
	return c
}
func TestOwnedGamepadEnumeratesHIDAndRetiresWithoutKeyboardConversion(t *testing.T) {
	hw := gamepadController(t)
	hw.completionFields = 8<<4 | 2<<15
	hw.haltFields = hw.completionFields
	hw.reports = [][]byte{{255, 0, 1, 0}, {128, 128, 0, 0}}
	fw := &ownedFirmware{t: t, controller: hw, gamepad: true}
	client, err := usbcontrol.New(context.Background(), fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var host *GamepadHost
	source, err := usbcontrol.OpenGamepad(context.Background(), client, func() (input.USBGamepadReports, error) {
		var e error
		host, e = OpenGamepad(hw, Config{PHYClockHz: 60000000})
		return host, e
	})
	if err != nil || fw.confirms != 1 || !hw.reportFetched || hw.protocol {
		t.Fatal(err)
	}
	if _, isKeyboard := any(host).(input.USBReports); isKeyboard {
		t.Fatal("gamepad exposed keyboard protocol")
	}
	for _, r := range hw.requests {
		if r.request == 11 || r.request == 10 {
			t.Fatal("boot-only request sent to gamepad")
		}
	}
	copy := host.ReportDescriptor()
	copy[0] ^= 255
	if bytes.Equal(copy, host.ReportDescriptor()) {
		t.Fatal("descriptor aliases controller policy")
	}
	adapter, err := input.NewUSBGamepad(source, host.ReportDescriptor(), input.GamepadButtons{Confirm: 1, Back: 2})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for n := 0; n < 150; n++ {
		events, e := adapter.Poll()
		if e != nil {
			t.Fatal(e)
		}
		for _, event := range events {
			keys = append(keys, event.Key)
		}
		if len(keys) >= 3 {
			break
		}
	}
	if len(keys) != 3 || keys[0] != "up" || keys[1] != "right" || keys[2] != "return" {
		t.Fatal(keys)
	}
	if err := source.Close(context.Background()); err != nil || fw.stops != 1 {
		t.Fatal(err)
	}
	accesses := hw.accesses
	if _, err := source.PollGamepad(); err == nil || hw.accesses != accesses {
		t.Fatal("closed gamepad touched hardware")
	}
}
func TestGamepadRejectsWrongLayoutAndAmbiguityBeforeConfirmation(t *testing.T) {
	for _, mode := range []string{"mouse", "oversize", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			hw := gamepadController(t)
			if mode == "mouse" {
				hw.reportDescriptor[3] = 2
			}
			if mode == "oversize" {
				hw.configuration[31] = 1
			}
			if mode == "ambiguous" {
				extra := bytes.Clone(hw.configuration[9:])
				extra[2] = 1
				hw.configuration = append(hw.configuration, extra...)
				hw.configuration[2] = byte(len(hw.configuration))
				hw.configuration[4] = 2
			}
			fw := &ownedFirmware{t: t, controller: hw, gamepad: true}
			client, err := usbcontrol.New(context.Background(), fw, 17)
			if err != nil {
				t.Fatal(err)
			}
			var partial *GamepadHost
			s, err := usbcontrol.OpenGamepad(context.Background(), client, func() (input.USBGamepadReports, error) {
				var e error
				partial, e = OpenGamepad(hw, Config{PHYClockHz: 60000000})
				return partial, e
			})
			if err == nil || s == nil || partial == nil || fw.confirms != 0 || fw.stops != 1 {
				t.Fatal(err, fw)
			}
			if mode == "ambiguous" && hw.configured {
				t.Fatal("ambiguous device was configured")
			}
			if mode != "ambiguous" && !bytes.Equal(partial.ReportDescriptor(), hw.reportDescriptor) {
				t.Fatal("failed schema lost the captured descriptor")
			}
			accesses := hw.accesses
			if _, err := s.PollGamepad(); err == nil || hw.accesses != accesses {
				t.Fatal("failed source reused")
			}
		})
	}
}
