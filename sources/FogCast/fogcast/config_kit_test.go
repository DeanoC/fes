package fogcast

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
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
	rendered, err := KitHostConfig(shippedKitTemplate(t), nil, kitHostFixture(root, "agent-token", "73dc9f5f-1a12-4a95-a820-a9b4e600769a"))
	if err != nil {
		t.Fatal(err)
	}
	config := loadRendered(t, rendered)
	if len(config.Targets) != 1 || config.SelectedTarget != KitHostTargetName {
		t.Fatalf("targets %+v selected %q", config.Targets, config.SelectedTarget)
	}
	target := config.Targets[0]
	if !target.Enabled || target.Address != "http://127.0.0.1:8182" || target.Agent != "agent-token" || target.TargetID != "73dc9f5f-1a12-4a95-a820-a9b4e600769a" {
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
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := KitHostConfig(shippedKitTemplate(t), existing, kitHostFixture(root, "new-token", "0d0f2c4e-5b6a-4c3d-8e9f-a1b2c3d4e5f6"))
	if err != nil {
		t.Fatal(err)
	}
	config := loadRendered(t, restarted)
	if len(config.Libraries) != 2 || config.Libraries[1].Path != added {
		t.Fatalf("libraries after restart %+v", config.Libraries)
	}
	if len(config.Targets) != 1 || config.Targets[0].Agent != "new-token" || config.Targets[0].TargetID != "0d0f2c4e-5b6a-4c3d-8e9f-a1b2c3d4e5f6" {
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
