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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

type installFiles struct {
	files       fstest.MapFS
	lease       *testLease
	opens       int
	mutations   []string
	fault       func(string) error
	afterCreate func(string)
}

func (f *installFiles) Open(name string) (fs.File, error) {
	f.opens++
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return nil, ErrState
	}
	return f.files.Open(name)
}
func (f *installFiles) mutate(name string, data []byte, mode fs.FileMode) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrState
	}
	f.mutations = append(f.mutations, name)
	if f.fault != nil {
		if err := f.fault(name); err != nil {
			return err
		}
	}
	if _, err := f.files.Open(name); err == nil {
		return fs.ErrExist
	}
	f.files[name] = &fstest.MapFile{Data: bytes.Clone(data), Mode: mode}
	if f.afterCreate != nil {
		f.afterCreate(name)
	}
	return nil
}
func (f *installFiles) Mkdir(_ context.Context, name string) error {
	return f.mutate(name, nil, fs.ModeDir|0755)
}
func (f *installFiles) CreateFile(_ context.Context, name string, data []byte) error {
	return f.mutate(name, data, 0444)
}

func installFixture(t *testing.T) (*Store, *installFiles, *InstallPlan) {
	t.Helper()
	body, request, _ := basic(t)
	plan, err := PrepareInstall(context.Background(), request, map[string][]byte{request.Path: body})
	if err != nil {
		t.Fatal(err)
	}
	f := &installFiles{files: fstest.MapFS{}, lease: lease()}
	s, err := New(f, f.lease)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, plan
}

func TestPrepareInstallUsesPrivateCanonicalContentWithoutAuthority(t *testing.T) {
	body, request, want := basic(t)
	files := map[string][]byte{request.Path: body}
	plan, err := PrepareInstall(context.Background(), request, files)
	if err != nil {
		t.Fatal(err)
	}
	body[0] ^= 1
	delete(files, request.Path)
	got, err := io.ReadAll(plan.Selection().Archive().Payload())
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("caller changed prepared data", err)
	}
	if !strings.HasPrefix(plan.Request().Path, InstallContentRoot+"/") || plan.Request().Path == request.Path {
		t.Fatal("upload path became destination")
	}
	s, _, _ := installFixture(t)
	if _, err = s.Contain(plan.Selection()); !errors.Is(err, ErrState) {
		t.Fatal("RAM preparation granted storage containment", err)
	}
	bad, request, _ := basic(t)
	bad[0] ^= 1
	if _, err = PrepareInstall(context.Background(), request, map[string][]byte{request.Path: bad}); err == nil {
		t.Fatal("corrupt archive admitted")
	}
	valid, request, _ := basic(t)
	if _, err = PrepareInstall(context.Background(), request, map[string][]byte{request.Path: valid, "other": {1}}); err == nil {
		t.Fatal("unselected upload content accepted")
	}
}

func TestInstallWritesOwnedContentThenCatalogAndReusesExactFiles(t *testing.T) {
	s, f, plan := installFixture(t)
	record := []byte(`{"format":1}`)
	if err := s.Install(context.Background(), plan, record); err != nil {
		t.Fatal(err)
	}
	last := f.mutations[len(f.mutations)-1]
	if !strings.HasPrefix(last, InstallCatalogRoot+"/") || len(f.mutations) != 5 {
		t.Fatal("record did not follow all content/directory creation", f.mutations)
	}
	count := len(f.mutations)
	if err := s.Install(context.Background(), plan, record); err != nil || len(f.mutations) != count {
		t.Fatal("exact installation was rewritten", err)
	}
	records, err := s.ReadInstallRecords(context.Background())
	if err != nil || len(records) != 1 || !bytes.Equal(records[0], record) {
		t.Fatal(records, err)
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	opens := f.opens
	if err = s.Install(context.Background(), plan, record); !errors.Is(err, ErrState) || f.opens != opens {
		t.Fatal("contained installer touched filesystem", err)
	}
	if _, err = s.ReadInstallRecords(context.Background()); !errors.Is(err, ErrState) || f.opens != opens {
		t.Fatal("contained catalog read touched filesystem", err)
	}
}

func TestInstallFailuresDoNotPublishRetryOrAdoptChangedOwnership(t *testing.T) {
	for _, kind := range []string{"readonly", "space", "lost-write", "mutation-readonly", "owner", "conflict", "readback", "record-commit"} {
		t.Run(kind, func(t *testing.T) {
			s, f, plan := installFixture(t)
			if kind == "conflict" {
				f.files[plan.Request().Path] = &fstest.MapFile{Data: []byte("wrong")}
			} else {
				if kind == "readback" {
					f.afterCreate = func(name string) {
						if strings.HasPrefix(name, InstallContentRoot+"/") {
							f.files[name].Data[0] ^= 1
						}
					}
				}
				f.fault = func(name string) error {
					if kind == "record-commit" && strings.HasPrefix(name, InstallCatalogRoot+"/") {
						f.files[name] = &fstest.MapFile{Data: []byte(`{"format":1}`)}
						return errors.New("publication reply lost after commit")
					}
					if kind == "readback" {
						return nil
					}
					if kind == "readonly" {
						return block.ErrReadOnly
					}
					if kind == "space" {
						return fat.ErrNoSpace
					}
					if !strings.HasPrefix(name, InstallContentRoot+"/") {
						return nil
					}
					if kind == "record-commit" {
						return nil
					}
					if kind == "owner" {
						f.lease.mu.Lock()
						f.lease.r.Generation++
						f.lease.mu.Unlock()
						return nil
					}
					if kind == "mutation-readonly" {
						return errors.Join(fat.ErrMutation, block.ErrReadOnly)
					}
					return errors.New("lost write reply")
				}
			}
			err := s.Install(context.Background(), plan, []byte(`{"format":1}`))
			if err == nil {
				t.Fatal("failure admitted")
			}
			for name := range f.files {
				if strings.HasPrefix(name, InstallCatalogRoot+"/") && kind != "record-commit" {
					t.Fatal("failed content published a record")
				}
			}
			uncertain := kind == "lost-write" || kind == "mutation-readonly" || kind == "owner" || kind == "readback" || kind == "record-commit"
			if uncertain {
				if !errors.Is(err, ErrRetired) {
					t.Fatal(err)
				}
				mutations := len(f.mutations)
				if err = s.Install(context.Background(), plan, []byte(`{}`)); !errors.Is(err, ErrRetired) || len(f.mutations) != mutations {
					t.Fatal("uncertain installer retried", err)
				}
			} else if _, err = s.ActiveReceipt(); err != nil {
				t.Fatal("safe refusal lost healthy storage", err)
			}
		})
	}
}

func TestInstalledRecordNamesAndDigestsAreMandatory(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-name", "changed", "too-many"} {
		t.Run(kind, func(t *testing.T) {
			s, f, plan := installFixture(t)
			if kind == "missing" {
				records, err := s.ReadInstallRecords(context.Background())
				if err != nil || len(records) != 0 {
					t.Fatal(records, err)
				}
				return
			}
			if err := s.Install(context.Background(), plan, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			for name, file := range f.files {
				if strings.HasPrefix(name, InstallCatalogRoot+"/") {
					if kind == "wrong-name" {
						delete(f.files, name)
						f.files[InstallCatalogRoot+"/other.JSN"] = file
					}
					if kind == "changed" {
						file.Data = []byte(`{"changed":true}`)
					}
				}
			}
			if kind == "too-many" {
				for i := range MaxInstallRecords + 1 {
					raw := []byte{byte(i)}
					f.files[InstallCatalogRoot+"/"+blobHash(raw)+".JSN"] = &fstest.MapFile{Data: raw}
				}
			}
			if _, err := s.ReadInstallRecords(context.Background()); err == nil {
				t.Fatal("invalid installed catalog accepted")
			}
			if kind == "too-many" {
				count := len(f.mutations)
				if err := s.Install(context.Background(), plan, []byte(`{"new":true}`)); err == nil || len(f.mutations) != count {
					t.Fatal("overflowed catalog was extended", err)
				}
			}
		})
	}
}

func TestOwnedInstallerRealFATReadbackRestartAndRetention(t *testing.T) {
	body, request, want := basic(t)
	plan, err := PrepareInstall(context.Background(), request, map[string][]byte{request.Path: body})
	if err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(t.TempDir(), "install.img")
	file, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("mformat", "-i", image, "-F", "::").CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	device, err := block.OpenFile(image, true)
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
	record := []byte(`{"format":1}`)
	if err = s.Install(context.Background(), plan, record); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command("mtype", "-i", image, "::"+plan.Request().Path).Output()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("independent archive readback", err)
	}
	volume, err = fat.Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(volume, lease())
	if err != nil {
		t.Fatal(err)
	}
	records, err := restarted.ReadInstallRecords(context.Background())
	if err != nil || len(records) != 1 || !bytes.Equal(records[0], record) {
		t.Fatal(records, err)
	}
	selection, err := restarted.Retain(context.Background(), plan.Request())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := restarted.Contain(selection)
	if err != nil {
		t.Fatal(err)
	}
	selection, err = handoff.Selection()
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(selection.Archive().Payload())
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("new installation did not retain expected payload", err)
	}
}

func TestInstallCancellationBeforeAndAfterContentMutation(t *testing.T) {
	for _, phase := range []string{"before", "directory", "content"} {
		t.Run(phase, func(t *testing.T) {
			s, f, plan := installFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before" {
				cancel()
			} else {
				f.fault = func(name string) error {
					if phase == "directory" || strings.HasPrefix(name, InstallContentRoot+"/") {
						cancel()
					}
					return nil
				}
			}
			err := s.Install(ctx, plan, []byte(`{"format":1}`))
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if phase == "content" {
				if !errors.Is(err, ErrRetired) {
					t.Fatal("unverified content remained usable", err)
				}
			} else {
				if _, err = s.ActiveReceipt(); err != nil {
					t.Fatal("known completed/no mutation lost healthy owner", err)
				}
				if phase == "before" && f.opens != 0 || phase == "directory" && len(f.mutations) != 1 {
					t.Fatal("cancellation performed later IO")
				}
			}
			for name := range f.files {
				if strings.HasPrefix(name, InstallCatalogRoot+"/") {
					t.Fatal("cancelled installation published a record")
				}
			}
		})
	}
}

func TestInstallPlanContentBytesCountsPrivateUpload(t *testing.T) {
	var empty *InstallPlan
	if empty.ContentBytes() != 0 {
		t.Fatal("nil plan has content")
	}
	body, request, _ := basic(t)
	plan, err := PrepareInstall(context.Background(), request, map[string][]byte{request.Path: body})
	if err != nil {
		t.Fatal(err)
	}
	want := len(body)
	body[0] ^= 1
	if plan.ContentBytes() != want {
		t.Fatal("private content byte count differs")
	}
}
