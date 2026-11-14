package hostapi_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/misteross/expansion"
)

// Exercises the real catalog, installed shell and imported part behind the
// same grouped source-available query used by the kit client.
func TestNativeVideoReadinessInGroupedSourceAvailableQuery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("selected_target = \"none\"\nrequest_timeout_seconds = 15\nupload_timeout_seconds = 120\n[[targets]]\nname = \"none\"\nenabled = false\naddress = \"\"\nagent = \"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: config, Index: filepath.Join(root, "library.sqlite3"), Staging: filepath.Join(root, "staging"), CorePackages: filepath.Join(root, "installed")}
	service, err := fogcast.Open(ctx, paths, &http.Client{Transport: noCatalogTargetTransport{t}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	compressed, err := os.Open("../../corepackage/testdata/expansion-shell.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	_ = z.Close()
	_ = compressed.Close()
	manifest, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	text := strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(payload)))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", sha)
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.native-pixels\"\nmajor = 1\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n"
	var archive bytes.Buffer
	for _, file := range []struct {
		name string
		data []byte
	}{{"manifest.toml", []byte(text)}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, file.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(file.data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))
		archive.Write(header)
		archive.Write(file.data)
		archive.Write(make([]byte, (512-len(file.data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	inspection, _, err := service.ImportCorePackage(ctx, int64(archive.Len()), bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	store, err := catalog.OpenContext(ctx, paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry, err := store.CreateCoreEntry(ctx, "Native video", inspection.Descriptor.Core.ID, inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?grouped=1&availability=ready")
		if response.Code != http.StatusOK {
			t.Fatalf("list: %d %s", response.Code, response.Body.String())
		}
		var page struct {
			Games []hostclient.Game `json:"games"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, game := range page.Games {
			if game.ID != entry.GameID {
				continue
			}
			if !game.VideoRequired || game.VideoReady != want || game.LaunchEligible() != want {
				t.Fatalf("want ready=%v game=%+v block=%s", want, game, game.LaunchBlock())
			}
			return
		}
		t.Fatalf("source-available entry missing from list: %+v", page.Games)
	}
	check(false)
	part, err := expansion.NewAsset(expansion.Manifest{CartSHA256: sha, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1,
		Map: expansion.ColecoNativeVideoMap, RecipeSHA256: strings.Repeat("a", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID,
		ShellPackageID: inspection.PackageID, ShellSHA256: sha, Slot: expansion.NativeVideoSlot, SlotMajor: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	var partArchive bytes.Buffer
	if err := part.Write(&partArchive); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ImportCoreVideoPart(ctx, int64(partArchive.Len()), &partArchive, "direct"); err != nil {
		t.Fatal(err)
	}
	check(true)
	// A missing shell leaves its requirements unknown; do not advertise Play
	// merely because the title's old catalog row and video part still exist.
	if err := os.Remove(filepath.Join(paths.CorePackages, inspection.PackageID+".fcore")); err != nil {
		t.Fatal(err)
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?grouped=1&availability=ready")
	var page struct {
		Games []hostclient.Game `json:"games"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Games) != 1 {
		t.Fatalf("missing package list: %d %s", response.Code, response.Body.String())
	}
	if game := page.Games[0]; game.LaunchEligible() || game.LaunchBlock() != hostclient.LaunchMissingPackage {
		t.Fatalf("missing package remained eligible: %+v", game)
	}
}
