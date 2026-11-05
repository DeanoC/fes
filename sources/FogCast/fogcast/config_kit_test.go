package fogcast

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

const (
	kitHostTestTargetID    = "11111111-1111-4111-8111-111111111111"
	kitHostRestartTargetID = "22222222-2222-4222-8222-222222222222"
)

func shippedKitTemplate(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../image/buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/fogcast/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadRendered(t *testing.T, data []byte) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load rendered config: %v\n%s", err, data)
	}
	return config
}

func kitHostFixture(root, token, targetID string) KitHost {
	return KitHost{AgentAddress: "http://127.0.0.1:8182", AgentToken: token, TargetID: targetID,
		State: filepath.Join(root, "state"), Staging: filepath.Join(root, "staging")}
}

func TestKitHostConfigFirstStartUsesTheShippedTemplate(t *testing.T) {
	root := t.TempDir()
	rendered, err := KitHostConfig(shippedKitTemplate(t), nil, kitHostFixture(root, "agent-token", kitHostTestTargetID))
	if err != nil {
		t.Fatal(err)
	}
	config := loadRendered(t, rendered)
	if len(config.Targets) != 1 || config.SelectedTarget != KitHostTargetName {
		t.Fatalf("targets %+v selected %q", config.Targets, config.SelectedTarget)
	}
	target := config.Targets[0]
	if !target.Enabled || target.Address != "http://127.0.0.1:8182" || target.Agent != "agent-token" || target.TargetID != kitHostTestTargetID {
		t.Fatalf("target %+v", target)
	}
	if !config.CorePackages.Kit || !config.FolderWatch.Off {
		t.Fatalf("core packages %+v folder watch %+v", config.CorePackages, config.FolderWatch)
	}
	if config.Firmware.BundledDir != "/usr/share/fogcast/firmware" || config.UploadTimeout != kitHostUploadTimeoutSeconds*time.Second || config.RequestTimeout != 12*time.Second {
		t.Fatalf("firmware %q upload %s request %s", config.Firmware.BundledDir, config.UploadTimeout, config.RequestTimeout)
	}
	if len(config.Libraries) != 1 || config.Libraries[0].Path != "/media/fat/games/sms" {
		t.Fatalf("libraries %+v", config.Libraries)
	}
	if !strings.Contains(string(rendered), filepath.Join(root, "state")) {
		t.Fatalf("state not rendered:\n%s", rendered)
	}
}

// A restart keeps what the API changed and rewrites what kit mode owns.
func TestKitHostConfigRestartKeepsLibrariesAndRewritesTheTarget(t *testing.T) {
	root := t.TempDir()
	first, err := KitHostConfig(shippedKitTemplate(t), nil, kitHostFixture(root, "old-token", ""))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(root, "md")
	if err := writeCanonicalConfig(path,
		[]catalog.Root{{ID: "sms-main", System: "sms", Path: "/media/fat/games/sms"}, {ID: "md-usb", System: "megadrive", Path: added}},
		[]TargetConfig{{Name: KitHostTargetName, Enabled: true, Address: "http://127.0.0.1:8182", Agent: "old-token", AgentSet: true, TargetID: "11111111-2222-4333-8444-555555555555"}},
		KitHostTargetName); err != nil {
		t.Fatal(err)
	}
	if rewritten, err := LoadConfig(path); err != nil || !rewritten.CorePackages.Kit || !rewritten.FolderWatch.Off {
		t.Fatalf("a settings rewrite dropped kit keys: %+v %+v %v", rewritten.CorePackages, rewritten.FolderWatch, err)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := KitHostConfig(shippedKitTemplate(t), existing, kitHostFixture(root, "new-token", kitHostRestartTargetID))
	if err != nil {
		t.Fatal(err)
	}
	config := loadRendered(t, restarted)
	if len(config.Libraries) != 2 || config.Libraries[1].Path != added {
		t.Fatalf("libraries after restart %+v", config.Libraries)
	}
	if len(config.Targets) != 1 || config.Targets[0].Agent != "new-token" || config.Targets[0].TargetID != kitHostRestartTargetID {
		t.Fatalf("target after restart %+v", config.Targets)
	}
	if strings.Contains(string(restarted), "old-token") {
		t.Fatalf("old token kept:\n%s", restarted)
	}
}

func TestKitHostConfigRejectsUnusableInputs(t *testing.T) {
	root := t.TempDir()
	template := shippedKitTemplate(t)
	if _, err := KitHostConfig(template, nil, KitHost{State: root, Staging: root}); err == nil {
		t.Fatal("missing agent accepted")
	}
	if _, err := KitHostConfig(template, nil, kitHostFixture("relative", "t", "")); err == nil {
		t.Fatal("relative state accepted")
	}
	if _, err := KitHostConfig(template, []byte("unknown_key = 1\n"), kitHostFixture(root, "t", "")); err == nil {
		t.Fatal("unknown key in the existing config accepted")
	}
}

func TestFolderWatchSecondsZeroTurnsTheWatchOff(t *testing.T) {
	base := "base_url = \"http://192.0.2.20:8182\"\ntoken = \"t\"\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 60\n"
	for _, test := range []struct {
		library string
		want    FolderWatchConfig
		fails   bool
	}{
		{"", FolderWatchConfig{}, false},
		{"[library]\nfolder_watch_seconds = 0\n", FolderWatchConfig{Off: true}, false},
		{"[library]\nfolder_watch_seconds = 45\n", FolderWatchConfig{Interval: 45 * time.Second}, false},
		{"[library]\nfolder_watch_seconds = -1\n", FolderWatchConfig{}, true},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(base+test.library), 0o600); err != nil {
			t.Fatal(err)
		}
		config, err := LoadConfig(path)
		if test.fails {
			if err == nil {
				t.Fatalf("%q accepted", test.library)
			}
			continue
		}
		if err != nil || config.FolderWatch != test.want {
			t.Fatalf("%q: %+v %v", test.library, config.FolderWatch, err)
		}
	}
}

func TestRunFolderWatchOffNeverReconciles(t *testing.T) {
	service := &Service{folderWatchOff: true}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// With the watch on, a zero Service would panic on its first reconcile.
	if err := service.RunFolderWatch(ctx); err != nil {
		t.Fatal(err)
	}
}

func kitModeService(t *testing.T, address string) *Service {
	t.Helper()
	return newService(
		Config{
			Targets:        []TargetConfig{{Name: KitHostTargetName, Enabled: true, Address: address, Agent: "agent-token", TargetID: "73dc9f5f-1a12-4a95-a820-a9b4e600769a"}},
			SelectedTarget: KitHostTargetName,
			CorePackages:   CorePackagesConfig{Kit: true},
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
}

// A slow or absent loopback agent must not move a kit host onto whatever
// answers mDNS for its target ID.
func TestKitHostNeverResolvesItsAgentByMDNS(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	service := kitModeService(t, closed.URL)
	resolved := 0
	service.resolveTarget = func(context.Context, string) ([]string, error) {
		resolved++
		return []string{"http://192.0.2.50:8182"}, nil
	}
	base, err := url.Parse(closed.URL)
	if err != nil {
		t.Fatal(err)
	}
	selected := service.targets[0]
	if _, address, err := service.probeTarget(context.Background(), targetclient.NewClient(base, "agent-token", http.DefaultClient), selected); err == nil || address != "" {
		t.Fatalf("probe = %q %v, want an error", address, err)
	}
	if resolved != 0 {
		t.Fatalf("resolved by mDNS %d times", resolved)
	}
	service.kitHost = false
	_, _, _ = service.probeTarget(context.Background(), targetclient.NewClient(base, "agent-token", http.DefaultClient), selected)
	if resolved != 1 {
		t.Fatalf("a desktop host resolved %d times, want 1", resolved)
	}
}

func TestKitHostRefusesSettingsEditsToItsTarget(t *testing.T) {
	ctx := context.Background()
	service := kitModeService(t, "http://127.0.0.1:8182")
	kit := service.targets[0]
	for name, next := range map[string]LibraryConfig{
		"another target": {Targets: []TargetConfig{kit, {Name: "lan", Enabled: true, Address: "http://192.0.2.50:8182", Agent: "other"}}, SelectedTarget: KitHostTargetName},
		"moved address":  {Targets: []TargetConfig{{Name: KitHostTargetName, Enabled: true, Address: "http://192.0.2.50:8182", Agent: kit.Agent, TargetID: kit.TargetID}}, SelectedTarget: KitHostTargetName},
	} {
		next.AttractIdleSeconds, next.PreferredRegions = 60, []string{"usa"}
		var apiErr *protocol.APIError
		if err := service.SetLibrarySettings(ctx, next); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
			t.Fatalf("%s: err = %v, want bad request", name, err)
		}
	}
	if len(service.targets) != 1 || service.targets[0].Address != kit.Address {
		t.Fatalf("targets changed: %+v", service.targets)
	}
	// The same target, sent back unchanged, still lets other settings change.
	if err := service.SetLibrarySettings(ctx, LibraryConfig{AttractIdleSeconds: 90, PreferredRegions: []string{"usa"}, Targets: []TargetConfig{kit}, SelectedTarget: KitHostTargetName}); err != nil {
		t.Fatalf("unchanged target: %v", err)
	}
}

func TestKitHostConfigKeepsALearnedTargetIDWhenTheAgentIsSlow(t *testing.T) {
	root := t.TempDir()
	learned := "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	first, err := KitHostConfig(shippedKitTemplate(t), nil, kitHostFixture(root, "token", learned))
	if err != nil {
		t.Fatal(err)
	}
	again, err := KitHostConfig(shippedKitTemplate(t), first, kitHostFixture(root, "token", ""))
	if err != nil {
		t.Fatal(err)
	}
	if config := loadRendered(t, again); config.Targets[0].TargetID != learned {
		t.Fatalf("target ID %q, want %q kept", config.Targets[0].TargetID, learned)
	}
}
