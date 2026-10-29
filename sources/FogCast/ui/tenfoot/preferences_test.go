package tenfoot

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/inputmap"
)

type testPreferenceStore struct {
	data              []byte
	readErr, writeErr error
	reads, writeCalls int
}

func (s *testPreferenceStore) ReadPreferences() ([]byte, error) {
	s.reads++
	return bytes.Clone(s.data), s.readErr
}

func (s *testPreferenceStore) WritePreferences(data []byte) error {
	s.writeCalls++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.data = bytes.Clone(data)
	return nil
}

func TestPreferenceStoreRestoresAndPersists(t *testing.T) {
	const guid = "0300e9f51f08000001e4000010010000"
	initial := tenfootPrefs{
		SafeAreaPct: 0.08, Layout: "shelf", AttractEnabled: boolPtr(false),
		Home: "rooms", HomeRoom: " arcade ", PinnedRooms: []string{"arcade", " arcade ", "", "computers"},
		ReducedMotion: true, Surround: boolPtr(false), Theme: "preserved-theme",
		Controllers: map[string]inputmap.Profile{guid: {Preset: inputmap.PresetSwapAB}},
	}
	data, err := encodeTenfootPrefs(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &testPreferenceStore{data: data}
	app := catalogApp(8)
	path := filepath.Join(t.TempDir(), "unused.json")
	app.SetPrefsPath(path)
	if err = app.SetPreferenceStore(store); err != nil {
		t.Fatal(err)
	}
	if app.safeAreaPct != 0.08 || app.grid.Mode != LayoutShelf || !app.attractDisabled || app.attractPrefEnabled ||
		!app.homeRooms || app.homeRoom != "arcade" || !app.reducedMotion || app.surround || strings.Join(app.pinnedRooms, ",") != "arcade,computers" {
		t.Fatalf("stored settings were not applied: safe=%v layout=%v attract=%v home=%v/%q motion=%v surround=%v pins=%v",
			app.safeAreaPct, app.grid.Mode, app.attractDisabled, app.homeRooms, app.homeRoom, app.reducedMotion, app.surround, app.pinnedRooms)
	}
	app.pinnedRooms = nil
	app.loadPinnedRoomsLocked()
	if len(app.pinnedRooms) != 2 {
		t.Fatalf("home did not read store: %v", app.pinnedRooms)
	}
	app.ConfigureAttract(false, true)
	if app.attractPrefEnabled {
		t.Fatal("forced attract configuration ignored stored preference")
	}
	if remap, err := app.padRemap(guid); err != nil || remap == nil {
		t.Fatalf("controller profile did not read store: %v %v", remap, err)
	}
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	if store.writeCalls != 1 {
		t.Fatalf("writes = %d", store.writeCalls)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("store write touched preference path: %v", err)
	}
	restored := NewApp(nil, 1280, 720, 10)
	if err = restored.SetPreferenceStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.grid.Mode != LayoutList || restored.safeAreaPct != 0.08 || !restored.reducedMotion || restored.surround {
		t.Fatalf("recreated App did not restore updated settings: mode=%v safe=%v motion=%v surround=%v", restored.grid.Mode, restored.safeAreaPct, restored.reducedMotion, restored.surround)
	}
	prefs, err := decodeTenfootPrefs(store.data)
	if err != nil || prefs.Theme != initial.Theme || len(prefs.Controllers) != 1 {
		t.Fatalf("saving layout discarded unrelated preferences: %#v %v", prefs, err)
	}
}

func TestPreferenceStoreRejectsFailedLoadAndLateInstall(t *testing.T) {
	app := NewApp(platformFixtureClient(), 1280, 720, 10)
	app.SetLayout(LayoutShelf)
	readFailure := errors.New("storage unavailable")
	if err := app.SetPreferenceStore(&testPreferenceStore{readErr: readFailure}); !errors.Is(err, readFailure) {
		t.Fatalf("read failure = %v", err)
	}
	for _, malformed := range []string{"", " ", "null", "[]", `{"layout":`, `{"layout":true}`} {
		if err := app.SetPreferenceStore(&testPreferenceStore{data: []byte(malformed)}); err == nil {
			t.Fatalf("malformed record accepted: %q", malformed)
		}
		if app.preferenceStore != nil || app.grid.Mode != LayoutShelf {
			t.Fatal("failed load changed App")
		}
	}
	app.Start(context.Background())
	defer app.Stop()
	store := &testPreferenceStore{data: []byte(`{}`)}
	if err := app.SetPreferenceStore(store); err == nil || store.reads != 0 {
		t.Fatalf("late install error=%v reads=%d", err, store.reads)
	}
}

func TestPreferenceStoreWriteFailureLeavesSavedRecord(t *testing.T) {
	var warning bytes.Buffer
	stateWarnMu.Lock()
	oldWriter := stateWarnWriter
	stateWarnWriter = &warning
	stateWarnMu.Unlock()
	t.Cleanup(func() {
		stateWarnMu.Lock()
		stateWarnWriter = oldWriter
		stateWarnMu.Unlock()
	})
	store := &testPreferenceStore{data: []byte(`{"safe_area_pct":0.08,"layout":"grid"}`), writeErr: errors.New("flush failed")}
	app := catalogApp(8)
	if err := app.SetPreferenceStore(store); err != nil {
		t.Fatal(err)
	}
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	if store.writeCalls != 1 || !strings.Contains(warning.String(), "flush failed") {
		t.Fatalf("write attempts=%d warning=%q", store.writeCalls, warning.String())
	}
	restored := NewApp(nil, 1280, 720, 10)
	if err := restored.SetPreferenceStore(store); err != nil || restored.grid.Mode != LayoutGrid {
		t.Fatalf("failed write changed saved settings: layout=%v error=%v", restored.grid.Mode, err)
	}
	store.data = []byte(`invalid`)
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	if store.writeCalls != 1 {
		t.Fatal("unreadable existing preferences were overwritten")
	}
}
