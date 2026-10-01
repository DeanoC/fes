package hostapi_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
)

func TestDefaultZX81TapesImportWithoutSelectingOrLaunching(t *testing.T) {
	service := &coreMediaAPIService{}
	server := httptest.NewServer(hostapi.New(service))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	tapes, err := client.ZX81Tapes(context.Background())
	if err != nil || len(tapes) != len(zx81tapes.Entries()) || service.calls != 0 {
		t.Fatalf("read shelf: %+v %v calls=%d", tapes, err, service.calls)
	}
	for _, tape := range tapes {
		media, err := client.ImportZX81Tape(context.Background(), tape)
		if err != nil || media.MediaID != tape.SHA256 {
			t.Fatalf("import %s: %+v %v", tape.ID, media, err)
		}
	}
	if service.calls != len(tapes) || len(service.args) != 0 {
		t.Fatal("import changed library selection")
	}
	if response := mediaRequest(hostapi.New(service), "POST", "/api/v1/library/zx81-tapes/missing/import", "", ""); response.Code != 404 {
		t.Fatalf("unknown tape: %d", response.Code)
	}
}
