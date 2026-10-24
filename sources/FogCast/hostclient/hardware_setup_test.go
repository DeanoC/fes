package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstalledZX81CreationFinishesEmptyROMWithoutPublicationCatalog(t *testing.T) {
	pkg, rom := strings.Repeat("a", 64), strings.Repeat("b", 64)
	entry := CoreEntry{GameID: "my-zx81", Title: "My Zx81", CoreID: "fes.zx81", PackageID: pkg}
	created, selected := false, ""
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/core-packages/" + pkg:
			json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "descriptor": map[string]any{
				"core": map[string]string{"id": "fes.zx81"},
				"rom":  map[string]any{"id": "machine-rom", "role": "machine-rom", "source_size": 8192},
			}})
		case "/api/v1/core-packages/" + pkg + "/media-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"media": []any{}})
		case "/api/v1/core-packages":
			json.NewEncoder(w).Encode(map[string]any{"packages": []any{}})
		case "/api/v1/library/core-entries":
			if r.Method == http.MethodGet {
				entries := []CoreEntry{}
				if created {
					entries = append(entries, entry)
				}
				json.NewEncoder(w).Encode(map[string]any{"entries": entries})
			} else {
				created = true
				json.NewEncoder(w).Encode(entry)
			}
		case "/api/v1/library/core-entries/my-zx81/rom":
			if r.Method == http.MethodPut {
				writes++
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				if body["expected_media_id"] != "" || body["package_id"] != pkg || body["rom_id"] != "machine-rom" {
					t.Errorf("unsafe ROM update %+v", body)
				}
				selected = body["media_id"]
			}
			binding := map[string]string{"game_id": entry.GameID}
			if selected != "" {
				binding["media_id"] = selected
				binding["package_id"] = pkg
				binding["rom_id"] = "machine-rom"
			}
			json.NewEncoder(w).Encode(binding)
		default:
			t.Errorf("unexpected catalogue/lifecycle call %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	setup, err := client.InstalledZX81Setup(context.Background(), pkg)
	if err != nil || len(setup.ROMs) != 1 || setup.ROMs[0].ID != "machine-rom" || setup.ROMs[0].SourceSize != 8192 {
		t.Fatalf("local setup requirement: %+v %v", setup, err)
	}
	for i := 0; i < 2; i++ {
		got, err := client.CreateInstalledZX81Entry(context.Background(), setup, entry.Title, map[string]string{"machine-rom": rom})
		if err != nil || got.GameID != entry.GameID {
			t.Fatalf("create/retry: %+v %v", got, err)
		}
	}
	if writes != 1 {
		t.Fatalf("retry overwrote ROM: %d writes", writes)
	}
	if _, err := client.CreateInstalledZX81Entry(context.Background(), setup, entry.Title, map[string]string{"machine-rom": strings.Repeat("c", 64)}); err == nil {
		t.Fatal("replaced existing ROM")
	}
}

func TestSelectCoreEntryPackageUsesExpectedPackageCAS(t *testing.T) {
	game, oldPackage, nextPackage := "zx81-workshop", strings.Repeat("a", 64), strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/library/core-entries/"+game {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["expected_package_id"] != oldPackage || body["package_id"] != nextPackage {
			t.Errorf("package selection body %+v err=%v", body, err)
			http.Error(w, "wrong package selection", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(CoreEntry{GameID: game, CoreID: "fes.zx81", PackageID: nextPackage})
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	got, err := client.SelectCoreEntryPackage(context.Background(), game, oldPackage, nextPackage)
	if err != nil || got.GameID != game || got.PackageID != nextPackage {
		t.Fatalf("selected entry %+v err=%v", got, err)
	}
	if _, err = client.SelectCoreEntryPackage(context.Background(), game, "bad", nextPackage); err == nil {
		t.Fatal("invalid expected package accepted")
	}
}
