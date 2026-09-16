package hostapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/kitlauncher"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

type sharedSessionFixture struct {
	Cases []sharedSessionFixtureCase `json:"cases"`
}

type sharedSessionFixtureCase struct {
	ID         string              `json:"id"`
	HTTPStatus int                 `json:"http_status"`
	Body       json.RawMessage     `json:"body"`
	Expect     sharedSessionExpect `json:"expect"`
}

type sharedSessionExpect struct {
	Common sharedSessionCommonExpect `json:"common"`
	Go     *sharedSessionGoExpect    `json:"go"`
	Kit    sharedSessionKitExpect    `json:"kit"`
}

type sharedSessionGoExpect struct {
	Accept *bool  `json:"accept"`
	State  string `json:"state"`
	GameID string `json:"game_id"`
}

type sharedSessionCommonExpect struct {
	Accept     bool   `json:"accept"`
	Split      bool   `json:"split"`
	State      string `json:"state"`
	GameID     string `json:"game_id"`
	System     string `json:"system"`
	Execution  string `json:"execution"`
	Media      string `json:"media"`
	HTTPStatus int    `json:"http_status"`
	ErrorCode  string `json:"error_code"`
}

type sharedSessionKitExpect struct {
	SessionID   string `json:"session_id"`
	Generation  string `json:"generation"`
	Gamepad     bool   `json:"gamepad"`
	HasKeyboard bool   `json:"has_keyboard"`
}

type sharedSessionRequest struct {
	Host          string
	CallerMarker  string
	Authorization string
	TargetID      string
}

type sharedSessionFixtureServer struct {
	mu       sync.Mutex
	current  sharedSessionFixtureCase
	requests []sharedSessionRequest
	handler  http.Handler
}

func newSharedSessionFixtureServer(t *testing.T, first sharedSessionFixtureCase) (*sharedSessionFixtureServer, *httptest.Server) {
	t.Helper()
	fixture := &sharedSessionFixtureServer{current: first}
	fixture.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, sharedSessionRequest{
			Host:          r.Host,
			CallerMarker:  r.Header.Get("X-Test-Caller-Transport"),
			Authorization: r.Header.Get("Authorization"),
			TargetID:      r.Header.Get("X-FogCast-Target-ID"),
		})
		current := fixture.current
		fixture.mu.Unlock()

		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(current.HTTPStatus)
		_, _ = w.Write(current.Body)
	})
	server := httptest.NewServer(fixture.handler)
	return fixture, server
}

func (s *sharedSessionFixtureServer) setCase(value sharedSessionFixtureCase) {
	s.mu.Lock()
	s.current = value
	s.mu.Unlock()
}

func (s *sharedSessionFixtureServer) requestSnapshot() []sharedSessionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sharedSessionRequest(nil), s.requests...)
}

type sharedSessionSummary struct {
	State        string
	GameID       string
	Execution    string
	InputState   string
	InputReady   bool
	InputSession string
	Generation   uint64
	Gamepad      bool
	Interfaces   []hostclient.SessionCoreInterface
}

func summarizeTenfoot(result tenfoot.SessionResult) sharedSessionSummary {
	summary := sharedSessionSummary{
		State:     result.State,
		GameID:    result.GameID,
		Execution: result.Execution,
	}
	if result.Input != nil {
		summary.InputState = result.Input.State
		summary.InputReady = result.Input.Ready
		summary.InputSession = result.Input.SessionID
	}
	if result.CorePackage != nil {
		summary.Generation = result.CorePackage.Generation
		summary.Gamepad = result.CorePackage.Gamepad
		summary.Interfaces = append([]hostclient.SessionCoreInterface(nil), result.CorePackage.ActiveInterfaces...)
	}
	return summary
}

func summarizeKit(result kitlauncher.Session) sharedSessionSummary {
	summary := sharedSessionSummary{
		State:        result.State,
		GameID:       result.GameID,
		Execution:    result.Execution,
		InputState:   result.Input.State,
		InputReady:   result.Input.Ready,
		InputSession: result.Input.SessionID,
	}
	if result.CorePackage != nil {
		summary.Generation = result.CorePackage.Generation
		summary.Gamepad = result.CorePackage.Gamepad
		for _, contract := range result.CorePackage.ActiveInterfaces {
			summary.Interfaces = append(summary.Interfaces, hostclient.SessionCoreInterface{
				ID: contract.ID, Major: contract.Major, Minor: contract.Minor,
			})
		}
	}
	return summary
}

type callerHeaderTransport struct {
	base        http.RoundTripper
	sawDeadline bool
}

func (t *callerHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if _, ok := request.Context().Deadline(); ok {
		t.sawDeadline = true
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("X-Test-Caller-Transport", "tenfoot")
	return t.base.RoundTrip(clone)
}

func loadSharedSessionFixture(t *testing.T) sharedSessionFixture {
	t.Helper()
	path := filepath.Join("..", "..", "hostclient", "testdata", "session-contract.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared session fixture: %v", err)
	}
	var fixture sharedSessionFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode shared session fixture: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("shared session fixture has no cases")
	}
	return fixture
}

func localhostURL(t *testing.T, serverURL string) string {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	u.Host = "localhost:" + u.Port()
	return u.String()
}

func TestSessionClientsConsumeSharedContractFixture(t *testing.T) {
	// Browser parse and presentation expectations remain owned by the Node
	// fixture tests; this matrix exercises only the two public Go clients.
	fixture := loadSharedSessionFixture(t)
	serverState, server := newSharedSessionFixtureServer(t, fixture.Cases[0])
	t.Cleanup(server.Close)
	apiURL := localhostURL(t, server.URL)

	baseClient := server.Client()
	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	callerTransport := &callerHeaderTransport{base: baseTransport}
	callerClient := &http.Client{
		Transport: callerTransport,
		Timeout:   3 * time.Second,
	}
	tenfootClient := tenfoot.NewClient(apiURL, callerClient)
	const token = "tttttttttttttttttttttttttttttttt"
	const targetID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	kitClient := kitlauncher.NewClient(kitlauncher.Config{API: apiURL, Token: token, TargetID: targetID})

	for _, testCase := range fixture.Cases {
		t.Run(testCase.ID, func(t *testing.T) {
			serverState.setCase(testCase)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			tenfootResult, tenfootErr := tenfootClient.Session(ctx)
			kitResult, kitErr := kitClient.Session(ctx)
			if testCase.Expect.Common.Split && !sharedSessionGoAccepts(testCase) {
				t.Fatal("split fixture must be accepted by Go clients")
			}
			if !sharedSessionGoAccepts(testCase) {
				if tenfootErr == nil || kitErr == nil {
					t.Fatalf("fixture %s accepted by tenfoot=%v kit=%v", testCase.ID, tenfootErr, kitErr)
				}
				if testCase.Expect.Common.HTTPStatus != 0 && tenfootResult.HTTPStatus != testCase.Expect.Common.HTTPStatus {
					t.Fatalf("tenfoot HTTP status=%d, want %d", tenfootResult.HTTPStatus, testCase.Expect.Common.HTTPStatus)
				}
				if want := testCase.Expect.Common.ErrorCode; want != "" && tenfootResult.ErrorCode != want {
					t.Fatalf("tenfoot error code=%q, want %q", tenfootResult.ErrorCode, want)
				}
				return
			}
			if tenfootErr != nil || kitErr != nil {
				t.Fatalf("fixture %s errors: tenfoot=%v kit=%v", testCase.ID, tenfootErr, kitErr)
			}

			tenfootSummary := summarizeTenfoot(tenfootResult)
			kitSummary := summarizeKit(kitResult)
			if !sameSharedSessionSummary(tenfootSummary, kitSummary) {
				t.Fatalf("client summaries differ: tenfoot=%+v kit=%+v", tenfootSummary, kitSummary)
			}
			common := testCase.Expect.Common
			if common.State != "" && tenfootSummary.State != common.State {
				t.Fatalf("common state=%q, want %q", tenfootSummary.State, common.State)
			}
			if common.GameID != "" && tenfootSummary.GameID != common.GameID {
				t.Fatalf("common game=%q, want %q", tenfootSummary.GameID, common.GameID)
			}
			if common.Execution != "" && tenfootSummary.Execution != common.Execution {
				t.Fatalf("common execution=%q, want %q", tenfootSummary.Execution, common.Execution)
			}
			if common.System != "" && tenfootResult.System != common.System {
				t.Fatalf("common system=%q, want %q", tenfootResult.System, common.System)
			}
			if common.Media != "" && tenfootResult.Media != common.Media {
				t.Fatalf("common media=%q, want %q", tenfootResult.Media, common.Media)
			}
			if testCase.ID == "active" {
				kitExpect := testCase.Expect.Kit
				if tenfootSummary.InputState != "attached" || !tenfootSummary.InputReady {
					t.Fatalf("input state=%q ready=%v, want attached/true", tenfootSummary.InputState, tenfootSummary.InputReady)
				}
				if tenfootSummary.InputSession != kitExpect.SessionID || kitExpect.SessionID == "" {
					t.Fatalf("input session=%q, want %q", tenfootSummary.InputSession, kitExpect.SessionID)
				}
				wantGeneration, err := parseUint64Fixture(t, kitExpect.Generation)
				if err != nil {
					t.Fatal(err)
				}
				if tenfootSummary.Generation != wantGeneration {
					t.Fatalf("generation=%d, want %d", tenfootSummary.Generation, wantGeneration)
				}
				if tenfootSummary.Gamepad != kitExpect.Gamepad {
					t.Fatalf("gamepad=%v, want %v", tenfootSummary.Gamepad, kitExpect.Gamepad)
				}
				if got := hasKeyboardInterface(tenfootSummary.Interfaces); got != kitExpect.HasKeyboard {
					t.Fatalf("keyboard interface=%v, want %v", got, kitExpect.HasKeyboard)
				}
				wantInterfaces := []hostclient.SessionCoreInterface{
					{ID: "fes.keyboard", Major: 1, Minor: 0},
					{ID: "fes.gamepad", Major: 1, Minor: 0},
				}
				if !slices.Equal(tenfootSummary.Interfaces, wantInterfaces) {
					t.Fatalf("interfaces=%v, want %v", tenfootSummary.Interfaces, wantInterfaces)
				}
			}
			if goExpect := testCase.Expect.Go; goExpect != nil {
				if goExpect.State != "" && tenfootSummary.State != goExpect.State {
					t.Fatalf("Go state=%q, want %q", tenfootSummary.State, goExpect.State)
				}
				if goExpect.GameID != "" && tenfootSummary.GameID != goExpect.GameID {
					t.Fatalf("Go game=%q, want %q", tenfootSummary.GameID, goExpect.GameID)
				}
			}
		})
	}

	records := serverState.requestSnapshot()
	if len(records) != len(fixture.Cases)*2 {
		t.Fatalf("session requests=%d, want %d", len(records), len(fixture.Cases)*2)
	}
	for index, record := range records {
		if !strings.HasPrefix(record.Host, "localhost:") {
			t.Fatalf("request %d host=%q, want localhost loopback", index, record.Host)
		}
		if index%2 == 0 {
			if record.CallerMarker != "tenfoot" {
				t.Fatalf("request %d caller marker=%q, want tenfoot transport", index, record.CallerMarker)
			}
			if record.Authorization != "" || record.TargetID != "" {
				t.Fatalf("tenfoot request %d unexpectedly carried kit identity auth=%q target=%q", index, record.Authorization, record.TargetID)
			}
		} else {
			if record.Authorization != "Bearer "+token || record.TargetID != targetID {
				t.Fatalf("kit request %d identity auth=%q target=%q", index, record.Authorization, record.TargetID)
			}
		}
	}
	if !callerTransport.sawDeadline {
		t.Fatal("tenfoot caller transport did not receive the context deadline")
	}
}

func sharedSessionGoAccepts(testCase sharedSessionFixtureCase) bool {
	if testCase.Expect.Go != nil && testCase.Expect.Go.Accept != nil {
		return *testCase.Expect.Go.Accept
	}
	return testCase.Expect.Common.Accept
}

func parseUint64Fixture(t *testing.T, value string) (uint64, error) {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("fixture kit generation is empty")
	}
	var parsed uint64
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return 0, fmt.Errorf("parse fixture kit generation %q: %w", value, err)
	}
	return parsed, nil
}

func hasKeyboardInterface(interfaces []hostclient.SessionCoreInterface) bool {
	for _, contract := range interfaces {
		if contract.ID == "fes.keyboard" && contract.Major == 1 && contract.Minor == 0 {
			return true
		}
	}
	return false
}

func TestSessionClientsPreserveMaxUint64Generation(t *testing.T) {
	fixture := loadSharedSessionFixture(t)
	var active sharedSessionFixtureCase
	for _, testCase := range fixture.Cases {
		if testCase.ID == "active" {
			active = testCase
			break
		}
	}
	if active.ID == "" {
		t.Fatal("shared session fixture has no active case")
	}
	active.Body = replaceSessionGeneration(t, active.Body, ^uint64(0))
	active.Expect.Kit.Generation = strconv.FormatUint(^uint64(0), 10)

	_, server := newSharedSessionFixtureServer(t, active)
	t.Cleanup(server.Close)
	apiURL := localhostURL(t, server.URL)
	tenfootResult, tenfootErr := tenfoot.NewClient(apiURL, server.Client()).Session(context.Background())
	if tenfootErr != nil {
		t.Fatalf("tenfoot session: %v", tenfootErr)
	}
	kitResult, kitErr := kitlauncher.NewClient(kitlauncher.Config{API: apiURL}).Session(context.Background())
	if kitErr != nil {
		t.Fatalf("kit session: %v", kitErr)
	}
	if tenfootResult.CorePackage == nil || tenfootResult.CorePackage.Generation != ^uint64(0) {
		t.Fatalf("tenfoot generation=%v, want %d", tenfootResult.CorePackage, ^uint64(0))
	}
	if kitResult.CorePackage == nil || kitResult.CorePackage.Generation != ^uint64(0) {
		t.Fatalf("kit generation=%v, want %d", kitResult.CorePackage, ^uint64(0))
	}
}

func replaceSessionGeneration(t *testing.T, body json.RawMessage, generation uint64) json.RawMessage {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode active fixture body: %v", err)
	}
	var core map[string]json.RawMessage
	if err := json.Unmarshal(envelope["core_package"], &core); err != nil {
		t.Fatalf("decode active fixture core package: %v", err)
	}
	core["generation"] = json.RawMessage(strconv.FormatUint(generation, 10))
	encodedCore, err := json.Marshal(core)
	if err != nil {
		t.Fatalf("encode active fixture core package: %v", err)
	}
	envelope["core_package"] = encodedCore
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode active fixture body: %v", err)
	}
	return encoded
}

func sameSharedSessionSummary(left, right sharedSessionSummary) bool {
	return left.State == right.State &&
		left.GameID == right.GameID &&
		left.Execution == right.Execution &&
		left.InputState == right.InputState &&
		left.InputReady == right.InputReady &&
		left.InputSession == right.InputSession &&
		left.Generation == right.Generation &&
		left.Gamepad == right.Gamepad &&
		slices.Equal(left.Interfaces, right.Interfaces)
}
