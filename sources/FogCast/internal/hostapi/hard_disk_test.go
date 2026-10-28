package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type hardDiskAPIService struct {
	coreMediaAPIService
	imported int64
	geometry *protocol.DiskGeometry
	cleared  bool
}

func (s *hardDiskAPIService) ImportHardDiskMedia(_ context.Context, size int64, body io.Reader) (fogcast.HardDiskMedia, bool, error) {
	n, _ := io.Copy(io.Discard, body)
	s.imported = n
	return fogcast.HardDiskMedia{CoreMedia: catalog.CoreMedia{MediaID: strings.Repeat("c", 64), Size: n},
		Geometry: protocol.DiskGeometry{Cylinders: 2, Heads: 16, SectorsPerTrack: 63}}, true, nil
}
func (s *hardDiskAPIService) SetCoreEntryDiskGeometry(_ context.Context, game, pkg, media string, g *protocol.DiskGeometry) (catalog.CoreEntry, error) {
	s.geometry, s.cleared = g, g == nil
	return catalog.CoreEntry{GameID: game, PackageID: pkg, MediaID: media, MediaRole: protocol.HardDiskRole, DiskGeometry: g}, nil
}

func TestHardDiskImportAndGeometryRoutes(t *testing.T) {
	s := &hardDiskAPIService{}
	handler := hostapi.New(s)
	image := strings.Repeat("x", 33<<20)
	out := mediaRequest(handler, "POST", "/api/v1/core-media/hard-disk", image, "application/octet-stream")
	var value fogcast.HardDiskMedia
	if out.Code != http.StatusCreated || json.Unmarshal(out.Body.Bytes(), &value) != nil || s.imported != 33<<20 || value.Geometry.Heads != 16 {
		t.Fatalf("import %d %s", out.Code, out.Body)
	}
	// The general import keeps its 32 MiB bound; the hard-disk route keeps whole sectors.
	if out := mediaRequest(handler, "POST", "/api/v1/core-media", image, "application/octet-stream"); out.Code != 400 {
		t.Fatalf("general import above 32 MiB: %d", out.Code)
	}
	if out := mediaRequest(handler, "POST", "/api/v1/core-media/hard-disk", image[:1<<20+1], "application/octet-stream"); out.Code != 400 {
		t.Fatalf("partial sector: %d", out.Code)
	}
	pkg, media := strings.Repeat("a", 64), strings.Repeat("c", 64)
	body := `{"expected_package_id":"` + pkg + `","expected_media_id":"` + media + `","geometry":{"cylinders":130,"heads":16,"sectors_per_track":63}}`
	if out := mediaRequest(handler, "PUT", "/api/v1/library/core-entries/ultima-vi/disk-geometry", body, "application/json"); out.Code != 200 || s.geometry == nil || s.geometry.Cylinders != 130 {
		t.Fatalf("override %d %s", out.Code, out.Body)
	}
	clear := `{"expected_package_id":"` + pkg + `","expected_media_id":"` + media + `","geometry":null}`
	if out := mediaRequest(handler, "PUT", "/api/v1/library/core-entries/ultima-vi/disk-geometry", clear, "application/json"); out.Code != 200 || !s.cleared {
		t.Fatalf("clear %d %s", out.Code, out.Body)
	}
	if out := mediaRequest(handler, "PUT", "/api/v1/library/core-entries/ultima-vi/disk-geometry", `{"geometry":null}`, "application/json"); out.Code != 400 {
		t.Fatalf("missing identities %d", out.Code)
	}
}
