// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"testing"
)

func usbContextFixture(t *testing.T, profile, core string) []byte {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(hpsContextFixture(t), &fields); err != nil {
		t.Fatal(err)
	}
	fields["programming_profile"] = profile
	fields["control_abi"] = uint32(0x10203041)
	if profile == NativeComputerProfile {
		fields["core_id"], fields["abi"] = core, "fes.computer"
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestUSBContextAcceptsHeldSourcesThroughReadOnlySurface(t *testing.T) {
	for _, item := range [][2]string{{HPSFirstDDRDiagnosticProfile, ""}, {HPSFirstROMDiagnosticProfile, ""}, {NativeComputerProfile, "fes.zx81"}, {NativeComputerProfile, "fes.zx80"}} {
		t.Run(item[0]+item[1], func(t *testing.T) {
			raw := usbContextFixture(t, item[0], item[1])
			digest := sha256.Sum256(raw)
			calls := 0
			cpu := hpsContextCPU(t, hpsContextReplies(digest), -1, &calls)
			v, err := NewUSBHPSContextVerifier(cpu, raw)
			if err != nil || calls != 0 {
				t.Fatal("inspection construction performed I/O or failed", err, calls)
			}
			methods := reflect.TypeOf(v)
			if methods.NumMethod() != 3 {
				t.Fatal("USB inspection exposed additional capabilities")
			}
			for _, name := range []string{"Identity", "Verify", "ProbeHPSIO"} {
				if _, ok := methods.MethodByName(name); !ok {
					t.Fatal("missing inspection method", name)
				}
			}
			if _, ok := any(v).(BootHandoffVerifier); ok {
				t.Fatal("source inspection became live boot authority")
			}
			observed, err := v.Verify(v.Identity())
			if err != nil || calls != 4 || observed.Digest != digest || !observed.Bridges.NeverEnabled {
				t.Fatal(observed, err, calls)
			}
			if item[0] != HPSFirstDDRDiagnosticProfile {
				if _, err := NewHPSContextVerifier(cpu, raw); err == nil || calls != 4 {
					t.Fatal("generic verifier inherited held source scope", err, calls)
				}
			}
		})
	}
}

func TestUSBContextRejectsMalformedUnsupportedAndLostReplies(t *testing.T) {
	raw := usbContextFixture(t, NativeComputerProfile, "fes.zx81")
	var original map[string]any
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{"abi": "other", "abi_major": 2, "abi_minor": 1, "control_abi": 0x10203042, "core_id": "fes.other", "programming_profile": HPSFirstWindowDiagnosticProfile} {
		changed := make(map[string]any)
		for k, v := range original {
			changed[k] = v
		}
		changed[field] = value
		encoded, _ := json.Marshal(changed)
		calls := 0
		if _, err := NewUSBHPSContextVerifier(hpsContextCPU(t, nil, -1, &calls), append(encoded, '\n')); err == nil || calls != 0 {
			t.Fatal(field, err, calls)
		}
	}
	for failure := 0; failure < 4; failure++ {
		calls := 0
		v, err := NewUSBHPSContextVerifier(hpsContextCPU(t, hpsContextReplies(sha256.Sum256(raw)), failure, &calls), raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(v.Identity()); err == nil || calls != failure+1 {
			t.Fatal("lost reply retried or became successful", failure, calls, err)
		}
	}
}
