package targetclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func pc386Descriptor() corepackage.Descriptor {
	d := validCoreInspectionDescriptor()
	d.Core.ID = "fes.pc386"
	d.ABI = corepackage.Contract{ID: "fes.computer", Major: 1, Minor: 1}
	d.Interfaces = []corepackage.Interface{{ID: "fes.video.fixed-720p60", Major: 1, Required: true},
		{ID: "fes.media.ata-disk", Major: 1, Required: true}, {ID: "fes.persistence.words", Major: 1, Required: true},
		{ID: "fes.pc386.machine", Major: 1, Required: true}}
	return d
}

// An agent that predates hard-disk staging has no such route: the host reports
// that clearly rather than as a transport failure.
func TestDiskImageClientMapsOldAgentAndStagesByDigest(t *testing.T) {
	sha := strings.Repeat("c", 64)
	for _, kind := range []string{"old agent", "absent", "staged"} {
		t.Run(kind, func(t *testing.T) {
			var uploaded int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "old agent" {
					http.NotFound(w, r)
					return
				}
				if r.Header.Get(protocol.DiskImageHeader) != sha {
					t.Errorf("digest header %q", r.Header.Get(protocol.DiskImageHeader))
				}
				switch r.URL.Path {
				case "/v1/library/disk-images/probe":
					status := protocol.DiskImageStatus{SHA256: sha}
					if kind == "staged" {
						status.Size, status.Staged = 1<<20, true
					}
					_ = json.NewEncoder(w).Encode(status)
				case "/v1/library/disk-images":
					data, _ := io.ReadAll(r.Body)
					uploaded = len(data)
					_ = json.NewEncoder(w).Encode(protocol.DiskImageStatus{SHA256: sha, Size: int64(len(data)), Staged: true})
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			status, err := client.DiskImage(context.Background(), sha)
			if kind == "old agent" {
				var apiErr *protocol.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation || !strings.Contains(apiErr.Message, "predates") {
					t.Fatalf("old agent: %v", err)
				}
				return
			}
			if err != nil || status.Staged != (kind == "staged") {
				t.Fatalf("probe %+v %v", status, err)
			}
			status, err = client.StageDiskImage(context.Background(), sha, 1<<20, strings.NewReader(strings.Repeat("x", 1<<20)))
			if err != nil || !status.Staged || uploaded != 1<<20 {
				t.Fatalf("stage %+v %v %d", status, err, uploaded)
			}
		})
	}
	client := targetclient.NewClient(&url.URL{Scheme: "http", Host: "127.0.0.1:1"}, "secret", http.DefaultClient)
	if _, err := client.StageDiskImage(context.Background(), sha, 1<<20+1, strings.NewReader("x")); err == nil {
		t.Fatal("partial sector sent")
	}
}

func TestMachineCoreDataAndLaunchHeaders(t *testing.T) {
	id := strings.Repeat("a", 64)
	settings := protocol.MachineSettings{Preset: 3, CodeProfile: 1, SBBase: 0x240, SBIRQ: 5, SBDMA: 1}
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		_, _ = io.ReadAll(r.Body)
		if r.URL.Path == "/v1/library/core/load" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"BAD_REQUEST","message":"stop here"}}`))
			return
		}
		revision := strings.Repeat("d", 64)
		_ = json.NewEncoder(w).Encode(protocol.CoreDataInspection{CoreData: protocol.CoreData{PackageID: id, CoreID: "fes.pc386", Mode: "persistent",
			Layout: &protocol.RuntimeContract{ID: "fes.pc386.machine", Major: 1}, Revision: revision, GameID: "ultima-vi", Machine: &settings}, Descriptor: pc386Descriptor()})
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := targetclient.NewClient(base, "secret", server.Client())
	result, err := client.InspectGameCoreData(context.Background(), 7, strings.NewReader("package"), id, "ultima-vi")
	if err != nil || result.Machine == nil || *result.Machine != settings || headers.Get(protocol.LibraryGameHeader) != "ultima-vi" {
		t.Fatalf("inspect %+v %v %v", result, err, headers)
	}
	update := protocol.CoreSettingsUpdate{ExpectedPackageID: id, ExpectedRevision: "absent", GameID: "ultima-vi", Machine: &settings}
	if _, err := client.UpdateCoreSettings(context.Background(), 7, strings.NewReader("package"), update); err != nil ||
		headers.Get(protocol.MachineSettingsHeader) != "3,0,1,0,576,5,1" || headers.Get("X-FogCast-Paddle-Speed") != "" {
		t.Fatalf("update %v %v", err, headers)
	}
	// A record for another game does not answer this one.
	if _, err := client.InspectGameCoreData(context.Background(), 7, strings.NewReader("package"), id, "ultima-vii"); err == nil {
		t.Fatal("accepted another game's record")
	}
	binding := protocol.LibraryMachineBinding{GameID: "ultima-vi", Disk: &protocol.StagedDisk{SHA256: strings.Repeat("c", 64), Size: 64 << 20,
		Geometry: protocol.DiskGeometry{Cylinders: 130, Heads: 16, SectorsPerTrack: 63}}}
	_, _ = client.LoadLibraryMachineCore(context.Background(), 7, strings.NewReader("package"), id, binding)
	if headers.Get(protocol.LibraryGameHeader) != "ultima-vi" || headers.Get(protocol.DiskImageHeader) != binding.Disk.SHA256 ||
		headers.Get(protocol.DiskGeometryHeader) != "130/16/63" || headers.Get("X-FogCast-Package-ID") != id {
		t.Fatalf("load headers %v", headers)
	}
}
