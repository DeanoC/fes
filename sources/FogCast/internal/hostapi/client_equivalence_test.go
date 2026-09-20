package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/fogcastcli"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/kitlauncher"
)

func TestUnifiedSessionClientsShareHostOwnerAcrossFESCoreEntries(t *testing.T) {
	tests := []struct {
		gameID    string
		coreID    string
		packageID string
		keyboard  bool
	}{
		{gameID: "fpga-pong", coreID: "fes.pong", packageID: strings.Repeat("a", 63) + "1"},
		{gameID: "fpga-zx81", coreID: "fes.zx81", packageID: strings.Repeat("b", 63) + "2", keyboard: true},
		{gameID: "fpga-coleco", coreID: "fes.coleco", packageID: strings.Repeat("c", 63) + "3"},
	}
	service := &fakeService{
		games: []catalog.Game{
			{ID: "fpga-pong", Title: "Pong", System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "fpga-zx81", Title: "ZX81", System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "fpga-coleco", Title: "ColecoVision", System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
		},
		status:  protocol.Status{State: protocol.StateIdle},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached, Ready: true}}
	hostHandler := hostapi.New(service, hostapi.WithRemoteInput(input))
	var launchRequests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read launch request: %v", err)
			} else {
				var request map[string]string
				if err := json.Unmarshal(body, &request); err != nil {
					t.Errorf("decode launch request: %v", err)
				} else {
					launchRequests = append(launchRequests, request["game_id"])
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		hostHandler.ServeHTTP(w, r)
	}))
	defer api.Close()
	kit := kitlauncher.NewClient(kitlauncher.Config{
		API:      api.URL,
		Token:    strings.Repeat("t", 32),
		TargetID: "73dc9f5f-1a12-4a95-a820-a9b4e600769a",
	})
	ctx := context.Background()
	var sessionID string

	for index, test := range tests {
		active := equivalenceActiveStatus(test, uint64(index+1))
		service.status = protocol.Status{State: protocol.StateIdle}
		service.launch = protocol.CachedLaunchResponse{Status: active}
		service.stopped = protocol.Status{State: protocol.StateIdle}

		if index%2 == 0 {
			launch := runEquivalenceCLI(t, ctx, api.URL, "launch", test.gameID)
			if launch.State != protocol.StateActive || launch.GameID == nil || *launch.GameID != test.gameID {
				t.Fatalf("%s CLI launch = %+v", test.gameID, launch)
			}
		} else {
			launch, err := kit.Library.Launch(ctx, test.gameID)
			if err != nil {
				t.Fatalf("%s kit launch: %v", test.gameID, err)
			}
			if launch.State != string(protocol.StateActive) || launch.GameID != test.gameID {
				t.Fatalf("%s kit launch = %+v", test.gameID, launch)
			}
			service.status = active
			status := runEquivalenceCLI(t, ctx, api.URL, "status")
			if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != test.gameID {
				t.Fatalf("%s CLI status after kit launch = %+v", test.gameID, status)
			}
		}
		service.status = active

		if got := len(launchRequests); got != index+1 || launchRequests[index] != test.gameID {
			t.Fatalf("%s launch requests = %v, want through %q", test.gameID, launchRequests, test.gameID)
		}
		current := readEquivalenceSession(t, ctx, api.URL)
		if sessionID == "" {
			sessionID = current.ID
		} else if current.ID != sessionID {
			t.Fatalf("%s session ID changed from %q to %q", test.gameID, sessionID, current.ID)
		}
		if current.State != protocol.StateActive || current.GameID == nil || *current.GameID != test.gameID {
			t.Fatalf("%s shared session = %+v", test.gameID, current)
		}
		if current.CorePackage == nil || current.CorePackage.PackageID != test.packageID {
			t.Fatalf("%s selected package = %+v, want %q", test.gameID, current.CorePackage, test.packageID)
		}

		if index%2 == 0 {
			stop, err := kit.Library.Stop(ctx)
			if err != nil {
				t.Fatalf("%s kit stop: %v", test.gameID, err)
			}
			if stop.State != string(protocol.StateIdle) {
				t.Fatalf("%s kit stop = %+v", test.gameID, stop)
			}
			service.status = protocol.Status{State: protocol.StateIdle}
			status := runEquivalenceCLI(t, ctx, api.URL, "status")
			if status.State != protocol.StateIdle {
				t.Fatalf("%s CLI status after kit stop = %+v", test.gameID, status)
			}
		} else {
			stop := runEquivalenceCLI(t, ctx, api.URL, "stop")
			if stop.State != protocol.StateIdle {
				t.Fatalf("%s CLI stop = %+v", test.gameID, stop)
			}
			service.status = protocol.Status{State: protocol.StateIdle}
			status, err := kit.Library.Session(ctx)
			if err != nil {
				t.Fatalf("%s kit status after CLI stop: %v", test.gameID, err)
			}
			if status.State != string(protocol.StateIdle) {
				t.Fatalf("%s kit status after CLI stop = %+v", test.gameID, status)
			}
		}
		if got := len(input.attach); got != index+1 {
			t.Fatalf("%s input attaches = %d, want %d", test.gameID, got, index+1)
		}
		if got := len(input.detach); got != index+1 {
			t.Fatalf("%s input detaches = %d, want %d", test.gameID, got, index+1)
		}
	}
}

func equivalenceActiveStatus(test struct {
	gameID    string
	coreID    string
	packageID string
	keyboard  bool
}, generation uint64) protocol.Status {
	gameID, coreID, system := test.gameID, test.coreID, catalog.CorePlatform
	activeInterfaces := []protocol.RuntimeInterface{}
	if test.keyboard {
		activeInterfaces = append(activeInterfaces, protocol.RuntimeInterface{ID: "fes.keyboard", Major: 1})
	}
	return protocol.Status{
		State:        protocol.StateActive,
		GameID:       &gameID,
		System:       &system,
		Development:  true,
		ObservedCore: &coreID,
		CorePackage: &protocol.CorePackageStatus{
			PackageID:        test.packageID,
			Generation:       generation,
			ABI:              protocol.RuntimeContract{ID: "fes.runtime", Major: 1},
			BuildID:          strings.Repeat("d", 64),
			ActiveInterfaces: activeInterfaces,
			Gamepad:          !test.keyboard,
		},
	}
}

type equivalenceCLIResult struct {
	ID          string                      `json:"id"`
	State       protocol.State              `json:"state"`
	GameID      *string                     `json:"game_id"`
	CorePackage *protocol.CorePackageStatus `json:"core_package"`
}

func runEquivalenceCLI(t *testing.T, ctx context.Context, apiURL string, args ...string) equivalenceCLIResult {
	t.Helper()
	command := append([]string{"--json", "--api", apiURL}, args...)
	var stdout, stderr bytes.Buffer
	if code := fogcastcli.Run(ctx, command, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("CLI %v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
	}
	var result equivalenceCLIResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("CLI %v JSON %q: %v", args, stdout.String(), err)
	}
	return result
}

func readEquivalenceSession(t *testing.T, ctx context.Context, apiURL string) equivalenceCLIResult {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/v1/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/session status=%d", response.StatusCode)
	}
	var result equivalenceCLIResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
