package buildinputs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/applianceupdate"
)

func TestSnapshotReadsSealedBuildInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	record := "format=1\n" +
		"mister_runtime_commit=1111111111111111111111111111111111111111\n" +
		"mister_agent_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"fogcast_kit_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"idle_sha256=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\n" +
		"megadrive_abi=mister\n" +
		"megadrive_sha256=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\n" +
		"fes_pong_package_id=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee\n"
	buildInputs := filepath.Join(root, "build-inputs")
	if err := os.WriteFile(buildInputs, []byte(record), 0o444); err != nil {
		t.Fatal(err)
	}
	selections := filepath.Join(root, "selections")
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selections, "snes.toml"),
		[]byte("format = 1\nsystem = 'snes'\nsha256 = 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	got := Snapshot(Paths{BuildInputs: buildInputs, Selections: selections, BootJSON: filepath.Join(root, "missing.json"), BootIDFile: filepath.Join(root, "boot_id")}, "abc123")
	if got == nil {
		t.Fatal("snapshot is nil")
	}
	sum := sha256.Sum256([]byte(record))
	if got.RecordSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("record sha = %s", got.RecordSHA256)
	}
	if got.RuntimeCommit != "1111111111111111111111111111111111111111" || got.AgentSHA256 == "" || got.KitSHA256 == "" {
		t.Fatalf("identity = %+v", got)
	}
	if got.AgentRevision != "abc123" || got.ABI != "mister" || got.PackageID == "" {
		t.Fatalf("revision/abi = %+v", got)
	}
	if got.Cores["megadrive"] == "" || got.Cores["snes"] == "" || got.Cores["idle"] != "" {
		t.Fatalf("cores = %#v", got.Cores)
	}
	if got.IdleSHA256 == "" {
		t.Fatalf("idle sha missing: %+v", got)
	}
	if got.ImageSHA256 != "" {
		t.Fatalf("unexpected image sha %q", got.ImageSHA256)
	}
}

func TestSnapshotKeepsBuildInputsOverSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fromRecord := strings.Repeat("d", 64)
	if err := os.WriteFile(filepath.Join(root, "build-inputs"), []byte(
		"format=1\nmegadrive_sha256="+fromRecord+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	selections := filepath.Join(root, "selections")
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selections, "megadrive.toml"),
		[]byte("system = 'megadrive'\nsha256 = '"+strings.Repeat("e", 64)+"'\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	got := Snapshot(Paths{BuildInputs: filepath.Join(root, "build-inputs"), Selections: selections,
		BootJSON: filepath.Join(root, "missing"), BootIDFile: filepath.Join(root, "missing")}, "")
	if got == nil || got.Cores["megadrive"] != fromRecord {
		t.Fatalf("cores = %#v", got)
	}
}

func TestSnapshotRejectsDuplicateBuildInputKeys(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "build-inputs")
	if err := os.WriteFile(path, []byte("format=1\nmegadrive_sha256="+strings.Repeat("a", 64)+"\nmegadrive_sha256="+strings.Repeat("b", 64)+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	got := Snapshot(Paths{BuildInputs: path, Selections: filepath.Join(root, "none"),
		BootJSON: filepath.Join(root, "none"), BootIDFile: filepath.Join(root, "none")}, "")
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSnapshotOmitsInvalidRecords(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "build-inputs")
	if err := os.WriteFile(path, []byte("format=2\nmister_runtime_commit=1\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	got := Snapshot(Paths{BuildInputs: path, Selections: filepath.Join(root, "none"), BootJSON: filepath.Join(root, "none"), BootIDFile: filepath.Join(root, "none")}, "")
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSnapshotUsesMatchingBootTicket(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bootID := "6f2656f4-885c-42b2-a2dd-36369295d4b8"
	image := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(root, "boot_id"), []byte(bootID+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ticket := filepath.Join(root, "boot.json")
	if err := os.WriteFile(ticket, []byte(`{"boot_id":"`+bootID+`","image_sha256":"`+image+`","trial":false}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := applianceupdate.ReadBootIdentity(ticket, filepath.Join(root, "boot_id")); err != nil {
		t.Fatal(err)
	}
	got := Snapshot(Paths{
		BuildInputs: filepath.Join(root, "missing"),
		Selections:  filepath.Join(root, "missing"),
		BootJSON:    ticket,
		BootIDFile:  filepath.Join(root, "boot_id"),
	}, "rev")
	if got == nil || got.ImageSHA256 != image || got.AgentRevision != "rev" {
		t.Fatalf("got %+v", got)
	}
}
