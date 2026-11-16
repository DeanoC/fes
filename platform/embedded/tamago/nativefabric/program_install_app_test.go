// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

// Navigate the public App snapshots with ordinary commands. No test calls an
// install/setup/launch API on the side or changes private App state.
type stagedInstallAppPlatform struct {
	fixture          *programMenuFixture
	stage, frames    int
	fault, sawError  bool
	installCompleted bool
	serial           *ProgramSerialPlatform
	presented        tenfoot.Snapshot
}

func (p *stagedInstallAppPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	in := tenfoot.PlatformInput{At: time.Now()}
	// Act on the last displayed view, as the serial operator must. Reading the
	// App's newer asynchronous state could select a control before it is shown.
	s := p.presented
	command := func(c tenfoot.Command) { in.Commands = []tenfoot.Command{c} }
	switch p.stage {
	case 0:
		if p.frames == 0 {
			break
		}
		if !s.Loading {
			if len(s.Games) != 0 {
				return in, fmt.Errorf("installer boot unexpectedly contains playable entries")
			}
			command(tenfoot.CmdSettings)
			p.stage++
		}
	case 1:
		if s.Settings.Open && !s.Settings.Loading && !s.Settings.Busy {
			switch s.Settings.Rows[s.Settings.Index].ID {
			case "section-updates":
				command(tenfoot.CmdSelect)
			case "systems":
				command(tenfoot.CmdSelect)
				p.stage++
			default:
				command(tenfoot.CmdDown)
			}
		}
	case 2:
		v := s.CoreLibrary
		if v.Open && !v.Busy {
			if len(v.Rows) != 2 || v.Rows[1].Core == nil || v.Rows[1].Core.ArtifactState != "available" {
				return in, fmt.Errorf("staged core missing from Systems: %+v", v)
			}
			if v.Index == 1 {
				command(tenfoot.CmdSelect)
				p.stage++
			} else {
				command(tenfoot.CmdDown)
			}
		}
	case 3:
		v := s.CoreLibrary
		if v.Open && !v.Busy && len(v.Rows) == 1 && v.Rows[0].Kind == "install" {
			if p.fault {
				p.fixture.r.f.files.installFault = func(string) error {
					return errors.Join(fat.ErrMutation, errors.New("uncertain installer write"))
				}
			}
			command(tenfoot.CmdSelect)
			p.stage++
		}
	case 4:
		v := s.CoreLibrary
		if !v.Open || v.Busy {
			break
		}
		if p.fault {
			if v.Status == "" || p.fixture.s.Status().State != ProgramRebootRequired || len(s.Games) != 0 {
				return in, fmt.Errorf("installer failure hidden: %+v, %+v", v, p.fixture.s.Status())
			}
			p.sawError = true
			return in, io.EOF
		}
		if p.fixture.s.Status().State != ProgramReady || p.fixture.s.Status().Generation != 1 {
			return in, fmt.Errorf("install did not restore Ready: %+v", p.fixture.s.Status())
		}
		p.installCompleted = true
		found := false
		for _, row := range v.Rows {
			found = found || row.Kind == "library"
		}
		if !found {
			return in, fmt.Errorf("installed setup has no route to its existing playable entry: %+v", v)
		}
		if v.Rows[v.Index].Kind == "library" {
			command(tenfoot.CmdSelect)
			p.stage++
		} else {
			command(tenfoot.CmdDown)
		}
	case 5:
		if !s.Loading && !s.CoreLibrary.Open && !s.Settings.Open && len(s.Games) == 1 {
			if s.Games[0].ID != "staged" || s.Games[0].Title != "Staged core" {
				return in, fmt.Errorf("installed catalogue changed: %+v", s.Games)
			}
			command(tenfoot.CmdSelect)
			p.stage++
		}
	case 6:
		if s.Session.State == "active" && p.fixture.s.Status().State == ProgramRunning {
			command(tenfoot.CmdStop)
			p.stage++
		}
	case 7:
		if s.Session.State == "idle" && !s.GPUParked && p.fixture.s.Status().State == ProgramReady {
			p.stage++
		}
	case 8:
		command(tenfoot.CmdQuit)
		p.stage++
	}
	return in, p.serial.observeProgramCommands(ctx, "uart", in.Commands)
}

func (p *stagedInstallAppPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	p.frames++
	if err := p.serial.Present(ctx, snapshot); err != nil {
		return err
	}
	p.presented = snapshot
	return nil
}

func TestStagedFirstCoreThroughFullTenfootApp(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			f, staged, err := stagedFixture(t, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := NewProgramBackend(f.s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = b.AttachStagedPrograms(context.Background(), []*StagedProgram{staged}); err != nil {
				t.Fatal(err)
			}
			device, err := gfx.NewSoftware(1280, 720)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b}), 1280, 720, 400)
			if err = app.SetPreferenceStore(nativePreferences{}); err != nil {
				t.Fatal(err)
			}
			p := &stagedInstallAppPlatform{fixture: f, fault: fault}
			var capture bytes.Buffer
			p.serial, err = NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(_ context.Context, raw []byte) error {
				_, err := capture.Write(raw)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			p.serial.EnableInstallerViews()
			platform, err := NewProgramMenuPlatform(f.s, p, device.Framebuffer)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = tenfoot.RunPlatform(ctx, app, device, platform)
			if directory := os.Getenv("FES_INSTALLER_MODEL_OUTPUT"); directory != "" {
				out, writeErr := os.OpenFile(filepath.Join(directory, "app-"+fmt.Sprint(fault)+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				_, writeErr = out.Write(capture.Bytes())
				closeErr := out.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatal(writeErr, closeErr)
				}
			}
			if err != nil && !(fault && errors.Is(err, ErrProgramRetired)) {
				t.Fatalf("stage %d: %v", p.stage, err)
			}
			if fault {
				if !(p.sawError || errors.Is(err, ErrProgramRetired)) || len(b.entries) != 0 || f.s.Status().State != ProgramRebootRequired {
					t.Fatal("failed installation escaped retirement", f.s.Status())
				}
				return
			}
			if p.stage != 9 || !p.installCompleted || p.frames < 5 || f.s.Status().Generation != 3 || len(b.entries) != 1 {
				t.Fatal(p.stage, p.frames, f.s.Status())
			}
			if err = f.s.ParkMenu(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
