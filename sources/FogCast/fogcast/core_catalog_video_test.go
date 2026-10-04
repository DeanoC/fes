package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corecatalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func publishedVideoFixture(t *testing.T, layout string) (*Service, *defaultMediaPackageClient, corepackage.Inspection, []byte, []expansion.Asset, Paths) {
	t.Helper()
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, layout)
	staged, err := corepackage.Stage(ctx, t.TempDir(), int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	inspection := corepackage.Inspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor}
	root := t.TempDir()
	paths := Paths{Index: filepath.Join(root, "library.db"), CorePackages: filepath.Join(root, "installed")}
	store, err := catalog.OpenContext(ctx, paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	packages, err := corepackage.NewStore(paths.CorePackages)
	if err != nil {
		t.Fatal(err)
	}
	client := &defaultMediaPackageClient{packageLibraryClient: &packageLibraryClient{
		fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}},
		inspection:        protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true},
	}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: 30 * time.Second}, paths, store,
		&fakeServiceScanner{}, &fakeServicePreparer{}, client, WithLibraryOverlayPath(filepath.Join(root, "library-settings.json")))
	s.corePackages = packages
	t.Cleanup(func() { _ = s.catalog.Close() })
	publication := t.TempDir()
	if err := os.WriteFile(filepath.Join(publication, "core.fcore"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	parts := make([]corepackage.FactoryVideoReference, 0, 2)
	for i, profile := range []string{"direct", "scanlines"} {
		var encoded bytes.Buffer
		if err := assets[i].Write(&encoded); err != nil {
			t.Fatal(err)
		}
		path := "core-video-parts/" + inspection.PackageID + "/" + assets[i].ID + ".tar"
		file := filepath.Join(publication, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, corepackage.FactoryVideoReference{Profile: profile, PartID: assets[i].ID,
			ArchivePath: path, ArchiveSize: int64(encoded.Len()), ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(encoded.Bytes()))})
	}
	entry := corecatalog.Entry{CoreID: "fes.coleco", Label: "ColecoVision", System: "coleco", Standing: "supported",
		PackageID: inspection.PackageID, ArchivePath: "core.fcore", ArchiveSize: int64(len(raw)),
		ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), VideoParts: parts}
	doc := map[string]any{"version": 1, "source_id": "fes-first-party", "entries": []corecatalog.Entry{entry}}
	s.coreCatalogPath = filepath.Join(publication, "catalog.json")
	s.coreLibrarySourceID = "video-publication-test"
	writePublishedVideoDocument(t, s.coreCatalogPath, doc)
	return s, client, inspection, raw, assets, paths
}

func writePublishedVideoDocument(t *testing.T, file string, doc map[string]any) {
	t.Helper()
	delete(doc, "catalog_sha256")
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Canonicalize nested structs through maps, matching the publisher's
	// sorted object keys instead of Go struct field order.
	var canonical map[string]any
	if err := json.Unmarshal(body, &canonical); err != nil {
		t.Fatal(err)
	}
	body, err = json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	canonical["catalog_sha256"] = fmt.Sprintf("%x", sha256.Sum256(body))
	body, err = json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
}

func editPublishedVideoDocument(t *testing.T, file string, edit func(map[string]any)) {
	t.Helper()
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc["entries"].([]any)[0].(map[string]any))
	writePublishedVideoDocument(t, file, doc)
}

func TestCoreCatalogVideoInstallFeedsOrdinaryPlay(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testCoreCatalogVideoInstallFeedsOrdinaryPlay(t, layout) })
	}
}

func testCoreCatalogVideoInstallFeedsOrdinaryPlay(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	s, base, inspection, raw, assets, _ := publishedVideoFixture(t, layout)
	installed, err := s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID)
	if err != nil || installed.PackageID != inspection.PackageID {
		t.Fatalf("install=%+v err=%v", installed, err)
	}
	parts, err := s.CoreVideoParts(ctx)
	if err != nil || len(parts) != 2 || parts[0].Profile != "direct" || parts[0].PartID != assets[0].ID || parts[1].Profile != "scanlines" || parts[1].PartID != assets[1].ID {
		t.Fatalf("parts=%+v err=%v", parts, err)
	}
	if base.coreCalls != 0 || base.stopCalls != 0 {
		t.Fatal("catalog install changed the active machine")
	}
	entry, err := s.CreateCoreEntry(ctx, "Factory video title", inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	bindVideoMedia(t, s, entry)
	importVideoCPUFixture(t, s, assets[2])
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", assets[2].ID); err != nil {
		t.Fatal(err)
	}
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	video, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || video.Builtin || video.PartID != assets[1].ID || video.EffectiveProfile != "scanlines" {
		t.Fatalf("resolved=%+v err=%v", video, err)
	}
	s.targets[0].Enabled = true
	s.targets[0].Address = "http://example.invalid:8182"
	s.targets[0].Agent = "test-token"
	client := &libraryVideoClient{defaultMediaPackageClient: base, root: t.TempDir(), active: coreEntryActiveStatus(inspection, 9, true)}
	client.stopFn = func(context.Context) (protocol.Status, error) {
		client.statusResult = protocol.Status{State: protocol.StateIdle}
		return client.statusResult, nil
	}
	s.targetClients[s.selectedTarget] = client
	launched, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := corepackage.ComposePartsArchive(ctx, raw, []expansion.Asset{assets[1], assets[2]})
	if err != nil {
		t.Fatal(err)
	}
	if client.partsCalls != 1 || client.coreCalls != 0 || !reflect.DeepEqual(launched.Status.CorePackage.PartsComposition, &want.Composition) {
		t.Fatalf("parts=%d core=%d status=%+v", client.partsCalls, client.coreCalls, launched.Status)
	}
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCoreCatalogVideoExistingBaseRemainsInstallableUntilCompanionsMatch(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testCoreCatalogVideoExistingBaseRemainsInstallableUntilCompanionsMatch(t, layout) })
	}
}

func testCoreCatalogVideoExistingBaseRemainsInstallableUntilCompanionsMatch(t *testing.T, layout string) {
	t.Helper()
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			ctx := context.Background()
			s, client, inspection, raw, assets, _ := publishedVideoFixture(t, layout)
			if _, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw)); err != nil {
				t.Fatal(err)
			}
			if partial {
				importVideoFixture(t, s, assets[0], "direct")
			}
			check := func(state string) {
				t.Helper()
				rows, err := s.AvailableCores(ctx)
				if err != nil || len(rows) != 1 || rows[0].ArtifactState != state ||
					rows[0].Descriptor == nil || !reflect.DeepEqual(*rows[0].Descriptor, inspection.Descriptor) {
					t.Fatalf("inventory=%+v expected=%s err=%v", rows, state, err)
				}
			}
			check("available")
			archive := filepath.Join(filepath.Dir(s.coreCatalogPath), "core.fcore")
			if err := os.Remove(archive); err != nil {
				t.Fatal(err)
			}
			check("unavailable")
			if err := os.WriteFile(archive, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID); err != nil {
				t.Fatal(err)
			}
			check("installed")
			if err := os.Remove(archive); err != nil {
				t.Fatal(err)
			}
			check("installed") // A complete cached install remains usable after publication removal.
			if client.coreCalls != 0 || client.stopCalls != 0 || client.inspections != 0 {
				t.Fatal("inventory or catalog install contacted the physical target")
			}
		})
	}
}

func TestCoreCatalogVideoRejectsCompanionsBeforeImport(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testCoreCatalogVideoRejectsCompanionsBeforeImport(t, layout) })
	}
}

func testCoreCatalogVideoRejectsCompanionsBeforeImport(t *testing.T, layout string) {
	t.Helper()
	for _, mode := range []string{"changed second archive", "wrong second part id", "CPU part", "incompatible shell", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, client, inspection, _, assets, _ := publishedVideoFixture(t, layout)
			editPublishedVideoDocument(t, s.coreCatalogPath, func(entry map[string]any) {
				part := entry["video_parts"].([]any)[1].(map[string]any)
				file := filepath.Join(filepath.Dir(s.coreCatalogPath), part["archive_path"].(string))
				switch mode {
				case "changed second archive":
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					data[len(data)/2] ^= 1
					if err := os.WriteFile(file, data, 0600); err != nil {
						t.Fatal(err)
					}
				case "wrong second part id":
					part["part_id"] = strings.Repeat("f", 64)
				case "CPU part", "incompatible shell":
					asset := assets[2]
					if mode == "incompatible shell" {
						manifest := assets[1].Manifest
						manifest.ShellBuildID = strings.Repeat("f", 32)
						var err error
						asset, err = expansion.NewAsset(manifest, assets[1].Cart)
						if err != nil {
							t.Fatal(err)
						}
					}
					var data bytes.Buffer
					if err := asset.Write(&data); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, data.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					part["part_id"], part["archive_size"], part["archive_sha256"] = asset.ID, data.Len(), fmt.Sprintf("%x", sha256.Sum256(data.Bytes()))
				case "symlink":
					if err := os.Rename(file, file+".retained"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Base(file)+".retained", file); err != nil {
						t.Fatal(err)
					}
				}
			})
			if _, err := corecatalog.Load(s.coreCatalogPath); err != nil {
				t.Fatalf("corruption fixture must retain valid catalog metadata: %v", err)
			}
			_, err := s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID)
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInvalidArchive {
				t.Fatalf("companion admission=%v", err)
			}
			packages, err := s.corePackages.List(ctx)
			if err != nil || len(packages) != 0 {
				t.Fatalf("published packages=%+v err=%v", packages, err)
			}
			parts, err := s.CoreVideoParts(ctx)
			if err != nil || len(parts) != 0 {
				t.Fatalf("published parts=%+v err=%v", parts, err)
			}
			if client.coreCalls != 0 || client.stopCalls != 0 {
				t.Fatal("invalid install reached the machine")
			}
		})
	}
}

func TestCoreCatalogVideoReopenAndIdempotencePreserveSelections(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testCoreCatalogVideoReopenAndIdempotencePreserveSelections(t, layout) })
	}
}

func testCoreCatalogVideoReopenAndIdempotencePreserveSelections(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	s, _, inspection, _, assets, paths := publishedVideoFixture(t, layout)
	if _, err := s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID); err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreEntry(ctx, "Retained factory title", inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	bindVideoMedia(t, s, entry)
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	importVideoCPUFixture(t, s, assets[2])
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", assets[2].ID); err != nil {
		t.Fatal(err)
	}
	wantEntry, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.catalog.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := catalog.OpenContext(ctx, paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	fresh := newService(Config{RequestTimeout: time.Second, UploadTimeout: 30 * time.Second}, paths, reopened,
		&fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{}, WithLibraryOverlayPath(s.libraryOverlayPath))
	fresh.coreCatalogPath, fresh.coreLibrarySourceID = s.coreCatalogPath, s.coreLibrarySourceID
	s = fresh
	t.Cleanup(func() { _ = s.catalog.Close() })
	s.corePackages, err = corepackage.NewStore(paths.CorePackages)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := s.CoreVideoParts(ctx)
	if err != nil || len(parts) != 2 || parts[1].PartID != assets[1].ID {
		t.Fatalf("parts=%+v err=%v", parts, err)
	}
	got, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || got != wantEntry {
		t.Fatalf("entry=%+v want=%+v err=%v", got, wantEntry, err)
	}
	video, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || video.PreferredProfile != profile || video.EffectiveProfile != profile || video.PartID != assets[1].ID || !video.Choices[1].Available {
		t.Fatalf("restart lost saved preference or compatible CPU/video selection: %+v %v", video, err)
	}
}

func TestCoreCatalogVideoConflictPreservesImportedProfile(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testCoreCatalogVideoConflictPreservesImportedProfile(t, layout) })
	}
}

func testCoreCatalogVideoConflictPreservesImportedProfile(t *testing.T, layout string) {
	t.Helper()
	ctx := context.Background()
	s, _, inspection, raw, assets, _ := publishedVideoFixture(t, layout)
	if _, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	want := importVideoFixture(t, s, assets[1], "direct")
	rows, err := s.AvailableCores(ctx)
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "available" || rows[0].Descriptor == nil {
		t.Fatalf("conflicting mapping cannot claim complete installation: %+v %v", rows, err)
	}
	_, err = s.InstallAvailableCore(ctx, "fes-first-party", "fes.coleco", inspection.PackageID)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeStaleRevision {
		t.Fatalf("conflict=%v", err)
	}
	parts, err := s.CoreVideoParts(ctx)
	if err != nil || !reflect.DeepEqual(parts, []catalog.CoreVideoPart{want}) {
		t.Fatalf("parts=%+v err=%v", parts, err)
	}
}
