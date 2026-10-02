package rooms

import (
	"strings"
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
		{name: "one viable among blocked", games: []hostclient.Game{marioUSA, offline}, query: "", want: AvailReady, n: 1},
		{name: "all blocked is unavailable", games: []hostclient.Game{offline, blockedGame("nes-smb-jp", "Super Mario Bros.", "nes", hostclient.LaunchBrowseOnly)}, query: "", want: AvailUnavailable, n: 2},
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
			status:  CheckingStatus,
			action:  CheckingAction,
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
			dest:    Destination{Kind: KindGame, Availability: AvailNeedsChoice, Matches: []hostclient.Game{readyGame("nes-smb-usa", "Super Mario Bros.", "nes"), readyGame("nes-smb-jp", "Super Mario Bros.", "nes")}},
			status:  EditionChoiceStatus,
			action:  EditionChoiceAction,
			confirm: ConfirmChoose,
		},
		{
			name: "backend choice",
			dest: Destination{Kind: KindGame, Availability: AvailNeedsChoice, Matches: []hostclient.Game{
				func() hostclient.Game {
					g := readyGame("fpga-data-storm", "Data Storm", "sms")
					g.Execution = "fpga_native"
					return g
				}(),
				func() hostclient.Game {
					g := readyGame("sms-data-storm", "Data Storm", "sms")
					g.Execution = hostclient.ExecutionHostOnly
					return g
				}(),
			}},
			status:  BackendChoiceStatus,
			action:  BackendChoiceAction,
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
			status:  CheckingStatus,
			action:  CheckingAction,
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
			name:    "settings action",
			dest:    Destination{Kind: KindAction, LauncherAction: "settings", Availability: AvailReady, Label: "Settings"},
			status:  "Open settings.",
			action:  "Open settings.",
			confirm: ConfirmLauncherAction,
		},
		{
			name:    "core ready",
			dest:    Destination{Kind: KindCore, PackageID: strings.Repeat("ab", 32), CoreID: "fes.pong", Label: "FES Pong", CoreLaunchable: true, Availability: AvailReady},
			status:  "Ready to play.",
			action:  "Play",
			confirm: ConfirmLaunchCore,
		},
		{
			name:    "core needs cartridge",
			dest:    Destination{Kind: KindCore, PackageID: strings.Repeat("cd", 32), CoreID: "fes.coleco", Label: "ColecoVision", CoreBlock: "Needs a cartridge", Availability: AvailUnavailable},
			status:  "Needs a cartridge",
			action:  "Needs a cartridge",
			confirm: ConfirmExplain,
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

func TestParseKindDropsUnknownAndAcceptsAction(t *testing.T) {
	t.Parallel()
	if parseKind("nope") != "" || parseKind("shell") != "" || parseKind("") != "" {
		t.Fatal("unknown kinds must stay empty")
	}
	if parseKind("action") != KindAction || parseKind(" ACTION ") != KindAction {
		t.Fatal("action kind must parse")
	}
	if parseKind("game") != KindGame || parseKind("room") != KindRoom {
		t.Fatal("existing kinds changed")
	}
	if parseKind("core") != KindCore || parseKind(" CORE ") != KindCore {
		t.Fatal("core kind must parse")
	}
}

func TestValidPackageIDAndCoreID(t *testing.T) {
	t.Parallel()
	good := strings.Repeat("ab", 32)
	if len(good) != 64 || !ValidPackageID(good) {
		t.Fatalf("package id %q", good)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64), "../" + good[:60], good + " "} {
		if ValidPackageID(bad) {
			t.Fatalf("package id accepted %q", bad)
		}
	}
	if !validCoreID("fes.pong") || !validCoreID("fes.sg1000") {
		t.Fatal("core ids must match the safe token")
	}
	for _, bad := range []string{"", "FES.pong", "fes.pong/../x", "fes pong", "../fes.pong"} {
		if validCoreID(bad) {
			t.Fatalf("core id accepted %q", bad)
		}
	}
}

func TestLauncherActionAllowlistIsSettingsOnly(t *testing.T) {
	t.Parallel()
	if !LauncherActionAllowed("settings") || !LauncherActionAllowed(" settings ") {
		t.Fatal("settings must be allowlisted")
	}
	for _, id := range []string{"", "Settings", "shell", "settings;rm", "../settings", "settings extra"} {
		if LauncherActionAllowed(id) {
			t.Fatalf("id %q must be rejected", id)
		}
	}
}

func TestLauncherActionStaysReadyUnderForeignLease(t *testing.T) {
	t.Parallel()
	d := Destination{Kind: KindAction, LauncherAction: "settings", Availability: AvailReady}
	d.FillCopy()
	got := ApplyForeignLease(d, true)
	if got.Availability != AvailReady || got.Confirm() != ConfirmLauncherAction || got.LeaseHeld {
		t.Fatalf("action under foreign lease %+v", got)
	}
	if got.Status == InUseStatus || got.Action == InUseDetail {
		t.Fatal("launcher action must not use the lease copy")
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

func TestClassifyGamesOnlyViableOptionsAreAChoice(t *testing.T) {
	t.Parallel()
	fpga := readyGame("fpga-data-storm", "Data Storm", "sms")
	fpga.Execution = "fpga_native"
	emu := readyGame("sms-data-storm", "Data Storm", "sms")
	emu.Execution = hostclient.ExecutionHostOnly
	state, matches := ClassifyGames([]hostclient.Game{fpga, emu}, "Data Storm")
	if state != AvailNeedsChoice || PlayChoiceKind(matches) != ChoiceBackend || len(matches) != 2 {
		t.Fatalf("backends %s %+v kind=%s", state, matches, PlayChoiceKind(matches))
	}
	dest := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: "Data Storm", Label: "Data Storm"}
	dest.FillCopy()
	if dest.Choice != ChoiceBackend || dest.Status != BackendChoiceStatus || dest.Confirm() != ConfirmChoose {
		t.Fatalf("backend copy %+v", dest)
	}

	blocked := fpga
	ready := false
	blocked.ReadyHere = &ready
	blocked.ReadyBlock = string(hostclient.LaunchVersionSkew)
	state, matches = ClassifyGames([]hostclient.Game{blocked, emu}, "Data Storm")
	if state != AvailReady || len(matches) != 1 || matches[0].ID != emu.ID {
		t.Fatalf("single viable %s %+v", state, matches)
	}

	busy := emu
	busy.ReadyHere = &ready
	busy.ReadyBlock = string(hostclient.LaunchLeaseHeld)
	state, matches = ClassifyGames([]hostclient.Game{blocked, busy}, "Data Storm")
	fail := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: "Data Storm"}
	fail.FillCopy()
	if state != AvailUnavailable || len(matches) != 2 || fail.Confirm() == ConfirmChoose || fail.Status == EditionChoiceStatus || fail.Status == BackendChoiceStatus {
		t.Fatalf("fail closed became a choice %+v confirm=%v", fail, fail.Confirm())
	}
	if fail.Status != "Can't play here yet." && fail.Status != InUseStatus {
		t.Fatalf("fail closed copy %q", fail.Status)
	}

	progress := blocked
	progress.ReadyBlock = string(hostclient.LaunchEnsureProgress)
	state, _ = ClassifyGames([]hostclient.Game{progress}, "Data Storm")
	if state != AvailChecking {
		t.Fatalf("checking %s", state)
	}
	if AvailMissing.Label() != "Unavailable" || AvailUnavailable.Label() != "Unavailable" || AvailReady.Label() != "Ready" || AvailNeedsChoice.Label() != "Needs a choice" || AvailChecking.Label() != "Checking" {
		t.Fatal("four-state labels")
	}
	if BackendLabel(fpga) != "FPGA" || BackendLabel(emu) != "Emulator" {
		t.Fatalf("backend labels %q %q", BackendLabel(fpga), BackendLabel(emu))
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
	needsCartridge := readyGame("sms-data-storm", "Data Storm 1.00", "sms")
	needsCartridge.ROMRequired = true
	state, matches = ClassifyGames([]hostclient.Game{needsCartridge}, "Data Storm")
	cart := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: "Data Storm"}
	cart.FillCopy()
	if state != AvailUnavailable || cart.Confirm() != ConfirmExplain || cart.Status != "Needs a cartridge" {
		t.Fatalf("missing cartridge: %s %+v", state, cart)
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
	if foreign.Status != InUseStatus || foreign.Action != InUseDetail {
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
	core := Destination{Kind: KindCore, CoreLaunchable: true, Availability: AvailReady, PackageID: strings.Repeat("ab", 32), CoreID: "fes.pong", Label: "FES Pong"}
	core.FillCopy()
	coreKept := ApplyForeignLease(core, true)
	if coreKept.Availability != AvailReady || coreKept.LeaseHeld || coreKept.Confirm() != ConfirmLaunchCore || coreKept.Status != "Ready to play." {
		t.Fatalf("core destination rewritten by foreign lease %+v confirm=%v", coreKept, coreKept.Confirm())
	}
}
