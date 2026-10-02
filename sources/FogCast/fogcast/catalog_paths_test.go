package fogcast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestResolveCatalogConfigPrefersSiblingAndExplicit(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "launcher.json")
	sibling := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(launcher, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("token = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(string) string { return "" }
	if got := ResolveCatalogConfig("", launcher, getenv); got != sibling {
		t.Fatalf("sibling = %q", got)
	}
	missing := filepath.Join(dir, "missing.toml")
	if got := ResolveCatalogConfig(missing, launcher, getenv); got != "" {
		t.Fatalf("missing explicit fell through to %q", got)
	}
	other := filepath.Join(dir, "other.toml")
	if err := os.WriteFile(other, []byte("token = \"y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveCatalogConfig("", launcher, func(key string) string {
		if key == "FOGCAST_CONFIG" {
			return other
		}
		return ""
	}); got != other {
		t.Fatalf("env = %q", got)
	}
	paths, err := PathsForConfig(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Config != sibling || filepath.Dir(paths.Index) != filepath.Join(dir, "state") {
		t.Fatalf("paths = %+v", paths)
	}
	if defaults, err := DefaultPaths(); err == nil {
		if paths.Index == defaults.Index {
			t.Fatal("sibling config reused the home library index")
		}
	}
}

func TestPathsForConfigReadOnlyDirectoryDoesNotUseTheConfigDir(t *testing.T) {
	dir := readOnlyConfigDir(t, "token = \"x\"\n")
	configPath := filepath.Join(dir, "config.toml")
	paths, err := PathsForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Config != configPath {
		t.Fatalf("config = %q", paths.Config)
	}
	assertStorageOutside(t, dir, paths)
	if _, err := os.Stat(paths.Staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(paths.Index)); err != nil {
		t.Fatal(err)
	}
	assertConfigDirUntouched(t, dir)
}

func TestPathsForConfigUsesExplicitStateWhenTheConfigDirIsReadOnly(t *testing.T) {
	writable := t.TempDir()
	state := filepath.Join(writable, "state")
	staging := filepath.Join(writable, "staging")
	body := "state = " + quoteTOML(state) + "\nstaging = " + quoteTOML(staging) + "\ntoken = \"x\"\n"
	dir := readOnlyConfigDir(t, body)
	paths, err := PathsForConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(paths.Index) != state || paths.Staging != staging {
		t.Fatalf("index %q staging %q", paths.Index, paths.Staging)
	}
	if strings.HasPrefix(paths.MetadataRoot, dir+string(os.PathSeparator)) || strings.HasPrefix(paths.MediaCache, dir+string(os.PathSeparator)) {
		t.Fatalf("metadata %q cache %q", paths.MetadataRoot, paths.MediaCache)
	}
	assertConfigDirUntouched(t, dir)
}

func TestPathsForConfigReadOnlyConfigIgnoresUnusableStateUnderUsr(t *testing.T) {
	const probe = "/usr/share/fogcast-catalog-state-probe"
	t.Cleanup(func() { _ = os.RemoveAll(probe) })
	body := "state = \"" + probe + "/state\"\nstaging = \"" + probe + "/staging\"\n"
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// A mode 0555 directory does not stop root, and the host CI user can
	// create directories under /usr without being root. The rejection has
	// to hold in both cases, so only a non-root run also locks the config
	// directory. Root still must ignore the /usr paths.
	if os.Geteuid() != 0 {
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := PathsForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	assertStorageOutside(t, dir, paths)
	root := defaultWritableCatalogRoot(configPath)
	if filepath.Dir(paths.Index) != filepath.Join(root, "state") || paths.Staging != filepath.Join(root, "staging") {
		t.Fatalf("index %q staging %q, want storage under %q", paths.Index, paths.Staging, root)
	}
	if _, err := os.Lstat(probe); !os.IsNotExist(err) {
		t.Fatalf("probe under /usr: %v", err)
	}
	assertConfigDirUntouched(t, dir)
}

func TestCatalogStoragePolicyRejectsUsrWithoutAWritabilityProbe(t *testing.T) {
	if configDirAcceptsCatalogStorage("/usr/share/fogcast/config.toml", "/usr/share/fogcast") {
		t.Fatal("a config under /usr was accepted as a place for catalog storage")
	}
	for _, path := range []string{"/usr", "/usr/", "/usr/share/fogcast/state", "/usr/local/fogcast"} {
		if !storageOnSystemReadOnlyTree(path) {
			t.Fatalf("allowed %q", path)
		}
	}
	for _, path := range []string{"/run/fogcast/catalog/state", "/tmp/fogcast", "/user/state", "/usrshare/state"} {
		if storageOnSystemReadOnlyTree(path) {
			t.Fatalf("rejected %q", path)
		}
	}
}

func TestPathsForConfigRejectsAPartialStorageSetting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("state = \"/tmp/fogcast-catalog-state\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PathsForConfig(path); err == nil {
		t.Fatal("partial state setting was accepted")
	}
}

func TestShippedKitCatalogStorageIsOffTheReadOnlyRoot(t *testing.T) {
	path := filepath.Join("..", "..", "..", "image", "buildroot", "board", "fogcast-target", "native-rootfs-overlay", "usr", "share", "fogcast", "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings catalogStorageSettings
	if err := toml.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.State != "/run/fogcast/catalog/state" || settings.Staging != "/run/fogcast/catalog/staging" {
		t.Fatalf("state=%q staging=%q", settings.State, settings.Staging)
	}
	for _, storage := range []string{settings.State, settings.Staging} {
		clean := filepath.Clean(storage)
		if !filepath.IsAbs(clean) || clean == "/usr" || strings.HasPrefix(clean, "/usr/") {
			t.Fatalf("storage path %q is under /usr", storage)
		}
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
}

func readOnlyConfigDir(t *testing.T, body string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write a mode 0555 directory")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertStorageOutside(t *testing.T, configDir string, paths Paths) {
	t.Helper()
	prefix := configDir + string(os.PathSeparator)
	for _, path := range []string{paths.Index, paths.Staging, paths.MetadataRoot, paths.MediaCache, paths.CorePackages, paths.UserLibrary, paths.LibrarySettings, paths.MediaIndex} {
		clean := filepath.Clean(path)
		if clean == configDir || strings.HasPrefix(clean, prefix) || clean == "/usr" || strings.HasPrefix(clean, "/usr/") {
			t.Fatalf("path %q is beside the read-only config or under /usr", path)
		}
	}
}

func assertConfigDirUntouched(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("config dir = %v", namesOf(entries))
	}
}

func namesOf(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func quoteTOML(path string) string {
	return "\"" + strings.ReplaceAll(path, "\\", "\\\\") + "\""
}
