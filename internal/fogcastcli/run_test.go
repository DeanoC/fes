package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeService struct {
	scanReport   catalog.ScanReport
	games        []catalog.Game
	health       protocol.Health
	status       protocol.Status
	launch       protocol.CachedLaunchResponse
	err          error
	closeErr     error
	searchQuery  string
	launchID     string
	closeCalls   int
	operationCtx context.Context
	progress     []fogcast.Progress
	operation    func(context.Context)
}

func (f *fakeService) Scan(ctx context.Context) (catalog.ScanReport, error) {
	f.operationCtx = ctx
	return f.scanReport, f.err
}

func (f *fakeService) Games(ctx context.Context) ([]catalog.Game, error) {
	f.operationCtx = ctx
	if f.operation != nil {
		f.operation(ctx)
	}
	return f.games, f.err
}

func (f *fakeService) Search(ctx context.Context, query string) ([]catalog.Game, error) {
	f.operationCtx = ctx
	f.searchQuery = query
	return f.games, f.err
}

func (f *fakeService) Launch(ctx context.Context, id string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	f.operationCtx = ctx
	f.launchID = id
	for _, update := range f.progress {
		progress(update)
	}
	return f.launch, f.err
}

func (f *fakeService) Health(ctx context.Context) (protocol.Health, error) {
	f.operationCtx = ctx
	return f.health, f.err
}

func (f *fakeService) Status(ctx context.Context) (protocol.Status, error) {
	f.operationCtx = ctx
	return f.status, f.err
}

func (f *fakeService) Stop(ctx context.Context) (protocol.Status, error) {
	f.operationCtx = ctx
	return f.status, f.err
}

func (f *fakeService) Close() error {
	f.closeCalls++
	return f.closeErr
}

func (f *fakeService) SyncFacets(ctx context.Context) (int, error) {
	f.operationCtx = ctx
	return 0, f.err
}

func TestRunAcceptsEveryCommandAndRejectsEveryWrongArityBeforeOpen(t *testing.T) {
	valid := [][]string{
		{"scan"}, {"games"}, {"search", "literal"}, {"launch", "snes-game-123456789abc"},
		{"health"}, {"status"}, {"stop"}, {"facets-sync"},
	}
	for _, args := range valid {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			service := &fakeService{health: protocol.Health{Ready: true}, status: protocol.Status{State: protocol.StateIdle}}
			exit := Run(context.Background(), args, io.Discard, io.Discard, func(context.Context, fogcast.Paths) (Service, error) {
				return service, nil
			})
			if exit != 0 || service.closeCalls != 1 {
				t.Fatalf("args=%q exit=%d close calls=%d", args, exit, service.closeCalls)
			}
		})
	}

	invalid := [][]string{
		nil, {"unknown"}, {"scan", "extra"}, {"games", "extra"}, {"search"}, {"search", "one", "two"},
		{"launch"}, {"launch", "one", "two"}, {"health", "extra"}, {"status", "extra"}, {"stop", "extra"},
	}
	for index, args := range invalid {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			opened := false
			var stderr bytes.Buffer
			exit := Run(context.Background(), args, io.Discard, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
				opened = true
				return &fakeService{}, nil
			})
			if exit != 2 || opened || stderr.String() != usageText {
				t.Fatalf("args=%q exit=%d opened=%v stderr=%q", args, exit, opened, stderr.String())
			}
		})
	}
}

func TestPublicCoreRecognizesSharedSMSCore(t *testing.T) {
	value := "SMS"
	got := publicCore(&value)
	if got == nil || *got != value {
		t.Fatalf("publicCore(SMS) = %v, want SMS", got)
	}
}

func TestRunUsesDefaultPathsAndOverridesOnlyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wantDefault := fogcast.Paths{
		Config:          filepath.Join(home, ".config", "fogcast", "config.toml"),
		Index:           filepath.Join(home, ".local", "share", "fogcast", "library.sqlite3"),
		Staging:         filepath.Join(home, ".cache", "fogcast", "staging"),
		MetadataRoot:    filepath.Join(home, ".cache", "fogcast", "metadata"),
		UserLibrary:     filepath.Join(home, ".local", "share", "fogcast", "library-user.sqlite3"),
		LibrarySettings: filepath.Join(home, ".local", "share", "fogcast", "library-settings.json"),
		MediaIndex:      filepath.Join(home, ".local", "share", "fogcast", "library-media.sqlite3"),
		MediaCache:      filepath.Join(home, ".cache", "fogcast", "library-media"),
	}

	for _, test := range []struct {
		name string
		args []string
		want fogcast.Paths
	}{
		{name: "default", args: []string{"games"}, want: wantDefault},
		{name: "explicit config", args: []string{"--config", "relative/private.toml", "games"}, want: fogcast.Paths{Config: "relative/private.toml", Index: wantDefault.Index, Staging: wantDefault.Staging, MetadataRoot: wantDefault.MetadataRoot, UserLibrary: wantDefault.UserLibrary, LibrarySettings: wantDefault.LibrarySettings, MediaIndex: wantDefault.MediaIndex, MediaCache: wantDefault.MediaCache}},
		{name: "explicit empty config", args: []string{"--config", "", "games"}, want: fogcast.Paths{Config: "", Index: wantDefault.Index, Staging: wantDefault.Staging, MetadataRoot: wantDefault.MetadataRoot, UserLibrary: wantDefault.UserLibrary, LibrarySettings: wantDefault.LibrarySettings, MediaIndex: wantDefault.MediaIndex, MediaCache: wantDefault.MediaCache}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var opened fogcast.Paths
			exit := Run(context.Background(), test.args, io.Discard, io.Discard, func(_ context.Context, paths fogcast.Paths) (Service, error) {
				opened = paths
				return &fakeService{}, nil
			})
			if exit != 0 || opened != test.want {
				t.Fatalf("exit=%d paths=%+v want=%+v", exit, opened, test.want)
			}
		})
	}
}

func TestRunScanHumanAndJSONOutputExposeCountersNotPrivatePaths(t *testing.T) {
	report := catalog.ScanReport{Roots: []catalog.RootReport{
		{RootID: "genesis-main", System: protocol.SystemMegaDrive, Added: 1, Updated: 2, Unchanged: 3, Invalid: 4, Missing: 5},
		{RootID: "snes-main", System: protocol.SystemSNES, Added: 6, Updated: 7, Unchanged: 8, Invalid: 9, Missing: 10, Offline: true, Reason: "/Volumes/private/token-secret"},
	}}

	var humanOut, humanErr bytes.Buffer
	humanService := &fakeService{scanReport: report}
	if exit := Run(context.Background(), []string{"scan"}, &humanOut, &humanErr, openFake(humanService)); exit != 0 {
		t.Fatalf("human exit=%d stderr=%q", exit, humanErr.String())
	}
	wantHuman := "genesis-main  Mega Drive  added=1  updated=2  unchanged=3  invalid=4  missing=5   online\n" +
		"snes-main     SNES        added=6  updated=7  unchanged=8  invalid=9  missing=10  offline\n"
	if humanOut.String() != wantHuman || humanErr.Len() != 0 {
		t.Fatalf("human stdout=%q stderr=%q", humanOut.String(), humanErr.String())
	}

	var jsonOut, jsonErr bytes.Buffer
	jsonService := &fakeService{scanReport: report}
	if exit := Run(context.Background(), []string{"--json", "scan"}, &jsonOut, &jsonErr, openFake(jsonService)); exit != 0 {
		t.Fatalf("JSON exit=%d stderr=%q", exit, jsonErr.String())
	}
	wantJSON := "{\"roots\":[{\"root_id\":\"genesis-main\",\"system\":\"megadrive\",\"added\":1,\"updated\":2,\"unchanged\":3,\"invalid\":4,\"missing\":5,\"offline\":false},{\"root_id\":\"snes-main\",\"system\":\"snes\",\"added\":6,\"updated\":7,\"unchanged\":8,\"invalid\":9,\"missing\":10,\"offline\":true}]}\n"
	if jsonOut.String() != wantJSON || jsonErr.Len() != 0 {
		t.Fatalf("JSON stdout=%q stderr=%q", jsonOut.String(), jsonErr.String())
	}
	assertOneJSONValue(t, jsonOut.Bytes())
	assertPrivateAbsent(t, jsonOut.String()+jsonErr.String()+humanOut.String()+humanErr.String())
}

func TestRunGamesAndSearchUseDeterministicCuratedResults(t *testing.T) {
	digest := strings.Repeat("a", 64)
	privateGame := catalog.Game{
		ID: "snes-zulu-123456789abc", Title: "Zulu", LibraryID: "private-library", RelativePath: "private/token.sfc",
		System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateMissing, RootOnline: false,
		Reason: "/Volumes/private/token-secret", Fingerprint: catalog.Fingerprint{SourceSize: 987654},
		Content: &catalog.Content{SHA256: digest, Size: 1234, Extension: "sfc"},
	}
	alpha := catalog.Game{ID: "megadrive-alpha-123456789abc", Title: "Alpha", LibraryID: "genesis-main", System: protocol.SystemMegaDrive, State: catalog.SourceStateAvailable, RootOnline: true}
	zuluTie := catalog.Game{ID: "snes-zulu-000000000000", Title: "Zulu", LibraryID: "snes-main", System: protocol.SystemSNES, State: catalog.SourceStateAvailable, RootOnline: true}
	unsorted := []catalog.Game{privateGame, zuluTie, alpha}

	for _, command := range []struct {
		name  string
		args  []string
		query string
	}{
		{name: "games", args: []string{"games"}},
		{name: "search literal", args: []string{"search", `_%\\Literal[query]`}, query: `_%\\Literal[query]`},
	} {
		t.Run(command.name+" human", func(t *testing.T) {
			service := &fakeService{games: unsorted}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), command.args, &stdout, &stderr, openFake(service))
			want := "megadrive-alpha-123456789abc  Mega Drive  available  Alpha\n" +
				"snes-zulu-000000000000        SNES        available  Zulu\n" +
				"snes-zulu-123456789abc        SNES        missing    Zulu\n"
			if exit != 0 || stdout.String() != want || stderr.Len() != 0 || service.searchQuery != command.query {
				t.Fatalf("exit=%d stdout=%q stderr=%q query=%q", exit, stdout.String(), stderr.String(), service.searchQuery)
			}
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
		t.Run(command.name+" JSON", func(t *testing.T) {
			service := &fakeService{games: unsorted}
			var stdout, stderr bytes.Buffer
			args := append([]string{"--json"}, command.args...)
			exit := Run(context.Background(), args, &stdout, &stderr, openFake(service))
			want := "{\"games\":[{\"id\":\"megadrive-alpha-123456789abc\",\"title\":\"Alpha\",\"system\":\"megadrive\",\"library_id\":\"genesis-main\",\"state\":\"available\",\"root_online\":true,\"content_cached\":false},{\"id\":\"snes-zulu-000000000000\",\"title\":\"Zulu\",\"system\":\"snes\",\"library_id\":\"snes-main\",\"state\":\"available\",\"root_online\":true,\"content_cached\":false},{\"id\":\"snes-zulu-123456789abc\",\"title\":\"Zulu\",\"system\":\"snes\",\"library_id\":\"private-library\",\"state\":\"missing\",\"root_online\":false,\"content_cached\":true}]}\n"
			if exit != 0 || stdout.String() != want || stderr.Len() != 0 || service.searchQuery != command.query {
				t.Fatalf("exit=%d stdout=%q stderr=%q query=%q", exit, stdout.String(), stderr.String(), service.searchQuery)
			}
			assertOneJSONValue(t, stdout.Bytes())
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
	}
}

func TestRunControlCommandsAndHealthExitStatus(t *testing.T) {
	gameID := "snes-game-123456789abc"
	system := protocol.SystemSNES
	core := "SNES"
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core}
	tests := []struct {
		name       string
		args       []string
		service    *fakeService
		wantExit   int
		wantHuman  string
		wantJSON   string
		wantLaunch string
	}{
		{name: "health ready", args: []string{"health"}, service: &fakeService{health: protocol.Health{APIVersion: "v2", AgentVersion: "test", Ready: true, MiSTerProcess: true, CommandPipe: true}}, wantHuman: "ready\n", wantJSON: "{\"api_version\":\"v2\",\"agent_version\":\"test\",\"ready\":true,\"mister_process\":true,\"command_pipe\":true}\n"},
		{name: "health not ready", args: []string{"health"}, service: &fakeService{health: protocol.Health{Ready: false}}, wantExit: 1, wantHuman: "not ready\n", wantJSON: "{\"api_version\":\"\",\"agent_version\":\"\",\"ready\":false,\"mister_process\":false,\"command_pipe\":false}\n"},
		{name: "status", args: []string{"status"}, service: &fakeService{status: active}, wantHuman: "active: snes-game-123456789abc (SNES)\n", wantJSON: "{\"state\":\"active\",\"game_id\":\"snes-game-123456789abc\",\"system\":\"snes\",\"core\":\"SNES\"}\n"},
		{name: "stop", args: []string{"stop"}, service: &fakeService{status: protocol.Status{State: protocol.StateIdle}}, wantHuman: "idle\n", wantJSON: "{\"state\":\"idle\"}\n"},
	}
	for _, test := range tests {
		t.Run(test.name+" human", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), test.args, &stdout, &stderr, openFake(test.service))
			if exit != test.wantExit || stdout.String() != test.wantHuman || stderr.Len() != 0 || test.service.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), test.service.closeCalls)
			}
		})
		t.Run(test.name+" JSON", func(t *testing.T) {
			copyService := *test.service
			copyService.closeCalls = 0
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), append([]string{"--json"}, test.args...), &stdout, &stderr, openFake(&copyService))
			want := test.wantJSON
			if want == "" {
				encoded, err := json.Marshal(copyService.status)
				if err != nil {
					t.Fatal(err)
				}
				want = string(encoded) + "\n"
			}
			if exit != test.wantExit || stdout.String() != want || stderr.Len() != 0 || copyService.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), copyService.closeCalls)
			}
			assertOneJSONValue(t, stdout.Bytes())
		})
	}
}

func TestRunSanitizesSuccessfulStatusAndStopOutput(t *testing.T) {
	private := "/Volumes/private/token-secret"
	oversizedGameID := strings.Repeat("a", 4096)
	privateSystem := protocol.System(private)
	privateCore := private + strings.Repeat("x", 4096)
	privateCode := protocol.ErrorCode(private)
	hostile := protocol.Status{
		State:        protocol.StateActive,
		GameID:       &oversizedGameID,
		System:       &privateSystem,
		ExpectedCore: &privateCore,
		ObservedCore: &privateCore,
		LastError:    &protocol.APIError{Code: privateCode, Message: private},
	}
	gameID := "snes-game-123456789abc"
	system := protocol.SystemSNES
	expected, observed := "SNES", "MENU"
	failed := protocol.Status{
		State: protocol.StateFailed, GameID: &gameID, System: &system,
		ExpectedCore: &expected, ObservedCore: &observed,
		LastError: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: private},
	}

	for _, command := range []string{"status", "stop"} {
		for _, test := range []struct {
			name      string
			status    protocol.Status
			wantHuman string
			wantJSON  string
		}{
			{name: "hostile optional fields", status: hostile, wantHuman: "active\n", wantJSON: "{\"state\":\"active\"}\n"},
			{name: "canonical failed fields", status: failed, wantHuman: "failed: snes-game-123456789abc (MENU) error=CORE_TIMEOUT\n", wantJSON: "{\"state\":\"failed\",\"game_id\":\"snes-game-123456789abc\",\"system\":\"snes\",\"core\":\"MENU\",\"error\":{\"code\":\"CORE_TIMEOUT\",\"message\":\"core transition timed out\"}}\n"},
		} {
			t.Run(command+"_"+test.name+"_human", func(t *testing.T) {
				service := &fakeService{status: test.status}
				var stdout, stderr bytes.Buffer
				exit := Run(context.Background(), []string{command}, &stdout, &stderr, openFake(service))
				if exit != 0 || stdout.String() != test.wantHuman || stderr.Len() != 0 || service.closeCalls != 1 {
					t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
				}
				if strings.Contains(stdout.String()+stderr.String(), private) || strings.Contains(stdout.String(), oversizedGameID) {
					t.Fatalf("output leaked hostile status: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			})
			t.Run(command+"_"+test.name+"_JSON", func(t *testing.T) {
				service := &fakeService{status: test.status}
				var stdout, stderr bytes.Buffer
				exit := Run(context.Background(), []string{"--json", command}, &stdout, &stderr, openFake(service))
				if exit != 0 || stdout.String() != test.wantJSON || stderr.Len() != 0 || service.closeCalls != 1 {
					t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
				}
				assertOneJSONValue(t, stdout.Bytes())
				if strings.Contains(stdout.String()+stderr.String(), private) || strings.Contains(stdout.String(), oversizedGameID) {
					t.Fatalf("output leaked hostile status: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestRunRejectsUnknownSuccessfulStatusStateWithoutReflection(t *testing.T) {
	private := protocol.State("/Volumes/private/token-secret")
	for _, command := range []string{"status", "stop"} {
		service := &fakeService{status: protocol.Status{State: private}}
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{command}, &stdout, &stderr, openFake(service))
		if exit != 1 || stdout.Len() != 0 || stderr.String() != "INTERNAL: FogCast operation failed internally\n" || service.closeCalls != 1 {
			t.Fatalf("command=%s exit=%d stdout=%q stderr=%q close=%d", command, exit, stdout.String(), stderr.String(), service.closeCalls)
		}
		assertPrivateAbsent(t, stdout.String()+stderr.String())
	}
}

func TestRunLaunchSeparatesHumanAndJSONProgress(t *testing.T) {
	gameID := "snes-game-123456789abc"
	system := protocol.SystemSNES
	core := "SNES"
	response := protocol.CachedLaunchResponse{
		Status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core},
		Content: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 1024, Extension: "sfc"},
	}
	progress := []fogcast.Progress{{Stage: "cache", Message: "checking target cache"}, {Stage: "launch", Message: "launching cached content"}}

	var humanOut, humanErr bytes.Buffer
	humanService := &fakeService{launch: response, progress: progress}
	if exit := Run(context.Background(), []string{"launch", gameID}, &humanOut, &humanErr, openFake(humanService)); exit != 0 {
		t.Fatalf("human exit=%d stderr=%q", exit, humanErr.String())
	}
	wantHuman := "cache: checking target cache\nlaunch: launching cached content\nactive: snes-game-123456789abc (SNES)\n"
	if humanOut.String() != wantHuman || humanErr.Len() != 0 || humanService.launchID != gameID {
		t.Fatalf("stdout=%q stderr=%q launch=%q", humanOut.String(), humanErr.String(), humanService.launchID)
	}

	var jsonOut, jsonErr bytes.Buffer
	jsonService := &fakeService{launch: response, progress: progress}
	if exit := Run(context.Background(), []string{"--json", "launch", gameID}, &jsonOut, &jsonErr, openFake(jsonService)); exit != 0 {
		t.Fatalf("JSON exit=%d stderr=%q", exit, jsonErr.String())
	}
	wantProgress := "{\"stage\":\"cache\",\"message\":\"checking target cache\"}\n{\"stage\":\"launch\",\"message\":\"launching cached content\"}\n"
	if jsonErr.String() != wantProgress {
		t.Fatalf("progress=%q", jsonErr.String())
	}
	assertOneJSONValue(t, jsonOut.Bytes())
	var decoded protocol.CachedLaunchResponse
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil || decoded.Content != response.Content || decoded.Status.State != protocol.StateActive {
		t.Fatalf("JSON=%q decoded=%+v error=%v", jsonOut.String(), decoded, err)
	}
}

func TestRunEveryCommandFailureIsTypedPrivateAndClosed(t *testing.T) {
	private := "/Volumes/private/token-secret"
	operationErr := errors.Join(&protocol.APIError{Code: protocol.CodeSourceUnavailable, Message: private}, errors.New(private))
	progress := []fogcast.Progress{{Stage: "cache", Message: "checking target cache"}}
	commands := []struct {
		name string
		args []string
	}{
		{name: "scan", args: []string{"scan"}},
		{name: "games", args: []string{"games"}},
		{name: "search", args: []string{"search", "literal"}},
		{name: "launch", args: []string{"launch", "snes-game-123456789abc"}},
		{name: "health", args: []string{"health"}},
		{name: "status", args: []string{"status"}},
		{name: "stop", args: []string{"stop"}},
	}
	for _, command := range commands {
		t.Run(command.name+" human", func(t *testing.T) {
			service := &fakeService{err: operationErr, progress: progress}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), command.args, &stdout, &stderr, openFake(service))
			wantStdout := ""
			if command.name == "launch" {
				wantStdout = "cache: checking target cache\n"
			}
			if exit != 1 || stdout.String() != wantStdout || stderr.String() != "SOURCE_UNAVAILABLE: game source is unavailable\n" || service.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
			}
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
		t.Run(command.name+" JSON", func(t *testing.T) {
			service := &fakeService{err: operationErr, progress: progress}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), append([]string{"--json"}, command.args...), &stdout, &stderr, openFake(service))
			wantStderr := ""
			if command.name == "launch" {
				wantStderr = "{\"stage\":\"cache\",\"message\":\"checking target cache\"}\n"
			}
			wantStdout := "{\"error\":{\"code\":\"SOURCE_UNAVAILABLE\",\"message\":\"game source is unavailable\"}}\n"
			if exit != 1 || stdout.String() != wantStdout || stderr.String() != wantStderr || service.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
			}
			assertOneJSONValue(t, stdout.Bytes())
			if command.name == "launch" {
				assertOneJSONValue(t, stderr.Bytes())
			}
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
	}
}

func TestRunReturnsConciseTypedErrorsWithoutPrivateDetailsAndStillCloses(t *testing.T) {
	private := "/Volumes/private/token-secret"
	typed := errors.Join(&protocol.APIError{Code: protocol.CodeSourceUnavailable, Message: private}, errors.New(private))
	for _, test := range []struct {
		name      string
		err       error
		wantHuman string
		wantJSON  string
	}{
		{name: "typed", err: typed, wantHuman: "SOURCE_UNAVAILABLE: game source is unavailable\n", wantJSON: "{\"error\":{\"code\":\"SOURCE_UNAVAILABLE\",\"message\":\"game source is unavailable\"}}\n"},
		{name: "hostile code", err: &protocol.APIError{Code: protocol.ErrorCode(private), Message: private}, wantHuman: "INTERNAL: FogCast operation failed internally\n", wantJSON: "{\"error\":{\"code\":\"INTERNAL\",\"message\":\"FogCast operation failed internally\"}}\n"},
		{name: "unknown", err: errors.New(private), wantHuman: "INTERNAL: FogCast operation failed internally\n", wantJSON: "{\"error\":{\"code\":\"INTERNAL\",\"message\":\"FogCast operation failed internally\"}}\n"},
		{name: "canceled", err: context.Canceled, wantHuman: "CANCELED: FogCast operation was canceled\n", wantJSON: "{\"error\":{\"code\":\"CANCELED\",\"message\":\"FogCast operation was canceled\"}}\n"},
	} {
		t.Run(test.name+" human", func(t *testing.T) {
			service := &fakeService{err: test.err}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), []string{"status"}, &stdout, &stderr, openFake(service))
			if exit != 1 || stdout.Len() != 0 || stderr.String() != test.wantHuman || service.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
			}
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
		t.Run(test.name+" JSON", func(t *testing.T) {
			service := &fakeService{err: test.err}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), []string{"--json", "status"}, &stdout, &stderr, openFake(service))
			if exit != 1 || stdout.String() != test.wantJSON || stderr.Len() != 0 || service.closeCalls != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
			}
			assertOneJSONValue(t, stdout.Bytes())
			assertPrivateAbsent(t, stdout.String()+stderr.String())
		})
	}
}

func TestRunContextCausesWinOverJoinedAPIErrors(t *testing.T) {
	private := "/Volumes/private/token-secret"
	commands := [][]string{
		{"health"},
		{"status"},
		{"stop"},
		{"launch", "snes-game-123456789abc"},
	}
	causes := []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: "CANCELED: FogCast operation was canceled\n"},
		{name: "deadline", err: context.DeadlineExceeded, want: "DEADLINE_EXCEEDED: FogCast operation timed out\n"},
	}
	for _, args := range commands {
		for _, cause := range causes {
			t.Run(args[0]+"_"+cause.name, func(t *testing.T) {
				joined := errors.Join(&protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: private}, cause.err)
				service := &fakeService{err: joined}
				var stdout, stderr bytes.Buffer
				exit := Run(context.Background(), args, &stdout, &stderr, openFake(service))
				if exit != 1 || stdout.Len() != 0 || stderr.String() != cause.want || service.closeCalls != 1 {
					t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
				}
				assertPrivateAbsent(t, stdout.String()+stderr.String())
			})
		}
	}
}

func TestRunReportsOpenAndCloseFailuresSafely(t *testing.T) {
	private := "/Volumes/private/token-secret"
	t.Run("open", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"games"}, &stdout, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
			return nil, errors.New(private)
		})
		if exit != 1 || stdout.Len() != 0 || stderr.String() != "INTERNAL: FogCast operation failed internally\n" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
		assertPrivateAbsent(t, stderr.String())
	})
	t.Run("close replaces success", func(t *testing.T) {
		service := &fakeService{closeErr: errors.New(private)}
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"games"}, &stdout, &stderr, openFake(service))
		if exit != 1 || stdout.Len() != 0 || stderr.String() != "INTERNAL: FogCast operation failed internally\n" || service.closeCalls != 1 {
			t.Fatalf("exit=%d stdout=%q stderr=%q close=%d", exit, stdout.String(), stderr.String(), service.closeCalls)
		}
		assertPrivateAbsent(t, stderr.String())
	})
}

func TestRunHandlesCanceledContextBeforeAndAfterOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	var stderr bytes.Buffer
	exit := Run(ctx, []string{"games"}, io.Discard, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
		opened = true
		return &fakeService{}, nil
	})
	if exit != 1 || opened || stderr.String() != "CANCELED: FogCast operation was canceled\n" {
		t.Fatalf("exit=%d opened=%v stderr=%q", exit, opened, stderr.String())
	}

	service := &fakeService{err: context.Canceled}
	stderr.Reset()
	exit = Run(context.Background(), []string{"scan"}, io.Discard, &stderr, openFake(service))
	if exit != 1 || service.operationCtx == nil || service.closeCalls != 1 || stderr.String() != "CANCELED: FogCast operation was canceled\n" {
		t.Fatalf("exit=%d context=%v close=%d stderr=%q", exit, service.operationCtx, service.closeCalls, stderr.String())
	}

	ctx, cancel = context.WithCancel(context.Background())
	service = &fakeService{operation: func(context.Context) { cancel() }}
	stderr.Reset()
	exit = Run(ctx, []string{"games"}, io.Discard, &stderr, openFake(service))
	if exit != 1 || service.closeCalls != 1 || stderr.String() != "CANCELED: FogCast operation was canceled\n" {
		t.Fatalf("success after cancellation: exit=%d close=%d stderr=%q", exit, service.closeCalls, stderr.String())
	}
}

type failWriter struct {
	err error
}

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunTreatsFinalAndProgressWriterFailuresAsOperationsAndCloses(t *testing.T) {
	privateErr := errors.New("write /Volumes/private/token-secret")
	t.Run("human final", func(t *testing.T) {
		service := &fakeService{}
		var stderr bytes.Buffer
		exit := Run(context.Background(), []string{"games"}, failWriter{privateErr}, &stderr, openFake(service))
		if exit != 1 || stderr.String() != "INTERNAL: FogCast operation failed internally\n" || service.closeCalls != 1 {
			t.Fatalf("exit=%d stderr=%q close=%d", exit, stderr.String(), service.closeCalls)
		}
		assertPrivateAbsent(t, stderr.String())
	})
	t.Run("JSON final", func(t *testing.T) {
		service := &fakeService{}
		var stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "games"}, failWriter{privateErr}, &stderr, openFake(service))
		if exit != 1 || stderr.String() != "INTERNAL: FogCast operation failed internally\n" || service.closeCalls != 1 {
			t.Fatalf("exit=%d stderr=%q close=%d", exit, stderr.String(), service.closeCalls)
		}
		assertPrivateAbsent(t, stderr.String())
	})
	t.Run("JSON progress", func(t *testing.T) {
		gameID := "snes-game-123456789abc"
		service := &fakeService{progress: []fogcast.Progress{{Stage: "cache", Message: "checking target cache"}}}
		var stdout bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "launch", gameID}, &stdout, failWriter{privateErr}, openFake(service))
		want := "{\"error\":{\"code\":\"INTERNAL\",\"message\":\"FogCast operation failed internally\"}}\n"
		if exit != 1 || stdout.String() != want || service.closeCalls != 1 {
			t.Fatalf("exit=%d stdout=%q close=%d", exit, stdout.String(), service.closeCalls)
		}
		assertPrivateAbsent(t, stdout.String())
	})
}

func TestRunHelpIsSuccessfulAndDoesNotOpenService(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		var stdout, stderr bytes.Buffer
		opened := false
		exit := Run(context.Background(), []string{arg}, &stdout, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
			opened = true
			return &fakeService{}, nil
		})
		if exit != 0 || opened || stdout.String() != usageText || stderr.Len() != 0 {
			t.Fatalf("arg=%q exit=%d opened=%v stdout=%q stderr=%q", arg, exit, opened, stdout.String(), stderr.String())
		}
	}
}

func TestRunVersionIsObservableWithoutOpeningService(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{name: "human", args: []string{"--version"}, wantStdout: "fogcast version=dev revision=unknown\n"},
		{name: "JSON", args: []string{"--json", "--version"}, wantStdout: "{\"version\":\"dev\",\"revision\":\"unknown\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), test.args, &stdout, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
				opened = true
				return &fakeService{}, nil
			})
			if exit != 0 || opened || stdout.String() != test.wantStdout || stderr.Len() != 0 {
				t.Fatalf("exit=%d opened=%v stdout=%q stderr=%q", exit, opened, stdout.String(), stderr.String())
			}
			if test.name == "JSON" {
				assertOneJSONValue(t, stdout.Bytes())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--version", "games"}, &stdout, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
		return &fakeService{}, nil
	})
	if exit != 2 || stdout.Len() != 0 || stderr.String() != usageText {
		t.Fatalf("version with command: exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(usageText, "fogcast --version") {
		t.Fatalf("help does not advertise version: %q", usageText)
	}
}

func openFake(service *fakeService) OpenService {
	return func(context.Context, fogcast.Paths) (Service, error) { return service, nil }
}

func assertOneJSONValue(t *testing.T, data []byte) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode JSON %q: %v", data, err)
	}
	if err := decoder.Decode(&value); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON contains another value: %q error=%v", data, err)
	}
	if bytes.Count(data, []byte{'\n'}) != 1 || len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("JSON is not exactly one newline-terminated value: %q", data)
	}
}

func assertPrivateAbsent(t *testing.T, output string) {
	t.Helper()
	for _, private := range []string{"/Volumes/private", "token-secret", "private/token.sfc", strings.Repeat("a", 64), "987654"} {
		if strings.Contains(output, private) {
			t.Fatalf("output leaked private/internal value %q: %q", private, output)
		}
	}
}
