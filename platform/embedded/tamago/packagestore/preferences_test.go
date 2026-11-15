// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

type preferenceFiles struct {
	*trackedFS
	writes int
	write  func(string, []byte) error
}

func (f *preferenceFiles) WriteFile(path string, data []byte) error {
	f.writes++
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	return f.write(path, data)
}

func TestPreferencesBoundedReadAndLease(t *testing.T) {
	for _, raw := range []string{`{"home":"library"}`, `[]`, `null`, `{"home":`, "", string(bytes.Repeat([]byte(" "), MaxPreferencesBytes+1))} {
		t.Run(raw[:min(15, len(raw))], func(t *testing.T) {
			l := lease()
			s, f := open(t, fstest.MapFS{PreferencesPath: &fstest.MapFile{Data: []byte(raw)}}, l)
			data, err := s.ReadPreferences(context.Background())
			if validPreferences([]byte(raw)) {
				if err != nil || string(data) != raw {
					t.Fatal(data, err)
				}
			} else if err == nil {
				t.Fatal("invalid object admitted")
			}
			if f.handles.Load() != 0 {
				t.Fatal("read leaked a file")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			opens := f.opens.Load()
			if _, err = s.ReadPreferences(ctx); !errors.Is(err, context.Canceled) || f.opens.Load() != opens {
				t.Fatal("cancelled read performed IO", err)
			}
			if err = s.Quiesce(); err != nil {
				t.Fatal(err)
			}
			if _, err = s.ReadPreferences(context.Background()); !errors.Is(err, ErrState) || f.opens.Load() != opens {
				t.Fatal("contained read performed IO", err)
			}
		})
	}
}

func TestPreferencesReaderFailuresAndOwnershipChange(t *testing.T) {
	for _, fault := range []string{"short", "long", "stalled", "close", "owner"} {
		t.Run(fault, func(t *testing.T) {
			l := lease()
			s, f := open(t, fstest.MapFS{PreferencesPath: &fstest.MapFile{Data: []byte(`{}`)}}, l)
			switch fault {
			case "short":
				f.readHook = func([]byte) (int, error) { return 0, io.EOF }
			case "long":
				f.readHook = func(data []byte) (int, error) { copy(data, "{} "); return 3, io.EOF }
			case "stalled":
				f.readHook = func([]byte) (int, error) { return 0, nil }
			case "close":
				f.closeErr = errors.New("close failed")
			case "owner":
				f.readHook = func(data []byte) (int, error) {
					l.mu.Lock()
					l.r.Owner++
					l.mu.Unlock()
					copy(data, "{}")
					return 2, io.EOF
				}
			}
			if data, err := s.ReadPreferences(context.Background()); err == nil || data != nil {
				t.Fatal("read failure admitted", data, err)
			}
			if f.handles.Load() != 0 {
				t.Fatal("file still open")
			}
			if fault == "owner" {
				if _, err := s.ActiveReceipt(); !errors.Is(err, ErrRetired) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPreferencesWriteErrorsNeverReplayUncertainIO(t *testing.T) {
	for _, fault := range []error{nil, block.ErrReadOnly, fat.ErrCapacity, fs.ErrNotExist, errors.New("flush reply lost")} {
		name := "success"
		if fault != nil {
			name = fault.Error()
		}
		t.Run(name, func(t *testing.T) {
			l := lease()
			tracked := &trackedFS{base: fstest.MapFS{}, lease: l}
			f := &preferenceFiles{trackedFS: tracked, write: func(path string, data []byte) error {
				if path != PreferencesPath || string(data) != `{"layout":"shelf"}` {
					t.Fatal("wrong file or bytes")
				}
				return fault
			}}
			s, err := New(f, l)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err = s.WritePreferences(ctx, []byte(`{}`)); !errors.Is(err, context.Canceled) || f.writes != 0 {
				t.Fatal(err)
			}
			if err = s.WritePreferences(context.Background(), []byte(`[]`)); err == nil || f.writes != 0 {
				t.Fatal("invalid settings reached storage")
			}
			err = s.WritePreferences(context.Background(), []byte(`{"layout":"shelf"}`))
			if !errors.Is(err, fault) {
				t.Fatal(err, fault)
			}
			_, activeErr := s.ActiveReceipt()
			uncertain := fault != nil && !errors.Is(fault, block.ErrReadOnly) && !errors.Is(fault, fat.ErrCapacity) && !errors.Is(fault, fs.ErrNotExist)
			if uncertain {
				if !errors.Is(activeErr, ErrRetired) {
					t.Fatal(activeErr)
				}
				if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrRetired) || f.writes != 1 {
					t.Fatal("uncertain write replayed", err)
				}
			} else {
				if activeErr != nil {
					t.Fatal(activeErr)
				}
				if err = s.Quiesce(); err != nil {
					t.Fatal(err)
				}
				if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrState) || f.writes != 1 {
					t.Fatal("contained write reached disk", err)
				}
			}
		})
	}
}

func TestPreferenceWriteSerializesShutdownAndDetectsChangedLease(t *testing.T) {
	l := lease()
	entered, release := make(chan struct{}), make(chan struct{})
	f := &preferenceFiles{trackedFS: &trackedFS{base: fstest.MapFS{}, lease: l}, write: func(string, []byte) error { close(entered); <-release; return nil }}
	s, err := New(f, l)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.WritePreferences(context.Background(), []byte(`{}`)) }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- s.Quiesce() }()
	l.mu.Lock()
	stops := l.stops
	l.mu.Unlock()
	if stops != 0 {
		t.Fatal("SD stopped before write/flush returned")
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-stopped; err != nil {
		t.Fatal(err)
	}
	if err = s.Quiesce(); !errors.Is(err, ErrState) {
		t.Fatal("shutdown repeated", err)
	}
	if err = s.Resume(); err != nil {
		t.Fatal(err)
	}
	f.write = func(string, []byte) error { l.mu.Lock(); l.r.Generation++; l.mu.Unlock(); return nil }
	if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrRetired) {
		t.Fatal("changed lease accepted", err)
	}
}

func TestPreferencesRealFATCleanRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sd.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	initial := filepath.Join(dir, "initial.json")
	// Reserve capacity while keeping the initial record a valid JSON object.
	if err = os.WriteFile(initial, append([]byte(`{}`), bytes.Repeat([]byte(" "), 4094)...), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mformat", "-i", path, "-F", "::"}, {"mmd", "-i", path, "::FES"}, {"mcopy", "-i", path, initial, "::" + PreferencesPath}} {
		if output, e := exec.Command(args[0], args[1:]...).CombinedOutput(); e != nil {
			t.Fatalf("%v: %v %s", args, e, output)
		}
	}
	device, err := block.OpenFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	volume, err := fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(volume, lease())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"layout":"shelf","reduced_motion":true}`)
	if err = s.WritePreferences(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	// Independent mtools reader verifies the directory size and file contents.
	got, err := exec.Command("mtype", "-i", path, "::"+PreferencesPath).Output()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(string(got), err)
	}
	volume, err = fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(volume, lease())
	if err != nil {
		t.Fatal(err)
	}
	got, err = restarted.ReadPreferences(context.Background())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(string(got), err)
	}
}
