package fogcast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type defaultMediaPackageClient struct {
	*packageLibraryClient
	mediaCalls   int
	mediaBody    []byte
	mediaBinding protocol.DevelopmentMediaBinding
	mediaStatus  protocol.Status
	mediaErr     error
}

func (c *defaultMediaPackageClient) LoadDevelopmentMedia(_ context.Context, _ int64, body io.Reader, binding protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	c.mediaCalls++
	c.mediaBody, _ = io.ReadAll(body)
	c.mediaBinding = binding
	return c.mediaStatus, c.mediaErr
}

func colecoLibraryPackageFixture(t *testing.T) []byte {
	t.Helper()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`id = "fes.pong"`), []byte(`id = "fes.coleco"`), 1)
	manifest = bytes.Replace(manifest, []byte(`name = "FES Pong"`), []byte(`name = "FES ColecoVision"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.simple-game"`), []byte(`id = "fes.simple-computer"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.gamepad"`), []byte(`id = "fes.media.blob"`), 1)
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(entry.data)))
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
		out.Write(header)
		out.Write(entry.data)
		out.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func newCoreEntryLaunchFixture(t *testing.T, raw []byte, title string) (*Service, *defaultMediaPackageClient, catalog.CoreEntry, corepackage.Inspection) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, root+"/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	packages, err := corepackage.NewStore(root + "/packages")
	if err != nil {
		t.Fatal(err)
	}
	base := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
	client := &defaultMediaPackageClient{packageLibraryClient: base}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	inspection, created, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("import: %v new=%v", err, created)
	}
	base.inspection = protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true}
	entry, err := s.CreateCoreEntry(ctx, title, inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Descriptor.ABI.ID == "fes.simple-computer" {
		media := []byte("library media fixture")
		asset, _, err := s.ImportCoreMedia(ctx, int64(len(media)), bytes.NewReader(media))
		if err != nil {
			t.Fatal(err)
		}
		entry, err = s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", asset.MediaID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s, client, entry, inspection
}

func coreEntryActiveStatus(inspection corepackage.Inspection, generation uint64, media bool) protocol.Status {
	coreID := inspection.Descriptor.Core.ID
	interfaces := []protocol.RuntimeInterface{{ID: "fes.video.fixed-720p60", Major: 1}}
	if media {
		interfaces = append(interfaces, protocol.RuntimeInterface{ID: "fes.media.blob", Major: 1})
	}
	return protocol.Status{
		State: protocol.StateActive, Development: true, ObservedCore: &coreID,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: inspection.PackageID, Generation: generation,
			ABI:     protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: uint16(inspection.Descriptor.ABI.Major), Minor: uint16(inspection.Descriptor.ABI.Minor)},
			BuildID: inspection.Descriptor.Build.ID, ActiveInterfaces: interfaces,
			Gamepad: !media,
		},
	}
}

func TestLaunchCoreEntryLoadsSelectedMedia(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 7, true)
	client.mediaStatus = active
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	want := []byte("library media fixture")
	wantBinding := protocol.DevelopmentMediaBinding{PackageID: entry.PackageID, Generation: 7, Target: "dev"}
	if client.mediaCalls != 1 || !bytes.Equal(client.mediaBody, want) || client.mediaBinding != wantBinding {
		t.Fatalf("media calls=%d body=%d binding=%+v, want one exact upload binding=%+v", client.mediaCalls, len(client.mediaBody), client.mediaBinding, wantBinding)
	}
	if response.Status.GameID == nil || *response.Status.GameID != entry.GameID || response.Status.System == nil || *response.Status.System != catalog.CorePlatform {
		t.Fatalf("response status = %+v", response.Status)
	}
}

func TestLaunchCoreEntryWithoutMediaRemainsPackageOnly(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "FES Pong")
	active := coreEntryActiveStatus(inspection, 4, false)
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.mediaCalls != 0 {
		t.Fatalf("media calls = %d, want 0", client.mediaCalls)
	}
}

func TestLaunchCoreEntryStopsAfterSelectedMediaFailure(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 8, true)
	client.mediaStatus = active
	client.mediaErr = errors.New("diagnostic media rejected")
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil {
		t.Fatal("Launch succeeded after selected media failure")
	}
	if client.mediaCalls != 1 || client.stopCalls != 1 {
		t.Fatalf("media calls=%d stop calls=%d, want one each", client.mediaCalls, client.stopCalls)
	}
	if s.activeExecution != "" {
		t.Fatalf("active execution = %q after media failure cleanup", s.activeExecution)
	}
}

func TestLaunchCoreEntryReclassifiesSelectedMediaFailureAfterConfirmedStop(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 10, true)
	client.mediaStatus = active
	client.mediaErr = &protocol.APIError{Code: protocol.CodeBusy, Message: "diagnostic media rejected", Phase: "admission"}
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	response, err := s.Launch(ctx, entry.GameID, nil)
	if err == nil {
		t.Fatal("Launch succeeded after selected media failure")
	}
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Phase != "recovery" {
		t.Fatalf("media failure = %v, want recovery-classified API error", err)
	}
	if response.Status.State != protocol.StateIdle || response.Status.CorePackage != nil || response.Status.GameID != nil || response.Status.System != nil {
		t.Fatalf("response status = %+v, want confirmed idle status", response.Status)
	}
}

func TestLaunchCoreEntryMarksRecoveryWhenSelectedMediaCleanupFails(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 9, true)
	client.mediaStatus = active
	client.mediaErr = errors.New("diagnostic media rejected")
	client.stopResult = active
	client.stopErr = errors.New("target stop unavailable")
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	response, err := s.Launch(ctx, entry.GameID, nil)
	if err == nil {
		t.Fatal("Launch succeeded after selected media and cleanup failures")
	}
	if client.mediaCalls != 1 || client.stopCalls != 1 {
		t.Fatalf("media calls=%d stop calls=%d, want one each", client.mediaCalls, client.stopCalls)
	}
	if s.packageRejection == nil || response.Status.LastError == nil || response.Status.LastError.Phase != "recovery" {
		t.Fatalf("launch did not publish recovery marker: status=%+v rejection=%v", response.Status, s.packageRejection)
	}
	status, statusErr := s.Status(ctx)
	if statusErr != nil || status.LastError == nil || status.LastError.Phase != "recovery" || status.GameID != nil || status.System != nil {
		t.Fatalf("status hid pending recovery: %+v %v", status, statusErr)
	}

	client.stopErr = nil
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	if status, err = s.Stop(ctx); err != nil || status.State != protocol.StateIdle || s.packageRejection != nil {
		t.Fatalf("recovery retry: %+v %v rejection=%v", status, err, s.packageRejection)
	}
}
