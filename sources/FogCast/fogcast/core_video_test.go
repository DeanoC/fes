package fogcast

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// These are valid encoded FPGA fixtures, so tests exercise the actual frame
// linker and canonical transport, without depending on a local Quartus build.
func libraryVideoFixture(t *testing.T) ([]byte, []expansion.Asset) {
	return libraryVideoFixtureLayout(t, expansion.ColecoVideoLayout)
}

func libraryVideoFixtureLayout(t *testing.T, layout string) ([]byte, []expansion.Asset) {
	t.Helper()
	packed, err := os.ReadFile("../corepackage/testdata/expansion-shell.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	text := strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(payload)))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", sha)
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n"
	if layout == expansion.ColecoNativeVideoLayout {
		text = strings.ReplaceAll(text, expansion.VideoSlot, expansion.NativeVideoSlot)
	}
	var out bytes.Buffer
	for _, item := range []struct {
		name string
		data []byte
	}{{"manifest.toml", []byte(text)}, {"core.rbf", payload}} {
		h := make([]byte, 512)
		copy(h, item.name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(item.data)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = '0'
		copy(h[257:], "ustar\x00")
		copy(h[263:], "00")
		sum := 0
		for _, value := range h {
			sum += int(value)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(h)
		out.Write(item.data)
		out.Write(make([]byte, (512-len(item.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	staged, err := corepackage.Stage(context.Background(), t.TempDir(), int64(out.Len()), bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	var assets []expansion.Asset
	for i := 0; i < 3; i++ {
		slot, mapping, major := expansion.VideoSlot, expansion.ColecoVideoMap, 1
		if i == 2 {
			slot, mapping, major = expansion.ColecoSlot, expansion.ColecoMapV2, 2
		} else if layout == expansion.ColecoNativeVideoLayout {
			slot, mapping = expansion.NativeVideoSlot, expansion.ColecoNativeVideoMap
		}
		asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: sha, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1,
			Map: mapping, RecipeSHA256: strings.Repeat(fmt.Sprintf("%x", i+10), 64), Revision: strings.Repeat("c", 40), ShellBuildID: staged.Descriptor.Build.ID,
			ShellPackageID: staged.PackageID, ShellSHA256: sha, Slot: slot, SlotMajor: major}, payload)
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, asset)
	}
	return out.Bytes(), assets
}

type libraryVideoClient struct {
	*defaultMediaPackageClient
	root         string
	active       protocol.Status
	partsCalls   int
	lostResponse bool
	wrongReceipt bool
}

func (c *libraryVideoClient) LoadLibraryPartsCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, error) {
	c.partsCalls++
	staged, err := corepackage.StageParts(ctx, c.root, size, body)
	if err != nil {
		return protocol.Status{}, err
	}
	defer staged.Cleanup()
	if staged.PackageID != id || staged.PartsComposition == nil {
		return protocol.Status{}, errors.New("wrong parts identity")
	}
	status := c.active
	pkg := *status.CorePackage
	pkg.PartsComposition = staged.PartsComposition
	if c.wrongReceipt {
		tuple := *pkg.PartsComposition
		tuple.PayloadSHA256 = strings.Repeat("f", 64)
		pkg.PartsComposition = &tuple
	}
	status.CorePackage = &pkg
	c.statusResult, c.mediaStatus = status, status
	if c.lostResponse {
		return protocol.Status{}, errors.New("load response lost")
	}
	return status, nil
}

func importVideoFixture(t *testing.T, s *Service, asset expansion.Asset, profile string) catalog.CoreVideoPart {
	t.Helper()
	var buf bytes.Buffer
	if err := asset.Write(&buf); err != nil {
		t.Fatal(err)
	}
	row, err := s.ImportCoreVideoPart(context.Background(), int64(buf.Len()), &buf, profile)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func importVideoCPUFixture(t *testing.T, s *Service, asset expansion.Asset) {
	t.Helper()
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCoreExpansion(context.Background(), int64(archive.Len()), &archive); err != nil {
		t.Fatal(err)
	}
}

func bindVideoMedia(t *testing.T, s *Service, entry catalog.CoreEntry) {
	t.Helper()
	media := []byte("cartridge")
	m, _, err := s.ImportCoreMedia(context.Background(), int64(len(media)), bytes.NewReader(media))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(context.Background(), entry.GameID, entry.PackageID, "", "blob", m.MediaID); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryVideoPreferenceLaunchesExactPartsWithCPUAndMedia(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testLibraryVideoPreferenceLaunchesExactPartsWithCPUAndMedia(t, layout) })
	}
}

func testLibraryVideoPreferenceLaunchesExactPartsWithCPUAndMedia(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, layout)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Video library fixture", 30*time.Second)
	s.targets[0].Enabled = true
	s.targets[0].Address = "http://example.invalid:8182"
	s.targets[0].Agent = "test-token"
	c := &libraryVideoClient{defaultMediaPackageClient: base, root: t.TempDir(), active: coreEntryActiveStatus(inspection, 9, true)}
	c.stopFn = func(context.Context) (protocol.Status, error) {
		c.statusResult = protocol.Status{State: protocol.StateIdle}
		return c.statusResult, nil
	}
	video := importVideoFixture(t, s, assets[1], "scanlines")
	importVideoCPUFixture(t, s, assets[2])
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", assets[2].ID); err != nil {
		t.Fatal(err)
	}
	media := []byte("cartridge media")
	m, _, err := s.ImportCoreMedia(ctx, int64(len(media)), bytes.NewReader(media))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", m.MediaID); err != nil {
		t.Fatal(err)
	}
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	s.targetClients[s.selectedTarget] = c
	choices, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || choices.PartID != video.PartID || choices.Builtin || choices.EffectiveProfile != profile {
		t.Fatalf("choices=%+v err=%v", choices, err)
	}
	c.lostResponse = true
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatalf("launch: %v parts=%d core=%d stop=%d media=%d status=%+v", err, c.partsCalls, c.coreCalls, c.stopCalls, c.mediaCalls, c.statusResult)
	}
	want, err := corepackage.ComposePartsArchive(ctx, raw, []expansion.Asset{assets[1], assets[2]})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Status.CorePackage.PartsComposition, &want.Composition) || c.partsCalls != 1 || c.coreCalls != 0 {
		t.Fatalf("wrong library composition: %+v calls=%d", response.Status.CorePackage, c.partsCalls)
	}
	if !bytes.Equal(c.mediaBody, media) || c.mediaBinding.PackageID != entry.PackageID || c.mediaBinding.Generation != 9 {
		t.Fatalf("media bound to wrong generation: %+v", c.mediaBinding)
	}
	profile = "direct"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	if c.partsCalls != 1 || c.stopCalls != 0 || c.statusResult.CorePackage.PartsComposition.ID != want.Composition.ID {
		t.Fatal("preference change modified the active session")
	}
	if _, err := s.Stop(ctx); err != nil {
		t.Fatalf("stop: %v clients=%T targets=%+v", err, s.targetClients[s.selectedTarget], s.targets)
	}
	if c.stopCalls != 1 {
		t.Fatalf("Stop calls=%d", c.stopCalls)
	}
	importVideoFixture(t, s, assets[0], "direct")
	c.active = coreEntryActiveStatus(inspection, 10, true)
	response, err = s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err = corepackage.ComposePartsArchive(ctx, raw, []expansion.Asset{assets[0], assets[2]})
	if err != nil {
		t.Fatal(err)
	}
	if c.partsCalls != 2 || c.coreCalls != 0 || response.Status.CorePackage.Generation != 10 ||
		!reflect.DeepEqual(response.Status.CorePackage.PartsComposition, &want.Composition) {
		t.Fatalf("relaunch did not apply the saved preference: %+v", response.Status)
	}
}

func TestNativeVideoShellMissingOutputRejectsBeforeTargetMutation(t *testing.T) {
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, expansion.ColecoNativeVideoLayout)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Native shell")
	bindVideoMedia(t, s, entry)
	prior := coreEntryActiveStatus(inspection, 7, true)
	client.statusResult, client.mediaStatus = prior, prior
	for _, profile := range []string{"direct", "scanlines"} {
		if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || !strings.Contains(err.Error(), "requires a matching Direct video part") {
			t.Fatalf("native base launch for %s: %v", profile, err)
		}
		if client.coreCalls != 0 || client.stopCalls != 0 || client.mediaCalls != 0 || !reflect.DeepEqual(client.statusResult, prior) {
			t.Fatal("native vacant-shell rejection changed retained session or called target mutation")
		}
		video, err := s.CoreEntryVideo(ctx, entry.GameID)
		if err != nil || video.Builtin || video.PartID != "" || video.Choices[0].Available || video.Choices[0].Reason == "" || video.Choices[1].Available {
			t.Fatalf("native vacant shell advertised output: %+v %v", video, err)
		}
	}
	// A Scanlines part does not substitute for an explicitly selected Direct.
	importVideoFixture(t, s, assets[1], "scanlines")
	profile := "direct"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 || client.stopCalls != 0 || client.mediaCalls != 0 {
		t.Fatalf("selected Direct must require Direct: %v", err)
	}
}

func TestNativeVideoLibraryUsesOnlyExactShellParts(t *testing.T) {
	ctx := context.Background()
	raw, native := libraryVideoFixtureLayout(t, expansion.ColecoNativeVideoLayout)
	s, client, entry, _ := newCoreEntryLaunchFixture(t, raw, "Native video selection")
	bindVideoMedia(t, s, entry)
	raster, parts := libraryVideoFixture(t)
	if _, _, err := s.ImportCorePackage(ctx, int64(len(raster)), bytes.NewReader(raster)); err != nil {
		t.Fatal(err)
	}
	importVideoFixture(t, s, parts[0], "direct")
	video, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || video.Builtin || video.PartID != "" || video.Choices[0].Available {
		t.Fatalf("different shell supplied output: %+v %v", video, err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 || client.stopCalls != 0 || client.mediaCalls != 0 {
		t.Fatalf("different shell allowed launch: %v", err)
	}
	// Even correct package/payload/build identities cannot turn a raster
	// archive into a native part. The declared source contract must agree.
	manifest := native[0].Manifest
	manifest.Slot, manifest.Map = expansion.VideoSlot, expansion.ColecoVideoMap
	crossed, err := expansion.NewAsset(manifest, native[0].Cart)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := crossed.Write(&archive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCoreVideoPart(ctx, int64(archive.Len()), &archive, "direct"); err == nil {
		t.Fatal("native shell admitted a raster source contract")
	}
	rows, err := s.CoreVideoParts(ctx)
	if err != nil || len(rows) != 1 || rows[0].PackageID == entry.PackageID {
		t.Fatalf("rejected part changed inventory: %+v %v", rows, err)
	}
}

func TestLibraryVideoRejectsUnsupportedShellMarkers(t *testing.T) {
	for _, marker := range []string{
		"id = 'fes.fabric.video.native-pixels'\nmajor = 2\nminor = 0\nrequired = false\n",
		"id = 'fes.fabric.video.native-pixels'\nmajor = 1\nminor = 0\nrequired = true\n",
		"id = 'fes.fabric.video.native-pixels'\nmajor = 1\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = 'fes.fabric.video.raster-rgb888'\nmajor = 1\nminor = 0\nrequired = false\n",
	} {
		raw := colecoLibraryPackageContractsFixture(t, "fes.application", "\n[[interfaces]]\nid = 'fes.expansion.coleco-bus'\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\n"+marker)
		s, client, entry, _ := newCoreEntryLaunchFixture(t, raw, "Unsupported video contract")
		if _, err := s.CoreEntryVideo(context.Background(), entry.GameID); err == nil {
			t.Fatal("unsupported video contract advertised built-in output")
		}
		if _, err := s.Launch(context.Background(), entry.GameID, nil); err == nil || client.coreCalls != 0 || client.stopCalls != 0 || client.mediaCalls != 0 {
			t.Fatalf("unsupported video contract reached target mutation: %v", err)
		}
	}
}

type damagedVideoCatalog struct {
	*catalog.Store
	damaged string
}

func (c *damagedVideoCatalog) ReadCoreVideoPart(ctx context.Context, id string) (expansion.Asset, error) {
	if id == c.damaged {
		return expansion.Asset{}, catalog.ErrInvalidCoreVideoPart
	}
	return c.Store.ReadCoreVideoPart(ctx, id)
}

func TestLibraryVideoFallbackAndDamagedSelectedPartPreserveOwner(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testLibraryVideoFallbackAndDamagedSelectedPartPreserveOwner(t, layout) })
	}
}

func testLibraryVideoFallbackAndDamagedSelectedPartPreserveOwner(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, layout)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Video admission", 30*time.Second)
	bindVideoMedia(t, s, entry)
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	v, err := s.CoreEntryVideo(ctx, entry.GameID)
	builtin := layout == expansion.ColecoVideoLayout
	if err != nil || v.Builtin != builtin || v.Choices[0].Available != builtin || v.EffectiveProfile != "direct" || v.FallbackReason == "" {
		t.Fatalf("missing build fallback=%+v err=%v", v, err)
	}
	direct := importVideoFixture(t, s, assets[0], "direct")
	v, err = s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || v.Builtin || v.PartID != direct.PartID || v.FallbackReason == "" {
		t.Fatalf("part fallback=%+v err=%v", v, err)
	}
	c := &libraryVideoClient{defaultMediaPackageClient: base, root: t.TempDir(), active: coreEntryActiveStatus(inspection, 3, true)}
	s.targetClients[s.selectedTarget] = c
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	prior := c.statusResult
	s.catalog = &damagedVideoCatalog{Store: s.catalog.(*catalog.Store), damaged: direct.PartID}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil {
		t.Fatal("damaged part silently fell back")
	}
	if c.partsCalls != 1 || c.stopCalls != 0 || !reflect.DeepEqual(c.statusResult, prior) {
		t.Fatal("rejected launch modified the retained owner")
	}
	v, err = s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || v.Choices[0].Available || v.Choices[0].Reason == "" {
		t.Fatalf("damaged choice=%+v err=%v", v, err)
	}
	// An installed but damaged preferred Scanlines must not fall back
	// to a healthy Direct part either.
	scanlines := importVideoFixture(t, s, assets[1], "scanlines")
	s.catalog.(*damagedVideoCatalog).damaged = scanlines.PartID
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || c.partsCalls != 1 || c.stopCalls != 0 || !reflect.DeepEqual(c.statusResult, prior) {
		t.Fatalf("damaged preferred part substituted Direct or changed owner: %v", err)
	}
}

func TestLibraryVideoRejectsDifferentFullTupleAndCleansUp(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testLibraryVideoRejectsDifferentFullTupleAndCleansUp(t, layout) })
	}
}

func testLibraryVideoRejectsDifferentFullTupleAndCleansUp(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, layout)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Wrong video receipt", 30*time.Second)
	bindVideoMedia(t, s, entry)
	importVideoFixture(t, s, assets[0], "direct")
	c := &libraryVideoClient{defaultMediaPackageClient: base, root: t.TempDir(), active: coreEntryActiveStatus(inspection, 4, true), wrongReceipt: true}
	s.targetClients[s.selectedTarget] = c
	c.stopFn = func(context.Context) (protocol.Status, error) {
		c.statusResult = protocol.Status{State: protocol.StateIdle}
		return c.statusResult, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil {
		t.Fatal("accepted changed parts tuple")
	}
	if c.partsCalls != 1 || c.stopCalls != 1 || c.mediaCalls != 0 {
		t.Fatalf("mismatch cleanup calls parts=%d stop=%d media=%d", c.partsCalls, c.stopCalls, c.mediaCalls)
	}
}

func TestLibraryVideoUnmarkedCoreUsesBuiltinDirect(t *testing.T) {
	s, _, entry, _ := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Pong")
	profile := "scanlines"
	if err := s.PatchLibrarySettings(context.Background(), LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	v, err := s.CoreEntryVideo(context.Background(), entry.GameID)
	if err != nil || !v.Builtin || v.EffectiveProfile != "direct" || v.Choices[1].Available || v.FallbackReason == "" {
		t.Fatalf("unmarked core=%+v err=%v", v, err)
	}
}

func TestLibraryVideoSettingsPersistValidateAndPreservePartialPatches(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library-settings.json")
	newSettingsService := func() *Service {
		return newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{}, WithLibraryOverlayPath(path))
	}
	s := newSettingsService()
	if s.LibrarySettings().VideoProfile != "direct" {
		t.Fatal("default is not direct")
	}
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	seconds := 17
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{AttractIdleSeconds: &seconds}); err != nil {
		t.Fatal(err)
	}
	if got := newSettingsService().LibrarySettings(); got.VideoProfile != profile || got.AttractIdleSeconds != seconds {
		t.Fatalf("reloaded=%+v", got)
	}
	before, _ := os.ReadFile(path)
	profile = "crt"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err == nil {
		t.Fatal("unsupported profile accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || s.LibrarySettings().VideoProfile != "scanlines" {
		t.Fatal("invalid settings changed persisted preference")
	}
	if _, _, err := loadLibraryOverlay(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"video_profile":"crt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadLibraryOverlay(path); err == nil {
		t.Fatal("invalid persisted profile accepted")
	}
}
