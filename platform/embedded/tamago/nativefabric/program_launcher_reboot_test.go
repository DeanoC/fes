// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// These are USB boot-keyboard reports, not direct App/backend commands. The
// actual input adapter supplies neutral fences, core keys and the return chord.
// Physical USB/MMIO/firmware remain modeled; FAT allocation and flush are real.
type launcherKeyboardApp struct {
	fixture       *programMenuFixture
	source        *pausableKeyboard
	presented     tenfoot.Snapshot
	stage, cycles int
	changeLayout  bool
	initialLayout string
	frames        int
}

func (p *launcherKeyboardApp) key(modifier, usage byte) {
	p.source.reports = [][]byte{make([]byte, 8), {modifier, 0, usage, 0, 0, 0, 0, 0}, make([]byte, 8)}
}

func (p *launcherKeyboardApp) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	in := tenfoot.PlatformInput{At: time.Now()}
	s := p.presented
	ready := p.frames > 0 && !s.Loading && s.Session.State == "idle" && p.fixture.s.Status().State == ProgramReady
	switch p.stage {
	case 0:
		if ready && len(s.Games) == 2 {
			p.initialLayout = s.Grid.Mode.String()
			if p.changeLayout {
				p.key(0, 0x0f) // L: change layout, queued until owned SD checkpoint.
				p.stage = 1
			} else {
				p.stage = 2
			}
		}
	case 1:
		if s.Grid.Mode.String() != p.initialLayout {
			p.stage = 2
		}
	case 2:
		if ready && len(s.Games) == 2 {
			if s.Grid.Focus != p.cycles {
				p.key(0, 0x4f) // Right: choose the other installed title.
			} else {
				p.key(0, 0x28) // Enter: normal App launch.
				p.stage = 3
			}
		}
	case 3:
		if s.Session.State == "active" && p.fixture.s.Status().State == ProgramRunning {
			p.key(0, 4) // A and its release reach the running core.
			p.stage = 4
		}
	case 4:
		if p.fixture.r.f.cpu.keyboardRows != ([9]uint16{}) {
			return in, fmt.Errorf("core key release was lost")
		}
		p.key(5, 0x29) // Ctrl+Alt+Escape: App Stop, not Quit.
		p.stage = 5
	case 5:
		if ready {
			p.cycles++
			if p.cycles < 2 {
				p.stage = 2
			} else if p.changeLayout {
				p.initialLayout = s.Grid.Mode.String()
				p.key(0, 0x0f) // Exercise the final shutdown checkpoint too.
				p.stage = 6
			} else {
				p.key(0, 0x14) // Q: orderly App exit.
				p.stage = 7
			}
		}
	case 6:
		if ready && s.Grid.Mode.String() != p.initialLayout {
			p.key(0, 0x14)
			p.stage = 7
		}
	}
	return in, nil
}

func (p *launcherKeyboardApp) Present(_ context.Context, s tenfoot.Snapshot) error {
	p.presented = s
	p.frames++
	return nil
}

func writeLauncherFAT(t *testing.T, volume *fat.Volume, payload map[string][]byte) {
	t.Helper()
	names := make([]string, 0, len(payload))
	for name := range payload {
		names = append(names, name)
	}
	sort.Strings(names)
	dirs := map[string]bool{".": true}
	for _, name := range names {
		prefix := ""
		for _, part := range strings.Split(path.Dir(name), "/") {
			if prefix == "" {
				prefix = part
			} else {
				prefix += "/" + part
			}
			if !dirs[prefix] {
				if err := volume.Mkdir(context.Background(), prefix); err != nil {
					t.Fatal(err)
				}
				dirs[prefix] = true
			}
		}
		if err := volume.CreateFile(context.Background(), name, payload[name]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProgramLauncherKeyboardFATShutdownAndFreshBootPersistence(t *testing.T) {
	image := filepath.Join(t.TempDir(), "launcher.img")
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
	if raw, err := exec.Command("mformat", "-i", image, "-F", "::").CombinedOutput(); err != nil {
		t.Fatal(string(raw), err)
	}
	var saved []byte
	var savedLayout string
	var oldBackend *ProgramBackend
	for boot := range 2 {
		disk, err := block.OpenFile(image, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = disk.Close() })
		var startup *ProgramStartup
		f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
			volume, err := fat.Mount(&ownedInstallDisk{disk, f.r.f.lease})
			if err != nil {
				t.Fatal(err)
			}
			if boot == 0 {
				writeLauncherFAT(t, volume, launcherStaging(t, f))
			}
			f.r.f.store, err = packagestore.New(volume, f.r.f.lease)
			if err != nil {
				t.Fatal(err)
			}
			startup, err = ReadProgramStartup(context.Background(), f.r.f.store, true, true)
			if err != nil {
				t.Fatal(err)
			}
			f.request = startup.Menu
		})
		if err = f.s.AttachPreferences(startup.Preferences); err != nil {
			t.Fatal(err)
		}
		backend, err := NewProgramBackend(f.s, startup.Entries)
		if err != nil {
			t.Fatal(err)
		}
		if boot == 0 {
			if len(startup.Entries) != 0 || len(startup.Staged) != 2 {
				t.Fatal("fresh bootstrap is not empty with two install sources")
			}
			if err = backend.AttachStagedPrograms(context.Background(), startup.Staged); err != nil {
				t.Fatal(err)
			}
			client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
			rows, err := client.AvailableCores(context.Background())
			if err != nil || len(rows) != 2 {
				t.Fatal(rows, err)
			}
			for _, row := range rows {
				if err = client.InstallAvailableCore(context.Background(), row.CoreReference); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			// No installation/catalog attachment or copying RAM preferences on
			// the second boot. Both entries and settings come from fresh FAT IO.
			if len(startup.Entries) != 2 || startup.Entries[0].ID == startup.Entries[1].ID {
				t.Fatal("reboot did not discover both installed entries")
			}
			raw, _ := startup.Preferences.ReadPreferences()
			if !bytes.Equal(raw, saved) {
				t.Fatal("fresh startup lost saved preferences")
			}
			if err := oldBackend.ParkMenu(context.Background()); err == nil {
				t.Fatal("old shutdown owner was reusable")
			}
		}
		device, err := gfx.NewSoftware(1280, 720)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { device.Close() })
		app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend, Timeout: 100 * time.Second}), 1280, 720, 400)
		if err = app.SetPreferenceStore(startup.Preferences); err != nil {
			t.Fatal(err)
		}
		if boot == 1 && app.Snapshot().Grid.Mode.String() != savedLayout {
			t.Fatal("fresh App did not apply the saved layout")
		}
		source := &pausableKeyboard{}
		base := &launcherKeyboardApp{fixture: f, source: source, changeLayout: boot == 0}
		input, err := NewProgramInputPlatform(base, backend, source)
		if err != nil {
			t.Fatal(err)
		}
		platform, err := NewProgramMenuPlatform(f.s, input, device.Framebuffer)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = tenfoot.RunPlatform(ctx, app, device, platform)
		cancel()
		if err != nil || base.cycles != 2 || base.stage != 7 {
			t.Fatalf("boot=%d cycles=%d stage=%d view=%+v: %v", boot, base.cycles, base.stage, f.s.Status(), err)
		}
		if err = backend.ParkMenu(context.Background()); err != nil {
			t.Fatal(err)
		}
		if startup.Preferences.PreferenceSaveState().Pending || startup.Preferences.PreferenceSaveState().Failed ||
			f.s.Status().State != ProgramParked || f.r.f.lease.r.Active || !source.paused || f.r.f.cpu.keyboardWrites == 0 {
			t.Fatal("shutdown failed to checkpoint and retain contained owners")
		}
		saved, _ = startup.Preferences.ReadPreferences()
		savedLayout = app.Snapshot().Grid.Mode.String()
		oldBackend = backend
		if err = disk.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := exec.Command("mtype", "-i", image, "::"+packagestore.PreferencesPath).Output()
		if err != nil || !bytes.Equal(got, saved) {
			t.Fatal("independent preferences readback differs", err)
		}
		bootstrap, err := exec.Command("mtype", "-i", image, "::FES/CORESEL.JSN").Output()
		if err != nil || !bytes.Contains(bootstrap, []byte(`"programs":[]`)) {
			t.Fatal("installation rewrote the empty bootstrap", err)
		}
	}
}
