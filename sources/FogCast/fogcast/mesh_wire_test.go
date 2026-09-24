package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func TestEnableMeshContentDialsSelectedKit(t *testing.T) {
	const nodeID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer kit-token" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":  nodeID,
			"abis":     []map[string]any{{"id": "fes.simple-game", "major": 1}},
			"packages": []string{strings.Repeat("ab", 32)},
		})
	}))
	defer server.Close()
	service := &Service{
		meshEnsureConfig: true,
		meshHTTP:         &http.Client{},
		targets: []TargetConfig{{
			Name: "kit", Enabled: true, Address: server.URL, Agent: "kit-token", TargetID: nodeID,
		}},
		selectedTarget: "kit",
	}
	if _, on := service.GamesMeshReady(context.Background(), nil); on || hits != 0 {
		t.Fatalf("seam on before EnableMeshContent hits=%d on=%v", hits, on)
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); !on || hits != 1 {
		t.Fatalf("games ready on=%v hits=%d", on, hits)
	}
	snap, err := service.captureLaunchSnapshot("coleco-frogger", "")
	if err != nil || !snap.frozen || snap.executor == nil || snap.boundNode != nodeID {
		t.Fatalf("snapshot frozen=%v node=%s err=%v", snap.frozen, snap.boundNode, err)
	}
	if _, on := service.GamesMeshReady(context.Background(), nil); !on || hits != 1 {
		t.Fatalf("second ready redialed hits=%d on=%v", hits, on)
	}
}

func TestEnableMeshContentStaysOffWhenDialFailsOrDisabled(t *testing.T) {
	service := &Service{
		meshEnsureConfig: true,
		meshHTTP:         &http.Client{},
		targets: []TargetConfig{{
			Name: "kit", Enabled: true, Address: "http://127.0.0.1:1", Agent: "kit-token",
			TargetID: "73dc9f5f-1a12-4a95-a820-a9b4e600769a",
		}},
		selectedTarget: "kit",
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); on {
		t.Fatal("failed dial installed the seam")
	}
	off := &Service{meshEnsureConfig: false, targets: service.targets, selectedTarget: "kit"}
	off.EnableMeshContent()
	if _, on := off.GamesMeshReady(context.Background(), nil); on {
		t.Fatal("ensure = false installed the seam")
	}
}

func TestActivateMeshExecutorRebindsWhenSelectedTargetChanges(t *testing.T) {
	const (
		nodeA = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
		nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
		nodeC = "95fe1b71-3c34-6cb7-ca42-cbd6082298bc"
	)
	var hitsA, hitsB, hitsC atomic.Int32
	serverA := meshNodeServer(&hitsA, nodeA, "token-a")
	defer serverA.Close()
	serverB := meshNodeServer(&hitsB, nodeB, "token-b")
	defer serverB.Close()
	serverC := meshNodeServer(&hitsC, nodeC, "token-b")
	defer serverC.Close()
	service := &Service{
		meshEnsureConfig: true,
		targets: []TargetConfig{
			{Name: "kit-a", Enabled: true, Address: serverA.URL, Agent: "token-a", TargetID: nodeA},
			{Name: "kit-b", Enabled: true, Address: serverB.URL, Agent: "token-b", TargetID: nodeB},
		},
		selectedTarget: "kit-a",
	}
	service.EnableMeshContent()
	snap, err := service.captureLaunchSnapshot("coleco-frogger", "")
	if err != nil || !snap.frozen || snap.boundNode != nodeA || snap.executor == nil || snap.executor.NodeID() != nodeA {
		t.Fatalf("kit A frozen=%v bound=%s err=%v", snap.frozen, snap.boundNode, err)
	}
	if hitsA.Load() != 1 || hitsB.Load() != 0 {
		t.Fatalf("dial hits A=%d B=%d", hitsA.Load(), hitsB.Load())
	}
	service.selectedTarget = "kit-b"
	snap, err = service.captureLaunchSnapshot("coleco-frogger", "")
	if err != nil || !snap.frozen || snap.boundNode != nodeB || snap.executor == nil || snap.executor.NodeID() != nodeB {
		t.Fatalf("kit B frozen=%v bound=%s err=%v", snap.frozen, snap.boundNode, err)
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Fatalf("rebind hits A=%d B=%d", hitsA.Load(), hitsB.Load())
	}
	if _, on := service.GamesMeshReady(context.Background(), nil); !on || hitsB.Load() != 1 {
		t.Fatalf("ready after rebind hitsB=%d", hitsB.Load())
	}
	service.targets[1].Address = serverC.URL
	service.targets[1].TargetID = nodeC
	snap, err = service.captureLaunchSnapshot("coleco-frogger", "")
	if err != nil || !snap.frozen || snap.boundNode != nodeC || snap.executor == nil || snap.executor.NodeID() != nodeC {
		t.Fatalf("address change frozen=%v bound=%s err=%v", snap.frozen, snap.boundNode, err)
	}
	if hitsB.Load() != 1 || hitsC.Load() != 1 {
		t.Fatalf("address rebind hits B=%d C=%d", hitsB.Load(), hitsC.Load())
	}
	service.targets[1].Address = "http://127.0.0.1:1"
	if _, on := service.GamesMeshReady(context.Background(), nil); on {
		t.Fatal("failed rebind kept the previous executor")
	}
	snap, err = service.captureLaunchSnapshot("coleco-frogger", "")
	if err != nil || snap.frozen || snap.executor != nil {
		t.Fatalf("failed rebind frozen=%v err=%v", snap.frozen, err)
	}
}

func meshNodeServer(hits *atomic.Int32, node, token string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer "+token {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":  node,
			"abis":     []map[string]any{{"id": "fes.simple-game", "major": 1}},
			"packages": []string{strings.Repeat("ab", 32)},
		})
	}))
}

func TestLaunchOnNonSelectedKitUsesThatExecutor(t *testing.T) {
	const (
		nodeA = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
		nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	)
	var hitsA, hitsB atomic.Int32
	serverA := meshNodeServer(&hitsA, nodeA, "token-a")
	defer serverA.Close()
	serverB := meshContentServer(&hitsB, nodeB, "token-b")
	defer serverB.Close()
	entry, cart := fpgaMeshEntry("coleco-frogger")
	selected := &meshLaunchExecutor{
		node:    nodeA,
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		meshEnsureConfig: true,
		targets: []TargetConfig{
			{Name: "kit-a", Enabled: true, Address: serverA.URL, Agent: "token-a", TargetID: nodeA},
			{Name: "kit-b", Enabled: true, Address: serverB.URL, Agent: "token-b", TargetID: nodeB},
		},
		selectedTarget: "kit-a",
		targetClients: map[string]serviceClient{
			"kit-a": ownedMeshClient(),
			"kit-b": ownedMeshClient(),
		},
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); !on || hitsA.Load() != 1 || hitsB.Load() != 0 {
		t.Fatalf("selected dial on=%v hits A=%d B=%d", on, hitsA.Load(), hitsB.Load())
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: nodeA,
		Executor:  selected,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	snap, err := service.captureLaunchSnapshot(entry.TitleID, "kit-b")
	if err != nil {
		t.Fatal(err)
	}
	if snap.executor == nil || !snap.siblingExecutor || snap.executor == selected || snap.executor.NodeID() != nodeB || snap.nodeID != nodeB {
		t.Fatalf("sibling executor flag=%v node=%s", snap.siblingExecutor, snap.nodeID)
	}
	if err := service.revalidateLaunchSnapshot(snap); err != nil {
		t.Fatalf("revalidate %v", err)
	}
	service.meshMu.Lock()
	installed := service.meshExecute.Executor
	service.meshMu.Unlock()
	if installed != selected {
		t.Fatal("explicit launch replaced the selected session")
	}
	beforeB := hitsB.Load()
	_, err = service.LaunchOn(context.Background(), entry.TitleID, "kit-b", nil)
	if errors.Is(err, meshcontent.ErrUnboundNode) || beforeB == hitsB.Load() {
		t.Fatalf("sibling launch err=%v hitsB %d -> %d", err, beforeB, hitsB.Load())
	}
	if len(selected.pulls) != 0 || len(selected.links) != 0 {
		t.Fatalf("selected executor pulled %+v linked %+v", selected.pulls, selected.links)
	}
	same, err := service.captureLaunchSnapshot(entry.TitleID, "kit-a")
	if err != nil || same.siblingExecutor || same.executor != selected {
		t.Fatalf("selected launch sibling=%v err=%v", same.siblingExecutor, err)
	}
}

func TestLaunchOnNonSelectedHostEntryStaysOnSelectedExecutor(t *testing.T) {
	const (
		nodeA = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
		nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	)
	var hitsA, hitsB atomic.Int32
	serverA := meshNodeServer(&hitsA, nodeA, "token-a")
	defer serverA.Close()
	serverB := meshContentServer(&hitsB, nodeB, "token-b")
	defer serverB.Close()
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(cart)},
	}
	selected := &meshLaunchExecutor{
		node:    nodeA,
		sources: map[string]bool{cart.String(): true},
	}
	service := &Service{
		meshEnsureConfig: true,
		targets: []TargetConfig{
			{Name: "kit-a", Enabled: true, Address: serverA.URL, Agent: "token-a", TargetID: nodeA},
			{Name: "kit-b", Enabled: true, Address: serverB.URL, Agent: "token-b", TargetID: nodeB},
		},
		selectedTarget: "kit-a",
		targetClients: map[string]serviceClient{
			"kit-a": ownedMeshClient(),
			"kit-b": ownedMeshClient(),
		},
		catalog: &fakeServiceCatalog{gameErr: errors.New("stop after ensure")},
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); !on {
		t.Fatal("selected kit did not install the seam")
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: nodeA,
		Executor:  selected,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "kit-b", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("err %v", err)
	}
	if hitsB.Load() != 0 || len(selected.pulls) != 1 || selected.pulls[0] != cart {
		t.Fatalf("hitsB=%d pulls %+v", hitsB.Load(), selected.pulls)
	}
}

func TestLaunchOnMismatchedSiblingDoesNotPullOnSelectedExecutor(t *testing.T) {
	const (
		nodeA = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
		nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	)
	var hitsA, hitsB atomic.Int32
	serverA := meshNodeServer(&hitsA, nodeA, "token-a")
	defer serverA.Close()
	serverB := meshNodeServer(&hitsB, "11111111-1111-1111-1111-111111111111", "token-b")
	defer serverB.Close()
	entry, _ := fpgaMeshEntry("coleco-frogger")
	selected := &meshLaunchExecutor{
		node: nodeA,
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		meshEnsureConfig: true,
		targets: []TargetConfig{
			{Name: "kit-a", Enabled: true, Address: serverA.URL, Agent: "token-a", TargetID: nodeA},
			{Name: "kit-b", Enabled: true, Address: serverB.URL, Agent: "token-b", TargetID: nodeB},
		},
		selectedTarget: "kit-a",
		targetClients:  map[string]serviceClient{"kit-a": ownedMeshClient(), "kit-b": ownedMeshClient()},
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); !on {
		t.Fatal("selected kit did not install the seam")
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: nodeA,
		Executor:  selected,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "kit-b", nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if len(selected.pulls) != 0 || len(selected.links) != 0 {
		t.Fatalf("mismatched sibling pulled %+v", selected.pulls)
	}
}

func meshContentServer(hits *atomic.Int32, node, token string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/v1/mesh/content/node":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"node_id":  node,
				"abis":     []map[string]any{{"id": "fes.simple-game", "major": 1}},
				"packages": []string{strings.Repeat("ab", 32)},
			})
		case "/v1/mesh/content/slot":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "missing"})
		case "/v1/mesh/content/source":
			_ = json.NewEncoder(w).Encode(map[string]bool{"advertises": false})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestEnableMeshContentRefusesMismatchedNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"node_id":"11111111-1111-1111-1111-111111111111","abis":[]}`)
	}))
	defer server.Close()
	service := &Service{
		meshEnsureConfig: true,
		targets: []TargetConfig{{
			Name: "kit", Enabled: true, Address: server.URL, Agent: "kit-token",
			TargetID: "73dc9f5f-1a12-4a95-a820-a9b4e600769a",
		}},
		selectedTarget: "kit",
	}
	service.EnableMeshContent()
	if _, on := service.GamesMeshReady(context.Background(), nil); on {
		t.Fatal("mismatched node installed the seam")
	}
}

func TestMeshCatalogEntryWithoutCoreEntryIsFalse(t *testing.T) {
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := &Service{catalog: store}
	if _, ok := service.meshCatalogEntry("coleco-frogger"); ok {
		t.Fatal("missing core entry was projected")
	}
}

func TestMeshContentServesCoreMediaAndExpansionCart(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	payload := []byte("household-bios")
	media, _, err := store.ImportCoreMediaStream(ctx, int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	cart := bytes.Repeat([]byte{0x55}, 45000)
	sum := sha256.Sum256(cart)
	packageID := strings.Repeat("a", 64)
	asset, err := expansion.NewAsset(expansion.Manifest{
		CartSHA256:     fmt.Sprintf("%x", sum),
		CartSize:       int64(len(cart)),
		Device:         expansion.Device,
		Format:         1,
		Map:            expansion.Map,
		RecipeSHA256:   strings.Repeat("b", 64),
		Revision:       strings.Repeat("c", 40),
		ShellBuildID:   strings.Repeat("d", 32),
		ShellPackageID: packageID,
		ShellSHA256:    strings.Repeat("e", 64),
		Slot:           expansion.Slot,
		SlotMajor:      1,
	}, cart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportCoreExpansion(ctx, asset); err != nil {
		t.Fatal(err)
	}
	service := &Service{catalog: store}
	mediaID, err := meshcontent.FromSHA256(media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if !service.MeshContentAdvertises(ctx, mediaID) {
		t.Fatal("core media was not advertised")
	}
	reader, err := service.OpenMeshContent(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("core media %q err %v", got, err)
	}
	cartID, err := meshcontent.FromSHA256(fmt.Sprintf("%x", sum))
	if err != nil {
		t.Fatal(err)
	}
	if !service.MeshContentAdvertises(ctx, cartID) {
		t.Fatal("expansion cart was not advertised")
	}
	reader, err = service.OpenMeshContent(ctx, cartID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, cart) {
		t.Fatalf("cart len %d err %v", len(got), err)
	}
	missing := meshcontent.SumSHA256([]byte("library-rom"))
	if service.MeshContentAdvertises(ctx, missing) {
		t.Fatal("unknown digest was advertised")
	}
	if _, err := service.OpenMeshContent(ctx, missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing open err %v", err)
	}
}
