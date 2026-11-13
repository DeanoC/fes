package tenfoot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func settingsLabels(snap Snapshot) string {
	var labels []string
	for _, row := range snap.Settings.Rows {
		labels = append(labels, row.Label)
	}
	return strings.Join(labels, ", ")
}

// pairedKitSettingsApp is the kit's menu display paired with a host, which
// serves it no library settings.
func pairedKitSettingsApp(t *testing.T) *App {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.client.paired = true
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetSafeAreaPct(DefaultSafeAreaPct)
	app.SetKitDisplay("kit-a")
	return app
}

func TestSettingsKitSectionsShowTheKitsOwnFacts(t *testing.T) {
	was := kitWiredStatus
	kitWiredStatus = func() string { return "Connected  ·  192.0.2.7" }
	t.Cleanup(func() { kitWiredStatus = was })
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "sha256:0123456789abcdef0123")
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", "fedcba9876543210fedc")
	app := pairedKitSettingsApp(t)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	snap := app.Snapshot()
	if got := settingsLabels(snap); got != "Games folders, Firmware, Display & sound, Network, Updates, About & help" {
		t.Fatalf("sections = %q", got)
	}
	if got := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionNetwork).Value; got != "Wired  ·  Connected  ·  192.0.2.7" {
		t.Fatalf("network summary = %q", got)
	}
	if got := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionDisplay).Value; got != "1280×720  ·  5% safe area" {
		t.Fatalf("display summary = %q", got)
	}

	focusSettingsRow(t, app, "wired", now)
	if snap := app.Snapshot(); snap.Settings.Title != "Network" || settingsLabels(snap) != "Wired" {
		t.Fatalf("network = %#v", snap.Settings)
	}
	focusSettingsRow(t, app, "resolution", now)
	if snap := app.Snapshot(); settingsLabels(snap) != "Resolution, Safe area, Reduced motion, TV surround · kit HDMI" || snap.Settings.Rows[0].Value != "1280×720" {
		t.Fatalf("display = %#v", snap.Settings)
	}
	focusSettingsRow(t, app, "versions", now)
	snap = app.Snapshot()
	if got := settingsLabels(snap); got != "Versions, Kit ID, Run setup again, Diagnostics, Factory reset" {
		t.Fatalf("about = %q", got)
	}
	if got := settingsRowByID(t, app, "versions").Value; got != "Software 0123456789ab  ·  image fedcba987654" {
		t.Fatalf("versions = %q", got)
	}
	if got := settingsRowByID(t, app, "kit-id").Value; got != "kit-a" {
		t.Fatalf("kit id = %q", got)
	}
	if !strings.HasPrefix(snap.Settings.Hint, "About & help  ·  ") || !strings.HasSuffix(snap.Settings.Hint, " back") {
		t.Fatalf("hint = %q", snap.Settings.Hint)
	}
}

func TestSettingsHostLauncherListsItsKitsAndNoWiredRow(t *testing.T) {
	app := catalogSettingsApp(t, 4)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, func(snap Snapshot) bool { return snap.Settings.Open && !snap.Settings.Loading })
	if _, ok := settingsSectionOfRow(app, "wired"); ok {
		t.Fatal("a host launcher must not report the host's own network as the kit's")
	}
	if _, ok := settingsSectionOfRow(app, "kit-id"); ok {
		t.Fatal("kit id without a kit")
	}
	focusSettingsRow(t, app, "target", now)
	if app.Snapshot().Settings.Title != "Network" {
		t.Fatalf("targets = %#v", app.Snapshot().Settings)
	}
	if _, ok := settingsSectionOfRow(app, "video-profile"); !ok {
		t.Fatal("host video preference missing from Display & sound")
	}
}

func TestSettingsShowAllCoresListsLibraryAndRooms(t *testing.T) {
	app := pairedKitSettingsApp(t)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if strings.Contains(settingsLabels(app.Snapshot()), "Library & rooms") {
		t.Fatal("Library & rooms listed without Show all cores")
	}
	focusSettingsRow(t, app, "all-cores", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdBack, now)
	snap := app.Snapshot()
	if snap.Settings.Title != "About & help" || snap.Settings.Rows[snap.Settings.Index].ID != settingsSectionRowPrefix+settingsSectionDiagnostics {
		t.Fatalf("Back from Diagnostics = %#v", snap.Settings)
	}
	app.HandleCommand(CmdBack, now)
	if got := settingsLabels(app.Snapshot()); !strings.HasSuffix(got, "About & help, Library & rooms") {
		t.Fatalf("sections = %q", got)
	}
	focusSettingsRow(t, app, "layout", now)
	if got := settingsLabels(app.Snapshot()); got != "Layout, Home, Attract" {
		t.Fatalf("paired Library & rooms = %q", got)
	}
}

func TestSettingsFactoryResetAsksTwiceThenErasesMenuSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08, ReducedMotion: true, SetupDone: true, FirmwareInfoSeen: []string{"spectrum"}}); err != nil {
		t.Fatal(err)
	}
	app := startFirstRun(t, dir, 1280, 720)
	app.SetSafeAreaPct(0.08)
	app.SetReducedMotion(true)
	waitFor(t, app, "Home", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	manager := app.driveManagerLocked()
	app.mu.Unlock()
	if _, err := manager.Save(storage.Drive{Kind: "smb", Source: "//nas/games", Username: "player", Domain: "HOME", Password: "kit-secret"}); err != nil {
		t.Fatal(err)
	}
	drivesPath := filepath.Join(dir, "drives.json")
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "factory-reset", now)
	app.HandleCommand(CmdSelect, now)
	if got := settingsRowByID(t, app, "factory-reset").Value; got != "A again to erase" {
		t.Fatalf("confirm = %q", got)
	}
	app.HandleCommand(CmdUp, now)
	app.HandleCommand(CmdDown, now)
	if got := settingsRowByID(t, app, "factory-reset").Value; got != "Erases settings, not your files" {
		t.Fatalf("moving away must cancel: %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cancelled reset touched tenfoot.json: %v", err)
	}
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	waitRoom(t, app, "example.welcome")
	snap := app.Snapshot()
	if snap.Settings.Open || snap.ReducedMotion || snap.SafeAreaPct != DefaultSafeAreaPct {
		t.Fatalf("after reset: settings=%v motion=%v safe=%v", snap.Settings.Open, snap.ReducedMotion, snap.SafeAreaPct)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("tenfoot.json survived the reset: %v", err)
	}
	if _, err := os.Stat(drivesPath); !os.IsNotExist(err) {
		t.Fatalf("drives.json survived the reset: %v", err)
	}
	// Re-adding the same connection without a password must not recover the
	// SMB password cached by the manager before the reset.
	if _, err := manager.Save(storage.Drive{Kind: "smb", Source: "//nas/games", Username: "player", Domain: "HOME", PasswordSet: true}); err != nil {
		t.Fatal(err)
	}
	remembered, err := manager.List(context.Background(), false)
	if err != nil || len(remembered) != 1 || remembered[0].PasswordSet {
		t.Fatalf("reset retained an SMB password in memory: %+v, %v", remembered, err)
	}
	app.mu.Lock()
	seen := app.firmwareCardSeenLocked("spectrum")
	app.mu.Unlock()
	if seen {
		t.Fatal("reset kept the firmware cards seen")
	}
}

func TestSettingsFactoryResetWithoutTheGateGoesHome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08}); err != nil {
		t.Fatal(err)
	}
	app := startHomeLoungeIn(t, dir, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "Home", func(s Snapshot) bool { return homeFamilySettled(s) })
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "factory-reset", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "Home", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	if snap.Settings.Open {
		t.Fatal("reset left Settings open")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("tenfoot.json survived the reset: %v", err)
	}
}

func TestSettingsFirmwareListsEachFamilyAndOpensItsCard(t *testing.T) {
	bundled := protocol.BundledFirmwareList{Firmware: []protocol.BundledFirmware{
		{ID: "emutos", Name: "EmuTOS", Version: "1.3", CoreID: "fes.atari-st", ROMID: "atari-st-firmware", Size: 192 * 1024, MediaID: strings.Repeat("a", 64), Licence: "GPL-2.0-or-later", LicenceName: "GNU GPL 2.0 or later", Notice: "EmuTOS notice", OriginURL: "https://emutos.sourceforge.io"},
		{ID: "spectrum48", Name: "Spectrum ROM", Version: "1", CoreID: "fes.spectrum", ROMID: "spectrum-firmware", Size: 16 * 1024, MediaID: strings.Repeat("b", 64), Licence: "Amstrad", LicenceName: "Amstrad permission", Notice: "Amstrad notice", OriginURL: "https://example.invalid"},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/firmware/bundled" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(bundled)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.client.paired = true
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, func(Snapshot) bool { return settingsRowByID(t, app, "firmware-0").Value != "Checking…" })
	want := map[string]string{
		"firmware-0": "Needs your files",
		"firmware-1": "Built in for 1 of 4 models",
		"firmware-2": "Built in  ·  EmuTOS",
		"firmware-3": "Needs your files",
		settingsSectionRowPrefix + settingsSectionFirmware: "Some models need your files",
	}
	for id, value := range want {
		if got := settingsRowByID(t, app, id).Value; got != value {
			t.Fatalf("%s = %q, want %q", id, got, value)
		}
	}
	focusSettingsRow(t, app, "firmware-2", now)
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "firmware card", func(s Snapshot) bool { return s.FirmwareCard.Open })
	if !snap.Settings.Open || snap.FirmwareCard.Content.Title != "Built-in firmware: EmuTOS" {
		t.Fatalf("card = %#v settings open %v", snap.FirmwareCard.Content, snap.Settings.Open)
	}
	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.FirmwareCard.Open || !snap.Settings.Open || snap.Settings.Title != "Firmware" {
		t.Fatalf("Back from the card: card %v settings %#v", snap.FirmwareCard.Open, snap.Settings)
	}
	focusSettingsRow(t, app, "firmware-3", now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "C64 card", func(s Snapshot) bool {
		return s.FirmwareCard.Open && s.FirmwareCard.Content.Secondary == "Set up system"
	})
	app.HandleCommand(CmdDetails, now)
	if snap := app.Snapshot(); snap.Settings.Open || snap.FirmwareCard.Open {
		t.Fatalf("Set up system must leave Settings: settings %v card %v", snap.Settings.Open, snap.FirmwareCard.Open)
	}
}

func TestSettingsSectionsNativeRender(t *testing.T) {
	was := kitWiredStatus
	kitWiredStatus = func() string { return "Connected  ·  192.0.2.7" }
	t.Cleanup(func() { kitWiredStatus = was })
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "sha256:0123456789abcdef0123")
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", "fedcba9876543210fedc")
	for _, size := range []struct{ w, h int }{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			dir := t.TempDir()
			if err := saveTenfootPrefs(filepath.Join(dir, "tenfoot.json"), tenfootPrefs{SafeAreaPct: DefaultSafeAreaPct, SetupDone: true}); err != nil {
				t.Fatal(err)
			}
			host := newRoomHost(t)
			client := NewClient(host.server.URL, host.server.Client())
			// Pair the fixture before Start publishes the client to pollSession.
			// Settings asks this paired host for no library settings.
			client.paired = true
			app := NewApp(client, size.w, size.h, 50)
			app.SetPrefsPath(filepath.Join(dir, "tenfoot.json"))
			app.SetRooms(rooms.NewIndex(rooms.Examples()), t.TempDir())
			app.SetHomeRooms(true)
			app.SetHomeRoom(installedCoresHomeID)
			app.SetKitLocal(&fakeLocalCores{cores: homeFixtureCores()}, &fakePadFeed{})
			app.SetFirstRun(true)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			app.SetKitDisplay("kit-a")
			app.SetSafeAreaPct(DefaultSafeAreaPct)
			waitFor(t, app, "Home", func(s Snapshot) bool { return homeFamilySettled(s) })
			now := time.Now()
			app.HandleCommand(CmdSettings, now)
			snap := waitFor(t, app, "sections", func(s Snapshot) bool {
				return s.Settings.Open && !s.Settings.Loading && !strings.Contains(settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionFirmware).Value, "Checking")
			})
			writeHomeScreenshot(t, snap, "settings-sections")
			for _, page := range []struct{ name, row string }{
				{"settings-firmware", "firmware-0"},
				{"settings-display", "resolution"},
				{"settings-network", "wired"},
				{"settings-about", "versions"},
				{"settings-diagnostics", "all-cores"},
			} {
				focusSettingsRow(t, app, page.row, now)
				writeHomeScreenshot(t, app.Snapshot(), page.name)
			}
			focusSettingsRow(t, app, "factory-reset", now)
			app.HandleCommand(CmdSelect, now)
			writeHomeScreenshot(t, app.Snapshot(), "settings-reset-confirm")
			focusSettingsRow(t, app, "firmware-2", now)
			app.HandleCommand(CmdSelect, now)
			writeHomeScreenshot(t, waitFor(t, app, "card", func(s Snapshot) bool { return s.FirmwareCard.Open }), "settings-firmware-card")
		})
	}
}

func TestSettingsFactoryResetWaitsForARunningOrStartingMachine(t *testing.T) {
	app := pairedKitSettingsApp(t)
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app.SetPrefsPath(path)
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08, SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "factory-reset", now)
	app.mu.Lock()
	app.launch.Phase = "launching"
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if !snap.Settings.Open || snap.Settings.Status != "Stop the running machine before a factory reset." {
		t.Fatalf("settings = %#v", snap.Settings)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reset during a launch erased tenfoot.json: %v", err)
	}
}

func TestSettingsPointerOffFactoryResetCancelsTheConfirm(t *testing.T) {
	app := pairedKitSettingsApp(t)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "factory-reset", now)
	app.HandleCommand(CmdSelect, now)
	app.mu.Lock()
	armed := app.settingsIndex
	app.applyPointerFocusLocked(PointerHit{Kind: PointerSettings, Index: 0})
	app.applyPointerFocusLocked(PointerHit{Kind: PointerSettings, Index: armed})
	app.mu.Unlock()
	if got := settingsRowByID(t, app, "factory-reset").Value; got != "Erases settings, not your files" {
		t.Fatalf("pointer away and back kept the confirm: %q", got)
	}
}

func TestSettingsCloseDropsAFirmwareCardStillLoading(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/library/firmware/bundled" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.client.paired = true
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "firmware-2", now)
	app.HandleCommand(CmdSelect, now)
	app.mu.Lock()
	loading := app.firmwareCard.Loading
	app.mu.Unlock()
	if !loading {
		t.Fatal("card request did not start")
	}
	leaveSettings(t, app, now)
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.firmwareCard.Loading || app.firmwareCard.Open {
		t.Fatal("closing Settings kept the firmware card request")
	}
}
