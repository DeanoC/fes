// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func (f *programTestFiles) Mkdir(_ context.Context, name string) error {
	return f.installFile(name, nil, fs.ModeDir|0755)
}
func (f *programTestFiles) CreateFile(_ context.Context, name string, data []byte) error {
	return f.installFile(name, data, 0444)
}
func (f *programTestFiles) installFile(name string, data []byte, mode fs.FileMode) error {
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return ErrProgramSession
	}
	f.installOperations = append(f.installOperations, name)
	if f.installFault != nil {
		if err := f.installFault(name); err != nil {
			return err
		}
	}
	if file, err := f.files.Open(name); err == nil {
		file.Close()
		return fs.ErrExist
	}
	f.files[name] = &fstest.MapFile{Data: bytes.Clone(data), Mode: mode}
	return nil
}

func programUpload(f *programTestFiles, r packagestore.Request) map[string][]byte {
	files := map[string][]byte{r.Path: bytes.Clone(f.files[r.Path].Data)}
	if r.MetadataPath != "" {
		files[r.MetadataPath] = bytes.Clone(f.files[r.MetadataPath].Data)
	}
	for _, s := range r.Sources {
		files[s.Path] = bytes.Clone(f.files[s.Path].Data)
	}
	if r.Tape.Path != "" {
		files[r.Tape.Path] = bytes.Clone(f.files[r.Tape.Path].Data)
	}
	return files
}

func newProgramInstallFixture(t *testing.T) (*programMenuFixture, *ProgramBackend, *ProgramInstall, map[string][]byte) {
	t.Helper()
	f := newProgramMenuFixture(t, "")
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	b, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "existing", Title: "Existing core", Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	files := programUpload(f.r.f.files, request)
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	plan, err := b.PrepareInstall(context.Background(), "installed", "Installed core", request, files)
	if err != nil {
		t.Fatal(err)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || f.r.f.lease.r.Active {
		t.Fatal("RAM preflight touched contained hardware")
	}
	return f, b, plan, files
}

func TestOwnedInstallPublishesThenLaunchesReturnsAndSurvivesDiscovery(t *testing.T) {
	f, b, plan, input := newProgramInstallFixture(t)
	for _, data := range input {
		data[0] ^= 1
	}
	if err := b.Install(context.Background(), 0, plan); err != nil {
		t.Fatal(err)
	}
	if f.s.Status().State != ProgramReady || f.s.Status().Generation != 1 || f.r.f.lease.r.Active || f.r.f.cpu.configurations != 2 || len(b.entries) != 2 {
		t.Fatal("installation did not restore Ready before publication", f.s.Status())
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	for range 2 {
		play, err := client.Launch(context.Background(), "installed")
		if err != nil || play.State != "active" {
			t.Fatal(play, err)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh boot/store discovers the exact persisted request before its menu.
	restart := romProgramFixtureFor(t, NativeComputerProfile, false)
	for name, file := range f.r.f.files.files {
		copy := *file
		copy.Data = bytes.Clone(file.Data)
		restart.f.files.files[name] = &copy
	}
	entries, err := ReadInstalledPrograms(context.Background(), restart.f.store, []ProgramEntry{b.entries[0]})
	if err != nil || len(entries) != 2 || entries[1].ID != "installed" {
		t.Fatal(entries, err)
	}
	selection, err := restart.f.store.Retain(context.Background(), entries[1].Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restart.f.loader.Preflight(selection); err != nil {
		t.Fatal("discovered install no longer admitted", err)
	}
}

func TestOwnedInstallSafeAndUncertainFailures(t *testing.T) {
	for _, kind := range []string{"readonly", "space", "lost", "restore"} {
		t.Run(kind, func(t *testing.T) {
			f, b, plan, _ := newProgramInstallFixture(t)
			if kind == "restore" {
				f.fault = "frame"
			} else {
				f.r.f.files.installFault = func(name string) error {
					if kind == "readonly" {
						return block.ErrReadOnly
					}
					if kind == "space" {
						return fat.ErrNoSpace
					}
					if strings.HasPrefix(name, packagestore.InstallContentRoot+"/") {
						return errors.Join(fat.ErrMutation, errors.New("lost completion"))
					}
					return nil
				}
			}
			err := b.Install(context.Background(), 0, plan)
			if err == nil || len(b.entries) != 1 {
				t.Fatal("failed installation advertised playable entry", err)
			}
			if kind == "readonly" || kind == "space" {
				if f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
					t.Fatal("safe refusal failed menu restoration")
				}
			} else {
				if f.s.Status().State != ProgramRebootRequired {
					t.Fatal("uncertain installation did not retire", err)
				}
				mutations := len(f.r.f.files.installOperations)
				if err = b.Install(context.Background(), f.s.Status().Generation, plan); err == nil || len(f.r.f.files.installOperations) != mutations {
					t.Fatal("failed installation retried")
				}
			}
		})
	}
}

func TestOwnedInstallRejectsStaleRunningDuplicateAndForeignRequests(t *testing.T) {
	f, b, plan, _ := newProgramInstallFixture(t)
	if err := b.Install(context.Background(), 1, plan); err == nil {
		t.Fatal("stale generation installed")
	}
	other, foreign, _, _ := newProgramInstallFixture(t)
	if err := foreign.Install(context.Background(), 0, plan); err == nil || len(other.r.f.files.installOperations) != 0 {
		t.Fatal("foreign loader adopted install")
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	play, err := client.Launch(context.Background(), "existing")
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	request := b.entries[0].Request
	if _, err = b.PrepareInstall(context.Background(), "background", "Background core", request, programUpload(f.r.f.files, request)); err != nil || calls != f.r.cpu.calls || opens != f.r.f.files.opens {
		t.Fatal("background RAM preparation touched hardware", err)
	}
	if err = b.Install(context.Background(), f.s.Status().Generation, plan); err == nil || calls != f.r.cpu.calls || opens != f.r.f.files.opens {
		t.Fatal("running install accessed hardware", err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if err = b.Install(context.Background(), f.s.Status().Generation, plan); err != nil {
		t.Fatal(err)
	}
	mutations := len(f.r.f.files.installOperations)
	calls = f.r.cpu.calls
	if err = b.Install(context.Background(), f.s.Status().Generation, plan); err == nil || len(f.r.f.files.installOperations) != mutations || calls != f.r.cpu.calls {
		t.Fatal("duplicate ID touched hardware")
	}
}

func TestOwnedInstallRejectsPlatformMismatchBeforeHardwareOrPersistence(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	request := b.entries[0].Request
	files := programUpload(f.r.f.files, request)
	var record selectedROMProgramRecord
	if err := json.Unmarshal(files[request.MetadataPath], &record); err != nil {
		t.Fatal(err)
	}
	record.Platform.HPSConfigurationSHA256 = strings.Repeat("9", 64)
	raw, err := canonicalProgramRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	files[request.MetadataPath] = raw
	digest := sha256.Sum256(raw)
	request.MetadataSHA256 = hex.EncodeToString(digest[:])
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	if _, err = b.PrepareInstall(context.Background(), "incompatible", "Incompatible core", request, files); err == nil {
		t.Fatal("mismatched platform installed")
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("platform rejection touched hardware or storage")
	}
}

func TestInstalledProgramDiscoveryRejectsDuplicateAndUnsafePaths(t *testing.T) {
	for _, kind := range []string{"duplicate", "path", "schema"} {
		t.Run(kind, func(t *testing.T) {
			f, b, plan, _ := newProgramInstallFixture(t)
			r := installedProgramRecord{1, "nano-installed-program-v1", "installed", "Installed core", programChoice{Path: plan.entry.Request.Path, PackageID: plan.entry.Request.PackageID, MetadataPath: plan.entry.Request.MetadataPath, MetadataSHA256: plan.entry.Request.MetadataSHA256}}
			if kind == "path" {
				r.Program.MetadataPath = "FES/PREFS.JSON"
			}
			if kind == "schema" {
				r.Format = 2
			}
			raw, err := canonicalProgramRecord(r)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			f.r.f.files.files[packagestore.InstallCatalogRoot+"/"+hex.EncodeToString(digest[:])+".JSN"] = &fstest.MapFile{Data: raw}
			// Discovery belongs to an active boot-storage phase.
			restart := romProgramFixtureFor(t, NativeComputerProfile, false)
			for name, file := range f.r.f.files.files {
				restart.f.files.files[name] = file
			}
			initial := []ProgramEntry{b.entries[0]}
			if kind == "duplicate" {
				initial[0].ID = "installed"
			}
			if _, err = ReadInstalledPrograms(context.Background(), restart.f.store, initial); err == nil {
				t.Fatal("invalid installed entry discovered")
			}
		})
	}
}

func TestOwnedInstallCachedCatalogStaysReadableUntilComplete(t *testing.T) {
	f, b, plan, _ := newProgramInstallFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.r.f.files.installFault = func(name string) error {
		if strings.HasPrefix(name, packagestore.InstallContentRoot+"/") {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- b.Install(context.Background(), 0, plan) }()
	<-entered
	r, _ := http.NewRequest("GET", "http://native.invalid/api/v1/games", nil)
	response, err := b.RoundTrip(r)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	var body bytes.Buffer
	_, err = body.ReadFrom(response.Body)
	response.Body.Close()
	if err != nil || bytes.Contains(body.Bytes(), []byte(`"id":"installed"`)) {
		close(release)
		t.Fatal("unfinished installation was advertised", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	response, err = b.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	body.Reset()
	_, err = body.ReadFrom(response.Body)
	response.Body.Close()
	if err != nil || !bytes.Contains(body.Bytes(), []byte(`"id":"installed"`)) {
		t.Fatal("completed installation not advertised", err)
	}
}

func TestOwnedInstallPausesInputAndKeepsFailuresDrained(t *testing.T) {
	for _, phase := range []string{"success", "pause", "write", "resume"} {
		t.Run(phase, func(t *testing.T) {
			f, b, plan, _ := newProgramInstallFixture(t)
			source := &pausableKeyboard{}
			if _, err := NewProgramInputPlatform(&programInputBase{}, b, source); err != nil {
				t.Fatal(err)
			}
			if phase == "pause" {
				source.pauseErr = errors.New("pause failed")
			}
			if phase == "resume" {
				source.resumeErr = errors.New("resume failed")
			}
			if phase == "write" {
				f.r.f.files.installFault = func(string) error { return errors.Join(fat.ErrMutation, errors.New("write failed")) }
			}
			err := b.Install(context.Background(), 0, plan)
			if phase == "success" {
				if err != nil || source.pauses != 1 || source.resumes != 1 || source.paused || len(b.entries) != 2 {
					t.Fatal("successful install missed input boundaries", err)
				}
				return
			}
			if err == nil || len(b.entries) != 1 {
				t.Fatal("input failure advertised entry", err)
			}
			if phase == "pause" && len(f.r.f.files.installOperations) != 0 {
				t.Fatal("failed pause allowed writes")
			}
			if phase == "write" && (!source.paused || source.resumes != 0) {
				t.Fatal("uncertain write resumed input")
			}
			if phase == "resume" && !source.paused {
				t.Fatal("failed resume lost drained state")
			}
		})
	}
}

type ownedInstallDisk struct {
	block.Device
	lease packagestore.Lease
}

func (d *ownedInstallDisk) active() error {
	r, ok := d.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return errors.New("FAT IO while SD contained")
	}
	return nil
}
func (d *ownedInstallDisk) ReadSectors(lba uint64, b []byte) error {
	if err := d.active(); err != nil {
		return err
	}
	return d.Device.ReadSectors(lba, b)
}
func (d *ownedInstallDisk) WriteSectors(lba uint64, b []byte) error {
	if err := d.active(); err != nil {
		return err
	}
	return d.Device.WriteSectors(lba, b)
}
func (d *ownedInstallDisk) Flush() error {
	if err := d.active(); err != nil {
		return err
	}
	return d.Device.Flush()
}

func TestOwnedInstallRealFATLaunchReturnAndFreshBoot(t *testing.T) {
	ownedInstallRealFATCycle(t, func(b *ProgramBackend, request packagestore.Request, files map[string][]byte) (*ProgramInstall, error) {
		return b.PrepareInstall(context.Background(), "persisted", "Persisted core", request, files)
	})
}

func ownedInstallRealFATCycle(t *testing.T, prepare func(*ProgramBackend, packagestore.Request, map[string][]byte) (*ProgramInstall, error)) {
	t.Helper()
	image := filepath.Join(t.TempDir(), "native-install.img")
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
	disk, err := block.OpenFile(image, true)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	var request packagestore.Request
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		request, _, _ = addROMProgram(t, f.r, 0, 8192)
		volume, err := fat.Mount(&ownedInstallDisk{disk, f.r.f.lease})
		if err != nil {
			t.Fatal(err)
		}
		dirs := map[string]bool{".": true}
		ensure := func(dir string) {
			current := ""
			for _, part := range strings.Split(dir, "/") {
				if part == "." {
					continue
				}
				if current == "" {
					current = part
				} else {
					current += "/" + part
				}
				if !dirs[current] {
					if err := volume.Mkdir(context.Background(), current); err != nil {
						t.Fatal(err)
					}
					dirs[current] = true
				}
			}
		}
		names := make([]string, 0, len(f.r.f.files.files))
		for name := range f.r.f.files.files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			item := f.r.f.files.files[name]
			if item.Mode.IsDir() {
				ensure(name)
				continue
			}
			ensure(path.Dir(name))
			if err := volume.CreateFile(context.Background(), name, item.Data); err != nil {
				t.Fatal(err)
			}
		}
		f.r.f.store, err = packagestore.New(volume, f.r.f.lease)
		if err != nil {
			t.Fatal(err)
		}
	})
	b, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "existing", Title: "Existing", Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepare(b, request, programUpload(f.r.f.files, request))
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Install(context.Background(), 0, plan); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	play, err := client.Launch(context.Background(), "persisted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	var discovered []ProgramEntry
	restart := newProgramMenuFixtureContext(t, context.Background(), "", func(next *programMenuFixture) {
		volume, err := fat.Mount(&ownedInstallDisk{disk, next.r.f.lease})
		if err != nil {
			t.Fatal(err)
		}
		next.r.f.store, err = packagestore.New(volume, next.r.f.lease)
		if err != nil {
			t.Fatal(err)
		}
		discovered, err = ReadInstalledPrograms(context.Background(), next.r.f.store, []ProgramEntry{{ID: "existing", Title: "Existing", Request: request}})
		if err != nil {
			t.Fatal(err)
		}
	})
	newBackend, err := NewProgramBackend(restart.s, discovered)
	if err != nil {
		t.Fatal(err)
	}
	client = hostclient.NewClient("http://native.invalid", &http.Client{Transport: newBackend})
	play, err = client.Launch(context.Background(), "persisted")
	if err != nil || play.CorePackage.PackageID != request.PackageID {
		t.Fatal("fresh boot could not launch installation", err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if f.r.f.lease.r.Active || restart.r.f.lease.r.Active {
		t.Fatal("launch/return left SD active behind the menu")
	}
	got, err := exec.Command("mtype", "-i", image, "::"+plan.entry.Request.Path).Output()
	if err != nil || !bytes.Equal(got, f.r.f.files.files[request.Path].Data) {
		t.Fatal("independent installed archive differs", err)
	}
}
