package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaImportReadFunc func([]byte) (int, error)

func (f coreMediaImportReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestCoreMediaCapabilitiesServiceImportReadErrors(t *testing.T) {
	for _, name := range []string{"canceled-during-read", "deadline-during-read", "wrapped-deadline", "ordinary-read-failure"} {
		t.Run(name, func(t *testing.T) {
			s, client := coreDataService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := error(context.Canceled)
			reader := coreMediaImportReadFunc(func(p []byte) (int, error) { p[0] = 1; cancel(); return 1, nil })
			switch name {
			case "deadline-during-read":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer stop()
				want = context.DeadlineExceeded
				reader = func([]byte) (int, error) { <-ctx.Done(); return 0, ctx.Err() }
			case "wrapped-deadline":
				want = context.DeadlineExceeded
				reader = func([]byte) (int, error) { return 0, fmt.Errorf("body read: %w", want) }
			case "ordinary-read-failure":
				want = io.ErrUnexpectedEOF
				reader = func([]byte) (int, error) { return 0, want }
			}
			got, created, err := s.ImportCoreMedia(ctx, 2, reader)
			if got != (catalog.CoreMedia{}) || created {
				t.Fatalf("failed import published: %+v %v", got, created)
			}
			var apiErr *protocol.APIError
			if name == "ordinary-read-failure" {
				if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
					t.Fatalf("ordinary read error=%v, want BAD_REQUEST", err)
				}
			} else if !errors.Is(err, want) || (errors.As(err, &apiErr) && apiErr.Code == protocol.CodeBadRequest) {
				t.Fatalf("read error=%v, want preserved %v without BAD_REQUEST", err, want)
			}
			if client.healthCalls != 0 || client.statusCalls != 0 || client.coreCalls != 0 || client.libraryCalls != 0 || client.stopCalls != 0 {
				t.Fatal("import contacted target")
			}
		})
	}
}

type capabilitiesMediaCatalog struct {
	*catalog.Store
	infoCalls, openCalls int
	forged               *catalog.CoreEntry
}

func (c *capabilitiesMediaCatalog) CoreMediaInfo(ctx context.Context, id string) (catalog.CoreMedia, error) {
	c.infoCalls++
	return c.Store.CoreMediaInfo(ctx, id)
}

func (c *capabilitiesMediaCatalog) OpenCoreMedia(ctx context.Context, id string) (catalog.CoreMedia, io.ReadCloser, error) {
	c.openCalls++
	return c.Store.OpenCoreMedia(ctx, id)
}

func (c *capabilitiesMediaCatalog) CoreEntry(ctx context.Context, id string) (catalog.CoreEntry, error) {
	if c.forged != nil && c.forged.GameID == id {
		return *c.forged, nil
	}
	return c.Store.CoreEntry(ctx, id)
}

func TestCoreMediaCapabilitiesServiceLargeImportAndMetadata(t *testing.T) {
	ctx := context.Background()
	s, client := coreDataService(t)
	store := &capabilitiesMediaCatalog{Store: s.catalog.(*catalog.Store)}
	s.catalog = store
	for _, size := range []int{16385, 1 << 20} {
		body := bytes.Repeat([]byte{0xa5}, size)
		media, created, err := s.ImportCoreMedia(ctx, int64(size), bytes.NewReader(body))
		if err != nil || !created || media.Size != int64(size) || media.MediaID != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatalf("size=%d import=%+v created=%v err=%v", size, media, created, err)
		}
		got, err := s.CoreMedia(ctx, media.MediaID)
		if err != nil || got != media {
			t.Fatalf("metadata=%+v want=%+v err=%v", got, media, err)
		}
	}
	if store.infoCalls != 2 || store.openCalls != 0 {
		t.Fatalf("metadata must use info only: info=%d open=%d", store.infoCalls, store.openCalls)
	}
	if client.healthCalls != 0 || client.statusCalls != 0 || client.coreCalls != 0 || client.libraryCalls != 0 || client.stopCalls != 0 {
		t.Fatal("import/metadata contacted target")
	}
}

func TestCoreMediaCapabilitiesServiceOversizeAdmissionPreservesEntry(t *testing.T) {
	ctx := context.Background()
	s, client, entry, _ := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Legacy bounded media")
	large := importServiceCoreMedia(t, s, bytes.Repeat([]byte{0xa5}, 16385))
	store := &capabilitiesMediaCatalog{Store: s.catalog.(*catalog.Store)}
	s.catalog = store
	before, err := s.CoreEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"select", "create", "forged-launch"} {
		t.Run(operation, func(t *testing.T) {
			switch operation {
			case "select":
				_, err = s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "blob", large.MediaID)
			case "create":
				_, err = s.CreateCoreMediaEntry(ctx, "Oversized new title", entry.PackageID, "blob", large.MediaID)
			case "forged-launch":
				forged := entry
				forged.MediaID = large.MediaID
				store.forged = &forged
				_, err = s.Launch(ctx, entry.GameID, nil)
				store.forged = nil
			}
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest || apiErr.Phase != "request" {
				t.Fatalf("oversize error=%v, want BAD_REQUEST/request", err)
			}
			if store.openCalls != 0 || client.coreCalls != 0 || client.mediaCalls != 0 || client.stopCalls != 0 {
				t.Fatalf("oversize opened/delivered media: open=%d core=%d media=%d stop=%d", store.openCalls, client.coreCalls, client.mediaCalls, client.stopCalls)
			}
			stored, err := s.CoreEntry(ctx, entry.GameID)
			if err != nil || stored != entry {
				t.Fatalf("old entry changed: %+v err=%v", stored, err)
			}
			after, err := s.CoreEntries(ctx)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("entry inventory changed: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
	if store.infoCalls != 3 {
		t.Fatalf("expected metadata admission for each operation, got %d", store.infoCalls)
	}
}

// Fixed-width manifest replacements preserve the existing canonical archive's
// member sizes and checksums; import still validates the resulting descriptor.
func capabilitiesFixtureReplace(t *testing.T, raw []byte, old, next string) []byte {
	t.Helper()
	if len(old) != len(next) || bytes.Count(raw, []byte(old)) != 1 {
		t.Fatalf("fixture replacement must be unique and fixed-width: %q -> %q", old, next)
	}
	return bytes.Replace(raw, []byte(old), []byte(next), 1)
}

type offlineCapabilitiesClient struct {
	*persistenceLibraryClient
	inspectCalls, dataCalls int
}

func (c *offlineCapabilitiesClient) InspectCore(context.Context, int64, io.Reader) (protocol.CoreInspection, error) {
	c.inspectCalls++
	return protocol.CoreInspection{}, errors.New("target offline")
}

func (c *offlineCapabilitiesClient) InspectCoreData(context.Context, int64, io.Reader, string) (protocol.CoreDataInspection, error) {
	c.dataCalls++
	return protocol.CoreDataInspection{}, errors.New("target offline")
}

func TestCoreMediaCapabilitiesServiceOfflineDeclaredContracts(t *testing.T) {
	legacy := serviceMediaPackageFixture(t, "0.1.0", true)
	abi := "[abi]\nid = \"fes.simple-computer\"\nmajor = 1\nminor = 0"
	blob := "id = \"fes.media.blob\"\nmajor = 1\nminor = 0"
	for _, tc := range []struct {
		name  string
		raw   []byte
		known bool
	}{
		{"legacy", legacy, true},
		{"arbitrary-core-id", capabilitiesFixtureReplace(t, legacy, "id = \"fes.pong\"", "id = \"odd.core\""), true},
		{"absent-media-interface", serviceMediaPackageFixture(t, "0.1.0", false), false},
		{"unknown-interface", capabilitiesFixtureReplace(t, legacy, "fes.media.blob", "new.media.blob"), false},
		{"unknown-interface-major", capabilitiesFixtureReplace(t, legacy, blob, "id = \"fes.media.blob\"\nmajor = 2\nminor = 0"), false},
		{"unknown-interface-minor", capabilitiesFixtureReplace(t, legacy, blob, "id = \"fes.media.blob\"\nmajor = 1\nminor = 1"), false},
		{"non-media-abi", libraryPackageFixture(t, "0.1.0"), false},
		{"unknown-abi", capabilitiesFixtureReplace(t, legacy, "fes.simple-computer", "new.simple-computer"), false},
		{"unknown-abi-major", capabilitiesFixtureReplace(t, legacy, abi, "[abi]\nid = \"fes.simple-computer\"\nmajor = 2\nminor = 0"), false},
		{"unknown-abi-minor", capabilitiesFixtureReplace(t, legacy, abi, "[abi]\nid = \"fes.simple-computer\"\nmajor = 1\nminor = 1"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, base := coreDataService(t)
			base.healthErr, base.statusErr = errors.New("target offline"), errors.New("target offline")
			client := &offlineCapabilitiesClient{persistenceLibraryClient: base}
			s.targetClients[s.selectedTarget] = client
			installed, _, err := s.ImportCorePackage(ctx, int64(len(tc.raw)), bytes.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.CoreMediaCapabilities(ctx, installed.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			if got.PackageID != installed.PackageID || got.Source != "declared-contract" || got.Compatibility != "unknown" || got.ImportMaxBytes != catalog.MaxCoreMediaBytes {
				t.Fatalf("capabilities metadata=%+v", got)
			}
			if got.ImportMaxBytes != 32<<20 {
				t.Fatalf("import limit=%d, want 32 MiB", got.ImportMaxBytes)
			}
			want := []protocol.CoreMediaCapability{}
			if tc.known {
				want = append(want, protocol.CoreMediaCapability{
					Role: "blob", Format: "raw", MinBytes: 1, MaxBytes: 16384,
					Interface: protocol.RuntimeContract{ID: "fes.media.blob", Major: 1},
					Transport: "fes-simple-computer-mailbox-v1",
				})
			}
			if !reflect.DeepEqual(got.Media, want) {
				t.Fatalf("media=%+v want=%+v", got.Media, want)
			}
			if client.inspectCalls != 0 || client.dataCalls != 0 || base.healthCalls != 0 || base.statusCalls != 0 || base.libraryCalls != 0 || base.coreCalls != 0 || base.stopCalls != 0 {
				t.Fatal("offline capabilities contacted target")
			}
			// The old package is consumed verbatim; capability discovery must
			// not require a rewritten manifest or a new installed identity.
			after, raw, err := s.readInstalledCore(ctx, installed.PackageID)
			if err != nil || !reflect.DeepEqual(after, installed) || !bytes.Equal(raw, tc.raw) {
				t.Fatalf("capability discovery rewrote package: err=%v", err)
			}
		})
	}
}

func TestCoreMediaCapabilitiesServiceLegacySmallLaunch(t *testing.T) {
	ctx := context.Background()
	raw := colecoLibraryPackageFixture(t)
	s, client, entry, installed := newCoreEntryLaunchFixture(t, raw, "Unchanged legacy package")
	loads := recordServiceCoreMediaLoads(client, installed)
	capabilities, err := s.CoreMediaCapabilities(ctx, installed.PackageID)
	if err != nil || len(capabilities.Media) != 1 || capabilities.Media[0].MaxBytes != 16384 {
		t.Fatalf("legacy capabilities=%+v err=%v", capabilities, err)
	}
	result, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.DevelopmentMediaBinding{PackageID: installed.PackageID, Generation: 1, Target: "dev"}
	if *loads != 1 || client.mediaCalls != 1 || string(client.mediaBody) != "library media fixture" || client.mediaBinding != want || result.Status.GameID == nil || *result.Status.GameID != entry.GameID {
		t.Fatalf("legacy launch: loads=%d media=%d bytes=%q binding=%+v status=%+v", *loads, client.mediaCalls, client.mediaBody, client.mediaBinding, result.Status)
	}
	_, after, err := s.readInstalledCore(ctx, installed.PackageID)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("legacy launch rewrote installed package: %v", err)
	}
}
