package fogcast

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLibrarySourcesConfigRoundTripAndKitRegeneration(t *testing.T) {
	token := "source-test-token-0123456789abcdef"
	root := t.TempDir()
	template := shippedKitTemplate(t)
	first, err := KitHostConfig(template, nil, kitHostFixture(root, "old-token", kitHostTestTargetID))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.toml")
	first = append([]byte("library_source_token = \""+token+"\"\n"), first...)
	first = append(first, []byte("\n[[library_sources]]\nid = \"powerboat\"\nurl = \"http://192.0.2.12:8788/\"\ntoken = \""+token+"\"\n")...)
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []LibrarySourceConfig{{ID: "powerboat", URL: "http://192.0.2.12:8788", Token: token}}
	if !reflect.DeepEqual(cfg.LibrarySources, want) {
		t.Fatalf("loaded sources=%+v", cfg.LibrarySources)
	}
	if cfg.LibrarySourceToken != token {
		t.Fatalf("serving token did not load")
	}
	// The existing settings writer must preserve source declarations.
	if err := writeCanonicalConfig(path, cfg.Libraries, cfg.Targets, cfg.SelectedTarget); err != nil {
		t.Fatal(err)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := KitHostConfig(template, rewritten, kitHostFixture(root, "new-token", kitHostRestartTargetID))
	if err != nil {
		t.Fatal(err)
	}
	got := loadRendered(t, restarted)
	if !reflect.DeepEqual(got.LibrarySources, want) || got.LibrarySourceToken != token || got.Targets[0].Agent != "new-token" || !got.FolderWatch.Off {
		t.Fatalf("after restart sources=%+v target=%+v watch=%+v", got.LibrarySources, got.Targets, got.FolderWatch)
	}
}

func TestNormalizeLibrarySourcesRejectsInvalidOriginsAndIDs(t *testing.T) {
	token := "source-test-token-0123456789abcdef"
	valid := LibrarySourceConfig{ID: "home-server", URL: "http://192.0.2.1:8788", Token: token}
	for _, tc := range []struct {
		name string
		in   []LibrarySourceConfig
	}{
		{"empty id", []LibrarySourceConfig{{URL: valid.URL}}},
		{"missing token", []LibrarySourceConfig{{ID: "home", URL: valid.URL}}},
		{"short token", []LibrarySourceConfig{{ID: "home", URL: valid.URL, Token: "short"}}},
		{"bad id", []LibrarySourceConfig{{ID: "bad id", URL: valid.URL}}},
		{"duplicate", []LibrarySourceConfig{valid, valid}},
		{"relative", []LibrarySourceConfig{{ID: "home", URL: "/source"}}},
		{"scheme", []LibrarySourceConfig{{ID: "home", URL: "ftp://example.test"}}},
		{"credentials", []LibrarySourceConfig{{ID: "home", URL: "http://user:pass@example.test"}}},
		{"path", []LibrarySourceConfig{{ID: "home", URL: "http://example.test/path"}}},
		{"query", []LibrarySourceConfig{{ID: "home", URL: "http://example.test/?q=1"}}},
		{"fragment", []LibrarySourceConfig{{ID: "home", URL: "http://example.test/#fragment"}}},
		{"missing host", []LibrarySourceConfig{{ID: "home", URL: "http:///"}}},
		{"too many", make([]LibrarySourceConfig, 17)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeLibrarySources(tc.in); err == nil {
				t.Fatalf("accepted %+v", tc.in)
			}
		})
	}
	got, err := normalizeLibrarySources([]LibrarySourceConfig{{ID: " home-server ", URL: " http://192.0.2.1:8788/ ", Token: token}})
	if err != nil || !reflect.DeepEqual(got, []LibrarySourceConfig{valid}) {
		t.Fatalf("normalized=%+v err=%v", got, err)
	}
}

func TestSetLibrarySourcesPreservesSettingsAndFailedWrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	initial := `base_url = "http://127.0.0.1:8182"
token = "agent-token"
request_timeout_seconds = 19
upload_timeout_seconds = 65
library_source_token = "source-test-token-0123456789abcdef"
[library]
attract_idle_seconds = 42
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newService(cfg, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{}, WithConfigPath(path))
	want := []LibrarySourceConfig{{ID: "home", URL: "http://192.0.2.1:8788", Token: "source-test-token-0123456789abcdef"}}
	if err := s.SetLibrarySources(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil || !reflect.DeepEqual(loaded.LibrarySources, want) || loaded.LibrarySourceToken != "source-test-token-0123456789abcdef" || loaded.Library.AttractIdleSeconds != 42 || loaded.RequestTimeout.Seconds() != 19 || loaded.Token != "agent-token" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrarySources(context.Background(), []LibrarySourceConfig{{ID: "bad", URL: "http://host/path"}}); err == nil {
		t.Fatal("invalid source accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || !reflect.DeepEqual(s.LibrarySources(), want) {
		t.Fatal("invalid source changed file or live settings")
	}
	// A reported failure at publication must not leave the disk and live
	// source settings disagreeing on the next restart.
	canonicalConfigPublishErrorHook = func() error { return errors.New("simulated directory sync failure") }
	t.Cleanup(func() { canonicalConfigPublishErrorHook = nil })
	if err := s.SetLibrarySources(context.Background(), []LibrarySourceConfig{{ID: "other", URL: "http://192.0.2.2:8788"}}); err == nil {
		t.Fatal("injected publication failure accepted")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) || !reflect.DeepEqual(s.LibrarySources(), want) {
		t.Fatalf("failed write changed persisted or live source: %s", strings.TrimSpace(string(after)))
	}
}
