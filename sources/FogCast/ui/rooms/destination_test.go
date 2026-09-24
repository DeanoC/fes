package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func readyGame(id, title, system string) hostclient.Game {
	return hostclient.Game{ID: id, Title: title, System: system, Launchable: true, State: "available", RootOnline: true}
}

func blockedGame(id, title, system string, block hostclient.LaunchBlock) hostclient.Game {
	g := hostclient.Game{ID: id, Title: title, System: system, Launchable: true, State: "available", RootOnline: true}
	switch block {
	case hostclient.LaunchBrowseOnly:
		g.Launchable = false
	case hostclient.LaunchSourceOffline:
		g.RootOnline = false
	case hostclient.LaunchUnreadable:
		g.State = "invalid"
	case hostclient.LaunchNotReady:
		g.State = "pending"
	case hostclient.LaunchMissingFirmware:
		g.FirmwareRequired = true
		g.FirmwareReady = false
	}
	return g
}

func TestClassifyGamesFiveStates(t *testing.T) {
	t.Parallel()
	mario := readyGame("nes-smb", "Super Mario Bros.", "nes")
	marioUSA := readyGame("nes-smb-usa", "Super Mario Bros.", "nes")
	marioJP := readyGame("nes-smb-jp", "Super Mario Bros.", "nes")
	offline := blockedGame("snes-smw", "Super Mario World", "snes", hostclient.LaunchSourceOffline)

	cases := []struct {
		name  string
		games []hostclient.Game
		query string
		want  Availability
		n     int
	}{
		{name: "missing", games: []hostclient.Game{mario}, query: "Yoshi's Island", want: AvailMissing, n: 0},
		{name: "ready exact", games: []hostclient.Game{mario, readyGame("nes-smb3", "Super Mario Bros. 3", "nes")}, query: "Super Mario Bros.", want: AvailReady, n: 1},
		{name: "needs choice", games: []hostclient.Game{marioUSA, marioJP}, query: "Super Mario Bros.", want: AvailNeedsChoice, n: 2},
		{name: "unavailable", games: []hostclient.Game{offline}, query: "Super Mario World", want: AvailUnavailable, n: 1},
		{name: "empty query one ready", games: []hostclient.Game{mario}, query: "", want: AvailReady, n: 1},
		{name: "empty query many", games: []hostclient.Game{mario, marioUSA}, query: "", want: AvailNeedsChoice, n: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, matches := ClassifyGames(tc.games, tc.query)
			if got != tc.want {
				t.Fatalf("state %s want %s", got, tc.want)
			}
			if len(matches) != tc.n {
				t.Fatalf("matches %d want %d", len(matches), tc.n)
			}
		})
	}
}

func TestDestinationCopyAndConfirmNeverNoOp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dest    Destination
		status  string
		action  string
		confirm ConfirmIntent
	}{
		{
			name:    "checking",
			dest:    Destination{Kind: KindGame, Availability: AvailChecking, Label: "Donkey Kong"},
			status:  "Matching this title in your library…",
			action:  "Wait — still checking.",
			confirm: ConfirmWait,
		},
		{
			name:    "missing",
			dest:    Destination{Kind: KindGame, Availability: AvailMissing, Query: "Picross"},
			status:  "Not in this household's library (Picross).",
			action:  "Open the library to add it.",
			confirm: ConfirmOpenLibrary,
		},
		{
			name:    "needs choice",
			dest:    Destination{Kind: KindGame, Availability: AvailNeedsChoice},
			status:  "Several editions match. Choose one.",
			action:  "Choose an edition.",
			confirm: ConfirmChoose,
		},
		{
			name:    "unavailable",
			dest:    Destination{Kind: KindGame, Availability: AvailUnavailable, Matches: []hostclient.Game{blockedGame("x", "X", "snes", hostclient.LaunchBrowseOnly)}},
			status:  "This platform is browse-only on this host.",
			action:  "See why this title cannot play.",
			confirm: ConfirmExplain,
		},
		{
			name:    "frogger missing firmware",
			dest:    Destination{Kind: KindGame, Availability: AvailUnavailable, Matches: []hostclient.Game{blockedGame("fpga-frogger", "Frogger", "fpga", hostclient.LaunchMissingFirmware)}},
			status:  "Coleco BIOS required. Import household firmware before Play.",
			action:  "Import Coleco BIOS.",
			confirm: ConfirmImportFirmware,
		},
		{
			name:    "ready",
			dest:    Destination{Kind: KindGame, Availability: AvailReady, GameID: "nes-smb"},
			status:  "Ready to play.",
			action:  "Play",
			confirm: ConfirmLaunch,
		},
		{
			name:    "room",
			dest:    Destination{Kind: KindRoom, Label: "Sports Island", RoomID: "example.mario-sports"},
			status:  "Enter room.",
			action:  "Enter room.",
			confirm: ConfirmEnterRoom,
		},
		{
			name:    "unresolved",
			dest:    Destination{Kind: KindUnresolved, Availability: AvailChecking},
			status:  "Matching this title in your library…",
			action:  "Wait — still checking.",
			confirm: ConfirmWait,
		},
		{
			name:    "library",
			dest:    Destination{Kind: KindLibrary, Label: "Library"},
			status:  "Browse the full library.",
			action:  "Open library.",
			confirm: ConfirmOpenLibraryBrowse,
		},
		{
			name:    "unresolved location",
			dest:    Destination{Kind: KindUnresolved, Label: "ColecoVision"},
			status:  "Choose a title from this location.",
			action:  "Open the title list.",
			confirm: ConfirmNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.dest
			d.FillCopy()
			if d.Status != tc.status {
				t.Fatalf("status %q want %q", d.Status, tc.status)
			}
			if d.Action != tc.action {
				t.Fatalf("action %q want %q", d.Action, tc.action)
			}
			if d.Confirm() != tc.confirm {
				t.Fatalf("confirm %v want %v", d.Confirm(), tc.confirm)
			}
		})
	}
}

func TestCuratorAttribution(t *testing.T) {
	t.Parallel()
	d := Destination{Note: "Keep Yoshi.", NoteBy: "FogCast"}
	if got := d.CuratorAttribution(); got != "Note from FogCast" {
		t.Fatalf("attr %q", got)
	}
	var empty Destination
	if empty.CuratorAttribution() != "" {
		t.Fatal("empty note must stay hidden")
	}
}

func TestClassifyDoesNotPreferLaterContainsOverExact(t *testing.T) {
	t.Parallel()
	games := []hostclient.Game{
		readyGame("nes-smb3", "Super Mario Bros. 3", "nes"),
		readyGame("nes-smb", "Super Mario Bros.", "nes"),
	}
	state, matches := ClassifyGames(games, "Super Mario Bros.")
	if state != AvailReady || len(matches) != 1 || matches[0].ID != "nes-smb" {
		t.Fatalf("got %s %+v", state, matches)
	}
}

func TestApplyEditionPreferenceSkipsReaskWhenSavedMatchExists(t *testing.T) {
	t.Parallel()
	usa := readyGame("nes-smb-usa", "Super Mario Bros.", "nes")
	jp := readyGame("nes-smb-jp", "Super Mario Bros.", "nes")
	dest := Destination{
		Kind: KindGame, Label: "Super Mario Bros.", Query: "Super Mario Bros.", Platform: "nes",
		Availability: AvailNeedsChoice, Matches: []hostclient.Game{usa, jp},
	}
	dest.FillCopy()
	if dest.Confirm() != ConfirmChoose {
		t.Fatalf("unsaved confirm %v", dest.Confirm())
	}
	applied := ApplyEditionPreference(dest, "nes-smb-usa")
	if applied.Availability != AvailReady || applied.Confirm() != ConfirmLaunch || applied.GameID != "nes-smb-usa" {
		t.Fatalf("saved ready %+v confirm=%v", applied, applied.Confirm())
	}
	if applied.Action != "Play" || len(applied.Matches) != 1 {
		t.Fatalf("saved panel %+v", applied)
	}
	stale := ApplyEditionPreference(dest, "nes-missing")
	if stale.Availability != AvailNeedsChoice || stale.Confirm() != ConfirmChoose {
		t.Fatalf("stale must still force a choice %+v", stale)
	}
	blocked := blockedGame("nes-smb-usa", "Super Mario Bros.", "nes", hostclient.LaunchBrowseOnly)
	unavail := Destination{
		Kind: KindGame, Query: "Super Mario Bros.", Platform: "nes",
		Availability: AvailNeedsChoice, Matches: []hostclient.Game{blocked, jp},
	}
	got := ApplyEditionPreference(unavail, "nes-smb-usa")
	if got.Availability != AvailUnavailable || got.Confirm() != ConfirmExplain {
		t.Fatalf("preferred unavailable %+v confirm=%v", got, got.Confirm())
	}
	firmware := blockedGame("fpga-frogger", "Frogger", "fpga", hostclient.LaunchMissingFirmware)
	fwDest := Destination{
		Kind: KindGame, Query: "Frogger", Platform: "fpga",
		Availability: AvailNeedsChoice, Matches: []hostclient.Game{firmware, jp},
	}
	fwGot := ApplyEditionPreference(fwDest, "fpga-frogger")
	if fwGot.Availability != AvailUnavailable || fwGot.Confirm() != ConfirmImportFirmware {
		t.Fatalf("preferred missing firmware %+v confirm=%v", fwGot, fwGot.Confirm())
	}
	if DestinationPreferenceKey(dest) != "super mario bros|nes" {
		t.Fatalf("key %q", DestinationPreferenceKey(dest))
	}
}

func TestClassifyColecoFirmwareReadiness(t *testing.T) {
	t.Parallel()
	graphics := readyGame("fpga-graphics-i", "Graphics I", "fpga")
	frogger := readyGame("fpga-frogger", "Frogger", "fpga")
	frogger.FirmwareRequired = true
	state, matches := ClassifyGames([]hostclient.Game{graphics}, "Graphics I")
	if state != AvailReady || len(matches) != 1 {
		t.Fatalf("graphics-i: %s %+v", state, matches)
	}
	state, matches = ClassifyGames([]hostclient.Game{frogger}, "Frogger")
	if state != AvailUnavailable || len(matches) != 1 || matches[0].LaunchBlock() != hostclient.LaunchMissingFirmware {
		t.Fatalf("frogger without BIOS: %s %+v", state, matches)
	}
	frogger.FirmwareReady = true
	state, matches = ClassifyGames([]hostclient.Game{frogger}, "Frogger")
	if state != AvailReady || len(matches) != 1 {
		t.Fatalf("frogger with BIOS: %s %+v", state, matches)
	}
	packageReady := readyGame("fpga-donkey-kong-72a5215aeed0", "Donkey Kong", "coleco")
	packageReady.FirmwareRequired = true
	packageReady.FirmwareReady = true
	state, matches = ClassifyGames([]hostclient.Game{packageReady}, "Donkey Kong")
	if state != AvailReady || len(matches) != 1 || matches[0].LaunchBlock() != "" {
		t.Fatalf("firmware-ready FPGA Coleco package: %s %+v", state, matches)
	}
	raw := blockedGame("coleco-donkey-kong", "Donkey Kong", "coleco", hostclient.LaunchBrowseOnly)
	state, matches = ClassifyGames([]hostclient.Game{raw}, "Donkey Kong")
	if state != AvailUnavailable || len(matches) != 1 || matches[0].LaunchBlock() != hostclient.LaunchBrowseOnly {
		t.Fatalf("raw coleco cart: %s %+v", state, matches)
	}
}

func TestForeignLeaseIsUnavailableInUseAndOwnedLeaseStaysReady(t *testing.T) {
	t.Parallel()
	ready := Destination{
		Kind: KindGame, Availability: AvailReady, GameID: "nes-smb", Label: "Super Mario Bros.",
		Matches: []hostclient.Game{readyGame("nes-smb", "Super Mario Bros.", "nes")},
	}
	ready.FillCopy()
	owned := ApplyForeignLease(ready, false)
	if owned.Availability != AvailReady || owned.Confirm() != ConfirmLaunch || owned.LeaseHeld || owned.Status != "Ready to play." {
		t.Fatalf("same-shell retained lease %+v confirm=%v", owned, owned.Confirm())
	}
	foreign := ApplyForeignLease(ready, true)
	if foreign.Availability != AvailUnavailable || !foreign.LeaseHeld || foreign.Confirm() != ConfirmExplain {
		t.Fatalf("foreign lease %+v confirm=%v", foreign, foreign.Confirm())
	}
	if foreign.Status != "This executor is in use." || foreign.Action != "Do not take the lease." {
		t.Fatalf("in-use copy status=%q action=%q", foreign.Status, foreign.Action)
	}
	firmware := Destination{
		Kind: KindGame, Availability: AvailUnavailable,
		Matches: []hostclient.Game{blockedGame("fpga-frogger", "Frogger", "fpga", hostclient.LaunchMissingFirmware)},
	}
	firmware.FillCopy()
	kept := ApplyForeignLease(firmware, true)
	if kept.Confirm() != ConfirmImportFirmware || kept.LeaseHeld {
		t.Fatalf("firmware block became in use %+v", kept)
	}
	hostGame := readyGame("snes-mario", "Super Mario World", "snes")
	hostGame.Execution = hostclient.ExecutionHostOnly
	hostReady := Destination{
		Kind: KindGame, Availability: AvailReady, GameID: hostGame.ID, Label: hostGame.Title,
		Matches: []hostclient.Game{hostGame},
	}
	hostReady.FillCopy()
	hostKept := ApplyForeignLease(hostReady, true)
	if hostKept.Availability != AvailReady || hostKept.LeaseHeld || hostKept.Confirm() != ConfirmLaunch || hostKept.Status != "Ready to play." {
		t.Fatalf("host-only foreign lease %+v confirm=%v", hostKept, hostKept.Confirm())
	}
}
