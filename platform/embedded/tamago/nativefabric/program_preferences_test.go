// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// The modeled filesystem implements the real store's synchronous write/flush
// contract. Every access independently checks the SD lease's current receipt.
func (f *programTestFiles) WriteFile(path string, data []byte) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return errors.New("write while SD contained")
	}
	f.preferenceWrites++
	if f.preferenceWrite != nil {
		if err := f.preferenceWrite(); err != nil {
			return err
		}
	}
	f.files[path] = &fstest.MapFile{Data: bytes.Clone(data)}
	return nil
}

func preferenceMenuFixture(t *testing.T, persistent bool) (*programMenuFixture, *ProgramPreferences) {
	t.Helper()
	var prefs *ProgramPreferences
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		f.r.f.files.files[packagestore.PreferencesPath] = &fstest.MapFile{Data: []byte(`{"home":"library","attract_enabled":false}`)}
		var err error
		prefs, err = LoadProgramPreferences(context.Background(), f.r.f.store, persistent)
		if err != nil {
			t.Fatal(err)
		}
	})
	if err := f.s.AttachPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	return f, prefs
}

func TestProgramPreferencesCheckpointLaunchReturnAndShutdown(t *testing.T) {
	f, prefs := preferenceMenuFixture(t, true)
	app := tenfoot.NewApp(nil, 640, 480, 100)
	if err := app.SetPreferenceStore(prefs); err != nil {
		t.Fatal(err)
	}
	app.HandleCommand(tenfoot.CmdLayoutCycle, time.Time{})
	if !prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 0 {
		t.Fatal("menu setting touched contained SD")
	}
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	session, err := f.s.Launch(context.Background(), 0, request)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 1 {
		t.Fatal("launch missed active SD checkpoint")
	}
	first := bytes.Clone(f.r.f.files.files[packagestore.PreferencesPath].Data)
	app.HandleCommand(tenfoot.CmdLayoutCycle, time.Time{})
	if !prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 1 {
		t.Fatal("running change accessed SD")
	}
	if _, err = f.s.Return(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 2 || bytes.Equal(first, f.r.f.files.files[packagestore.PreferencesPath].Data) {
		t.Fatal("return missed latest settings")
	}
	app.HandleCommand(tenfoot.CmdLayoutCycle, time.Time{})
	configurations := f.r.f.cpu.configurations
	if err = f.s.ParkMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 3 || f.r.f.lease.r.Active || f.s.Status().State != ProgramParked || f.r.f.cpu.configurations != configurations {
		t.Fatal("final save did not leave storage contained")
	}
	if err = prefs.WritePreferences([]byte(`{}`)); err == nil {
		t.Fatal("closed preferences accepted a later write")
	}
	// A new boot owns a new SD lease; shutdown itself grants no resume.
	restart := serviceTest(t)
	restart.files.files[packagestore.PreferencesPath] = &fstest.MapFile{Data: bytes.Clone(f.r.f.files.files[packagestore.PreferencesPath].Data)}
	reloaded, err := LoadProgramPreferences(context.Background(), restart.store, true)
	if err != nil {
		t.Fatal(err)
	}
	restored := tenfoot.NewApp(nil, 640, 480, 100)
	if err = restored.SetPreferenceStore(reloaded); err != nil {
		t.Fatal(err)
	}
	before, _ := prefs.ReadPreferences()
	after, _ := reloaded.ReadPreferences()
	if !bytes.Equal(before, after) {
		t.Fatal("restart lost settings", string(before), string(after))
	}
}

func TestProgramPreferencesFailuresDoNotReplayOrProgramAfterUnknownWrite(t *testing.T) {
	for _, fault := range []error{fat.ErrCapacity, errors.New("write flush lost")} {
		t.Run(fault.Error(), func(t *testing.T) {
			f, prefs := preferenceMenuFixture(t, true)
			f.r.f.files.preferenceWrite = func() error { return fault }
			if err := prefs.WritePreferences([]byte(`{"layout":"shelf"}`)); !errors.Is(err, tenfoot.ErrPreferencesPending) {
				t.Fatal(err)
			}
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			session, err := f.s.Launch(context.Background(), 0, request)
			state := prefs.PreferenceSaveState()
			if !state.Pending || !state.Failed || f.r.f.files.preferenceWrites != 1 {
				t.Fatal("failure not retained")
			}
			if errors.Is(fault, fat.ErrCapacity) {
				if err != nil || f.s.Status().State != ProgramRunning {
					t.Fatal("safe capacity refusal blocked core", err)
				}
				if _, err = f.s.Return(context.Background(), session); err != nil {
					t.Fatal(err)
				}
				if err = f.s.ParkMenu(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || f.s.Status().State != ProgramRebootRequired || f.r.f.cpu.configurations != 1 {
				t.Fatal("unknown write did not retire before core programming", err)
			}
			if f.r.f.files.preferenceWrites != 1 {
				t.Fatal("failed write automatically retried")
			}
		})
	}
}

func TestProgramPreferencesConcurrentUpdateRemainsPending(t *testing.T) {
	f, prefs := preferenceMenuFixture(t, true)
	old, newer := []byte(`{"layout":"shelf"}`), []byte(`{"layout":"list"}`)
	if err := prefs.WritePreferences(old); !errors.Is(err, tenfoot.ErrPreferencesPending) {
		t.Fatal(err)
	}
	f.r.f.files.preferenceWrite = func() error {
		if err := prefs.WritePreferences(newer); !errors.Is(err, tenfoot.ErrPreferencesPending) {
			t.Fatal(err)
		}
		return nil
	}
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	session, err := f.s.Launch(context.Background(), 0, request)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.PreferenceSaveState().Pending || !bytes.Equal(f.r.f.files.files[packagestore.PreferencesPath].Data, old) {
		t.Fatal("newer pending edit lost")
	}
	f.r.f.files.preferenceWrite = nil
	if _, err = f.s.Return(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if prefs.PreferenceSaveState().Pending || !bytes.Equal(f.r.f.files.files[packagestore.PreferencesPath].Data, newer) {
		t.Fatal("latest edit not saved on return")
	}
}

func TestProgramPreferencesExplicitBindingAndVolatileLoad(t *testing.T) {
	f, prefs := preferenceMenuFixture(t, false)
	if err := f.s.AttachPreferences(prefs); err == nil {
		t.Fatal("duplicate attachment accepted")
	}
	if err := prefs.WritePreferences([]byte(`{"home":"rooms"}`)); err != nil {
		t.Fatal(err)
	}
	if prefs.PreferenceSaveState().Pending || f.r.f.files.preferenceWrites != 0 {
		t.Fatal("read-only profile enabled persistence")
	}
	other := serviceTest(t)
	defaults, err := LoadProgramPreferences(context.Background(), other.store, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = LoadProgramPreferences(context.Background(), other.store, true); err == nil {
		t.Fatal("persistent mode accepted missing preallocated file")
	}
	if raw, _ := defaults.ReadPreferences(); !bytes.Contains(raw, []byte(`"home":"library"`)) {
		t.Fatal("missing file did not load volatile defaults")
	}
	if err := other.s.AttachPreferences(&ProgramPreferences{store: f.r.f.store}); err == nil {
		t.Fatal("foreign store admitted")
	}
	if err := other.s.AttachPreferences(&ProgramPreferences{store: other.store}); err == nil {
		t.Fatal("persistent store without menu lifecycle admitted")
	}
}

func TestProgramPreferencesShutdownWriteFailure(t *testing.T) {
	for _, fault := range []error{fat.ErrCapacity, errors.New("final flush lost")} {
		t.Run(fault.Error(), func(t *testing.T) {
			f, prefs := preferenceMenuFixture(t, true)
			f.r.f.files.preferenceWrite = func() error { return fault }
			if err := prefs.WritePreferences([]byte(`{"layout":"shelf"}`)); !errors.Is(err, tenfoot.ErrPreferencesPending) {
				t.Fatal(err)
			}
			err := f.s.ParkMenu(context.Background())
			if f.r.f.files.preferenceWrites != 1 || !prefs.PreferenceSaveState().Failed || f.r.f.cpu.configurations != 1 {
				t.Fatal("failed shutdown save lost its failure or reconfigured FPGA")
			}
			if errors.Is(fault, fat.ErrCapacity) {
				if err != nil || f.s.Status().State != ProgramParked || f.r.f.lease.r.Active {
					t.Fatal("safe refusal failed containment", err)
				}
			} else if err == nil || f.s.Status().State != ProgramRebootRequired {
				t.Fatal("uncertain shutdown write did not retire", err)
			}
			if err = prefs.WritePreferences([]byte(`{}`)); err == nil {
				t.Fatal("shutdown accepted later settings")
			}
		})
	}
}
