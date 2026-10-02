package fogcast

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveCatalogConfig chooses the catalog file an installed launcher boots.
// An explicit path is used only when that file exists; a missing explicit
// path does not fall through to the home library. Otherwise the order is
// FOGCAST_CONFIG, FES_HOST_CONFIG, config.toml beside launcherConfig, then
// DefaultPaths when that file exists. An empty result means keep the
// configured host API.
func ResolveCatalogConfig(explicit, launcherConfig string, getenv func(string) string) string {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if regularFile(explicit) {
			return explicit
		}
		return ""
	}
	for _, key := range []string{"FOGCAST_CONFIG", "FES_HOST_CONFIG"} {
		if path := strings.TrimSpace(getenv(key)); path != "" && regularFile(path) {
			return path
		}
	}
	if strings.TrimSpace(launcherConfig) != "" {
		sibling := filepath.Join(filepath.Dir(launcherConfig), "config.toml")
		if regularFile(sibling) {
			return sibling
		}
	}
	if defaults, err := DefaultPaths(); err == nil && regularFile(defaults.Config) {
		return defaults.Config
	}
	return ""
}

// PathsForConfig is the catalog Paths for one resolved config file.
// The default user config keeps DefaultPaths. Any other file uses a state
// directory beside that file so a kit or a test does not open the operator's
// home library.
func PathsForConfig(configPath string) (Paths, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return Paths{}, fmt.Errorf("catalog config path is empty")
	}
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() {
		return Paths{}, fmt.Errorf("catalog config is unavailable")
	}
	configPath = filepath.Clean(configPath)
	if defaults, err := DefaultPaths(); err == nil && filepath.Clean(defaults.Config) == configPath {
		return defaults, nil
	}
	dir := filepath.Dir(configPath)
	state := filepath.Join(dir, "state")
	paths := Paths{
		Config:          configPath,
		Index:           filepath.Join(state, "library.sqlite3"),
		Staging:         filepath.Join(dir, "staging"),
		MetadataRoot:    filepath.Join(dir, "metadata"),
		UserLibrary:     filepath.Join(state, "library-user.sqlite3"),
		LibrarySettings: filepath.Join(state, "library-settings.json"),
		MediaIndex:      filepath.Join(state, "library-media.sqlite3"),
		MediaCache:      filepath.Join(dir, "media-cache"),
		CorePackages:    filepath.Join(state, "core-packages"),
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return Paths{}, err
	}
	if err := os.MkdirAll(paths.Staging, 0o700); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
