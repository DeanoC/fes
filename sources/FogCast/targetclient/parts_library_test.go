package targetclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/misteross/expansion"
)

func libraryPartsStatus() protocol.Status {
	id := strings.Repeat("a", 64)
	c := expansion.PartsComposition{PackageID: id, Layout: expansion.ColecoVideoLayout, Parts: []expansion.PartSelection{{Role: "video", PartID: strings.Repeat("c", 64)}}, ShellSHA256: strings.Repeat("d", 64), PayloadSHA256: strings.Repeat("e", 64), PayloadSize: 40408}
	c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: strings.Repeat("b", 32), PersistenceMode: "volatile", ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.video.fixed-720p60", Major: 1}}, PartsComposition: &c}}
}

func TestLibraryPartsClientCarriesExactIdentityAndRejectsOtherReceipts(t *testing.T) {
	for _, name := range []string{"library", "wrong package", "missing parts", "wrong digest", "mixed receipt", "persistence", "lost reply", "ordinary load"} {
		t.Run(name, func(t *testing.T) {
			status := libraryPartsStatus()
			id := status.CorePackage.PackageID
			switch name {
			case "wrong package":
				status.CorePackage.PackageID = strings.Repeat("f", 64)
			case "missing parts":
				status.CorePackage.PartsComposition = nil
			case "wrong digest":
				status.CorePackage.PartsComposition.PayloadSHA256 = strings.Repeat("f", 64)
			case "mixed receipt":
				status.CorePackage.ROMLink = &corepackage.ROMLinkIdentity{}
			case "persistence":
				status.CorePackage.PersistenceMode = "persistent"
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				path := "/v1/library/core/parts"
				if name == "ordinary load" {
					path = "/v1/development/core"
				}
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != path || r.URL.RawQuery != "" || r.ContentLength != 5 || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/octet-stream" || string(body) != "parts" {
					t.Errorf("request %s headers=%v body=%s", r.URL, r.Header, body)
				}
				if name != "ordinary load" && r.Header.Get("X-FogCast-Package-ID") != id {
					t.Error("missing exact package")
				}
				if name == "lost reply" {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				json.NewEncoder(w).Encode(status)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			var err error
			if name == "ordinary load" {
				_, err = client.LoadCore(context.Background(), 5, strings.NewReader("parts"))
			} else {
				_, err = client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), id)
			}
			if (err == nil) != (name == "library") || calls.Load() != 1 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestLibraryPartsClientUsesSharedKitLease(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			io.WriteString(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"parts-owner"}`)
		case "/v1/kit/release":
			io.WriteString(w, `{"state":"free"}`)
		case "/v1/library/core/parts":
			calls.Add(1)
			if r.Header.Get(targetclient.KitLeaseHeader) != "parts-owner" {
				t.Error("missing lease")
			}
			json.NewEncoder(w).Encode(libraryPartsStatus())
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "secret", server.Client(), "test", "video")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "secret", server.Client()).WithKitLease(lease)
	_, err := client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), strings.Repeat("a", 64))
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestLibraryAtaDiskLoadCarriesGameAndSelectedDiskBeforePackage(t *testing.T) {
	id := strings.Repeat("a", 64)
	base := strings.Repeat("c", 64)
	gameID := "ultima-vi"
	cylinders, heads, sectors, ok := protocol.AtaDiskGeometry(protocol.AtaDiskMinBytes)
	if !ok {
		t.Fatal("ATA geometry unavailable")
	}
	disk := protocol.AtaDiskSelection{BaseMediaID: base, Size: uint32(protocol.AtaDiskMinBytes), Cylinders: cylinders,
		Heads: heads, SectorsPerTrack: sectors}
	noSave := false
	unit := protocol.MediaUnitStatus{Unit: protocol.AtaDiskUnit, Interface: protocol.AtaDiskInterface(),
		MinBytes: uint32(protocol.AtaDiskMinBytes), MaxBytes: uint32(protocol.AtaDiskMaxBytes),
		ChunkBytes: protocol.AtaDiskChunkBytes, State: protocol.MediaUnitReady, Saveable: &noSave,
		Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: gameID, BaseMediaID: base, Revision: "absent"}}
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1, Minor: 1},
		BuildID: strings.Repeat("b", 32), PersistenceMode: "persistent",
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtaDiskInterface().ID, Major: protocol.AtaDiskInterface().Major}},
		MediaUnits:       []protocol.MediaUnitStatus{unit},
	}}
	packageBytes := []byte("package archive")
	diskBytes := bytes.Repeat([]byte{0x5a}, int(disk.Size))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if r.URL.Path != "/v1/library/core/load" || r.ContentLength != int64(len(packageBytes)+len(diskBytes)) ||
			r.Header.Get("X-FogCast-Package-ID") != id || r.Header.Get("X-FogCast-Game-ID") != gameID ||
			r.Header.Get(protocol.CorePackageSizeHeader) != strconv.Itoa(len(packageBytes)) ||
			r.Header.Get(protocol.AtaDiskSizeHeader) != strconv.Itoa(int(disk.Size)) ||
			r.Header.Get(protocol.AtaDiskBaseIDHeader) != base ||
			r.Header.Get(protocol.AtaDiskCylindersHeader) != strconv.Itoa(int(cylinders)) ||
			r.Header.Get(protocol.AtaDiskHeadsHeader) != strconv.Itoa(int(heads)) ||
			r.Header.Get(protocol.AtaDiskSectorsHeader) != strconv.Itoa(int(sectors)) ||
			!bytes.Equal(body, append(append([]byte(nil), diskBytes...), packageBytes...)) {
			t.Errorf("request path=%s length=%d headers=%v body=%q", r.URL.Path, r.ContentLength, r.Header, body)
		}
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	client := targetclient.NewClient(baseURL, "secret", server.Client())
	got, err := client.LoadLibraryCoreWithAtaDisk(context.Background(), int64(len(packageBytes)),
		bytes.NewReader(packageBytes), id, gameID, disk, bytes.NewReader(diskBytes))
	if err != nil || got.CorePackage == nil || got.CorePackage.PackageID != id {
		t.Fatalf("load status=%+v err=%v", got, err)
	}
}
