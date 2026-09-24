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
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
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
