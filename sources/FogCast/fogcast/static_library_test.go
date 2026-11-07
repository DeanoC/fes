package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// Synthetic shared conformance bytes: these tests never program hardware.
func staticLibraryArchive(t *testing.T, socket bool) []byte {
	t.Helper()
	root := "../corepackage/testdata/core-bundle-v5/"
	read := func(n string) []byte {
		t.Helper()
		b, err := os.ReadFile(root + n)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	manifest := read("manifests/valid-basic.toml")
	if socket {
		manifest = append(manifest, []byte("\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 2\nminor = 0\nrequired = false\n")...)
	}
	directory := t.TempDir()
	for _, m := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", read("payloads/base.rbf")}, {"persona.rbf", read("payloads/persona.rbf")}} {
		if err := os.WriteFile(filepath.Join(directory, m.name), m.data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
func staticLibraryAsset(t *testing.T, packageID string) expansion.Asset {
	t.Helper()
	root := "../corepackage/testdata/core-bundle-v5/expansions/ram16k/"
	b, err := os.ReadFile(root + "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m expansion.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m.ShellPackageID = packageID
	payload, err := os.ReadFile(root + "cart.rbf")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := expansion.NewAsset(m, payload)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

type staticLibraryClient struct {
	*defaultMediaPackageClient
	root                 string
	inspection           corepackage.Inspection
	raw                  []byte
	calls                int
	refuse, wrongReceipt bool
	receipts             []expansion.StaticComposition
}

func (c *staticLibraryClient) Stop(context.Context) (protocol.Status, error) {
	c.stopCalls++
	c.statusResult = protocol.Status{State: protocol.StateIdle, Development: true}
	return c.statusResult, nil
}
func (c *staticLibraryClient) LoadComposedCore(ctx context.Context, n int64, r io.Reader, id string) (protocol.Status, error) {
	if c.statusResult.State == protocol.StateActive {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "Stop is required before static loading"}
	}
	c.calls++
	staged, err := corepackage.StageStaticComposition(ctx, c.root, n, r)
	if err != nil {
		return protocol.Status{}, err
	}
	defer staged.Cleanup()
	if staged.PackageID != id || staged.StaticComposition == nil {
		return protocol.Status{}, errors.New("wrong static selection")
	}
	base, err := os.ReadFile(staged.PayloadPath)
	if err != nil {
		return protocol.Status{}, err
	}
	// The base stays the sealed core image, never a host-linked monolithic RBF.
	original, err := corepackage.ArchivePayload(c.raw)
	if err != nil || !bytes.Equal(base, original) {
		return protocol.Status{}, errors.New("sealed base changed")
	}
	if c.refuse {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Phase: "compatibility", Message: "static programming backend is unavailable"}
	}
	receipt := *staged.StaticComposition
	c.receipts = append(c.receipts, receipt)
	if c.wrongReceipt {
		empty, err := corepackage.ComposeStaticArchive(c.raw, nil)
		if err != nil {
			return protocol.Status{}, err
		}
		receipt = empty.Composition
	}
	status := coreEntryActiveStatus(c.inspection, uint64(c.calls), false)
	status.CorePackage.CoreID = c.inspection.Descriptor.Core.ID
	status.CorePackage.PersistenceMode = "volatile"
	status.CorePackage.StaticComposition = &receipt
	c.statusResult = status
	return status, nil
}
func newStaticLibrary(t *testing.T, socket bool) (*Service, *staticLibraryClient, catalog.CoreEntry, corepackage.Inspection) {
	t.Helper()
	raw := staticLibraryArchive(t, socket)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Static ZX81")
	peer := &staticLibraryClient{defaultMediaPackageClient: base, root: t.TempDir(), inspection: inspection, raw: raw}
	s.targetClients[s.selectedTarget] = peer
	base.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		t.Fatal("static package used plain or ROM-initialized load")
		return protocol.Status{}, nil
	}
	return s, peer, entry, inspection
}
func importStaticLibraryAsset(t *testing.T, s *Service, asset expansion.Asset) {
	t.Helper()
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCoreExpansion(context.Background(), int64(archive.Len()), &archive); err != nil {
		t.Fatal(err)
	}
}
func TestStaticLibraryDefaultSelectedAndClearedUseNormalColdLoad(t *testing.T) {
	ctx := context.Background()
	s, peer, entry, inspection := newStaticLibrary(t, true)
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || response.Status.CorePackage == nil || response.Status.CorePackage.StaticComposition == nil {
		t.Fatalf("default launch: %+v %v", response, err)
	}
	empty := *response.Status.CorePackage.StaticComposition
	if empty.HardwareID != expansion.EmptyPersonaID || empty.ExpansionID != "" {
		t.Fatal("default persona lost", empty)
	}
	asset := staticLibraryAsset(t, inspection.PackageID)
	importStaticLibraryAsset(t, s, asset)
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, inspection.PackageID, "", asset.ID); err != nil {
		t.Fatal(err)
	}
	// Changing saved selection is next-launch-only and does not touch live state.
	if peer.calls != 1 || *peer.statusResult.CorePackage.StaticComposition != empty {
		t.Fatal("saved selection changed running persona")
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || peer.calls != 1 || peer.stopCalls != 0 {
		t.Fatalf("active load did not preserve session: %v", err)
	}
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !comps[entry.GameID].ExpansionReady {
		t.Fatalf("readiness: %+v %v", comps, err)
	}
	response, err = s.Launch(ctx, entry.GameID, nil)
	if err != nil || response.Status.CorePackage == nil || response.Status.CorePackage.StaticComposition == nil {
		t.Fatalf("selected launch: %+v %v", response, err)
	}
	selected := *response.Status.CorePackage.StaticComposition
	if selected.HardwareID != expansion.RAM16KPersonaID || selected.ExpansionID != asset.ID || selected.ID == empty.ID {
		t.Fatal("selected receipt lost", selected)
	}
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, inspection.PackageID, asset.ID, ""); err != nil {
		t.Fatal(err)
	}
	response, err = s.Launch(ctx, entry.GameID, nil)
	if err != nil || response.Status.CorePackage == nil || response.Status.CorePackage.StaticComposition == nil || *response.Status.CorePackage.StaticComposition != empty || peer.calls != 3 {
		t.Fatalf("cleared launch: %+v %v", response, err)
	}
}
func TestStaticLibraryRejectsUndeclaredSocketAndChangedRoot(t *testing.T) {
	for _, socket := range []bool{false, true} {
		t.Run(map[bool]string{false: "undeclared-socket", true: "changed-frozen-root"}[socket], func(t *testing.T) {
			s, peer, _, inspection := newStaticLibrary(t, socket)
			asset := staticLibraryAsset(t, inspection.PackageID)
			if socket {
				asset.Manifest.Partial.RootQDBSHA256 = asset.Manifest.Partial.InterfaceSHA256
				var err error
				asset, err = expansion.NewAsset(asset.Manifest, asset.Cart)
				if err != nil {
					t.Fatal(err)
				}
			}
			var archive bytes.Buffer
			if err := asset.Write(&archive); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ImportCoreExpansion(context.Background(), int64(archive.Len()), &archive); err == nil || peer.calls != 0 || peer.stopCalls != 0 {
				t.Fatal("invalid selection imported", err)
			}
		})
	}
}
func TestStaticLibraryBindsReplyAndDoesNotFallbackOnUnavailableBackend(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable-backend", true: "different-valid-persona"}[wrong], func(t *testing.T) {
			ctx := context.Background()
			s, peer, entry, inspection := newStaticLibrary(t, true)
			asset := staticLibraryAsset(t, inspection.PackageID)
			importStaticLibraryAsset(t, s, asset)
			if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, inspection.PackageID, "", asset.ID); err != nil {
				t.Fatal(err)
			}
			peer.refuse = !wrong
			peer.wrongReceipt = wrong
			if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || peer.calls != 1 {
				t.Fatalf("unaccepted launch: %v calls=%d", err, peer.calls)
			}
			if wrong && peer.stopCalls != 1 {
				t.Fatalf("wrong persona not retired: %d", peer.stopCalls)
			}
			if !wrong && peer.stopCalls != 0 {
				t.Fatal("pre-admission refusal stopped existing state")
			}
		})
	}
}

func TestStaticLibrarySealedFirmwareCannotUseHouseholdUploadOrAnotherRoot(t *testing.T) {
	ctx := context.Background()
	s, peer, entry, inspection := newStaticLibrary(t, true)
	firmware, _, err := s.ImportCoreMedia(ctx, 8192, bytes.NewReader(bytes.Repeat([]byte{0xff}, 8192)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, firmware.MediaID); err != nil {
		t.Fatal(err)
	}
	store := s.catalog.(coreFirmwareCatalog)
	required, err := store.CreateFirmwareRequiredEntry(ctx, "Unsealed old setup", entry.CoreID, entry.PackageID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{required.GameID})
	if err != nil || comps[required.GameID].FirmwareReady {
		t.Fatal("household object filled sealed firmware", comps, err)
	}
	if _, err := s.Launch(ctx, required.GameID, nil); err == nil || peer.calls != 0 || peer.stopCalls != 0 {
		t.Fatal("late firmware request reached target", err)
	}
	candidate := inspection
	candidate.PackageID = "different-static-root"
	candidate.Descriptor.Interfaces = append(append([]corepackage.Interface{}, inspection.Descriptor.Interfaces...), corepackage.Interface{ID: "fes.video.session-display", Major: 1, Required: true}, corepackage.Interface{ID: "fes.memory.hps-ddr", Major: 1, Required: true}, corepackage.Interface{ID: "fes.media.zx81-tape", Major: 1, Required: true})
	if !zx81SessionDisplayDescriptor(candidate.Descriptor, nil) {
		t.Fatal("candidate must otherwise qualify for selector substitution")
	}
	if choices := zx81SessionDisplayCandidates([]corepackage.Inspection{candidate}, inspection.PackageID, inspection.Descriptor); len(choices) != 0 {
		t.Fatal("sealed static root was substituted", choices)
	}
}
