package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestCorePackageReadsExactInstalledIdentity(t *testing.T) {
	id := strings.Repeat("a", 64)
	result := corepackage.Inspection{PackageID: id, Descriptor: corepackage.Descriptor{Core: corepackage.Core{ID: "fes.ts1000"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/core-packages/"+id {
			t.Errorf("unexpected lookup: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	got, err := client.CorePackage(t.Context(), id)
	if err != nil || got.PackageID != id || got.Descriptor.Core.ID != "fes.ts1000" {
		t.Fatalf("package %+v err=%v", got, err)
	}
	result.PackageID = strings.Repeat("b", 64)
	if _, err := client.CorePackage(context.Background(), id); err == nil {
		t.Fatal("accepted another installed package")
	}
	if _, err := client.CorePackage(context.Background(), "fes.ts1000"); err == nil {
		t.Fatal("accepted a core name as package identity")
	}
}
