package targetcache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLaunchMapRoundTripAndForget(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), targetcache.LaunchMapName)
	m, err := targetcache.OpenLaunchMap(path)
	if err != nil {
		t.Fatal(err)
	}
	content := protocol.ContentIdentity{
		SHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Size:      4,
		Extension: "md",
	}
	if err := m.Remember("megadrive-sonic-aaaaaa", protocol.SystemMegaDrive, content); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Lookup("megadrive-sonic-aaaaaa")
	if !ok || got.System != protocol.SystemMegaDrive || got.Content != content {
		t.Fatalf("lookup = %#v ok=%v", got, ok)
	}
	reopened, err := targetcache.OpenLaunchMap(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok = reopened.Lookup("megadrive-sonic-aaaaaa")
	if !ok || got.Content != content {
		t.Fatalf("reopen = %#v ok=%v", got, ok)
	}
	if err := reopened.Forget("megadrive-sonic-aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Lookup("megadrive-sonic-aaaaaa"); ok {
		t.Fatal("forgot entry still present")
	}
}

func TestLaunchMapRejectsInvalidAndPartNames(t *testing.T) {
	t.Parallel()
	if _, err := targetcache.OpenLaunchMap("relative/launch-map.json"); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := targetcache.OpenLaunchMap(filepath.Join(t.TempDir(), "other.json")); err == nil {
		t.Fatal("wrong name accepted")
	}
	path := filepath.Join(t.TempDir(), targetcache.LaunchMapName)
	m, err := targetcache.OpenLaunchMap(path)
	if err != nil {
		t.Fatal(err)
	}
	content := protocol.ContentIdentity{
		SHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Size:      4,
		Extension: "md",
	}
	if err := m.Remember("Not-A-Slug", protocol.SystemMegaDrive, content); err == nil {
		t.Fatal("invalid game id accepted")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid remember wrote file: %v", err)
	}
}

// launch-map.json.tmp aliases launch-map.json on the kit's /media/fat exFAT
// (#317), so writes must use a dot-prefixed temporary instead.
func TestLaunchMapWriteNeverUsesTargetPrefixedTemp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, targetcache.LaunchMapName)
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	m, err := targetcache.OpenLaunchMap(path)
	if err != nil {
		t.Fatal(err)
	}
	content := protocol.ContentIdentity{
		SHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Size:      4,
		Extension: "md",
	}
	if err := m.Remember("megadrive-sonic-aaaaaa", protocol.SystemMegaDrive, content); err != nil {
		t.Fatalf("launch map write used a launch-map.json-prefixed temp: %v", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != targetcache.LaunchMapName || names[1] != targetcache.LaunchMapName+".tmp" {
		t.Fatalf("unexpected directory contents %v", names)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("launch map mode: %v %v", fi, err)
	}
}
