package kitlauncher

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestSharedLaunchEligibility(t *testing.T) {
	data, err := os.ReadFile("../../hostclient/testdata/launch-eligibility.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string          `json:"name"`
		Game   hostclient.Game `json:"game"`
		Block  string          `json:"block"`
		Reason string          `json:"reason"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty shared fixture")
	}
	for _, tc := range cases {
		for _, surface := range []string{"grid", "detail", "strip", "attract"} {
			t.Run(tc.Name+"/"+surface, func(t *testing.T) {
				m := eligibilityModel(tc.Game, surface)
				action := pressNamed(&m, "a", time.Unix(1, 0))
				if got, want := action == "launch", tc.Block == ""; got != want {
					t.Fatalf("action=%q block=%q reason=%q", action, tc.Block, tc.Reason)
				}
				if action == "launch" && m.consumeLaunchID() != tc.Game.ID {
					t.Fatal("wrong launch ID")
				}
			})
		}
	}
}

func eligibilityModel(game hostclient.Game, surface string) Model {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog([]hostclient.Game{game})
	switch surface {
	case "detail":
		m.openDetail(time.Unix(1, 0))
	case "strip":
		m.SetStrip([]hostclient.Game{game}, "Recent")
		m.enterStrip()
		pressNamed(&m, "a", time.Unix(1, 0))
	case "attract":
		m.attractItems = []hostclient.AttractItem{stillItem(game.ID, game.Title, handleAA())}
		m.AttractActive = true
	}
	return m
}

func TestLaunchEligibilityRetainsReadinessAndSessionGates(t *testing.T) {
	game := hostclient.Game{ID: "ready", State: "available", RootOnline: true, Launchable: true}
	for _, surface := range []string{"grid", "detail", "strip", "attract"} {
		for _, gate := range []string{"disconnected", "target-unready", "busy", "active", "failed"} {
			t.Run(surface+"/"+gate, func(t *testing.T) {
				m := eligibilityModel(game, surface)
				switch gate {
				case "disconnected":
					m.Connected = false
				case "target-unready":
					m.TargetReady = false
				case "busy":
					m.Busy = true
				default:
					m.Session.State = gate
				}
				if action := pressNamed(&m, "a", time.Unix(2, 0)); action != "" {
					t.Fatalf("action=%q", action)
				}
			})
		}
	}
}

func TestLaunchEligibilityRefresh(t *testing.T) {
	ready := hostclient.Game{ID: "game", State: "available", RootOnline: true, Launchable: true}
	for _, field := range []string{"state", "root"} {
		for _, surface := range []string{"grid", "detail", "strip", "attract"} {
			t.Run(field+"/"+surface, func(t *testing.T) {
				m := eligibilityModel(ready, surface)
				blocked := ready
				if field == "state" {
					blocked.State = "invalid"
				} else {
					blocked.RootOnline = false
				}
				if surface == "strip" {
					m.ApplyStrip([]hostclient.Game{blocked}, "Recent")
				} else {
					m.ApplyCatalog([]hostclient.Game{blocked})
				}
				if action := pressNamed(&m, "a", time.Unix(2, 0)); action != "" {
					t.Fatalf("stale eligibility launched: %q", action)
				}
			})
		}
	}
}

func TestAttractEligibilityRequiresKnownGame(t *testing.T) {
	ready := hostclient.Game{ID: "game", State: "available", RootOnline: true, Launchable: true}
	for _, source := range []string{"catalog", "grid", "strip", "absent", "playlist-blocked", "catalog-blocked"} {
		t.Run(source, func(t *testing.T) {
			m := eligibilityModel(ready, "attract")
			m.Catalog, m.Games = nil, nil
			want := true
			switch source {
			case "catalog":
				m.Catalog = []hostclient.Game{ready}
			case "grid":
				m.Games = []hostclient.Game{ready}
			case "strip":
				m.Strip = []hostclient.Game{ready}
			case "absent":
				want = false
			case "playlist-blocked":
				m.Catalog = []hostclient.Game{ready}
				m.attractItems[0].Launchable = false
				want = false
			case "catalog-blocked":
				m.Games, m.Strip = []hostclient.Game{ready}, []hostclient.Game{ready}
				ready.RootOnline = false
				m.Catalog = []hostclient.Game{ready}
				want = false
			}
			action := pressNamed(&m, "a", time.Unix(2, 0))
			if (action == "launch") != want {
				t.Fatalf("action=%q wantLaunch=%v", action, want)
			}
			if m.AttractActive {
				t.Fatal("A did not dismiss attract")
			}
			if want && m.consumeLaunchID() != "game" {
				t.Fatal("wrong launch ID")
			}
		})
	}
}
