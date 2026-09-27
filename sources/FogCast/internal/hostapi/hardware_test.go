package hostapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type hardwareAPIService struct {
	expansionAPIService
	machines     []fogcast.HardwareMachine
	presentation catalog.CoreExpansionPresentation
}

func (s *hardwareAPIService) Hardware(context.Context) ([]fogcast.HardwareMachine, error) {
	return s.machines, nil
}

func (s *hardwareAPIService) CoreExpansionPresentation(context.Context, string) (catalog.CoreExpansionPresentation, error) {
	return s.presentation, nil
}

func (s *hardwareAPIService) SetCoreExpansionPresentation(_ context.Context, id, label, description string) (catalog.CoreExpansionPresentation, error) {
	s.presentation = catalog.CoreExpansionPresentation{ExpansionID: id, Label: label, Description: description}
	return s.presentation, nil
}

func TestHardwareHostClientKeepsDraftSeparateFromActiveReceipt(t *testing.T) {
	ctx := context.Background()
	packageID, cardID, gameID := strings.Repeat("a", 64), strings.Repeat("b", 64), "my-zx81"
	s := &hardwareAPIService{machines: []fogcast.HardwareMachine{{
		GameID: gameID, Title: "My ZX81", CoreID: "fes.zx81", PackageID: packageID, PackageReady: true, FirmwareReady: true, Ready: true,
		Socket:  fogcast.HardwareSocket{ID: "rear", Label: "Rear expansion socket", Supported: true},
		Choices: []fogcast.HardwareExpansion{{ExpansionID: cardID, Label: "Workshop card", Description: "Test description", Ready: true}},
	}}}
	s.status = protocol.Status{State: protocol.StateActive, GameID: &gameID, CorePackage: &protocol.CorePackageStatus{
		PackageID: packageID, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		Composition: &expansion.Composition{PackageID: packageID, ExpansionID: cardID},
	}}
	server := httptest.NewServer(hostapi.New(s))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	view, err := client.Hardware(ctx)
	if err != nil || len(view.Machines) != 1 || view.Session == nil {
		t.Fatalf("hardware %+v %v", view, err)
	}
	if view.Machines[0].DraftExpansionID != "" || view.Session.GameID != gameID || view.Session.State != "active" || view.Session.CorePackage.Composition == nil || view.Session.CorePackage.Composition.ExpansionID != cardID || view.Session.CorePackage.Generation != 9 {
		t.Fatalf("lost draft/active distinction: %+v session=%+v", view.Machines[0], view.Session)
	}
	if value, err := client.SelectCoreEntryExpansion(ctx, gameID, packageID, "", cardID); err != nil || value.ExpansionID != cardID {
		t.Fatalf("fit %+v %v", value, err)
	}
	if !reflect.DeepEqual(s.bound, []string{gameID, packageID, "", cardID}) {
		t.Fatalf("fit omitted exact compare-and-swap: %v", s.bound)
	}
	if _, err := client.SelectCoreEntryExpansion(ctx, gameID, packageID, cardID, ""); err != nil || !reflect.DeepEqual(s.bound, []string{gameID, packageID, cardID, ""}) {
		t.Fatalf("remove omitted compare-and-swap: %v %v", s.bound, err)
	}
	if _, err := client.SetCoreExpansionPresentation(ctx, cardID, "Named test card", "Describes the exact imported card."); err != nil {
		t.Fatal(err)
	}
	copy, err := client.CoreExpansionPresentation(ctx, cardID)
	if err != nil || copy.Label != "Named test card" || copy.Description != "Describes the exact imported card." || copy.ExpansionID != cardID {
		t.Fatalf("presentation %+v %v", copy, err)
	}
	if s.launchCalls != 0 || s.coreCalls != 0 || len(s.stopCtxErrs) != 0 {
		t.Fatal("hardware read or draft write changed the running session")
	}
}

func TestHardwareHostSnapshotRetainsDraftsWhenTargetUnavailable(t *testing.T) {
	s := &hardwareAPIService{machines: []fogcast.HardwareMachine{{GameID: "my-zx81", Title: "My ZX81"}}}
	s.statusErr = errors.New("private transport detail")
	server := httptest.NewServer(hostapi.New(s))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	view, err := client.Hardware(context.Background())
	if err != nil || len(view.Machines) != 1 || view.Session != nil || view.SessionError == "" || strings.Contains(view.SessionError, "private") {
		t.Fatalf("unavailable target became an idle session or hid the draft: %+v %v", view, err)
	}
}

func TestHardwarePresentationRequiresExplicitBoundedFields(t *testing.T) {
	s := &hardwareAPIService{}
	handler := hostapi.New(s)
	for _, body := range []string{`{}`, `{"label":"card"}`, `{"label":"card","description":"detail","ready":true}`} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/core-expansions/"+strings.Repeat("a", 64)+"/presentation", strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest || s.presentation.ExpansionID != "" {
			t.Fatalf("presentation accepted omitted fields or admission override: %d %s", response.Code, response.Body.String())
		}
	}
}
