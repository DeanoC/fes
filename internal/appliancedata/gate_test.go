package appliancedata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/appliance"
)

func testManifest(image []byte) release.Manifest {
	return release.Manifest{
		Format: 1, Board: release.Board, BootABI: release.BootABI, Version: "v1",
		KernelSHA256: strings.Repeat("a", 64), ImageSHA256: fmt.Sprintf("%x", sha256.Sum256(image)),
		ImageSize: int64(len(image)), FESRevision: strings.Repeat("b", 40),
		FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40),
	}
}

func writeFactory(t *testing.T, path string, m release.Manifest) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBootTicket(t *testing.T, jsonPath, idPath, bootID, image string, trial bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idPath, []byte(bootID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ticket := fmt.Sprintf(`{"boot_id":%q,"image_sha256":%q,"trial":%t}`, bootID, image, trial)
	if err := os.WriteFile(jsonPath, []byte(ticket), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadUpdateGateDirectRootIsClear(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gate, err := ReadUpdateGate(GatePaths{
		Factory:     filepath.Join(root, "missing-factory.json"),
		MountInfo:   filepath.Join(root, "missing-mountinfo"),
		BootJSON:    filepath.Join(root, "boot.json"),
		BootIDPath:  filepath.Join(root, "boot_id"),
		ReleaseRoot: filepath.Join(root, "releases"),
	})
	if err != nil || gate.Trial || gate.Pending || gate.Corrupt {
		t.Fatalf("direct-root gate = %+v %v", gate, err)
	}
}

func TestReadUpdateGateReportsTrialPendingAndCorrupt(t *testing.T) {
	t.Parallel()
	image := bytes.Repeat([]byte{0x53, 0xef}, 2048)
	image[1080], image[1081], image[1120] = 0x53, 0xef, 0x40
	m := testManifest(image)
	bootID := "11111111-2222-4333-8444-555555555555"

	t.Run("trial", func(t *testing.T) {
		root := t.TempDir()
		factory := filepath.Join(root, "factory.json")
		releases := filepath.Join(root, "releases")
		writeFactory(t, factory, m)
		store, err := appliance.New(releases, m)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(image)); err != nil {
			t.Fatal(err)
		}
		writeBootTicket(t, filepath.Join(releases, "boot.json"), filepath.Join(root, "boot_id"), bootID, m.ImageSHA256, true)
		gate, err := ReadUpdateGate(GatePaths{
			Factory: factory, MountInfo: filepath.Join(root, "mounts"),
			BootJSON: filepath.Join(releases, "boot.json"), BootIDPath: filepath.Join(root, "boot_id"),
			ReleaseRoot: releases,
		})
		if err != nil || !gate.Trial || gate.Pending || gate.Corrupt {
			t.Fatalf("trial gate = %+v %v", gate, err)
		}
	})

	t.Run("pending", func(t *testing.T) {
		root := t.TempDir()
		factory := filepath.Join(root, "factory.json")
		releases := filepath.Join(root, "releases")
		writeFactory(t, factory, m)
		store, err := appliance.New(releases, m)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(image)); err != nil {
			t.Fatal(err)
		}
		candidate := bytes.Clone(image)
		candidate[0]++
		cm := testManifest(candidate)
		if err := store.Stage(context.Background(), cm, cm.ImageSize, bytes.NewReader(candidate)); err != nil {
			t.Fatal(err)
		}
		if err := store.Activate(cm.ImageSHA256); err != nil {
			t.Fatal(err)
		}
		writeBootTicket(t, filepath.Join(releases, "boot.json"), filepath.Join(root, "boot_id"), bootID, m.ImageSHA256, false)
		gate, err := ReadUpdateGate(GatePaths{
			Factory: factory, MountInfo: filepath.Join(root, "mounts"),
			BootJSON: filepath.Join(releases, "boot.json"), BootIDPath: filepath.Join(root, "boot_id"),
			ReleaseRoot: releases,
		})
		if err != nil || gate.Trial || !gate.Pending || gate.Corrupt {
			t.Fatalf("pending gate = %+v %v", gate, err)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		root := t.TempDir()
		factory := filepath.Join(root, "factory.json")
		releases := filepath.Join(root, "releases")
		writeFactory(t, factory, m)
		if _, err := appliance.New(releases, m); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(releases, "state.json"), []byte("{not-checksummed}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		writeBootTicket(t, filepath.Join(releases, "boot.json"), filepath.Join(root, "boot_id"), bootID, m.ImageSHA256, false)
		gate, err := ReadUpdateGate(GatePaths{
			Factory: factory, MountInfo: filepath.Join(root, "mounts"),
			BootJSON: filepath.Join(releases, "boot.json"), BootIDPath: filepath.Join(root, "boot_id"),
			ReleaseRoot: releases,
		})
		if err != nil || !gate.Corrupt || gate.Trial || gate.Pending {
			t.Fatalf("corrupt gate = %+v %v", gate, err)
		}
	})
}

func TestReadUpdateGateKnownGoodIsClear(t *testing.T) {
	t.Parallel()
	image := bytes.Repeat([]byte{0x53, 0xef}, 2048)
	image[1080], image[1081], image[1120] = 0x53, 0xef, 0x40
	m := testManifest(image)
	root := t.TempDir()
	factory := filepath.Join(root, "factory.json")
	releases := filepath.Join(root, "releases")
	writeFactory(t, factory, m)
	store, err := appliance.New(releases, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(image)); err != nil {
		t.Fatal(err)
	}
	bootID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	writeBootTicket(t, filepath.Join(releases, "boot.json"), filepath.Join(root, "boot_id"), bootID, m.ImageSHA256, false)
	gate, err := ReadUpdateGate(GatePaths{
		Factory: factory, MountInfo: filepath.Join(root, "mounts"),
		BootJSON: filepath.Join(releases, "boot.json"), BootIDPath: filepath.Join(root, "boot_id"),
		ReleaseRoot: releases,
	})
	if err != nil || gate.Trial || gate.Pending || gate.Corrupt {
		t.Fatalf("known-good gate = %+v %v", gate, err)
	}
}

func TestReadUpdateGateDamagedBootstrapWithoutFactory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mounts := filepath.Join(root, "mountinfo")
	body := "36 35 98:0 / /.fes-bootstrap rw - ext4 /dev/loop0 rw\n"
	if err := os.WriteFile(mounts, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadUpdateGate(GatePaths{
		Factory: filepath.Join(root, "missing"), MountInfo: mounts,
		ReleaseRoot: filepath.Join(root, "releases"),
	})
	if err == nil || !strings.Contains(err.Error(), "factory") {
		t.Fatalf("damaged bootstrap = %v", err)
	}
}

func TestPrepareRefusesWhenUpdateStatusUnreadable(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	cfg.EvaluateGate = func() (Gate, error) { return Gate{}, fmt.Errorf("ticket mismatch") }
	result, err := Prepare(cfg)
	if err == nil || result.Skipped != SkipUpdate {
		t.Fatalf("unreadable update = %+v %v", result, err)
	}
	if len(mounter.volumes) != 0 {
		t.Fatal("unreadable update still mounted p3")
	}
}
