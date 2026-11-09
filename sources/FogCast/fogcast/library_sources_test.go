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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type remoteSourceTarget struct {
	*defaultMediaPackageClient
	insertions int
	inserted   []byte
	binding    protocol.MediaUnitBinding
	unitStatus protocol.Status
}

func (c *remoteSourceTarget) InsertMedia(_ context.Context, size int64, body io.Reader, binding protocol.MediaUnitBinding) (protocol.Status, error) {
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		return protocol.Status{}, errors.New("bad cassette transfer")
	}
	c.insertions++
	c.inserted = data
	c.binding = binding
	c.statusResult = c.unitStatus
	return c.unitStatus, nil
}

func (c *remoteSourceTarget) EjectMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error) {
	return protocol.Status{}, errors.New("unexpected cassette eject")
}

type remoteSourceFixture struct {
	service       *Service
	store         *catalog.Store
	target        *remoteSourceTarget
	entry         catalog.CoreEntry
	inspection    corepackage.Inspection
	payload       []byte
	mediaID       string
	firmwareID    string
	server        *httptest.Server
	objects       atomic.Int32
	listing       atomic.Int32
	objectHandler func(http.ResponseWriter, *http.Request)
}

func newRemoteSourceFixture(t *testing.T, payload []byte, handler func(http.ResponseWriter, *http.Request)) *remoteSourceFixture {
	t.Helper()
	ctx := context.Background()
	archive, firmware := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, false, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x42}, zx81MachineROMBytes))
	s, base, _, inspection := newCoreEntryLaunchFixture(t, archive, "Existing machine", 30*time.Second)
	// The generic fixture injects a client but leaves its target disabled.
	// Remote-source preparation requires an enabled configured target.
	s.targets = []TargetConfig{{Name: s.selectedTarget, Enabled: true}}
	store := s.catalog.(*catalog.Store)
	fixture := &remoteSourceFixture{service: s, store: store, payload: payload, mediaID: fmt.Sprintf("%x", sha256.Sum256(payload)), inspection: inspection, objectHandler: handler}
	fw, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	fixture.firmwareID = fw.MediaID
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries":
			fixture.listing.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []catalog.CoreEntry{{GameID: "remote-game", Title: "Guess Number", CoreID: "fes.zx81", PackageID: inspection.PackageID, MediaRole: "cassette", MediaID: fixture.mediaID, MediaName: "guess.p"}}})
		case "/api/v1/mesh/content/object":
			fixture.objects.Add(1)
			if r.URL.Query().Get("id") != "sha256:"+fixture.mediaID {
				http.Error(w, "bad digest", 400)
				return
			}
			if fixture.objectHandler != nil {
				fixture.objectHandler(w, r)
			} else {
				_, _ = w.Write(payload)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.server.Close)
	s.librarySources = []LibrarySourceConfig{{ID: "home", URL: fixture.server.URL}}
	result, err := s.RefreshLibrarySource(ctx, "home")
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("refresh = %+v, %v", result, err)
	}
	fixture.entry = result.Entries[0]
	if _, err := s.SelectCoreEntryROM(ctx, fixture.entry.GameID, fixture.entry.PackageID, "machine-rom", "", fw.MediaID); err != nil {
		t.Fatal(err)
	}
	status := coreEntryActiveStatus(inspection, 7, false)
	status.CorePackage.Gamepad = false
	status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.zx81-tape", Major: 1}}
	status.CorePackage.ROMLink = &corepackage.ROMLinkIdentity{ROMID: inspection.Descriptor.ROM.ID, Role: inspection.Descriptor.ROM.Role,
		MapSHA256: inspection.Descriptor.ROM.SHA256, SourceSHA256: fw.MediaID, SourceSize: int64(len(firmware)),
		ProgrammedSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("linked"))), ProgrammedSize: 6}
	status.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Zx81TapeInterface(), MinBytes: 1, MaxBytes: 16384, ChunkBytes: 512, State: "empty"}}
	ready := status
	readyPackage := *status.CorePackage
	readyPackage.MediaUnits = append([]protocol.MediaUnitStatus(nil), status.CorePackage.MediaUnits...)
	readyPackage.MediaUnits[0].State = "ready"
	ready.CorePackage = &readyPackage
	target := &remoteSourceTarget{defaultMediaPackageClient: base, unitStatus: ready}
	base.coreLoad = func(_ context.Context, size int64, reader io.Reader) (protocol.Status, error) {
		body, err := io.ReadAll(reader)
		if err != nil || int64(len(body)) != size || !bytes.Contains(body, firmware) {
			return protocol.Status{}, errors.New("bad core transfer or missing local firmware")
		}
		base.statusResult = status
		return status, nil
	}
	s.targetClients[s.selectedTarget] = target
	s.pairedTargetClients[s.selectedTarget] = target
	fixture.target = target
	return fixture
}

func TestRemoteSourceIndexesThenLaunchesAndReplaysOffline(t *testing.T) {
	ctx := context.Background()
	f := newRemoteSourceFixture(t, []byte("a ZX81 tape"), nil)
	if f.objects.Load() != 0 {
		t.Fatal("indexing downloaded tape")
	}
	entry, err := f.store.CoreEntry(ctx, f.entry.GameID)
	if err != nil || entry.MediaID != "" || entry.Title != "Guess Number" {
		t.Fatalf("indexed entry = %+v, %v", entry, err)
	}
	origin, found, err := f.store.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || !found || !origin.Pending || origin.MediaID != f.mediaID || origin.SourceID != "home" {
		t.Fatalf("origin = %+v, %v, %v", origin, found, err)
	}
	page, err := f.store.QueryGames(ctx, catalog.Query{Platform: catalog.CorePlatform, Limit: 20})
	visible := false
	for _, game := range page.Games {
		visible = visible || game.ID == entry.GameID
	}
	if err != nil || !visible {
		t.Fatalf("indexed title not visible: %+v, %v", page, err)
	}
	if _, err := f.service.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if f.objects.Load() != 1 || f.target.coreCalls != 1 || f.target.insertions != 1 || !bytes.Equal(f.target.inserted, f.payload) || f.target.binding.PackageID != entry.PackageID || f.target.binding.Generation != 7 || f.target.binding.Unit != 0 {
		t.Fatalf("first launch requests=%d cores=%d inserts=%d payload=%q", f.objects.Load(), f.target.coreCalls, f.target.insertions, f.target.inserted)
	}
	bound, err := f.store.CoreEntry(ctx, entry.GameID)
	if err != nil || bound.MediaID != f.mediaID || bound.MediaRole != "cassette" || bound.MediaName != "guess.p" {
		t.Fatalf("bound = %+v, %v", bound, err)
	}
	f.server.Close()
	if _, err := f.service.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("offline replay: %v", err)
	}
	if f.objects.Load() != 1 || f.target.coreCalls != 2 || f.target.insertions != 2 {
		t.Fatalf("offline replay requests=%d cores=%d inserts=%d", f.objects.Load(), f.target.coreCalls, f.target.insertions)
	}
}

func TestRemoteSourceRejectedMediaPreservesRunningOwner(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		code int
	}{
		{"wrong hash", []byte("different tape"), 200},
		{"truncated", []byte("a ZX81"), 200},
		{"oversize", bytes.Repeat([]byte{1}, 16385), 200},
		{"unavailable", nil, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRemoteSourceFixture(t, []byte("a ZX81 tape"), func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write(tc.body)
			})
			prior := targetPlay{execution: ExecutionFPGANative, gameID: "already-running", system: catalog.CorePlatform}
			f.service.plays[f.service.selectedTarget] = prior
			f.service.activeTarget = f.service.selectedTarget
			f.service.activeExecution = ExecutionFPGANative
			f.service.activeGameID = prior.gameID
			f.target.statusResult = protocol.Status{State: protocol.StateActive, GameID: &prior.gameID}
			_, launchErr := f.service.Launch(context.Background(), f.entry.GameID, nil)
			var apiErr *protocol.APIError
			if !errors.As(launchErr, &apiErr) || apiErr.Code != protocol.CodeSourceUnavailable {
				t.Fatalf("bad source error = %v", launchErr)
			}
			if f.target.coreCalls != 0 || f.target.stopCalls != 0 || f.target.insertions != 0 || f.service.plays[f.service.selectedTarget] != prior || f.service.activeGameID != prior.gameID || f.service.activeExecution != prior.execution || f.target.statusResult.State != protocol.StateActive || f.target.statusResult.GameID == nil || *f.target.statusResult.GameID != prior.gameID {
				t.Fatalf("failed fetch changed owner or target: loads=%d stops=%d inserts=%d play=%+v", f.target.coreCalls, f.target.stopCalls, f.target.insertions, f.service.plays[f.service.selectedTarget])
			}
			entry, err := f.store.CoreEntry(context.Background(), f.entry.GameID)
			origin, _, originErr := f.store.RemoteCoreEntry(context.Background(), f.entry.GameID)
			if err != nil || originErr != nil || entry.MediaID != "" || !origin.Pending {
				t.Fatalf("failed fetch selected media: %+v %+v %v %v", entry, origin, err, originErr)
			}
			if _, err := f.store.CoreMediaInfo(context.Background(), f.mediaID); !errors.Is(err, catalog.ErrCoreMediaNotFound) {
				t.Fatalf("unexpected expected-content import: %v", err)
			}
			if len(tc.body) != 0 {
				wrongID := fmt.Sprintf("%x", sha256.Sum256(tc.body))
				if _, err := f.store.CoreMediaInfo(context.Background(), wrongID); !errors.Is(err, catalog.ErrCoreMediaNotFound) {
					t.Fatalf("rejected source bytes were imported: %v", err)
				}
			}
		})
	}
}

func TestRemoteSourceUnavailableRefreshRetainsIndexedTitle(t *testing.T) {
	f := newRemoteSourceFixture(t, []byte("tape"), nil)
	f.server.Close()
	if _, err := f.service.RefreshLibrarySource(context.Background(), "home"); err == nil {
		t.Fatal("offline refresh unexpectedly succeeded")
	}
	entry, origin, found, err := f.store.RemoteCoreEntryBySource(context.Background(), "home", "remote-game")
	if err != nil || !found || entry != f.entry || !origin.Pending || origin.MediaID != f.mediaID {
		t.Fatalf("failed refresh changed indexed title: %+v %+v %v %v", entry, origin, found, err)
	}
	if f.objects.Load() != 0 {
		t.Fatal("refresh attempted media download")
	}
}

func TestRemoteSourceUserSelectionWinsWhileDownloadInFlight(t *testing.T) {
	arrived, resume := make(chan struct{}), make(chan struct{})
	payload := []byte("source tape")
	f := newRemoteSourceFixture(t, payload, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		select {
		case <-resume:
			_, _ = w.Write(payload)
		case <-r.Context().Done():
		}
	})
	done := make(chan error, 1)
	go func() { _, err := f.service.Launch(context.Background(), f.entry.GameID, nil); done <- err }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("download did not start")
	}
	// Direct catalog mutation models an independent user selection committed
	// while the remote transfer is in progress. Its no-op value alone would
	// pass an ordinary package/media CAS; the pending marker must reject it.
	_, err := f.store.SelectCoreEntryMedia(context.Background(), f.entry.GameID, f.entry.PackageID, "", "", "")
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("launch overwrote intervening selection")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launch did not finish")
	}
	entry, err := f.store.CoreEntry(context.Background(), f.entry.GameID)
	origin, _, originErr := f.store.RemoteCoreEntry(context.Background(), f.entry.GameID)
	if err != nil || originErr != nil || entry.MediaID != "" || origin.Pending || f.target.coreCalls != 0 || f.target.insertions != 0 {
		t.Fatalf("intervening selection lost: entry=%+v origin=%+v core=%d insert=%d errs=%v/%v", entry, origin, f.target.coreCalls, f.target.insertions, err, originErr)
	}
}

func TestRemoteSourceCanceledDownloadDoesNotBindOrLaunch(t *testing.T) {
	arrived := make(chan struct{})
	f := newRemoteSourceFixture(t, []byte("tape"), func(_ http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.service.Launch(ctx, f.entry.GameID, nil); done <- err }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("download did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled launch succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled launch did not finish")
	}
	entry, err := f.store.CoreEntry(context.Background(), f.entry.GameID)
	origin, _, originErr := f.store.RemoteCoreEntry(context.Background(), f.entry.GameID)
	if err != nil || originErr != nil || entry.MediaID != "" || !origin.Pending || f.target.coreCalls != 0 || f.target.insertions != 0 {
		t.Fatalf("canceled download changed state: entry=%+v origin=%+v core=%d insert=%d errs=%v/%v", entry, origin, f.target.coreCalls, f.target.insertions, err, originErr)
	}
}

func TestRemoteSourceFailureDoesNotBindExplicitTarget(t *testing.T) {
	f := newRemoteSourceFixture(t, []byte("tape"), func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	s := f.service
	original := s.selectedTarget
	s.targets = []TargetConfig{{Name: original, Enabled: true}, {Name: "spare", Enabled: true}}
	s.targetClients["spare"] = f.target
	s.activeTarget, s.boundKitTarget = original, original
	_, err := s.LaunchOn(context.Background(), f.entry.GameID, "spare", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeSourceUnavailable {
		t.Fatalf("source error = %v", err)
	}
	if s.activeTarget != original || s.boundKitTarget != original || f.target.coreCalls != 0 {
		t.Fatalf("failed source rebound target: active=%q bound=%q loads=%d", s.activeTarget, s.boundKitTarget, f.target.coreCalls)
	}
}

func TestRemoteSourceTargetDriftDuringFetchFailsClosed(t *testing.T) {
	for _, withMesh := range []bool{false, true} {
		t.Run(fmt.Sprintf("mesh=%t", withMesh), func(t *testing.T) {
			var f *remoteSourceFixture
			f = newRemoteSourceFixture(t, []byte("tape"), func(w http.ResponseWriter, _ *http.Request) {
				f.service.targetMu.Lock()
				f.service.selectedTarget = "spare"
				f.service.targetMu.Unlock()
				_, _ = w.Write([]byte("tape"))
			})
			s := f.service
			original := s.selectedTarget
			s.targets = []TargetConfig{{Name: original, Enabled: true, TargetID: "node-a"}, {Name: "spare", Enabled: true, TargetID: "node-b"}}
			s.targetClients["spare"] = f.target
			s.activeTarget, s.boundKitTarget = original, original
			if withMesh {
				s.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "node-a", Executor: &meshLaunchExecutor{node: "node-a"}, Entry: func(string) (meshcontent.Entry, bool) { return meshcontent.Entry{}, false }})
			}
			_, err := s.Launch(context.Background(), f.entry.GameID, nil)
			if !errors.Is(err, ErrLaunchSnapshot) || f.target.coreCalls != 0 || s.activeTarget != original || s.boundKitTarget != original {
				t.Fatalf("target drift = %v loads=%d active=%q bound=%q", err, f.target.coreCalls, s.activeTarget, s.boundKitTarget)
			}
		})
	}
}

func TestRemoteSourceMeshEnsureUsesFetchedCassette(t *testing.T) {
	ctx := context.Background()
	f := newRemoteSourceFixture(t, []byte("tape"), nil)
	s := f.service
	s.targets = []TargetConfig{{Name: s.selectedTarget, Enabled: true, TargetID: "node-a"}}
	f.target.meshLeaseGeneration = "owned-grant"
	cassette, err := meshcontent.FromSHA256(f.mediaID)
	if err != nil {
		t.Fatal(err)
	}
	exec := &meshLaunchExecutor{node: "node-a", held: map[string]meshcontent.SlotState{cassette.String(): meshcontent.StatePresent}, abis: []meshcontent.EligibleABI{{ID: "fes.computer", Major: 1}}}
	s.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "node-a", Executor: exec, Entry: s.meshCatalogEntry})
	if _, err := s.Launch(ctx, f.entry.GameID, nil); err != nil {
		t.Fatalf("mesh launch: %v", err)
	}
	if f.target.coreCalls != 1 || f.target.insertions != 1 || f.objects.Load() != 1 || len(exec.pulls) != 0 {
		t.Fatalf("mesh launch source=%d core=%d cassette=%d pulls=%+v", f.objects.Load(), f.target.coreCalls, f.target.insertions, exec.pulls)
	}
}

func TestRemoteSourceFirmwareChangeDuringFetchFailsClosed(t *testing.T) {
	for _, withMesh := range []bool{false, true} {
		t.Run(fmt.Sprintf("mesh=%t", withMesh), func(t *testing.T) {
			ctx := context.Background()
			f := newRemoteSourceFixture(t, []byte("tape"), nil)
			replacement := bytes.Repeat([]byte{0x33}, zx81MachineROMBytes)
			media, _, err := f.service.ImportCoreMedia(ctx, int64(len(replacement)), bytes.NewReader(replacement))
			if err != nil {
				t.Fatal(err)
			}
			f.objectHandler = func(w http.ResponseWriter, _ *http.Request) {
				_, err := f.store.SelectCoreEntryROM(ctx, f.entry.GameID, f.entry.PackageID, "machine-rom", zx81MachineROMBytes, f.firmwareID, media.MediaID)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				_, _ = w.Write(f.payload)
			}
			if withMesh {
				f.service.targets = []TargetConfig{{Name: f.service.selectedTarget, Enabled: true, TargetID: "node-a"}}
				f.service.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "node-a", Executor: &meshLaunchExecutor{node: "node-a"}, Entry: f.service.meshCatalogEntry})
			}
			_, err = f.service.Launch(ctx, f.entry.GameID, nil)
			if !errors.Is(err, ErrLaunchSnapshot) || f.target.coreCalls != 0 || f.target.insertions != 0 {
				t.Fatalf("firmware change = %v core=%d cassette=%d", err, f.target.coreCalls, f.target.insertions)
			}
			entry, err := f.store.CoreEntry(ctx, f.entry.GameID)
			if err != nil || entry.MediaID != "" {
				t.Fatalf("firmware drift bound cassette: %+v %v", entry, err)
			}
		})
	}
}

func TestRemoteSourceMeshSessionChangeDuringFetchFailsClosed(t *testing.T) {
	for _, withMesh := range []bool{false, true} {
		t.Run(fmt.Sprintf("mesh=%t", withMesh), func(t *testing.T) {
			f := newRemoteSourceFixture(t, []byte("tape"), nil)
			s := f.service
			s.targets = []TargetConfig{{Name: s.selectedTarget, Enabled: true, TargetID: "node-a"}}
			if withMesh {
				s.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "node-a", Executor: &meshLaunchExecutor{node: "node-a"}, Entry: s.meshCatalogEntry})
			}
			f.objectHandler = func(w http.ResponseWriter, _ *http.Request) {
				s.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "other-node", Executor: &meshLaunchExecutor{node: "other-node"}, Entry: s.meshCatalogEntry})
				_, _ = w.Write(f.payload)
			}
			_, err := s.Launch(context.Background(), f.entry.GameID, nil)
			if !errors.Is(err, ErrLaunchSnapshot) || f.target.coreCalls != 0 || f.target.insertions != 0 {
				t.Fatalf("mesh session change = %v core=%d cassette=%d", err, f.target.coreCalls, f.target.insertions)
			}
		})
	}
}

func TestRemoteSourcePackageAndExpansionChangeDuringFetchFailsClosed(t *testing.T) {
	for _, kind := range []string{"package", "expansion"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newRemoteSourceFixture(t, []byte("tape"), nil)
			var expansionID string
			if kind == "expansion" {
				cart := bytes.Repeat([]byte{0x55}, 45000)
				asset, err := expansion.NewAsset(expansion.Manifest{
					CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)),
					Device: expansion.Device, Format: 1, Map: expansion.Map,
					RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40),
					ShellBuildID: strings.Repeat("d", 32), ShellPackageID: f.entry.PackageID,
					ShellSHA256: strings.Repeat("e", 64), Slot: expansion.Slot, SlotMajor: 1,
				}, cart)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.ImportCoreExpansion(ctx, asset); err != nil {
					t.Fatal(err)
				}
				expansionID = asset.ID
			}
			f.objectHandler = func(w http.ResponseWriter, _ *http.Request) {
				var err error
				if kind == "package" {
					_, err = f.store.SelectCoreEntry(ctx, f.entry.GameID, f.entry.CoreID, f.entry.PackageID, strings.Repeat("a", 64))
				} else {
					_, err = f.store.SelectCoreEntryExpansion(ctx, f.entry.GameID, f.entry.PackageID, "", expansionID)
				}
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				_, _ = w.Write(f.payload)
			}
			_, err := f.service.Launch(ctx, f.entry.GameID, nil)
			if !errors.Is(err, ErrLaunchSnapshot) || f.target.coreCalls != 0 || f.target.insertions != 0 {
				t.Fatalf("%s changed: err=%v core=%d cassette=%d", kind, err, f.target.coreCalls, f.target.insertions)
			}
			entry, err := f.store.CoreEntry(ctx, f.entry.GameID)
			if err != nil || entry.MediaID != "" {
				t.Fatalf("drift bound cassette: %+v %v", entry, err)
			}
		})
	}
}

func TestRemoteSourceEntryOnlySessionWithoutMeshExecutor(t *testing.T) {
	f := newRemoteSourceFixture(t, []byte("tape"), nil)
	f.service.SetMeshExecuteSession(MeshExecuteSession{Entry: f.service.meshCatalogEntry})
	if _, err := f.service.Launch(context.Background(), f.entry.GameID, nil); err != nil {
		t.Fatalf("entry-only launch: %v", err)
	}
	if f.target.coreCalls != 1 || f.target.insertions != 1 {
		t.Fatalf("core=%d cassette=%d", f.target.coreCalls, f.target.insertions)
	}
}
