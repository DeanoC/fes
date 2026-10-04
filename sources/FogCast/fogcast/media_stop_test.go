package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
			s := &Service{requestTimeout: time.Second, uploadTimeout: 10 * time.Millisecond, selectedTarget: "kit", activeTarget: "kit", activeExecution: ExecutionFPGANative, activePackageID: p.PackageID, activePackageGeneration: p.Generation, targets: []TargetConfig{{Name: "kit", Enabled: true}}, targetClients: map[string]serviceClient{"kit": client}}
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
	if s.activePackageID != "" {
		t.Fatal("test must start without remembered package")
	}
	status, err := s.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || statuses != 1 || stops != 1 || budget < 149*time.Second || budget > 150*time.Second {
		t.Fatal(status, err, statuses, stops, budget)
	}
}
