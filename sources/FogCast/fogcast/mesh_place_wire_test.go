package fogcast

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/internal/meshpref"
)

const (
	wireKitA = "11111111-1111-4111-8111-111111111111"
	wireKitB = "22222222-2222-4222-8222-222222222222"
	wireKitC = "33333333-3333-4333-8333-333333333333"
	wireKitD = "44444444-4444-4444-8444-444444444444"
	wireKitE = "55555555-5555-4555-8555-555555555555"

	wireAgentToken = "agent-token"
)

var wireSimpleGame = meshcontent.EligibleABI{ID: "fes.simple-game", Major: 1}

// placementAgent is a kit agent that serves only its content node
// document, with the agent Bearer token.
type placementAgent struct {
	server *httptest.Server
	mu     sync.Mutex
	nodeID string
	abis   []meshcontent.EligibleABI
	fail   bool
	reads  int
}

func newPlacementAgent(t *testing.T, nodeID string, abis ...meshcontent.EligibleABI) *placementAgent {
	t.Helper()
	agent := &placementAgent{nodeID: nodeID, abis: abis}
	agent.server = httptest.NewServer(http.HandlerFunc(agent.serve))
	t.Cleanup(agent.server.Close)
	return agent
}

func (a *placementAgent) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer "+wireAgentToken {
		http.Error(w, "unexpected request", http.StatusUnauthorized)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	if a.fail {
		http.Error(w, "kit unavailable", http.StatusServiceUnavailable)
		return
	}
	type abi struct {
		ID    string `json:"id"`
		Major int    `json:"major"`
	}
	doc := struct {
		NodeID string `json:"node_id"`
		ABIs   []abi  `json:"abis"`
	}{NodeID: a.nodeID, ABIs: []abi{}}
	for _, eligible := range a.abis {
		doc.ABIs = append(doc.ABIs, abi{ID: eligible.ID, Major: eligible.Major})
	}
	_ = json.NewEncoder(w).Encode(doc)
}

func (a *placementAgent) readCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reads
}

func (a *placementAgent) setFail(fail bool) {
	a.mu.Lock()
	a.fail = fail
	a.mu.Unlock()
}

func wireKitNode(id string) MeshNode {
	return MeshNode{NodeID: id, TargetID: id, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}
}

func wireTarget(name, id string, agent *placementAgent) TargetConfig {
	return TargetConfig{Name: name, Enabled: true, TargetID: id, Address: agent.server.URL, Agent: wireAgentToken}
}

func wiredPlacementService(targets []TargetConfig, nodes ...MeshNode) *Service {
	s := &Service{
		targets:        targets,
		selectedTarget: targets[0].Name,
		targetClients:  map[string]serviceClient{},
		displayMemory:  meshpref.New(),
		meshNodes:      nodes,
	}
	s.meshPlacementConfig = MeshPlacementConfig{Enabled: true}
	s.EnableMeshPlacement()
	return s
}

func TestEnableMeshPlacementFollowsConfig(t *testing.T) {
	off := &Service{displayMemory: meshpref.New()}
	off.EnableMeshPlacement()
	if off.meshPlacement {
		t.Fatal("placement turned on without [mesh] placement")
	}
	on := &Service{displayMemory: meshpref.New(), meshPlacementConfig: MeshPlacementConfig{Enabled: true}}
	on.EnableMeshPlacement()
	if !on.meshPlacement || on.meshHTTP == nil {
		t.Fatalf("placement %v http %v", on.meshPlacement, on.meshHTTP)
	}
	if on.meshEnsure || on.meshEnsureConfig {
		t.Fatal("placement turned mesh ensure on")
	}
}

func TestPlacementCandidatesComeFromTheInventoryAndNodeDocuments(t *testing.T) {
	den := newPlacementAgent(t, wireKitA, wireSimpleGame)
	impostor := newPlacementAgent(t, wireKitA, wireSimpleGame)
	attic := newPlacementAgent(t, wireKitD, wireSimpleGame)
	skewed := newPlacementAgent(t, wireKitE, wireSimpleGame)
	disabled := wireTarget("attic", wireKitD, attic)
	disabled.Enabled = false
	skew := wireKitNode(wireKitE)
	skew.Mesh = "2.0"
	s := wiredPlacementService([]TargetConfig{
		wireTarget("den", wireKitA, den),
		wireTarget("living", wireKitB, impostor),
		disabled,
		wireTarget("loft", wireKitE, skewed),
	}, wireKitNode(wireKitA), wireKitNode(wireKitB), wireKitNode(wireKitC), wireKitNode(wireKitD), skew)

	got := s.placementCandidates(context.Background())
	if len(got) != 5 {
		t.Fatalf("candidates %+v", got)
	}
	// Candidates keep inventory order. Place reads that order when
	// several native_emu nodes can run a title.
	for i, want := range []string{wireKitA, wireKitB, wireKitC, wireKitD, wireKitE} {
		if got[i].NodeID != want {
			t.Fatalf("candidate %d is %s, want inventory order %s", i, got[i].NodeID, want)
		}
	}
	byID := map[string]meshplace.Candidate{}
	for _, candidate := range got {
		byID[candidate.NodeID] = candidate
	}
	a := byID[wireKitA]
	if !a.MeshMajorOK || !a.DisplaySink || !a.InputSource || len(a.Execute) != 1 || a.Execute[0] != meshcontent.ExecuteFPGANative {
		t.Fatalf("kit a %+v", a)
	}
	if len(a.ABIs) != 1 || a.ABIs[0] != wireSimpleGame {
		t.Fatalf("kit a abis %+v", a.ABIs)
	}
	if abis := byID[wireKitB].ABIs; len(abis) != 0 {
		t.Fatalf("a document naming another node was eligibility: %+v", abis)
	}
	if abis := byID[wireKitC].ABIs; len(abis) != 0 {
		t.Fatalf("an unconfigured node was eligible: %+v", abis)
	}
	if abis := byID[wireKitD].ABIs; len(abis) != 0 {
		t.Fatalf("a disabled kit was eligible: %+v", abis)
	}
	if e := byID[wireKitE]; e.MeshMajorOK || len(e.ABIs) != 1 {
		t.Fatalf("mesh-major skew %+v", e)
	}
	if den.readCount() != 1 || impostor.readCount() != 1 || attic.readCount() != 0 {
		t.Fatalf("reads den %d impostor %d attic %d", den.readCount(), impostor.readCount(), attic.readCount())
	}
}

func TestPlacementNodeReadsAreReusedAndBackOff(t *testing.T) {
	ctx := context.Background()
	den := newPlacementAgent(t, wireKitA, wireSimpleGame)
	s := wiredPlacementService([]TargetConfig{wireTarget("den", wireKitA, den)}, wireKitNode(wireKitA))
	expire := func(age time.Duration) {
		s.meshMu.Lock()
		read := s.placementNodes[wireKitA]
		read.at = time.Now().Add(-age)
		s.placementNodes[wireKitA] = read
		s.meshMu.Unlock()
	}

	for i := 0; i < 3; i++ {
		if abis := s.placementNodeABIs(ctx, wireKitA); len(abis) != 1 {
			t.Fatalf("read %d abis %+v", i, abis)
		}
	}
	if den.readCount() != 1 {
		t.Fatalf("reads %d, want one reused read", den.readCount())
	}

	expire(placementNodeTTL)
	den.setFail(true)
	if abis := s.placementNodeABIs(ctx, wireKitA); len(abis) != 0 {
		t.Fatalf("a failed read kept eligibility %+v", abis)
	}
	if abis := s.placementNodeABIs(ctx, wireKitA); len(abis) != 0 || den.readCount() != 2 {
		t.Fatalf("backoff abis %+v reads %d", abis, den.readCount())
	}

	expire(placementNodeBackoff)
	den.setFail(false)
	if abis := s.placementNodeABIs(ctx, wireKitA); len(abis) != 1 || den.readCount() != 3 {
		t.Fatalf("after backoff abis %+v reads %d", abis, den.readCount())
	}

	moved := newPlacementAgent(t, wireKitA)
	s.targetMu.Lock()
	s.targets[0].Address = moved.server.URL
	s.targetMu.Unlock()
	if abis := s.placementNodeABIs(ctx, wireKitA); len(abis) != 0 || moved.readCount() != 1 {
		t.Fatalf("a changed address reused the previous read: abis %+v reads %d", abis, moved.readCount())
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	fresh := wiredPlacementService([]TargetConfig{wireTarget("den", wireKitA, den)}, wireKitNode(wireKitA))
	if abis := fresh.placementNodeABIs(canceled, wireKitA); len(abis) != 0 {
		t.Fatalf("canceled read %+v", abis)
	}
	if _, cached := fresh.placementNodes[wireKitA]; cached {
		t.Fatal("a canceled read was remembered as a failure")
	}
}

func TestProductionPlacementAsk(t *testing.T) {
	ctx := context.Background()
	den := newPlacementAgent(t, wireKitA, wireSimpleGame)

	off := wiredPlacementService([]TargetConfig{wireTarget("den", wireKitA, den)}, wireKitNode(wireKitA))
	off.meshPlacement = false
	if ask := off.placementAsk(ctx); ask != nil {
		t.Fatalf("placement off built %+v", ask)
	}

	empty := wiredPlacementService([]TargetConfig{wireTarget("den", wireKitA, den)})
	if ask := empty.placementAsk(ctx); ask != nil {
		t.Fatalf("empty inventory built %+v", ask)
	}

	on := wiredPlacementService([]TargetConfig{wireTarget("den", wireKitA, den)}, wireKitNode(wireKitA))
	on.meshPlacementConfig.Override = wireKitA
	ask := on.placementAsk(ctx)
	if ask == nil || len(ask.Candidates) != 1 || ask.OverrideNodeID != wireKitA || len(ask.Candidates[0].ABIs) != 1 {
		t.Fatalf("production ask %+v", ask)
	}

	installed := &MeshPlacementAsk{Candidates: []meshplace.Candidate{placeFPGACandidate("node-z", true)}}
	on.SetMeshPlacementAsk(installed)
	if got := on.placementAsk(ctx); got == nil || len(got.Candidates) != 1 || got.Candidates[0].NodeID != "node-z" || got.OverrideNodeID != "" {
		t.Fatalf("installed ask did not win: %+v", got)
	}
}

// wiredPongFixture is a projected Pong core entry on the selected kit
// "dev" (wireKitA), with production placement on and the ensure seam off.
func wiredPongFixture(t *testing.T) (*Service, *defaultMediaPackageClient, string, corepackage.Inspection, *placementAgent) {
	t.Helper()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Standalone Pong")
	client.coreLoad = pongCoreLoad(inspection)
	den := newPlacementAgent(t, wireKitA, wireSimpleGame)
	s.targets = []TargetConfig{wireTarget("dev", wireKitA, den)}
	s.selectedTarget = "dev"
	s.meshNodes = []MeshNode{wireKitNode(wireKitA)}
	s.meshPlacementConfig = MeshPlacementConfig{Enabled: true}
	s.EnableMeshPlacement()
	return s, client, entry.GameID, inspection, den
}

func TestProductionPlacementSelectsTheBoundKitWithTheSeamOff(t *testing.T) {
	s, client, gameID, _, _ := wiredPongFixture(t)

	rows, on := s.GamesMeshReady(context.Background(), []string{gameID})
	if !on {
		t.Fatal("placement view off")
	}
	row := rows[gameID]
	if row.Placement != meshplace.OutcomeSelected || !row.PlacementOnly || row.Ready || row.Block != "" || row.NextAction != "" {
		t.Fatalf("row %#v", row)
	}
	if recorded := s.MeshSessionPlacement(); recorded != (MeshPlacement{}) {
		t.Fatalf("games read recorded %+v", recorded)
	}

	boundName, err := launchAndObserveBind(t, s, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if boundName != "dev" || s.selectedTarget != "dev" || s.activeExecution != ExecutionFPGANative {
		t.Fatalf("bound %q selected %q execution %q", boundName, s.selectedTarget, s.activeExecution)
	}
	want := MeshPlacement{Execute: wireKitA, DisplaySink: wireKitA, InputSource: wireKitA}
	if got := s.MeshSessionPlacement(); got != want {
		t.Fatalf("placement %+v", got)
	}
	if client.coreCalls != 1 {
		t.Fatalf("core loads %d, want one on the bound kit", client.coreCalls)
	}
	if got := s.PlaceOptions().LastDisplaySink; got != wireKitA {
		t.Fatalf("last sink %q", got)
	}
	if s.meshEnsure || s.meshExecute.Executor != nil {
		t.Fatalf("ensure %v executor %v", s.meshEnsure, s.meshExecute.Executor)
	}
}

func TestProductionPlacementNeedsATieBreakBetweenTwoKits(t *testing.T) {
	s, _, gameID, _, _ := wiredPongFixture(t)
	living := newPlacementAgent(t, wireKitB, wireSimpleGame)
	s.targets = append(s.targets, wireTarget("living", wireKitB, living))
	s.meshNodes = append(s.meshNodes, wireKitNode(wireKitB))

	rows, on := s.GamesMeshReady(context.Background(), []string{gameID})
	if !on || rows[gameID].Placement != meshplace.OutcomeUnresolved || !rows[gameID].PlacementOnly {
		t.Fatalf("rows %#v on %v", rows, on)
	}

	s.SetDisplayPreference(wireKitA)
	rows, on = s.GamesMeshReady(context.Background(), []string{gameID})
	if !on || rows[gameID].Placement != meshplace.OutcomeSelected {
		t.Fatalf("preference rows %#v on %v", rows, on)
	}

	s.SetDisplayPreference("")
	s.meshPlacementConfig.Override = wireKitC
	rows, _ = s.GamesMeshReady(context.Background(), []string{gameID})
	if rows[gameID].Placement != meshplace.OutcomeUnresolved {
		t.Fatalf("an override naming no candidate was replaced: %#v", rows[gameID])
	}
	s.meshPlacementConfig.Override = wireKitB
	rows, _ = s.GamesMeshReady(context.Background(), []string{gameID})
	if rows[gameID].Placement != meshplace.OutcomeSelected {
		t.Fatalf("override %#v", rows[gameID])
	}
}

func TestProductionPlacementFailsClosedWhenNoKitListsTheABI(t *testing.T) {
	s, _, gameID, _, den := wiredPongFixture(t)
	den.mu.Lock()
	den.abis = []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}
	den.mu.Unlock()
	rows, on := s.GamesMeshReady(context.Background(), []string{gameID})
	if !on || rows[gameID].Placement != meshplace.OutcomeFailClosed || !rows[gameID].PlacementOnly {
		t.Fatalf("rows %#v on %v", rows, on)
	}
}

func TestProductionPlacementRebindsAnotherKitWithTheSeamOff(t *testing.T) {
	s, client, gameID, _, _ := wiredPongFixture(t)
	living := newPlacementAgent(t, wireKitB, wireSimpleGame)
	s.targets = append(s.targets, wireTarget("living", wireKitB, living))
	s.meshNodes = append(s.meshNodes, wireKitNode(wireKitB))
	keeper := &placementKeepClient{defaultMediaPackageClient: client}
	s.targetClients["living"] = keeper
	s.SetDisplayPreference(wireKitB)

	boundName, err := launchAndObserveBind(t, s, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if boundName != "living" || s.selectedTarget != "living" || s.activeExecution != ExecutionFPGANative {
		t.Fatalf("bound %q selected %q execution %q", boundName, s.selectedTarget, s.activeExecution)
	}
	owned, _ := keeper.MeshKitLease()
	if keeper.claims != 1 || keeper.releases != 0 || !owned {
		t.Fatalf("claims %d releases %d owned %v", keeper.claims, keeper.releases, owned)
	}
	want := MeshPlacement{Execute: wireKitB, DisplaySink: wireKitB, InputSource: wireKitB}
	if got := s.MeshSessionPlacement(); got != want {
		t.Fatalf("placement %+v", got)
	}
	if got := s.PlaceOptions().LastDisplaySink; got != wireKitB {
		t.Fatalf("last sink %q", got)
	}
	if s.meshEnsure || s.meshExecute.Executor != nil {
		t.Fatalf("ensure %v executor %v", s.meshEnsure, s.meshExecute.Executor)
	}
}

func TestExplicitTargetLaunchSkipsPlacement(t *testing.T) {
	s, client, gameID, _, _ := wiredPongFixture(t)
	living := newPlacementAgent(t, wireKitB, wireSimpleGame)
	s.targets = append(s.targets, wireTarget("living", wireKitB, living))
	s.meshNodes = append(s.meshNodes, wireKitNode(wireKitB))
	keeper := &placementKeepClient{defaultMediaPackageClient: client}
	s.targetClients["living"] = keeper
	s.SetDisplayPreference(wireKitB)

	if _, err := s.LaunchOn(context.Background(), gameID, "dev", nil); err != nil {
		t.Fatal(err)
	}
	if keeper.claims != 0 {
		t.Fatalf("placement claimed kit b for an explicit target: %d", keeper.claims)
	}
	if got := s.MeshSessionPlacement(); got != (MeshPlacement{}) {
		t.Fatalf("explicit launch recorded %+v", got)
	}
	if s.selectedTarget != "dev" || s.activeTarget != "dev" {
		t.Fatalf("selected %q active %q", s.selectedTarget, s.activeTarget)
	}
	if living.readCount() != 0 {
		t.Fatalf("explicit launch read kit b's node document %d times", living.readCount())
	}
}

func TestPlacementOffKeepsTheSeamOffView(t *testing.T) {
	s, _, gameID, _, den := wiredPongFixture(t)
	s.meshPlacement = false
	if rows, on := s.GamesMeshReady(context.Background(), []string{gameID}); on || len(rows) != 0 {
		t.Fatalf("placement off rows %#v on %v", rows, on)
	}
	if _, err := s.Launch(context.Background(), gameID, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.MeshSessionPlacement(); got != (MeshPlacement{}) {
		t.Fatalf("placement off recorded %+v", got)
	}
	if den.readCount() != 0 {
		t.Fatalf("placement off read the node document %d times", den.readCount())
	}
}

func TestPlacementOnlyViewWithoutAProjectedTitleReadsNoKit(t *testing.T) {
	s, _, _, _, den := wiredPongFixture(t)
	if rows, on := s.GamesMeshReady(context.Background(), []string{"snes-mario"}); on || len(rows) != 0 {
		t.Fatalf("rows %#v on %v", rows, on)
	}
	if den.readCount() != 0 {
		t.Fatalf("a page with no projected title read the kit %d times", den.readCount())
	}
}

func TestInstalledAskWithAnEntryOnlySessionCarriesPlacementOnly(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	s := phase0PlacementService()
	s.SetMeshExecuteSession(MeshExecuteSession{
		Entry: func(id string) (meshcontent.Entry, bool) { return entry, id == entry.TitleID },
	})
	s.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-a", true), placeFPGACandidate("node-b", true)},
	})
	rows, on := s.GamesMeshReady(context.Background(), []string{entry.TitleID, "other-title"})
	if !on || len(rows) != 1 {
		t.Fatalf("rows %#v on %v", rows, on)
	}
	if row := rows[entry.TitleID]; row.Placement != meshplace.OutcomeUnresolved || !row.PlacementOnly || row.Ready {
		t.Fatalf("row %#v", row)
	}
}
