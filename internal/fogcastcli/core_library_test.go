package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/corepackage"
)

func TestCoreSelectionCommandUsesHostCAS(t *testing.T) {
	old, next := strings.Repeat("a", 64), strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/v1/library/core-entries/core-pong" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		var value map[string]string
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
		}
		if value["package_id"] != next || value["expected_package_id"] != old {
			t.Errorf("selection=%v", value)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"game_id":"core-pong","package_id":"` + next + `"}`))
	}))
	defer server.Close()
	result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-select", "core-pong", old, next})
	if result.err != nil || result.exit != 0 {
		t.Fatalf("result %+v", result)
	}
}

func TestCoreInstallRequiresExactInspectionResponse(t *testing.T) {
	archivePath, archive, inspection := cliCoreArchive(t)
	wrong := inspection
	wrong.Descriptor.Core.Name = "Contradictory core"
	for _, test := range []struct {
		name     string
		response any
		wantOK   bool
	}{
		{"exact", inspection, true},
		{"missing descriptor", map[string]string{"package_id": inspection.PackageID}, false},
		{"wrong descriptor", wrong, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != http.MethodPost || r.URL.Path != "/api/v1/core-packages" ||
					r.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(body, archive) {
					t.Errorf("request method=%s path=%s type=%q body=%d err=%v", r.Method, r.URL.Path, r.Header.Get("Content-Type"), len(body), err)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-install", archivePath})
			if (result.err == nil) != test.wantOK {
				t.Fatalf("result = %+v, wantOK=%v", result, test.wantOK)
			}
		})
	}
}

func cliCoreArchive(t *testing.T) (string, []byte, corepackage.Inspection) {
	t.Helper()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
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
		archive.Write(header)
		archive.Write(entry.data)
		archive.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	data := archive.Bytes()
	path := filepath.Join(t.TempDir(), "core.fcore")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(inspection.Descriptor, corepackage.Descriptor{}) {
		t.Fatal("empty fixture descriptor")
	}
	return path, data, inspection
}
