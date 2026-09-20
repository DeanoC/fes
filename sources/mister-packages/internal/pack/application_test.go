package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationContractAndLegacyConstants(t *testing.T) {
	root := repoRoot(t)
	app, err := LoadABI(filepath.Join(root, "packages/abi/fes_application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "fes.application" || app.Tag != 3 || app.Major != 1 || app.Minor != 0 {
		t.Fatalf("application metadata: %#v", app)
	}
	for _, legacy := range []struct{ file, prefix string }{{"fes_simple_game.yaml", "FesGp"}, {"fes_simple_computer.yaml", "FesSimpleComputer"}} {
		abi, err := LoadABI(filepath.Join(root, "packages/abi", legacy.file))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range app.Constants {
			suffix := strings.TrimPrefix(c.Name, "FesApplication")
			if suffix == "AbiTag" {
				continue
			}
			if value, ok := abi.Constant(legacy.prefix + suffix); ok && value != uint32(c.Value) {
				t.Errorf("legacy %s differs for %s", legacy.file, suffix)
			}
		}
	}
	want := []string{"fes.gamepad", "fes.video.fixed-720p60", "fes.media.blob", "fes.media.blob-stream", "fes.audio.pcm-s16-stereo-48k", "fes.gamepad.ports", "fes.keypad.ports", "fes.firmware.blob"}
	if len(app.Interfaces) != len(want) {
		t.Fatal("interface count")
	}
	for i, iface := range app.Interfaces {
		if iface.ID != want[i] || iface.CapabilityBit != uint8(i) || iface.Major != 1 || iface.Minor != 0 {
			t.Fatalf("interface %d: %#v", i, iface)
		}
	}
	for name, want := range map[string]uint32{"AbiTag": 3, "OpcodeExecution": 2, "OpcodeButtons": 3, "ButtonMask": 255, "OpcodeMediaStreamAbort": 12, "OpcodeControllerButtons": 13, "OpcodeControllerKeypad": 14, "ControllerPortCount": 2, "ControllerButtonMask": 255, "ControllerKeypadMask": 4095, "OpcodeFirmwareBegin": 15, "OpcodeFirmwareData": 16, "OpcodeFirmwareCommit": 17, "FirmwareBytes": 8192} {
		got, ok := app.Constant("FesApplication" + name)
		if !ok || got != want {
			t.Fatalf("%s=%d", name, got)
		}
	}
}

func TestApplicationGoldenExchanges(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata/fes-application-v1/exchanges.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Scenarios []struct {
			Name                 string
			Capabilities         uint16
			InitialRequestToggle bool   `json:"initial_request_toggle"`
			BuildID              string `json:"build_id"`
			Exchanges            []fesGPExchangeFixture
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Scenarios) != 2 {
		t.Fatal("expected two independent sessions")
	}
	for i, s := range fixture.Scenarios {
		t.Run(s.Name, func(t *testing.T) {
			caps := uint16(2)
			if i == 1 {
				caps = 7
			}
			if s.Capabilities != caps || s.BuildID != "00112233445566778899aabbccddeeff" {
				t.Fatal("identity fixture")
			}
			words := []uint16{0x4546, 0x3153, 1, 0, 3, 1, 0, caps, 0x1100, 0x3322, 0x5544, 0x7766, 0x9988, 0xbbaa, 0xddcc, 0xffee}
			toggle := s.InitialRequestToggle
			held, ready, open := true, false, false
			total, received := uint32(0), uint32(0)
			for n, x := range s.Exchanges {
				if len(x.GPO) != 2 || x.GPO[0]^x.GPO[1] != 0x80000000 || (x.GPO[0]&0x80000000 != 0) != toggle {
					t.Fatalf("%s: request framing", x.Name)
				}
				toggle = !toggle
				op, index, arg := x.GPO[1]>>24&127, x.GPO[1]>>16&255, x.GPO[1]&65535
				response, code := uint16(0), uint16(0)
				switch op {
				case 1:
					if index >= 16 {
						code = 2
					} else if arg != 0 {
						code = 3
					} else {
						response = words[index]
					}
				case 2:
					if index != 0 {
						code = 2
					} else if arg > 1 {
						code = 3
					} else if arg == 1 && (open || caps&4 != 0 && !ready) {
						code = 4
					} else {
						held = arg == 0
					}
				case 3:
					if caps&1 == 0 {
						code = 1
					} else if index != 0 {
						code = 2
					} else if arg > 255 {
						code = 3
					}
				case 4:
					if caps&4 == 0 {
						code = 1
					} else if !held || open {
						code = 4
					} else if index != 0 {
						code = 2
					} else if arg == 0 || arg > 16384 {
						code = 3
					} else {
						total, received = arg, 0
						open, ready = true, false
					}
				case 5:
					if caps&4 == 0 {
						code = 1
					} else if !held || !open {
						code = 4
					} else if index > 1 {
						code = 2
					} else {
						count := uint32(2)
						if index == 1 {
							count = 1
						}
						if received+count > total || index == 1 && (received+1 != total || arg > 255) {
							code = 3
						} else {
							received += count
						}
					}
				case 6:
					if caps&4 == 0 {
						code = 1
					} else if !held || !open || received != total {
						code = 4
					} else if index != 0 {
						code = 2
					} else if arg != 0 {
						code = 3
					} else {
						open, ready = false, true
					}
				default:
					code = 1
				}
				if code != 0 {
					response = code
				}
				want := uint32(0xf5000000) | uint32(response)
				if toggle {
					want |= 0x800000
				}
				if code != 0 {
					want |= 0x400000
				}
				if x.GPI != want || x.Data != response {
					t.Fatalf("%s: response %#x want %#x", x.Name, x.GPI, want)
				}
				if n < 16 && (op != 1 || index != uint32(n) || arg != 0) {
					t.Fatal("identity must precede activation")
				}
			}
		})
	}
}

func TestControllerPortGoldenFraming(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata/fes-application-v1/controllers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Capabilities uint16
		Exchanges    []struct {
			Name    string
			GPO     []uint32
			GPI     uint32
			Data    uint16
			Buttons [2]uint16
			Keypad  [2]uint16
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Capabilities != 98 || len(fixture.Exchanges) != 11 {
		t.Fatal("controller fixture identity")
	}
	requests := []uint32{0x01070000, 0x0d000011, 0x0d010028, 0x0e000400, 0x0e010800, 0x0d0200ff, 0x0d000100, 0x0e011000, 0x03000001, 0x0d000000, 0x02000000}
	responses := []uint16{98, 0, 0, 0, 0, 2, 3, 3, 1, 0, 0}
	for i, x := range fixture.Exchanges {
		oldToggle := uint32(i%2) << 31
		newToggle := oldToggle ^ 0x80000000
		if len(x.GPO) != 2 || x.GPO[0] != requests[i]|oldToggle || x.GPO[1] != requests[i]|newToggle {
			t.Fatalf("%s framing", x.Name)
		}
		want := uint32(0xf5000000) | newToggle>>8 | uint32(responses[i])
		if i >= 5 && i <= 8 {
			want |= 0x400000
		}
		if x.GPI != want || x.Data != responses[i] {
			t.Fatalf("%s response", x.Name)
		}
		if i >= 4 && i <= 8 && (x.Buttons != [2]uint16{17, 40} || x.Keypad != [2]uint16{1024, 2048}) {
			t.Fatalf("%s isolated state", x.Name)
		}
	}
	last := fixture.Exchanges[10]
	if last.Buttons != [2]uint16{} || last.Keypad != [2]uint16{} {
		t.Fatal("Hold must clear both ports")
	}
}
