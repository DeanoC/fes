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
			if d.Confirm() != tc.confirm || d.Confirm() == ConfirmNone {
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
