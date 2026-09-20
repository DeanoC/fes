package fogcast

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// Reuse the package fixture's payload and descriptor, changing only the
// contracts needed for media-capable package selection tests.
func serviceMediaPackageFixture(t *testing.T, version string, blob bool, extras ...string) []byte {
	t.Helper()
	raw := libraryPackageFixture(t, version, extras...)
	reader := tar.NewReader(bytes.NewReader(raw))
	var out bytes.Buffer
	offset := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		canonical := bytes.Clone(raw[offset : offset+512])
		offset += 512 + (len(body)+511)/512*512
		if header.Name == "manifest.toml" {
			body = bytes.Replace(body, []byte("fes.simple-game"), []byte("fes.simple-computer"), 1)
			if blob {
				body = bytes.Replace(body, []byte("fes.gamepad"), []byte("fes.media.blob"), 1)
			}
		}
		// Preserve the fixture's restricted ustar header, updating size/checksum.
		copy(canonical[124:136], fmt.Sprintf("%011o\x00", len(body)))
		copy(canonical[148:156], "        ")
		sum := 0
		for _, b := range canonical {
			sum += int(b)
		}
		copy(canonical[148:156], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(canonical)
		out.Write(body)
		out.Write(make([]byte, (512-len(body)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func TestServiceCoreMediaPackageUpgradePreservesSelection(t *testing.T) {
	ctx := context.Background()
	s, client, entry, first := newCoreEntryLaunchFixture(t, serviceMediaPackageFixture(t, "0.1.0", true), "Upgradeable media title")
	loads := recordServiceCoreMediaLoads(client, first)
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	oldBinding := client.mediaBinding
	raw := serviceMediaPackageFixture(t, "0.2.0", true)
	next, created, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created || next.PackageID == first.PackageID {
		t.Fatalf("replacement import: %+v created=%v err=%v", next, created, err)
	}
	selected, err := s.SelectCoreEntry(ctx, entry.GameID, entry.PackageID, next.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	want := entry
	want.PackageID = next.PackageID
	stored, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || selected != want || stored != want {
		t.Fatalf("upgrade lost entry/media state: selected=%+v stored=%+v want=%+v err=%v", selected, stored, want, err)
	}
	if *loads != 1 || client.mediaCalls != 1 || client.stopCalls != 0 || client.mediaBinding != oldBinding {
		t.Fatal("package selection mutated the active target")
	}
	active := coreEntryActiveStatus(next, 2, true)
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		*loads++
		client.statusResult, client.mediaStatus = active, active
		return active, nil
	}
	result, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := protocol.DevelopmentMediaBinding{PackageID: next.PackageID, Generation: 2, Target: "dev"}
	if *loads != 2 || client.mediaCalls != 2 || string(client.mediaBody) != "library media fixture" || client.mediaBinding != binding || result.Status.GameID == nil || *result.Status.GameID != entry.GameID {
		t.Fatalf("upgraded launch: loads=%d uploads=%d body=%q binding=%+v status=%+v", *loads, client.mediaCalls, client.mediaBody, client.mediaBinding, result.Status)
	}
}

func TestServiceCoreMediaPackageWithoutBlobRejected(t *testing.T) {
	ctx := context.Background()
	s, client, entry, first := newCoreEntryLaunchFixture(t, serviceMediaPackageFixture(t, "0.1.0", true), "Bound media title")
	loads := recordServiceCoreMediaLoads(client, first)
	raw := serviceMediaPackageFixture(t, "0.2.0", false)
	next, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if next.Descriptor.Core.ID != first.Descriptor.Core.ID || next.Descriptor.ABI != first.Descriptor.ABI {
		t.Fatal("replacement must retain core and ABI")
	}
	_, err = s.SelectCoreEntry(ctx, entry.GameID, entry.PackageID, next.PackageID)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
		t.Fatalf("missing blob selection error=%v", err)
	}
	stored, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || stored != entry {
		t.Fatalf("rejected upgrade changed entry: %+v err=%v", stored, err)
	}
	if *loads != 0 || client.mediaCalls != 0 || client.stopCalls != 0 {
		t.Fatal("rejected upgrade mutated target")
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	if *loads != 1 || client.mediaCalls != 1 || client.mediaBinding.PackageID != first.PackageID || string(client.mediaBody) != "library media fixture" {
		t.Fatal("rejected upgrade changed subsequent package/media launch")
	}
}

func TestServiceCoreMediaPackagePersistenceMismatchRejected(t *testing.T) {
	ctx := context.Background()
	s, client := coreDataService(t)
	importPackage := func(version, layout string) corepackage.Inspection {
		t.Helper()
		extra := ""
		if layout != "" {
			extra = "\n[[interfaces]]\nid = \"fes.persistence.words\"\nmajor = 1\nminor = 0\nrequired = true\n\n[[interfaces]]\nid = \"" + layout + "\"\nmajor = 1\nminor = 0\nrequired = true\n"
		}
		raw := serviceMediaPackageFixture(t, version, true, extra)
		value, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if layout != "" {
			client.layouts[value.PackageID] = &protocol.RuntimeContract{ID: layout, Major: 1}
		}
		return value
	}
	first := importPackage("0.1.0", "fes.pong.progress")
	same := importPackage("0.2.0", "fes.pong.progress")
	other := importPackage("0.3.0", "example.other")
	volatile := importPackage("0.4.0", "")
	media := importServiceCoreMedia(t, s, []byte("persistent core media"))
	entry, err := s.CreateCoreMediaEntry(ctx, "Persistent media title", first.PackageID, "blob", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	record := client.record
	for _, candidate := range []corepackage.Inspection{other, volatile} {
		_, err := s.SelectCoreEntry(ctx, entry.GameID, entry.PackageID, candidate.PackageID)
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeIncompatibleData {
			t.Fatalf("layout=%+v selection error=%v", client.layouts[candidate.PackageID], err)
		}
		stored, err := s.CoreEntry(ctx, entry.GameID)
		if err != nil || stored != entry {
			t.Fatalf("persistence rejection lost selection: %+v err=%v", stored, err)
		}
	}
	// A matching layout remains selectable with the same bound media.
	selected, err := s.SelectCoreEntry(ctx, entry.GameID, entry.PackageID, same.PackageID)
	want := entry
	want.PackageID = same.PackageID
	if err != nil || selected != want {
		t.Fatalf("matching-layout upgrade=%+v want=%+v err=%v", selected, want, err)
	}
	if client.libraryCalls != 0 || client.mutateCalls != 0 || client.stopCalls != 0 || client.record != record {
		t.Fatal("package selection changed target or persistent record")
	}
}

func importServiceCoreMedia(t *testing.T, s *Service, body []byte) catalog.CoreMedia {
	t.Helper()
	media, _, err := s.ImportCoreMedia(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return media
}

func recordServiceCoreMediaLoads(client *defaultMediaPackageClient, inspection corepackage.Inspection) *int {
	loads := new(int)
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		*loads++
		active := coreEntryActiveStatus(inspection, uint64(*loads), true)
		client.statusResult, client.mediaStatus = active, active
		return active, nil
	}
	return loads
}

func TestServiceCoreMediaDistinctTitlesUseImmutableBlobs(t *testing.T) {
	ctx := context.Background()
	s, client, first, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "First title")
	firstBytes, secondBytes := []byte("first cartridge"), []byte("second cartridge")
	firstMedia := importServiceCoreMedia(t, s, firstBytes)
	secondMedia := importServiceCoreMedia(t, s, secondBytes)
	if firstMedia.MediaID == secondMedia.MediaID {
		t.Fatal("distinct media received the same identity")
	}
	var err error
	first, err = s.SelectCoreEntryMedia(ctx, first.GameID, first.PackageID, first.MediaID, "blob", firstMedia.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateCoreMediaEntry(ctx, "Second title", first.PackageID, "blob", secondMedia.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if first.GameID == second.GameID || first.CoreID != second.CoreID || first.PackageID != second.PackageID {
		t.Fatalf("expected distinct titles on one package: first=%+v second=%+v", first, second)
	}
	// Import snapshots caller-owned buffers; later edits cannot replace either blob.
	firstBytes[0], secondBytes[0] = 'X', 'Y'
	again, created, err := s.ImportCoreMedia(ctx, int64(len("first cartridge")), strings.NewReader("first cartridge"))
	if err != nil || created || again != firstMedia {
		t.Fatalf("reimport=%+v created=%v err=%v", again, created, err)
	}
	loads := recordServiceCoreMediaLoads(client, inspection)
	for i, tc := range []struct {
		entry catalog.CoreEntry
		body  string
	}{{first, "first cartridge"}, {second, "second cartridge"}, {first, "first cartridge"}} {
		result, err := s.Launch(ctx, tc.entry.GameID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status.GameID == nil || *result.Status.GameID != tc.entry.GameID {
			t.Fatalf("launch title=%+v, want %s", result.Status.GameID, tc.entry.GameID)
		}
		wantBinding := protocol.DevelopmentMediaBinding{PackageID: first.PackageID, Generation: uint64(i + 1), Target: "dev"}
		if *loads != i+1 || client.mediaCalls != i+1 || string(client.mediaBody) != tc.body || client.mediaBinding != wantBinding {
			t.Fatalf("loads=%d uploads=%d bytes=%q binding=%+v", *loads, client.mediaCalls, client.mediaBody, client.mediaBinding)
		}
	}
}

func TestServiceCoreMediaSelectionOnlyAffectsNextLaunch(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Selectable title")
	loads := recordServiceCoreMediaLoads(client, inspection)
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	oldBody := bytes.Clone(client.mediaBody)
	oldBinding := client.mediaBinding
	replacement := importServiceCoreMedia(t, s, []byte("replacement cartridge"))
	selected, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "blob", replacement.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.GameID != entry.GameID || selected.PackageID != entry.PackageID || selected.MediaID != replacement.MediaID {
		t.Fatalf("selection=%+v", selected)
	}
	if *loads != 1 || client.mediaCalls != 1 || client.stopCalls != 0 || !bytes.Equal(client.mediaBody, oldBody) || client.mediaBinding != oldBinding {
		t.Fatal("import/selection mutated the active target")
	}
	status, err := s.Status(ctx)
	if err != nil || status.State != protocol.StateActive || status.CorePackage == nil || status.CorePackage.Generation != oldBinding.Generation || status.GameID == nil || *status.GameID != entry.GameID {
		t.Fatalf("active session changed: %+v err=%v", status, err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	if *loads != 2 || client.mediaCalls != 2 || string(client.mediaBody) != "replacement cartridge" || client.mediaBinding.Generation != 2 {
		t.Fatalf("next launch: loads=%d uploads=%d body=%q binding=%+v", *loads, client.mediaCalls, client.mediaBody, client.mediaBinding)
	}
}

// Override only the selected row to exercise launch's defensive validation of
// missing or unsupported persisted selections. All other catalog operations
// still use the fixture's real SQLite store.
type invalidServiceCoreMediaCatalog struct {
	*catalog.Store
	entry catalog.CoreEntry
}

func (c *invalidServiceCoreMediaCatalog) CoreEntry(ctx context.Context, id string) (catalog.CoreEntry, error) {
	if id == c.entry.GameID {
		return c.entry, nil
	}
	return c.Store.CoreEntry(ctx, id)
}

func TestServiceCoreMediaRejectedBeforeLoad(t *testing.T) {
	for _, name := range []string{"missing-media", "unsupported-descriptor", "unsupported-role"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			raw := colecoLibraryPackageFixture(t)
			if name == "unsupported-descriptor" {
				raw = libraryPackageFixture(t, "0.1.0")
			}
			s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Rejected title")
			media := importServiceCoreMedia(t, s, []byte("selected bytes"))
			role, id := "blob", media.MediaID
			wantCode := protocol.CodeUnsupportedOperation
			switch name {
			case "missing-media":
				id, wantCode = strings.Repeat("a", 64), protocol.CodeROMNotFound
			case "unsupported-role":
				role, wantCode = "cartridge", protocol.CodeBadRequest
			}
			loads := recordServiceCoreMediaLoads(client, inspection)
			_, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, role, id)
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != wantCode {
				t.Fatalf("selection error=%v, want %s", err, wantCode)
			}
			unchanged, err := s.CoreEntry(ctx, entry.GameID)
			if err != nil || unchanged != entry {
				t.Fatalf("rejected selection changed entry: %+v err=%v", unchanged, err)
			}
			entry.MediaRole, entry.MediaID = role, id
			s.catalog = &invalidServiceCoreMediaCatalog{Store: s.catalog.(*catalog.Store), entry: entry}
			_, err = s.Launch(ctx, entry.GameID, nil)
			apiErr = nil
			if !errors.As(err, &apiErr) || apiErr.Code != wantCode {
				t.Fatalf("launch error=%v, want %s", err, wantCode)
			}
			if *loads != 0 || client.mediaCalls != 0 || client.stopCalls != 0 {
				t.Fatalf("rejection mutated target: loads=%d media=%d stops=%d", *loads, client.mediaCalls, client.stopCalls)
			}
		})
	}
}

func TestServiceCoreMediaClearLeavesColecoPackageOnly(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco without media")
	loads := recordServiceCoreMediaLoads(client, inspection)
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.MediaID != "" || cleared.MediaRole != "" || cleared.CoreID != "fes.coleco" || cleared.GameID != entry.GameID || cleared.PackageID != entry.PackageID {
		t.Fatalf("cleared entry=%+v", cleared)
	}
	if *loads != 1 || client.mediaCalls != 1 || client.stopCalls != 0 {
		t.Fatal("clearing media mutated the active target")
	}
	for i := 0; i < 2; i++ {
		result, err := s.Launch(ctx, entry.GameID, nil)
		if err != nil || result.Status.GameID == nil || *result.Status.GameID != entry.GameID {
			t.Fatalf("package-only launch=%+v err=%v", result, err)
		}
		if client.mediaCalls != 1 || *loads != i+2 {
			t.Fatalf("default media reappeared: loads=%d media=%d", *loads, client.mediaCalls)
		}
	}
	stored, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || stored != cleared {
		t.Fatalf("launch restored cleared selection: %+v err=%v", stored, err)
	}
}

func TestServiceCoreMediaSelectionWaitsForLaunch(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Atomic selection")
	s.uploadTimeout = 5 * time.Second
	replacement := importServiceCoreMedia(t, s, []byte("next launch media"))
	entered, release := make(chan struct{}), make(chan struct{})
	active := coreEntryActiveStatus(inspection, 1, true)
	client.coreLoad = func(ctx context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return protocol.Status{}, ctx.Err()
		}
		client.statusResult, client.mediaStatus = active, active
		return active, nil
	}
	launched := make(chan error, 1)
	go func() {
		_, err := s.Launch(ctx, entry.GameID, nil)
		launched <- err
	}()
	// Always release and join the launch before the fixture closes its catalog.
	defer func() {
		close(release)
		if err := <-launched; err != nil {
			t.Errorf("held launch: %v", err)
		}
		if client.mediaCalls != 1 || string(client.mediaBody) != "library media fixture" {
			t.Errorf("launch did not retain its selected snapshot: calls=%d body=%q", client.mediaCalls, client.mediaBody)
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("launch did not reach package load")
	}
	selectionCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err := s.SelectCoreEntryMedia(selectionCtx, entry.GameID, entry.PackageID, entry.MediaID, "blob", replacement.MediaID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selection escaped in-flight launch admission: %v", err)
	}
	stored, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || stored.MediaID != entry.MediaID {
		t.Fatalf("in-flight selection changed: %+v err=%v", stored, err)
	}
}
