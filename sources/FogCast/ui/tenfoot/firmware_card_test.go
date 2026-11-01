package tenfoot

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

var (
	testEmuTOS = protocol.BundledFirmware{ID: "emutos-192us", Name: "EmuTOS", Version: "1.4", CoreID: "fes.atari-st", ROMID: "atari-st-firmware",
		Size: 196608, MediaID: strings.Repeat("e", 64), Licence: "GPL-2.0-only", LicenceName: "GNU General Public License v2",
		LicenceText: "GNU GENERAL PUBLIC LICENSE\nVersion 2", Notice: "EmuTOS is free software.", OriginURL: "https://example.invalid/emutos.zip",
		SourceURL: "https://example.invalid/emutos-src.tar.gz", SourceSHA256: strings.Repeat("f", 64)}
	amstradNotice = "Amstrad have kindly given their permission for the redistribution of their copyrighted material but retain that copyright."
)

func testSpectrumROM(coreID, name string) protocol.BundledFirmware {
	return protocol.BundledFirmware{ID: coreID, Name: name, Version: "1", CoreID: coreID, ROMID: "rom", Size: 16384, MediaID: strings.Repeat("a", 64),
		Licence: "LicenseRef-Amstrad-ROM-permission", LicenceName: "Amstrad's permission to redistribute", Notice: amstradNotice, OriginURL: "https://example.invalid/roms.tar.gz"}
}

func spectrumModels() []firmwareCardModel {
	return []firmwareCardModel{{"fes.spectrum", "ZX Spectrum 48K"}, {"fes.spectrum128", "ZX Spectrum 128K"}, {"fes.spectrum-plus3", "ZX Spectrum +3"}, {"fes.pentagon", "Pentagon 128"}}
}

func TestFirmwareCardCopyBuiltIn(t *testing.T) {
	c, ok := firmwareCardCopy("Atari ST", []firmwareCardModel{{"fes.atari-st", "Atari ST"}}, []protocol.BundledFirmware{testEmuTOS}, true)
	if !ok || c.Title != "Built-in firmware: EmuTOS" || c.Kicker != "First time on the Atari ST" || c.Primary != "Continue to Atari ST" || c.Secondary != "Licence" {
		t.Fatalf("card %+v", c)
	}
	if len(c.Licence) != 1 || !strings.Contains(c.Licence[0], "GNU General Public License v2") {
		t.Fatalf("licence %q", c.Licence)
	}
	view := strings.Join(c.LicenceView, "\n")
	for _, want := range []string{"GPL-2.0-only", "Source code: https://example.invalid/emutos-src.tar.gz", "Version 2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("licence page lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(strings.Join(c.Footnote, " "), "Shown once") {
		t.Fatalf("first-use footnote %q", c.Footnote)
	}
	again, _ := firmwareCardCopy("Atari ST", []firmwareCardModel{{"fes.atari-st", "Atari ST"}}, []protocol.BundledFirmware{testEmuTOS}, false)
	if again.Primary != "Close" || strings.Contains(strings.Join(again.Footnote, " "), "Shown once") {
		t.Fatalf("reopened card %+v", again)
	}
}

func TestFirmwareCardCopyMixedFamilyShowsAmstradNoticeOnce(t *testing.T) {
	bundled := []protocol.BundledFirmware{testSpectrumROM("fes.spectrum", "48K ROM"), testSpectrumROM("fes.spectrum128", "128K ROMs"), testSpectrumROM("fes.spectrum-plus3", "+3 ROMs")}
	c, ok := firmwareCardCopy("ZX Spectrum", spectrumModels(), bundled, true)
	if !ok || c.Title != "Built-in firmware for most models" {
		t.Fatalf("card %+v", c)
	}
	body := strings.Join(c.Body, " ")
	if !strings.Contains(body, "Pentagon 128 needs a 48 KiB image") || strings.Contains(body, "ZX Spectrum 48K needs") {
		t.Fatalf("body %q", body)
	}
	if len(c.Licence) != 1 || !strings.Contains(c.Licence[0], amstradNotice) {
		t.Fatalf("Amstrad notice must show once: %q", c.Licence)
	}
}

func TestFirmwareCardCopyUserSupplied(t *testing.T) {
	c, ok := firmwareCardCopy("Commodore 64", []firmwareCardModel{{"fes.c64", "Commodore 64"}}, []protocol.BundledFirmware{testEmuTOS}, true)
	if !ok || c.Title != "The Commodore 64 needs your firmware" || c.Secondary != "Set up system" || len(c.Licence) != 0 {
		t.Fatalf("card %+v", c)
	}
	if !strings.Contains(strings.Join(c.Body, " "), "36 KiB") {
		t.Fatalf("body %q", c.Body)
	}
	// An older host lists nothing, so nothing is claimed as built in.
	old, ok := firmwareCardCopy("ZX Spectrum", spectrumModels(), nil, true)
	if !ok || old.Title != "The ZX Spectrum needs your firmware" || len(old.Body) != 5 {
		t.Fatalf("older host card %+v", old)
	}
	if _, ok := firmwareCardCopy("Pong", []firmwareCardModel{{"fes.pong", "Pong"}}, nil, true); ok {
		t.Fatal("a family without firmware got a card")
	}
}

func firmwareCardHost(t *testing.T, status int, list []protocol.BundledFirmware) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/firmware/bundled" {
			t.Errorf("unexpected %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		calls.Add(1)
		w.WriteHeader(status)
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(protocol.BundledFirmwareList{Firmware: list})
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func waitFirmwareCard(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		loading := a.firmwareCard.Loading
		a.mu.Unlock()
		if !loading {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("firmware card request did not finish")
}

func stAction(firstUse bool) rooms.Action {
	return rooms.Action{Kind: rooms.ActionFirmwareInfo, CoreID: "fes.atari-st", CoreIDs: []string{"fes.atari-st"}, FamilyID: "fes.atari-st", FamilyLabel: "Atari ST", FirstUse: firstUse}
}

func TestFirmwareCardIsShownOncePerFamily(t *testing.T) {
	server, calls := firmwareCardHost(t, http.StatusOK, []protocol.BundledFirmware{testEmuTOS})
	prefs := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(prefs, tenfootPrefs{SafeAreaPct: 0.05, Layout: "grid", Theme: "night"}); err != nil {
		t.Fatal(err)
	}
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.SetPrefsPath(prefs)
	a.mu.Lock()
	a.openFirmwareCardLocked(stAction(true))
	a.mu.Unlock()
	waitFirmwareCard(t, a)
	a.mu.Lock()
	if !a.firmwareCard.Open || a.firmwareCardSnapshotLocked().Content.Title != "Built-in firmware: EmuTOS" {
		t.Fatalf("card not open: %+v", a.firmwareCard)
	}
	a.handleFirmwareCardLocked(CmdDetails)
	if !a.firmwareCard.LicenceView {
		t.Fatal("Y did not open the licence page")
	}
	a.handleFirmwareCardLocked(CmdBack)
	if !a.firmwareCard.Open || a.firmwareCard.LicenceView {
		t.Fatal("B on the licence page must return to the card")
	}
	a.handleFirmwareCardLocked(CmdSelect)
	open := a.firmwareCard.Open
	a.mu.Unlock()
	if open {
		t.Fatal("A did not close the card")
	}
	saved, err := loadTenfootPrefs(prefs)
	if err != nil || len(saved.FirmwareInfoSeen) != 1 || saved.FirmwareInfoSeen[0] != "fes.atari-st" || saved.Theme != "night" {
		t.Fatalf("saved prefs %+v err=%v", saved, err)
	}

	// A later launch reads the record and asks the host nothing.
	b := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	b.ctx = context.Background()
	b.SetPrefsPath(prefs)
	before := calls.Load()
	b.mu.Lock()
	b.openFirmwareCardLocked(stAction(true))
	loading := b.firmwareCard.Loading
	b.mu.Unlock()
	if loading || calls.Load() != before {
		t.Fatal("a seen family's first-use card was requested again")
	}
	// The Firmware button still reopens it.
	b.mu.Lock()
	b.openFirmwareCardLocked(stAction(false))
	b.mu.Unlock()
	waitFirmwareCard(t, b)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.firmwareCard.Open || b.firmwareCard.Content.Primary != "Close" {
		t.Fatalf("reopened card %+v", b.firmwareCard)
	}
}

func TestFirmwareCardBackDoesNotRecordFamily(t *testing.T) {
	server, _ := firmwareCardHost(t, http.StatusOK, []protocol.BundledFirmware{testEmuTOS})
	prefs := filepath.Join(t.TempDir(), "tenfoot.json")
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.SetPrefsPath(prefs)
	a.mu.Lock()
	a.openFirmwareCardLocked(stAction(true))
	a.mu.Unlock()
	waitFirmwareCard(t, a)
	a.mu.Lock()
	a.handleFirmwareCardLocked(CmdBack)
	open, seen := a.firmwareCard.Open, a.firmwareCardSeenLocked("fes.atari-st")
	a.mu.Unlock()
	if open || seen {
		t.Fatalf("B closed=%v recorded=%v", !open, seen)
	}
	if _, err := os.Stat(prefs); !os.IsNotExist(err) {
		t.Fatalf("B wrote prefs: %v", err)
	}
}

func TestFirmwareCardFailsClosed(t *testing.T) {
	// An older host has no route: the card names the user's file and claims
	// no built-in firmware.
	server, _ := firmwareCardHost(t, http.StatusNotFound, nil)
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	a.mu.Lock()
	a.openFirmwareCardLocked(stAction(true))
	a.mu.Unlock()
	waitFirmwareCard(t, a)
	a.mu.Lock()
	if !a.firmwareCard.Open || a.firmwareCard.Content.Title != "The Atari ST needs your firmware" {
		t.Fatalf("older host card %+v", a.firmwareCard)
	}
	a.mu.Unlock()

	// A host that cannot be asked shows nothing, and nothing is recorded.
	failing, _ := firmwareCardHost(t, http.StatusInternalServerError, nil)
	b := NewApp(NewClient(failing.URL, failing.Client()), 1280, 720, 10)
	b.ctx = context.Background()
	b.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	b.mu.Lock()
	b.openFirmwareCardLocked(stAction(true))
	b.mu.Unlock()
	waitFirmwareCard(t, b)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.firmwareCard.Open || b.firmwareCardSeenLocked("fes.atari-st") {
		t.Fatalf("failed request opened or recorded the card: %+v", b.firmwareCard)
	}
}

func TestFirmwareCardRender(t *testing.T) {
	bundled := []protocol.BundledFirmware{testEmuTOS, testSpectrumROM("fes.spectrum", "48K ROM"), testSpectrumROM("fes.spectrum128", "128K ROMs"), testSpectrumROM("fes.spectrum-plus3", "+3 ROMs")}
	scenes := map[string]FirmwareCardSnapshot{}
	st, _ := firmwareCardCopy("Atari ST", []firmwareCardModel{{"fes.atari-st", "Atari ST"}}, bundled, true)
	scenes["built-in"] = FirmwareCardSnapshot{Open: true, Content: st}
	scenes["licence"] = FirmwareCardSnapshot{Open: true, LicenceView: true, Content: st}
	spectrum, _ := firmwareCardCopy("ZX Spectrum", spectrumModels(), bundled, true)
	scenes["mixed"] = FirmwareCardSnapshot{Open: true, Content: spectrum}
	c64, _ := firmwareCardCopy("Commodore 64", []firmwareCardModel{{"fes.c64", "Commodore 64"}}, bundled, true)
	scenes["your-rom"] = FirmwareCardSnapshot{Open: true, Content: c64}
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		for name, card := range scenes {
			t.Run(fmt.Sprintf("%s-%dx%d", name, size[0], size[1]), func(t *testing.T) {
				app := libraryRenderApp(t, size[0], size[1], true)
				snap := app.Snapshot()
				snap.Grid.Safe = insetsFromPct(size[0], size[1], DefaultSafeAreaPct)
				snap.Grid.Layout(size[0], size[1])
				snap.FirmwareCard = card
				r := firmwareCardRect(snap.Grid, card)
				if r.X-4 < snap.Grid.contentLeft() || r.Y-4 < snap.Grid.contentTop() || r.X+r.W+4 > snap.Grid.Width-snap.Grid.Safe.Right || r.Y+r.H+4 > snap.Grid.Height-snap.Grid.Safe.Bottom {
					t.Fatalf("card escapes the safe area: %+v safe %+v", r, snap.Grid.Safe)
				}
				dev, err := gfx.NewSoftware(size[0], size[1])
				if err != nil {
					t.Fatal(err)
				}
				presentFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
				if out := strings.TrimSpace(os.Getenv("FES_ROOM_SCREENSHOT_DIR")); out != "" {
					path := filepath.Join(out, fmt.Sprintf("firmware-card-%s-%dx%d.png", name, size[0], size[1]))
					file, err := os.Create(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(file, dev.Snapshot()); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
