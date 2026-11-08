// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestProgramCatalogBoundedNamesCannotChoosePackagePaths(t *testing.T) {
	choices := []packagestore.Request{{Path: "selected", PackageID: strings.Repeat("a", 64)}}
	raw := []byte("{\"entries\":[{\"id\":\"zx81\",\"program\":0,\"title\":\"ZX81\"}],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")
	read := func(data []byte) ([]ProgramEntry, error) {
		return ReadProgramCatalog(context.Background(), fstest.MapFS{"UI.JSON": &fstest.MapFile{Data: data}}, "UI.JSON", choices)
	}
	entries, err := read(raw)
	if err != nil || len(entries) != 1 || entries[0].Request.Path != "selected" {
		t.Fatal(entries, err)
	}
	for _, bad := range [][]byte{bytes.Replace(raw, []byte(`"program":0`), []byte(`"program":1`), 1), bytes.Replace(raw, []byte(`"program":0`), []byte(`"program":-1`), 1), bytes.Replace(raw, []byte(`"title":"ZX81"`), []byte(`"title":"ZX81","path":"evil"`), 1), bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1), bytes.Repeat([]byte("x"), 8193)} {
		if _, err := read(bad); err == nil {
			t.Fatal("invalid catalog accepted")
		}
	}
}
func TestSerialQuitWaitsForOwnedTransitionAndBoundsInput(t *testing.T) {
	f := serviceTest(t)
	queue := []byte("q")
	reads := 0
	p, err := NewProgramSerialPlatform(f.s, func() (byte, bool) {
		reads++
		if len(queue) == 0 {
			return 0, false
		}
		v := queue[0]
		queue = queue[1:]
		return v, true
	}, func(context.Context, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	v := f.s.Status()
	v.State = ProgramLoading
	f.s.state.publish(v)
	frame, err := p.Next(context.Background())
	if err != nil || len(frame.Commands) != 0 {
		t.Fatal("quit bypassed pending launch", frame, err)
	}
	v.State = ProgramRunning
	f.s.state.publish(v)
	frame, err = p.Next(context.Background())
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatal(frame, err)
	}
	frame, err = p.Next(context.Background())
	if err != nil || len(frame.Commands) != 0 {
		t.Fatal("Stop was replayed", frame, err)
	}
	v.State = ProgramReady
	f.s.state.publish(v)
	frame, err = p.Next(context.Background())
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdQuit {
		t.Fatal(frame, err)
	}
	p, err = NewProgramSerialPlatform(f.s, func() (byte, bool) { reads++; return 'z', true }, func(context.Context, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reads = 0
	if _, err = p.Next(context.Background()); err != nil || reads != 64 {
		t.Fatal("unbounded UART read", reads, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads = 0
	if _, err = p.Next(ctx); !errors.Is(err, context.Canceled) || reads != 0 {
		t.Fatal("cancelled poll read UART")
	}
}
func TestSerialEmitterFailureAndVolatilePreferenceOwnership(t *testing.T) {
	f := serviceTest(t)
	fault := errors.New("serial write lost")
	p, _ := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(context.Context, []byte) error { return fault })
	if err := p.Present(context.Background(), tenfoot.Snapshot{}); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	prefs := &ProgramPreferences{}
	raw := []byte(`{"home":"library"}`)
	if err := prefs.WritePreferences(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	got, _ := prefs.ReadPreferences()
	got[0] = '!'
	again, _ := prefs.ReadPreferences()
	if string(again) != `{"home":"library"}` {
		t.Fatal("preferences retain caller memory")
	}
}

func TestFullTenfootSerialNativeLaunchReturnAndFailures(t *testing.T) {
	for _, fault := range []string{"", "launch", "return"} {
		t.Run(fault, func(t *testing.T) {
			f, b, _ := backendFixture(t)
			if fault == "launch" {
				f.fault = "identity"
			}
			queue := []byte{}
			stage := 0
			states := 0
			fetches := map[uint64]bool{}
			var actions []string
			emit := func(_ context.Context, raw []byte) error {
				const actionPrefix = "FES_NATIVE_INPUT_ACTION source=uart action="
				if bytes.HasPrefix(raw, []byte(actionPrefix)) {
					action, _, _ := strings.Cut(strings.TrimPrefix(string(raw), actionPrefix), " ")
					v := f.s.Status()
					if (action != "select" && action != "stop" && action != "quit") || string(raw) != fmt.Sprintf(actionPrefix+"%s state=%s owner=%x native_generation=%d\n", action, v.State, v.Owner, v.Generation) {
						return errors.New("unbound serial action observation")
					}
					actions = append(actions, action)
					return nil
				}
				if bytes.HasPrefix(raw, []byte("FES_NATIVE_CORE_FETCH ")) {
					var observation ProgramFetchObservation
					if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("FES_NATIVE_CORE_FETCH ")), &observation); err != nil {
						return err
					}
					expected, ok := f.s.FetchObservation()
					if !ok || observation != expected || fetches[observation.Generation] {
						return errors.New("unbound or duplicate fetch observation")
					}
					fetches[observation.Generation] = true
					return nil
				}
				var view struct {
					Display     string               `json:"display"`
					Loading     bool                 `json:"loading"`
					Titles      []string             `json:"titles"`
					Focus       int                  `json:"focus"`
					Session     string               `json:"session"`
					LaunchError string               `json:"launch_error"`
					Retry       bool                 `json:"retry_stop"`
					Native      ProgramServiceStatus `json:"native"`
				}
				if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("FES_NATIVE_TENFOOT_STATE ")), &view); err != nil {
					return err
				}
				states++
				if view.Display != "serial-state-only" {
					return errors.New("serial output pretended to be display")
				}
				if view.Native.State == ProgramRebootRequired && (view.LaunchError != "" || view.Retry) {
					queue = []byte("q")
					stage = 9
					return nil
				}
				switch stage {
				case 0:
					if !view.Loading && len(view.Titles) == 2 && view.Native.State == ProgramReady {
						queue = []byte("\r")
						stage = 1
					}
				case 1:
					if view.Session == "active" && view.Native.State == ProgramRunning {
						if fault == "return" {
							f.cpu.loseClose = true
						}
						queue = []byte("s")
						stage = 2
					}
				case 2:
					if view.Session == "idle" && view.Native.State == ProgramReady {
						queue = []byte("\x1b[C")
						stage = 3
					}
				case 3:
					if view.Focus == 1 {
						queue = []byte("\r")
						stage = 4
					}
				case 4:
					if view.Session == "active" && view.Native.State == ProgramRunning {
						queue = []byte("q")
						stage = 5
					}
				}
				return nil
			}
			platform, err := NewProgramSerialPlatform(f.s, func() (byte, bool) {
				if len(queue) == 0 {
					return 0, false
				}
				v := queue[0]
				queue = queue[1:]
				return v, true
			}, emit)
			if err != nil {
				t.Fatal(err)
			}
			device, err := gfx.NewSoftware(640, 480)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 10 * time.Second}), 640, 480, 100)
			if err := app.SetPreferenceStore(&ProgramPreferences{}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := tenfoot.RunPlatform(ctx, app, device, platform); err != nil {
				t.Fatalf("%v stage=%d state=%+v", err, stage, f.s.Status())
			}
			if fault == "" {
				if stage != 5 || len(fetches) != 2 || f.s.Status().State != ProgramReady || f.s.Status().Generation != 4 || !f.lease.r.Active || f.cpu.configurations != 2 {
					t.Fatal(stage, f.s.Status())
				}
			} else if stage != 9 || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active {
				t.Fatal("failure escaped containment")
			}
			if states < 2 {
				t.Fatal("App state was not emitted")
			}
			expectedActions := "select,stop,select,stop,quit"
			if fault == "launch" {
				expectedActions = "select,quit"
			} else if fault == "return" {
				expectedActions = "select,stop,quit"
			}
			if strings.Join(actions, ",") != expectedActions {
				t.Fatal("lifecycle action origins missing or repeated", actions)
			}
		})
	}
}
