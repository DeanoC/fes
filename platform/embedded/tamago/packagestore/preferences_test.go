// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// preferenceFiles models the trusted filesystem's copy-on-write replace and
// flush. Each replace independently checks the SD lease's current receipt.
type preferenceFiles struct {
	*trackedFS
	replaces int
	replace  func(string, []byte) error
}

func (f *preferenceFiles) ReplaceFile(ctx context.Context, path string, data []byte) error {
	f.replaces++
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	// An issued replace completes even if its requester disconnects.
	if ctx == nil || ctx.Err() != nil {
		return errors.New("replace given a missing or cancelled context")
	}
	return f.replace(path, data)
}

func preferenceFS(l *testLease) *trackedFS {
	return &trackedFS{base: fstest.MapFS{PreferencesPath: &fstest.MapFile{Data: []byte(`{}`)}}, lease: l}
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
	for _, c := range []struct {
		name    string
		fault   error
		retires bool
	}{
		{"success", nil, false},
		{"read-only", block.ErrReadOnly, false},
		{"capacity", fat.ErrCapacity, false},
		{"no-space", fat.ErrNoSpace, false},
		{"missing", fs.ErrNotExist, false},
		// sdhci socket write-protect after the first sector: FAT retired its mount.
		{"read-only-after-mutation", errors.Join(fat.ErrMutation, block.ErrReadOnly), true},
		{"flush-reply-lost", errors.New("flush reply lost"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := lease()
			tracked := preferenceFS(l)
			f := &preferenceFiles{trackedFS: tracked, replace: func(path string, data []byte) error {
				if path != PreferencesPath || string(data) != `{"layout":"shelf"}` {
					t.Fatal("wrong file or bytes")
				}
				return c.fault
			}}
			s, err := New(f, l)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err = s.WritePreferences(ctx, []byte(`{}`)); !errors.Is(err, context.Canceled) || f.replaces != 0 {
				t.Fatal(err)
			}
			if err = s.WritePreferences(context.Background(), []byte(`[]`)); err == nil || f.replaces != 0 {
				t.Fatal("invalid settings reached storage")
			}
			err = s.WritePreferences(context.Background(), []byte(`{"layout":"shelf"}`))
			if !errors.Is(err, c.fault) || f.replaces != 1 || tracked.handles.Load() != 0 {
				t.Fatal(err, c.fault, f.replaces)
			}
			_, activeErr := s.ActiveReceipt()
			if c.retires {
				if !errors.Is(activeErr, ErrRetired) || !errors.Is(err, ErrRetired) {
					t.Fatal("uncertain write left storage active", err, activeErr)
				}
				opens := tracked.opens.Load()
				if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrRetired) || f.replaces != 1 || tracked.opens.Load() != opens {
					t.Fatal("uncertain write replayed", err)
				}
			} else {
				if activeErr != nil {
					t.Fatal(activeErr)
				}
				if err = s.Quiesce(); err != nil {
					t.Fatal(err)
				}
				if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrState) || f.replaces != 1 {
					t.Fatal("contained write reached disk", err)
				}
			}
		})
	}
}

func TestPreferencesWriteRefusesMissingFileAndReadOnlyFilesystem(t *testing.T) {
	l := lease()
	f := &preferenceFiles{trackedFS: &trackedFS{base: fstest.MapFS{}, lease: l}, replace: func(string, []byte) error {
		t.Fatal("ReplaceFile must not create a missing preferences file")
		return nil
	}}
	s, err := New(f, l)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, fs.ErrNotExist) || f.replaces != 0 {
		t.Fatal(err, f.replaces)
	}
	if _, err = s.ActiveReceipt(); err != nil {
		t.Fatal("missing preferences retired store", err)
	}
	// Without ReplaceFile the filesystem is read-only: there is no other write path.
	readOnly, tracked := open(t, fstest.MapFS{PreferencesPath: &fstest.MapFile{Data: []byte(`{}`)}}, lease())
	if err = readOnly.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, block.ErrReadOnly) || tracked.opens.Load() != 0 {
		t.Fatal(err)
	}
	if _, err = readOnly.ActiveReceipt(); err != nil {
		t.Fatal("read-only refusal retired store", err)
	}
}

func TestPreferenceWriteSerializesShutdownAndDetectsChangedLease(t *testing.T) {
	l := lease()
	entered, release := make(chan struct{}), make(chan struct{})
	f := &preferenceFiles{trackedFS: preferenceFS(l), replace: func(string, []byte) error { close(entered); <-release; return nil }}
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
	f.replace = func(string, []byte) error { l.mu.Lock(); l.r.Generation++; l.mu.Unlock(); return nil }
	if err = s.WritePreferences(context.Background(), []byte(`{}`)); !errors.Is(err, ErrRetired) {
		t.Fatal("changed lease accepted", err)
	}
}

// preferencesImage formats a FAT32 image holding a 4096-byte FES/PREFS.JSON.
func preferencesImage(t *testing.T) string {
	t.Helper()
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
	return path
}

func TestPreferencesRealFATCleanRestart(t *testing.T) {
	path := preferencesImage(t)
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
	// Copy-on-write ReplaceFile can also grow preferences past the initial 4096-byte preallocation.
	grown := []byte(`{"layout":"shelf","reduced_motion":true,"padding":"` + string(bytes.Repeat([]byte("p"), 5120)) + `"}`)
	if err = restarted.WritePreferences(context.Background(), grown); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Quiesce(); err != nil {
		t.Fatal(err)
	}
	got, err = exec.Command("mtype", "-i", path, "::"+PreferencesPath).Output()
	if err != nil || !bytes.Equal(got, grown) {
		t.Fatal(len(got), err)
	}
	volume, err = fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	remounted, err := New(volume, lease())
	if err != nil {
		t.Fatal(err)
	}
	got, err = remounted.ReadPreferences(context.Background())
	if err != nil || !bytes.Equal(got, grown) {
		t.Fatal(len(got), err)
	}
}

// protectedDevice models sdhci's live socket write-protect check before each
// sector: from the protectAt'th WriteSectors call (1-based) every write is
// refused with a wrapped block.ErrReadOnly before it reaches the card.
type protectedDevice struct {
	*block.File
	writes, protectAt int
}

func (d *protectedDevice) WriteSectors(lba uint64, src []byte) error {
	d.writes++
	if d.protectAt > 0 && d.writes >= d.protectAt {
		return fmt.Errorf("sdhci: socket write protection: %w", block.ErrReadOnly)
	}
	return d.File.WriteSectors(lba, src)
}

// A write-protect refusal is safe only before the first device write. After
// it, the real FAT mount retires with ErrMutation joined to block.ErrReadOnly,
// and the store must retire with it.
func TestPreferencesRealFATWriteProtectAfterMutationRetiresStore(t *testing.T) {
	data := []byte(`{"layout":"shelf"}`)
	mount := func(t *testing.T, protectAt int) (*protectedDevice, *fat.Volume, *Store) {
		t.Helper()
		base, err := block.OpenFile(preferencesImage(t), true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { base.Close() })
		d := &protectedDevice{File: base, protectAt: protectAt}
		volume, err := fat.Mount(d)
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(volume, lease())
		if err != nil {
			t.Fatal(err)
		}
		return d, volume, s
	}
	happy, _, s := mount(t, 0)
	if err := s.WritePreferences(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if happy.writes < 2 {
		t.Fatal("replace wrote too few times to refuse mid-replace", happy.writes)
	}
	for protectAt := 1; protectAt <= happy.writes; protectAt++ {
		t.Run(fmt.Sprint(protectAt), func(t *testing.T) {
			d, _, s := mount(t, protectAt)
			err := s.WritePreferences(context.Background(), data)
			_, activeErr := s.ActiveReceipt()
			if !errors.Is(err, block.ErrReadOnly) {
				t.Fatal(err)
			}
			if protectAt == 1 {
				if errors.Is(err, fat.ErrMutation) || activeErr != nil {
					t.Fatal("pre-mutation refusal retired storage", err, activeErr)
				}
				d.protectAt = 0
				if err = s.WritePreferences(context.Background(), data); err != nil {
					t.Fatal("healthy store refused a later write", err)
				}
				return
			}
			if !errors.Is(err, fat.ErrMutation) || !errors.Is(err, ErrRetired) || !errors.Is(activeErr, ErrRetired) {
				t.Fatal("retired FAT mount left the store active", err, activeErr)
			}
			writes := d.writes
			if err = s.WritePreferences(context.Background(), data); !errors.Is(err, ErrRetired) || d.writes != writes {
				t.Fatal("retired store reached the device", err)
			}
		})
	}
	// A mount already retired by a direct volume write fails the existence
	// pre-check's Open with the same joined error, which must retire too.
	t.Run("open-on-retired-mount", func(t *testing.T) {
		_, volume, s := mount(t, 2)
		if err := volume.ReplaceFile(context.Background(), PreferencesPath, data); !errors.Is(err, fat.ErrMutation) || !errors.Is(err, block.ErrReadOnly) {
			t.Fatal(err)
		}
		err := s.WritePreferences(context.Background(), data)
		_, activeErr := s.ActiveReceipt()
		if !errors.Is(err, fat.ErrMutation) || !errors.Is(err, ErrRetired) || !errors.Is(activeErr, ErrRetired) {
			t.Fatal("retired FAT mount left the store active", err, activeErr)
		}
	})
}
