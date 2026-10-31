package rooms

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestRoomNameProjectionsPreserveMachineAndMediaIdentity(t *testing.T) {
	pkg, media := strings.Repeat("a", 64), strings.Repeat("b", 64)
	generated := preparedComputer("diagnostic-zx81", "ZX81 HIL790", "fes.zx81")
	generated.PackageID, generated.MediaID, generated.MediaRole = pkg, media, "blob"
	custom := generated
	custom.ID, custom.Title = "custom-zx81", "My ZX81 HIL790"
	svc := &hardwareFixture{fakeServices: fakeServices{games: []hostclient.Game{generated, custom}}, value: hostclient.HardwareSnapshot{
		Machines:  []hostclient.HardwareMachine{{GameID: generated.ID, Title: generated.Title, CoreID: generated.CoreID, PackageID: pkg, MediaID: media}},
		Computers: []hostclient.ShelfComputer{{GameID: "coleco", Title: "Coleco HIL 1120 123509", CoreID: "fes.coleco", PackageID: pkg}},
	}}
	pack := memPack(t, "names", `
function load()
  kit.cores(function(v) cores=v end)
  library.query({platform="fpga"}, function(v)
    games=v
    classified=destination.classify({v[1]}, {q=v[1].title})
    destination.set(classified)
  end)
  hardware.read(function(v) machine=v end)
end
function draw() gfx.text("Names",0,0) end
`, nil)
	r := newRoom(t, pack, Options{Services: svc, Local: &fakeLocalCores{cores: []localcores.Core{{CoreID: "fes.pong", PackageID: pkg, Name: "Pong HIL790 test", Launchable: true}}}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.CheckGlobal(`cores ~= nil and games ~= nil and machine ~= nil`) == nil })
	for _, check := range []string{
		`cores[1].name == "Pong" and cores[1].core_id == "fes.pong" and cores[1].package_id == "` + pkg + `"`,
		`games[1].title == "Sinclair ZX81" and games[1].id == "diagnostic-zx81" and games[1].package_id == "` + pkg + `" and games[1].media_id == "` + media + `"`,
		`games[2].title == "My ZX81 HIL790"`,
		`classified.label == "Sinclair ZX81" and classified.game_id == "diagnostic-zx81"`,
		`machine.machines[1].title == "Sinclair ZX81" and machine.machines[1].game_id == "diagnostic-zx81" and machine.machines[1].media_id == "` + media + `"`,
		`machine.computers[1].title == "ColecoVision" and machine.computers[1].core_id == "fes.coleco"`,
	} {
		if err := r.CheckGlobal(check); err != nil {
			t.Fatal(err)
		}
	}
	if cached, ok := r.CachedGame(generated.ID); !ok || cached.Title != generated.Title || cached.MediaID != media {
		t.Fatalf("presentation changed cached source metadata: %+v", cached)
	}
	if d := r.Destination(); d.Label != "Sinclair ZX81" || d.GameID != generated.ID || d.Matches[0].MediaID != media {
		t.Fatalf("classification republished the raw diagnostic name or changed media: %+v", d)
	}
	choice := ApplyEditionPreference(Destination{Kind: KindGame, Availability: AvailNeedsChoice, Matches: []hostclient.Game{generated, custom}}, generated.ID)
	if choice.Label != "Sinclair ZX81" || choice.GameID != generated.ID || choice.Matches[0].Title != generated.Title || choice.Matches[0].MediaID != media {
		t.Fatalf("saved edition lost its display name or raw identity: %+v", choice)
	}
}

func TestSystemsUseCanonicalNameOnceAndKeepCustomTitle(t *testing.T) {
	generated := preparedComputer("generated-coleco", "Coleco HIL 1120 123509", "fes.coleco")
	custom := preparedComputer("custom-zx81", "My ZX81 HIL790", "fes.zx81")
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: &fakeServices{games: []hostclient.Game{generated, custom}}, Index: NewIndex(Examples())})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "ColecoVision") && frameHas(f, custom.Title) })
	text := frameText(f)
	if strings.Contains(text, "ColecoVision  ·  ColecoVision") || !strings.Contains(text, "Sinclair ZX81  ·  "+custom.Title) {
		t.Fatalf("machine/title naming is duplicated or loses the custom title: %s", text)
	}
}

func TestPreparedSetupTitleKeepsCustomTitleAndMediaBinding(t *testing.T) {
	game := preparedComputer("custom-title", "My ZX81 HIL790", "fes.zx81")
	game.PackageID, game.MediaID = strings.Repeat("a", 64), strings.Repeat("b", 64)
	game.MediaRole = "blob"
	r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{game}}, game.ID, game.CoreID)
	f := stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) && frameHas(f, "Start machine") })
	if !frameHas(f, "Sinclair ZX81 setup") {
		t.Fatalf("setup header did not use its model: %s", frameText(f))
	}
	r.Activate("media")
	actions := r.TakeActions()
	if len(actions) != 1 || actions[0].GameID != game.ID || actions[0].PackageID != game.PackageID || actions[0].ExpectedMediaID != game.MediaID || actions[0].CoreID != game.CoreID {
		t.Fatalf("title projection changed the selected medium: %+v", actions)
	}
}
