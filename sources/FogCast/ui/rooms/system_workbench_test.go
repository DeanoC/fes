package rooms

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func loadSystemWorkbench(t *testing.T, svc Services, gameID, coreID string) *Instance {
	t.Helper()
	if _, ok := svc.(HardwareServices); !ok {
		svc = c64ServicesWithEmptyHardware{Services: svc}
	}
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services: svc, Width: 1280, Height: 720, InitialGameID: gameID, InitialCoreID: coreID,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSystemWorkbenchEmptyLibrarySetsUpEveryPlayableModel(t *testing.T) {
	r := loadSystemWorkbench(t, &fakeServices{}, "", "")
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
	families := [][]string{
		{"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500"},
		{"fes.c64"},
		{"fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3", "fes.pentagon"},
		{"fes.apple2"}, {"fes.atari-st"}, {"fes.coleco"}, {"fes.sms"}, {"fes.sg1000"},
		{"fes.pong"}, {"fes.catch"}, {"fes.ramtest"}, {"fes.riscv"},
	}
	for _, variants := range families {
		for i, id := range variants {
			r.Activate("setup")
			acts := r.TakeActions()
			if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup || acts[0].CoreID != id {
				t.Fatalf("setup %s: %+v", id, acts)
			}
			r.Activate("start")
			if acts := r.TakeActions(); len(acts) != 0 {
				t.Fatalf("empty system %s launched %+v", id, acts)
			}
			if i+1 < len(variants) {
				r.Activate("variant")
				stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
			}
		}
		r.Activate("system")
		stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
	}
	// Returning to a family restores its last browsed variant.
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].CoreID != "fes.ts1500" {
		t.Fatalf("ZX81 family forgot its variant: %+v", acts)
	}
}

func TestSystemWorkbenchSelectedModelMediaAndEditionKeepExactEntry(t *testing.T) {
	pkg, media := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	base := preparedComputer("spectrum-title", "Spectrum title", "fes.spectrum-plus3")
	base.PackageID, base.MediaID, base.MediaRole, base.VariantCount = pkg, media, "disk", 2
	edition := base
	edition.ID, edition.Region, edition.VariantCount = "spectrum-edition", "Europe", 0
	base.Variants = []hostclient.Game{edition}
	first := preparedComputer("apple-title", "Apple title", "fes.apple2")
	r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{first, base}}, edition.ID, base.CoreID)
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Europe") && strings.Contains(frameText(f), "Media for next start")
	})
	text := frameText(frame)
	for _, want := range []string{"ZX Spectrum +3", "Disk .dsk", media[:8], "Europe"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, ".trd") || strings.Contains(text, "Apple title") {
		t.Fatalf("wrong model/media guidance\n%s", text)
	}
	r.Activate("media")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareMedia || acts[0].GameID != edition.ID || acts[0].CoreID != base.CoreID || acts[0].PackageID != pkg || acts[0].ExpectedMediaID != media {
		t.Fatalf("media binding %+v", acts)
	}
	r.Activate("expansions")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareExpansions || acts[0].GameID != edition.ID || acts[0].PackageID != pkg {
		t.Fatalf("expansion binding %+v", acts)
	}
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != edition.ID {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestSystemWorkbenchNamesSavedMediaByRole(t *testing.T) {
	for _, tc := range []struct{ core, role, label string }{
		{"fes.spectrum-plus3", "disk", "Disk .dsk"},
		{"fes.pentagon", "disk", "Disk .trd"},
		{"fes.c64", "disk", "Disk .d64"},
		{"fes.spectrum", "cassette", "Tape or snapshot"},
		{"fes.spectrum-plus3", "cassette", "Tape or snapshot"},
		{"fes.zx81", "blob", "Cassette .p"},
		{"fes.coleco", "blob", "Linked cartridge"},
	} {
		t.Run(tc.core+"/"+tc.role, func(t *testing.T) {
			game := preparedComputer("saved-media", "Saved machine", tc.core)
			game.PackageID, game.MediaID, game.MediaRole = strings.Repeat("a", 64), strings.Repeat("b", 64), tc.role
			r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{game}}, game.ID, tc.core)
			want := tc.label + "  ·  " + game.MediaID[:8]
			frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Saved machine") && frameHas(f, "Set up system") })
			if !frameHas(frame, want) {
				t.Fatalf("saved %s media lacks its own label %q:\n%s", tc.role, want, frameText(frame))
			}
		})
	}
}

func TestSystemWorkbenchKeepsRunningMachineForLiveMediaAndResume(t *testing.T) {
	game := preparedComputer("c64-game", "C64 game", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadSystemWorkbench(t, host, game.ID, game.CoreID)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Stop machine") })
	r.Activate("disk")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != "c64-session" || acts[0].MediaBinding.Generation != 7 {
		t.Fatalf("live picker binding %+v", acts)
	}
	r.Activate("resume")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionResumeSession {
		t.Fatalf("resume %+v", acts)
	}
	r.Activate("setup")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("active machine offered next setup %+v", acts)
	}
}

func TestSystemWorkbenchResumeRefreshesAddedLibraryEntries(t *testing.T) {
	game := preparedComputer("new-title", "New title", "fes.apple2")
	svc := &fakeServices{details: map[string]hostclient.Game{}}
	r := loadSystemWorkbench(t, svc, "", game.CoreID)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "No Apple II entries yet.") })
	// A setup operation adds an entry while the workbench is covered by its
	// native picker. Resuming must reread library entries as well as hardware.
	svc.mu.Lock()
	svc.games = []hostclient.Game{game}
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), game.Title) })
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != game.ID {
		t.Fatalf("new entry launch %+v", acts)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	r.Step(time.Unix(3, 0))
}

type delayedFirstLibraryQueryServices struct {
	*fakeServices
	firstResult []hostclient.Game
	started     chan struct{}
	release     chan struct{}
	mu          sync.Mutex
	calls       int
	releaseOnce sync.Once
}

func (s *delayedFirstLibraryQueryServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, limit int) ([]hostclient.Game, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		close(s.started)
		select {
		case <-s.release:
			return append([]hostclient.Game(nil), s.firstResult...), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.fakeServices.QueryGames(ctx, q, limit)
}

func (s *delayedFirstLibraryQueryServices) releaseFirst() {
	s.releaseOnce.Do(func() { close(s.release) })
}

type delayedLibraryGameServices struct {
	*fakeServices
	blockedID   string
	detail      hostclient.Game
	started     chan struct{}
	release     chan struct{}
	returned    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (s *delayedLibraryGameServices) Game(ctx context.Context, id string) (hostclient.Game, error) {
	if id != s.blockedID {
		return s.fakeServices.Game(ctx, id)
	}
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
		close(s.returned)
		return s.detail, nil
	case <-ctx.Done():
		close(s.returned)
		return hostclient.Game{}, ctx.Err()
	}
}

func (s *delayedLibraryGameServices) releaseDetail() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func stepUntilLibrarySignal(t *testing.T, r *Instance, signal <-chan struct{}, description string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.Step(time.Now())
		select {
		case <-signal:
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("%s did not arrive", description)
}

func waitForLibraryPending(t *testing.T, r *Instance, game bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.pendingMu.Lock()
		found := false
		for _, result := range r.pending {
			if (game && result.game != nil) || (!game && result.games != nil) {
				found = true
				break
			}
		}
		r.pendingMu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("workbench library result did not reach the room")
}

func TestSystemWorkbenchIgnoresStaleLibraryQuery(t *testing.T) {
	old := preparedComputer("old-catalog-title", "Old catalog title", "fes.apple2")
	newer := preparedComputer("new-catalog-title", "New catalog title", "fes.apple2")
	svc := &delayedFirstLibraryQueryServices{
		fakeServices: &fakeServices{games: []hostclient.Game{newer}},
		firstResult:  []hostclient.Game{old},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	defer svc.releaseFirst()
	r := loadSystemWorkbench(t, svc, "", "fes.apple2")
	stepUntilLibrarySignal(t, r, svc.started, "initial library query")
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, newer.Title) })
	svc.releaseFirst()
	waitForLibraryPending(t, r, false)
	frame := r.Step(time.Now())
	if !frameHas(frame, newer.Title) || frameHas(frame, old.Title) {
		t.Fatalf("stale query replaced the refreshed workbench:\n%s", frameText(frame))
	}
}

func TestSystemWorkbenchIgnoresStaleEditionCallback(t *testing.T) {
	old := preparedComputer("old-grouped-title", "Old grouped title", "fes.apple2")
	old.VariantCount = 2
	detail := old
	detail.Title = "Old grouped detail"
	newer := preparedComputer("new-edition-title", "New edition title", "fes.apple2")
	svc := &delayedLibraryGameServices{
		fakeServices: &fakeServices{games: []hostclient.Game{old}},
		blockedID:    old.ID,
		detail:       detail,
		started:      make(chan struct{}),
		release:      make(chan struct{}),
		returned:     make(chan struct{}),
	}
	defer svc.releaseDetail()
	r := loadSystemWorkbench(t, svc, "", "fes.apple2")
	stepUntilLibrarySignal(t, r, svc.started, "grouped-edition lookup")
	svc.mu.Lock()
	svc.games = []hostclient.Game{newer}
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, newer.Title) })
	svc.releaseDetail()
	select {
	case <-svc.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("grouped-edition lookup did not return")
	}
	waitForLibraryPending(t, r, true)
	frame := r.Step(time.Now())
	if !frameHas(frame, newer.Title) || frameHas(frame, old.Title) || frameHas(frame, detail.Title) {
		t.Fatalf("stale edition callback replaced the refreshed workbench:\n%s", frameText(frame))
	}
}

func TestSystemWorkbenchLibraryErrorSurvivesModelNavigation(t *testing.T) {
	old := preparedComputer("old-spectrum-title", "Old Spectrum title", "fes.spectrum")
	svc := &fakeServices{games: []hostclient.Game{old}}
	r := loadSystemWorkbench(t, svc, "", old.CoreID)
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, old.Title) })
	svc.mu.Lock()
	svc.err = context.DeadlineExceeded
	svc.mu.Unlock()
	r.Resume()
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, context.DeadlineExceeded.Error()) })
	if frameHas(frame, old.Title) {
		t.Fatalf("failed refresh kept the old catalog:\n%s", frameText(frame))
	}
	r.Activate("variant")
	frame = r.Step(time.Now())
	if !frameHas(frame, context.DeadlineExceeded.Error()) || frameHas(frame, old.Title) {
		t.Fatalf("variant navigation hid the library error:\n%s", frameText(frame))
	}
	r.Activate("system")
	frame = r.Step(time.Now())
	if !frameHas(frame, context.DeadlineExceeded.Error()) || frameHas(frame, old.Title) {
		t.Fatalf("Next system hid the library error:\n%s", frameText(frame))
	}
}

func TestSystemWorkbenchSuccessfulRefreshClearsLibraryError(t *testing.T) {
	game := preparedComputer("recovered-title", "Recovered title", "fes.apple2")
	svc := &fakeServices{games: []hostclient.Game{game}}
	r := loadSystemWorkbench(t, svc, "", game.CoreID)
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) })

	svc.mu.Lock()
	svc.err = context.DeadlineExceeded
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, context.DeadlineExceeded.Error()) })

	svc.mu.Lock()
	svc.err = nil
	svc.mu.Unlock()
	r.Resume()
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) })
	if frameHas(frame, context.DeadlineExceeded.Error()) {
		t.Fatalf("successful refresh left the previous library error visible:\n%s", frameText(frame))
	}
}

func TestSystemWorkbenchSavesPlacedSelectionOnUnload(t *testing.T) {
	game := preparedComputer("placed-setup", "Placed setup", "fes.apple2")
	path := filepath.Join(t.TempDir(), "room.json")
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services:  c64ServicesWithEmptyHardware{Services: &fakeServices{games: []hostclient.Game{game}}},
		StorePath: path, Width: 1280, Height: 720, InitialGameID: game.ID, InitialCoreID: game.CoreID,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) })
	r.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Navigation struct {
			CoreID    string `json:"core_id"`
			GameID    string `json:"game_id"`
			EditionID string `json:"edition_id"`
		} `json:"navigation"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Navigation.CoreID != game.CoreID || state.Navigation.GameID != game.ID || state.Navigation.EditionID != game.ID {
		t.Fatalf("unload did not save the placed selection: %+v", state.Navigation)
	}
}

func TestSystemWorkbenchFailedRefreshPreservesSelectionOnUnload(t *testing.T) {
	game := preparedComputer("saved-after-error", "Saved after error", "fes.apple2")
	path := filepath.Join(t.TempDir(), "room.json")
	svc := &fakeServices{games: []hostclient.Game{game}}
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services:  c64ServicesWithEmptyHardware{Services: svc},
		StorePath: path, Width: 1280, Height: 720, InitialGameID: game.ID, InitialCoreID: game.CoreID,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) })

	svc.mu.Lock()
	svc.err = context.DeadlineExceeded
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, context.DeadlineExceeded.Error()) })
	r.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Navigation struct {
			CoreID      string `json:"core_id"`
			GameID      string `json:"game_id"`
			EditionID   string `json:"edition_id"`
			CoreChoices map[string]struct {
				GameID    string `json:"game_id"`
				EditionID string `json:"edition_id"`
			} `json:"core_choices"`
		} `json:"navigation"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	choice := state.Navigation.CoreChoices[game.CoreID]
	if state.Navigation.CoreID != game.CoreID || state.Navigation.GameID != game.ID || state.Navigation.EditionID != game.ID || choice.GameID != game.ID || choice.EditionID != game.ID {
		t.Fatalf("failed refresh erased saved selection: navigation=%+v core choice=%+v", state.Navigation, choice)
	}
}

func TestSystemWorkbenchUnknownCoreDoesNotImpersonateZX81(t *testing.T) {
	const unknown = "fes.future-system"
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services: c64ServicesWithEmptyHardware{Services: &fakeServices{}},
		Width:    1280, Height: 720, InitialCoreID: unknown,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, unknown) })
	if frameHas(frame, "ZX81") {
		t.Fatalf("unknown core used the ZX81 setup:\n%s", frameText(frame))
	}
	r.Activate("setup")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("unknown core offered setup %+v", acts)
	}
}

func TestSystemsRoomRoutesOtherFamiliesToExactModelWorkbench(t *testing.T) {
	game := preparedComputer("st-title", "Atari ST title", "fes.atari-st")
	r := newRoom(t, examplePack(t, "example.computers"), Options{
		Services: &fakeServices{games: []hostclient.Game{game}}, Index: NewIndex(Examples()), Width: 1280, Height: 720,
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), game.Title) })
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != game.ID || acts[0].InitialCoreID != game.CoreID {
		t.Fatalf("setup route %+v", acts)
	}
}

func TestSystemWorkbenchRefreshRetainsChangedMachineAndEdition(t *testing.T) {
	for _, action := range []string{"machine", "edition"} {
		t.Run(action, func(t *testing.T) {
			first := preparedComputer("first-title", "First title", "fes.c64")
			second := preparedComputer("second-title", "Second title", "fes.c64")
			if action == "edition" {
				first.VariantCount = 2
				first.Variants = []hostclient.Game{second}
			}
			r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{first, second}}, first.ID, first.CoreID)
			stepUntil(t, r, func(f Frame) bool {
				return strings.Contains(frameText(f), first.Title) && strings.Contains(frameText(f), "Start machine")
			})
			r.Activate(action)
			stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), second.Title) })
			r.Resume()
			// Wait for the refreshed library reply before drawing: the old
			// frame still contains the second title while that reply is in flight.
			deadline := time.Now().Add(3 * time.Second)
			for {
				r.pendingMu.Lock()
				ready := false
				for _, result := range r.pending {
					ready = ready || result.games != nil
				}
				r.pendingMu.Unlock()
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("library refresh did not complete")
				}
				time.Sleep(time.Millisecond)
			}
			frame := stepUntil(t, r, func(f Frame) bool {
				return strings.Contains(frameText(f), second.Title) && strings.Contains(frameText(f), "Start machine")
			})
			if strings.Contains(frameText(frame), "no longer available") {
				t.Fatalf("changed selection was treated as missing: %s", frameText(frame))
			}
			r.Activate("start")
			acts := r.TakeActions()
			if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != second.ID {
				t.Fatalf("refreshed selection %+v", acts)
			}
		})
	}
}
