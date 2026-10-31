package hostapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type pairedSlotService struct {
	pairedExpansionService
	pkg, card, selected, imported string
	bound                         [4]string
	slot                          int
}

func (s *pairedSlotService) ImportCoreExpansion(_ context.Context, _ int64, reader io.Reader) (catalog.CoreExpansion, error) {
	body, err := io.ReadAll(reader)
	s.imported = string(body)
	return catalog.CoreExpansion{ExpansionID: s.card, PackageID: s.pkg, Slot: 4}, err
}
func (s *pairedSlotService) CoreExpansions(context.Context) ([]catalog.CoreExpansion, error) {
	return []catalog.CoreExpansion{{ExpansionID: s.card, PackageID: s.pkg, Slot: 4}}, nil
}
func (s *pairedSlotService) CoreEntrySlotExpansions(_ context.Context, game string) (fogcast.CoreEntrySlotExpansions, error) {
	view := fogcast.CoreEntrySlotExpansions{GameID: game, PackageID: s.pkg, Sockets: []int{2, 4, 5, 7}, Expansions: []protocol.SlotExpansionStatus{}, Ready: true}
	if s.selected != "" {
		view.Expansions = append(view.Expansions, protocol.SlotExpansionStatus{Slot: 4, ExpansionID: s.selected, Ready: true})
	}
	return view, nil
}
func (s *pairedSlotService) SelectCoreEntrySlotExpansion(ctx context.Context, game, pkg string, slot int, expected, id string) (fogcast.CoreEntrySlotExpansions, error) {
	s.bound = [4]string{game, pkg, expected, id}
	s.slot = slot
	s.selected = id
	return s.CoreEntrySlotExpansions(ctx, game)
}
func TestPairedLauncherReadsImportsAndSelectsPhysicalSocket(t *testing.T) {
	service := &pairedSlotService{pairedExpansionService: pairedExpansionService{settingsFake: settingsFake{settings: twoKitSettings("kit-a")}}, pkg: strings.Repeat("a", 64), card: strings.Repeat("b", 64)}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)
	ctx := context.Background()
	cards, err := client.CoreExpansions(ctx)
	if err != nil || len(cards) != 1 || cards[0].Slot != 4 {
		t.Fatalf("cards %+v err=%v", cards, err)
	}
	view, err := client.CoreEntrySlotExpansions(ctx, "my-apple2")
	if err != nil || len(view.Sockets) != 4 {
		t.Fatalf("sockets %+v err=%v", view, err)
	}
	request, err := client.NewRequest(ctx, http.MethodPost, "/api/v1/core-expansions", strings.NewReader("private expansion bytes"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.HTTPClient().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || service.imported != "private expansion bytes" {
		t.Fatalf("import status=%d bytes=%q", response.StatusCode, service.imported)
	}
	if _, err := client.SelectCoreEntrySlotExpansion(ctx, "my-apple2", service.pkg, 4, "", service.card); err != nil {
		t.Fatal(err)
	}
	if service.slot != 4 || service.bound != [4]string{"my-apple2", service.pkg, "", service.card} || service.settings.SelectedTarget != "kit-a" || service.launchCalls != 0 || service.coreCalls != 0 {
		t.Fatalf("bound=%v slot=%d foreground=%s", service.bound, service.slot, service.settings.SelectedTarget)
	}
}
