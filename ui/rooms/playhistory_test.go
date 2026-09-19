package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestClassifyHistoryPlayedIsNotCompleted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		playCount    int64
		lastPlayedAt int64
		completed    bool
		wantPlayed   bool
		wantDone     bool
		wantLine     string
	}{
		{name: "unplayed", wantLine: ""},
		{name: "play count only", playCount: 1, wantPlayed: true, wantLine: "Played"},
		{name: "last played only", lastPlayedAt: 99, wantPlayed: true, wantLine: "Played"},
		{name: "both play facts", playCount: 3, lastPlayedAt: 100, wantPlayed: true, wantLine: "Played"},
		{name: "launch return without facts", wantLine: ""},
		{name: "explicit completed without play", completed: true, wantDone: true, wantLine: "Completed"},
		{name: "explicit completed and played", playCount: 2, completed: true, wantPlayed: true, wantDone: true, wantLine: "Played  ·  Completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyHistory(tc.playCount, tc.lastPlayedAt, tc.completed)
			if got.Played != tc.wantPlayed || got.Completed != tc.wantDone {
				t.Fatalf("history %+v want played=%v completed=%v", got, tc.wantPlayed, tc.wantDone)
			}
			if got.Line() != tc.wantLine {
				t.Fatalf("line %q want %q", got.Line(), tc.wantLine)
			}
			if got.Played && !got.Completed && got.Line() == "Completed" {
				t.Fatal("Played without an explicit record must not read as Completed")
			}
		})
	}
}

func TestGameHistoryNeverClaimsCompleted(t *testing.T) {
	t.Parallel()
	g := hostclient.Game{ID: "nes-smb", PlayCount: 4, LastPlayedAt: 99}
	got := GameHistory(g)
	if !got.Played || got.Completed || got.Line() != "Played" {
		t.Fatalf("catalog play facts %+v", got)
	}
	idle := GameHistory(hostclient.Game{ID: "nes-smb3"})
	if idle.Played || idle.Completed || idle.Line() != "" {
		t.Fatalf("unplayed catalog row %+v", idle)
	}
}

func TestDestinationFillHistoryDoesNotConflate(t *testing.T) {
	t.Parallel()
	played := Destination{
		Kind:    KindGame,
		GameID:  "snes-mario",
		Matches: []hostclient.Game{{ID: "snes-mario", PlayCount: 1, LastPlayedAt: 50}},
	}
	played.FillHistory()
	if !played.History.Played || played.History.Completed || played.History.Line() != "Played" {
		t.Fatalf("played dest %+v", played.History)
	}

	unplayed := Destination{
		Kind:    KindGame,
		GameID:  "snes-mario",
		Matches: []hostclient.Game{{ID: "snes-mario"}},
	}
	unplayed.FillHistory()
	if unplayed.History.Played || unplayed.History.Completed {
		t.Fatalf("unplayed dest %+v", unplayed.History)
	}

	choice := Destination{Kind: KindGame, Availability: AvailNeedsChoice, Matches: []hostclient.Game{
		{ID: "a", PlayCount: 1}, {ID: "b", PlayCount: 2},
	}}
	choice.FillHistory()
	if choice.History.Played || choice.History.Completed {
		t.Fatalf("needs-choice must not pick a history row %+v", choice.History)
	}

	resume := Destination{Kind: KindGame, GameID: "snes-mario", Matches: []hostclient.Game{{ID: "snes-mario"}}}
	resume.FillHistory()
	if resume.History.Completed || resume.History.Line() == "Completed" {
		t.Fatal("returning from a launch without a completion record must not claim Completed")
	}
}
