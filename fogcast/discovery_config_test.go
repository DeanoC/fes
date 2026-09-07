package fogcast

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetIdentityRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("request_timeout_seconds = 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id := "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	if err := writeCanonicalConfig(path, nil, []TargetConfig{{Name: "kit", TargetID: id}}, "kit"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	var raw fileConfig
	if err := toml.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Targets[0].TargetID != id || raw.RequestTimeoutSeconds != 7 {
		t.Fatalf("lost configuration: %+v", raw)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	for _, bad := range []string{"bad", "F2BB8D43-3CF5-4407-9A11-DFB7CB0086AA"} {
		if _, _, err := normalizeTargets([]TargetConfig{{Name: "kit", TargetID: bad}}, "kit"); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestPrepareTargetIdentityOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("request_timeout_seconds = 7\n[[targets]]\nname = 'kit'\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newService(Config{Targets: []TargetConfig{{Name: "kit"}}, SelectedTarget: "kit"}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, nil, WithConfigPath(path))
	name := "kit"
	var first []byte
	for i := 0; i < 2; i++ {
		if err := s.PatchLibrarySettings(context.Background(), LibraryConfigPatch{PrepareTarget: &name}); err != nil {
			t.Fatal(err)
		}
		if !discovery.ValidID(s.LibrarySettings().Targets[0].TargetID) {
			t.Fatal("not prepared")
		}
		body, _ := os.ReadFile(path)
		if i == 0 {
			first = body
		} else if string(body) != string(first) {
			t.Fatal("identity changed")
		}
	}
}

func TestLegacyConfigBindsTargetIdentity(t *testing.T) {
	raw := fileConfig{BaseURL: "http://127.0.0.1:8182", Token: "secret", TargetID: "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"}
	targets, selected, err := normalizeLoadedTargets(raw)
	if err != nil || selected != "dev" || targets[0].TargetID != raw.TargetID {
		t.Fatalf("targets=%+v err=%v", targets, err)
	}
	raw.TargetID = "bad"
	if _, _, err := normalizeLoadedTargets(raw); err == nil {
		t.Fatal("malformed legacy identity accepted")
	}
}

func TestPrepareTargetRejectsChangingActiveLegacyIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("base_url='http://127.0.0.1:8182'\ntoken='secret'\nrequest_timeout_seconds=7\nupload_timeout_seconds=60\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: "http://127.0.0.1:8182", Agent: "secret"}}, SelectedTarget: "kit"}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{}, WithConfigPath(path))
	s.activeExecution = ExecutionFPGANative
	s.activeTarget = "kit"
	name := "kit"
	err := s.PatchLibrarySettings(context.Background(), LibraryConfigPatch{PrepareTarget: &name})
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeBusy {
		t.Fatalf("error=%v", err)
	}
	if s.LibrarySettings().Targets[0].TargetID != "" {
		t.Fatal("active identity changed")
	}
	body, _ := os.ReadFile(path)
	if string(body) != string(original) {
		t.Fatal("private configuration changed")
	}
}

func TestConsecutiveOfflineTargetEditsWithoutReconciliation(t *testing.T) {
	for _, initialState := range []string{"", "disconnected"} {
		t.Run("state="+initialState, func(t *testing.T) {
			target := TargetConfig{Name: "kit", Enabled: true, Address: "http://127.0.0.1:1", Agent: "secret"}
			base, _ := url.Parse(target.Address)
			s := newService(Config{Targets: []TargetConfig{target}, SelectedTarget: "kit"}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, host.NewClient(base, "secret", nil))
			s.connection.State = initialState
			for _, address := range []string{"http://127.0.0.1:2", "http://127.0.0.1:3"} {
				target.Address = address
				targets := []TargetConfig{target}
				if err := s.PatchLibrarySettings(context.Background(), LibraryConfigPatch{Targets: &targets}); err != nil {
					t.Fatalf("edit to %s: %v", address, err)
				}
				if s.LibrarySettings().Targets[0].Address != address {
					t.Fatal("edit not published")
				}
			}
		})
	}
}
