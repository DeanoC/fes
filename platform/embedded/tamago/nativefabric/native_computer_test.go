// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNativeComputerBootSelectionIsExplicitAndPerformsNoIO(t *testing.T) {
	native, diagnostic := romProgramTestFor(t, NativeComputerProfile), romProgramTest(t)
	for _, r := range []*romProgramFixture{native, diagnostic} {
		calls := r.cpu.calls
		wrong := NewComputerHPSContextVerifier
		if r == native {
			wrong = NewROMHPSContextVerifier
		}
		if _, err := wrong(r.cpu, r.source, r.control, r.computer, r.romRaw); err == nil {
			t.Fatal("opposite boot constructor accepted a different profile")
		}
		if _, err := NewHPSContextVerifier(r.cpu, r.source); err == nil || r.cpu.calls != calls {
			t.Fatal("default constructor admitted an explicit ROM profile or performed I/O")
		}
	}
	for _, mutate := range []func(*HPSBootContext){
		func(c *HPSBootContext) { c.ABI = "fes.de25-embedded-diagnostic" },
		func(c *HPSBootContext) { c.Minor = 1 },
		func(c *HPSBootContext) { c.CoreID = "fes.other" },
		func(c *HPSBootContext) { c.ControlABI = 0x10203040 },
	} {
		c := native.f.loader.boot.context
		mutate(&c)
		if _, err := NewComputerHPSContextVerifier(native.cpu, programJSON(t, c), native.control, native.computer, native.romRaw); err == nil {
			t.Fatal("unreviewed native boot tuple accepted")
		}
	}
	var rom romControlMap
	if err := json.Unmarshal(native.romRaw, &rom); err != nil {
		t.Fatal(err)
	}
	rom.HPSMaster = true
	if _, err := NewComputerHPSContextVerifier(native.cpu, native.source, native.control, native.computer, programJSON(t, rom)); err == nil {
		t.Fatal("native boot added an HPS master")
	}
	if native.cpu.calls != 0 || native.f.loader.compatibility == nil || native.f.loader.support != nil {
		t.Fatal("native loader touched firmware or bypassed combined runtime compatibility")
	}
}

func TestNativeComputerRejectsSemanticAndDriverFailuresBeforeContainment(t *testing.T) {
	for _, fault := range []string{"missing-video", "optional-video", "unsupported-driver", "missing-policy"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, NativeComputerProfile)
			request, _, _ := addROMProgramChanged(t, r, 0, 8192, func(s string) string {
				video := "[[interfaces]]\nid = \"fes.video.fixed-720p60\"\nmajor = 1\nminor = 0\nrequired = true\n"
				switch fault {
				case "missing-video":
					return strings.Replace(s, video, "", 1)
				case "optional-video":
					return strings.Replace(s, video, strings.Replace(video, "true", "false", 1), 1)
				case "unsupported-driver":
					// Mouse is a recognized semantic computer interface, but this
					// compiled native personality does not implement its driver.
					return s + "\n[[interfaces]]\nid = \"fes.mouse.relative\"\nmajor = 1\nminor = 0\nrequired = true\n"
				}
				return s
			})
			if fault == "missing-policy" {
				r.f.loader.compatibility = nil
			}
			_, err := r.f.s.Launch(context.Background(), 0, request)
			if err == nil {
				t.Fatal("invalid normal package reached activation")
			}
			if fault == "unsupported-driver" && !strings.Contains(err.Error(), "platform driver lacks required interface") {
				t.Fatal("driver rejection did not come from the independent inventory", err)
			}
			if r.cpu.calls != 0 || !r.f.lease.r.Active || r.f.arena.active != nil || r.f.s.Status().State != ProgramReady {
				t.Fatal("rejected content touched firmware, contained SD or retired a healthy service")
			}
			if fault != "missing-policy" {
				good, _, _ := addROMProgram(t, r, 1, 4096)
				session, err := r.f.s.Launch(context.Background(), 0, good)
				if err != nil {
					t.Fatal("content rejection prevented a subsequent valid launch", err)
				}
				if _, err := r.f.s.Return(context.Background(), session); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNativeComputerRejectsChangedLiveBootAfterStorageContainment(t *testing.T) {
	r := romProgramTestFor(t, NativeComputerProfile)
	request, _, _ := addROMProgram(t, r, 0, 8192)
	r.cpu.digest[0] ^= 1 // Independent modeled secure boot context has changed.
	if _, err := r.f.s.Launch(context.Background(), 0, request); err == nil {
		t.Fatal("compiled source identity substituted for live boot ownership")
	}
	if r.f.s.Status().State != ProgramRebootRequired || r.f.lease.r.Active || r.cpu.configurations != 0 || r.f.arena.active != nil {
		t.Fatal("changed live context configured the FPGA or reopened SD")
	}
	calls := r.cpu.calls
	if _, err := r.f.s.Launch(context.Background(), 0, request); err == nil || r.cpu.calls != calls {
		t.Fatal("uncertain live boot was retried")
	}
}

func TestNativeComputerReturnFailureRetainsOwnershipAndCannotReplay(t *testing.T) {
	for _, fault := range []string{"hold", "close", "grant", "source"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, NativeComputerProfile)
			request, _, _ := addROMProgram(t, r, 0, 8192)
			session, err := r.f.s.Launch(context.Background(), 0, request)
			if err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "hold":
				r.cpu.stall = true
			case "close":
				r.cpu.loseClose = true
			case "grant":
				r.f.lease.fault = true
			case "source":
				r.cpu.digest[0] ^= 1
			}
			if _, err := r.f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || r.f.s.Status().State != ProgramRebootRequired || r.f.lease.r.Active {
				t.Fatal("uncertain native return resumed storage", err)
			}
			calls, grants := r.cpu.calls, r.f.lease.returns
			if _, err := r.f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || r.cpu.calls != calls || r.f.lease.returns != grants {
				t.Fatal("uncertain native return was replayed")
			}
		})
	}
}
