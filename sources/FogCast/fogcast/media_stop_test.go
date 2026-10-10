package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type mediaStopClient struct {
	fakeServiceClient
	savedCalls int
	budget     time.Duration
}

func (c *mediaStopClient) StopWithMediaSave(ctx context.Context) (protocol.Status, error) {
	c.savedCalls++
	d, _ := ctx.Deadline()
	c.budget = time.Until(d)
	select {
	case <-time.After(20 * time.Millisecond):
		return protocol.Status{State: protocol.StateIdle}, nil
	case <-ctx.Done():
		return protocol.Status{}, ctx.Err()
	}
}
func TestStopSelectsDurableDiskBudgetAndPreservesCallerCancellation(t *testing.T) {
	for _, mode := range []string{"bound", "volatile", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			p := &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
			if mode == "volatile" {
				p.MediaUnits[0].Persistence = nil
			}
			client := &mediaStopClient{fakeServiceClient: fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive, CorePackage: p}, stopResult: protocol.Status{State: protocol.StateIdle}}}
			s := &Service{requestTimeout: time.Second, uploadTimeout: 10 * time.Millisecond, selectedTarget: "kit", activeTarget: "kit", activeExecution: ExecutionFPGANative, plays: map[string]targetPlay{"kit": {execution: ExecutionFPGANative, packageID: p.PackageID, packageGeneration: p.Generation}}, targets: []TargetConfig{{Name: "kit", Enabled: true}}, targetClients: map[string]serviceClient{"kit": client}}
			ctx := context.Background()
			cancel := func() {}
			if mode == "canceled" {
				ctx, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
			}
			defer cancel()
			status, err := s.Stop(ctx)
			if mode == "volatile" {
				if err != nil || client.stopCalls != 1 || client.savedCalls != 0 {
					t.Fatal(status, err, client.stopCalls, client.savedCalls)
				}
			} else if mode == "bound" {
				if err != nil || status.State != protocol.StateIdle || client.savedCalls != 1 || client.stopCalls != 0 || client.budget < 149*time.Second {
					t.Fatal(status, err, client.budget)
				}
			} else if err == nil || client.savedCalls != 1 || client.stopCalls != 0 || client.budget > 5*time.Millisecond || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatal(err, client.budget)
			}
		})
	}
}

type discoveredStopTransport func(*http.Request) (*http.Response, error)

func (f discoveredStopTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRecoveredDiskStopReusesDiscoveryStatusWithoutRememberedPackage(t *testing.T) {
	const targetID = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	p := &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
	statuses, stops := 0, 0
	var budget time.Duration
	transport := discoveredStopTransport(func(r *http.Request) (*http.Response, error) {
		var reply any
		switch r.URL.Path {
		case "/v1/health":
			reply = protocol.Health{APIVersion: "v1", Ready: true, TargetID: targetID}
		case "/v1/kit/lease":
			reply = targetclient.KitOwnership{State: "free"}
		case "/v1/status":
			statuses++
			reply = protocol.Status{State: protocol.StateActive, Development: true, CorePackage: p}
		case "/v1/stop":
			stops++
			d, _ := r.Context().Deadline()
			budget = time.Until(d)
			reply = protocol.Status{State: protocol.StateIdle}
		default:
			t.Fatalf("unexpected %s", r.URL)
		}
		raw, _ := json.Marshal(reply)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	base, _ := url.Parse("http://kit")
	client := targetclient.NewClient(base, "secret", &http.Client{Timeout: 5 * time.Second, Transport: transport})
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: base.String(), Agent: "secret", TargetID: targetID}}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	if s.plays["kit"].packageID != "" {
		t.Fatal("test must start without remembered package")
	}
	status, err := s.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || statuses != 1 || stops != 1 || budget < 149*time.Second || budget > 150*time.Second {
		t.Fatal(status, err, statuses, stops, budget)
	}
}

// A local host play can coexist with a disk on a sibling kit. Its Stop must
// use that kit's observed persistence, regardless of the foreground cache.
func TestScopedDiskStopUsesOwnBindingWhileHostOnlyPlayContinues(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			p := &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady}}}
			if persistent {
				p.MediaUnits[0].Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}
			}
			healths, statuses, stops := 0, 0, 0
			var budget time.Duration
			transport := discoveredStopTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "sibling" {
					t.Fatalf("scoped Stop accessed foreground kit: %s", r.URL)
				}
				var reply any
				switch r.URL.Path {
				case "/v1/health":
					healths++
					reply = protocol.Health{APIVersion: "v1", Ready: true}
				case "/v1/status":
					statuses++
					reply = protocol.Status{State: protocol.StateActive, CorePackage: p}
				case "/v1/stop":
					stops++
					d, _ := r.Context().Deadline()
					budget = time.Until(d)
					reply = protocol.Status{State: protocol.StateIdle}
				default:
					t.Fatalf("unexpected %s", r.URL)
				}
				raw, _ := json.Marshal(reply)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})
			foreground, _ := url.Parse("http://foreground")
			sibling, _ := url.Parse("http://sibling")
			httpClient := &http.Client{Timeout: 5 * time.Second, Transport: transport}
			foregroundClient := targetclient.NewClient(foreground, "secret", httpClient)
			s := newService(Config{Targets: []TargetConfig{{Name: "foreground", Enabled: true, Address: foreground.String(), TargetID: "foreground-id"}, {Name: "sibling", Enabled: true, Address: sibling.String()}}, SelectedTarget: "foreground", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, foregroundClient)
			s.targetClients["sibling"] = targetclient.NewClient(sibling, "secret", httpClient)
			s.plays["sibling"] = targetPlay{execution: ExecutionFPGANative, packageID: p.PackageID, packageGeneration: p.Generation}
			s.activeExecution, s.activeTarget, s.activeGameID = ExecutionHostOnly, "host", "local-game"
			s.connection = TargetConnection{mediaDataBound: !persistent}
			status, err := s.StopTarget(context.Background(), "sibling")
			if err != nil || status.State != protocol.StateIdle || healths != 1 || statuses != 1 || stops != 1 {
				t.Fatal(status, err, healths, statuses, stops)
			}
			if persistent && (budget < 149*time.Second || budget > 150*time.Second) {
				t.Fatalf("persistent sibling Stop budget = %v", budget)
			}
			if !persistent && (budget <= 0 || budget > time.Second) {
				t.Fatalf("volatile sibling inherited foreground save budget: %v", budget)
			}
			if s.activeExecution != ExecutionHostOnly || s.activeTarget != "host" || s.activeGameID != "local-game" || s.plays["sibling"].execution != "" {
				t.Fatalf("scoped Stop changed host ownership or retained stopped kit: %+v", s.plays)
			}
		})
	}
}

func TestSiblingLibraryMediaLaunchKeepsIndependentHostOwner(t *testing.T) {
	s, sibling, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Sibling library media")
	active := coreEntryActiveStatus(inspection, 7, true)
	sibling.mediaStatus = active
	sibling.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		sibling.statusResult = active
		return active, nil
	}
	foreground := &fakeServiceClient{statusErr: errors.New("foreground must not be accessed")}
	s.targets = []TargetConfig{{Name: "foreground", Enabled: true, TargetID: "foreground-id"}, {Name: "sibling", Enabled: true, TargetID: "sibling-id"}}
	s.selectedTarget = "foreground"
	s.targetClients = map[string]serviceClient{"foreground": foreground, "sibling": sibling}
	host := &fakeHostExecutor{}
	s.hostExecutor = host
	s.activeExecution, s.activeTarget, s.activeGameID = ExecutionHostOnly, "local-host", "local-game"
	response, err := s.LaunchOn(context.Background(), entry.GameID, "sibling", nil)
	if err != nil {
		t.Fatalf("sibling library launch: %v", err)
	}
	want := protocol.DevelopmentMediaBinding{PackageID: entry.PackageID, Generation: 7, Target: "sibling", TargetID: "sibling-id"}
	if sibling.mediaCalls != 1 || sibling.mediaBinding != want || !bytes.Equal(sibling.mediaBody, []byte("library media fixture")) {
		t.Fatalf("sibling media calls=%d binding=%+v, want=%+v", sibling.mediaCalls, sibling.mediaBinding, want)
	}
	if response.Status.GameID == nil || *response.Status.GameID != entry.GameID || response.Status.System == nil || *response.Status.System != catalog.CorePlatform {
		t.Fatalf("sibling response lost library identity: %+v", response.Status)
	}
	if s.activeExecution != ExecutionHostOnly || s.activeTarget != "local-host" || s.activeGameID != "local-game" || host.stopCalls != 0 || foreground.statusCalls != 0 {
		t.Fatalf("sibling launch changed independent host or accessed foreground: execution=%q target=%q game=%q host stops=%d foreground reads=%d", s.activeExecution, s.activeTarget, s.activeGameID, host.stopCalls, foreground.statusCalls)
	}
	if play := s.plays["sibling"]; play.gameID != entry.GameID || play.packageID != entry.PackageID || play.packageGeneration != 7 {
		t.Fatalf("sibling launch lost kit ownership: %+v", play)
	}
}
