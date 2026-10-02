package fogcast

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// kitCatalogRoot is the writable catalog tree on the kit. /run is the tmpfs
// that already holds FogCast sockets and /run/fogcast/fesdata3. The catalog
// opener requires a private 0700 directory. The rootfs is mounted read-only,
// /var is not a separate writable mount, and /media/fat is exFAT, which does
// not keep that mode. The index is rebuilt from the FAT library on each boot.
const kitCatalogRoot = "/run/fogcast/catalog"

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

// catalogSystemReadOnlyRoot is the kit rootfs tree that stays read-only.
// Catalog storage there is rejected by policy. A writability probe is the
// wrong test: root on a development machine, or the host CI user, can
// create directories under /usr, and the kit still cannot use them.
const catalogSystemReadOnlyRoot = "/usr"

// PathsForConfig is the catalog Paths for one resolved config file.
// Explicit state and staging in the file win when both are absolute and
// usable. The default user config keeps DefaultPaths. Any other file on a
// writable directory uses state beside that file so a test does not open
// the operator's home library. A config on a read-only directory never
// creates those directories beside the file. Paths under /usr, and paths
// beside a config that itself lives under /usr, are unusable even when the
// current user can write them. They fall back to the writable default: the
// kit tmpfs when that is writable, and a private temp directory otherwise.
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
	dir := filepath.Dir(configPath)
	settings, err := readCatalogStorageSettings(configPath)
	if err != nil {
		return Paths{}, fmt.Errorf("catalog config is unavailable")
	}
	if settings.configured() {
		state, staging, err := settings.absoluteDirs()
		if err != nil {
			if configDirAcceptsCatalogStorage(configPath, dir) {
				return Paths{}, err
			}
		} else if !storageOnSystemReadOnlyTree(state) && !storageOnSystemReadOnlyTree(staging) {
			paths, prepErr := prepareCatalogPaths(configPath, state, staging)
			if prepErr == nil {
				return paths, nil
			}
			if configDirAcceptsCatalogStorage(configPath, dir) {
				return Paths{}, prepErr
			}
		}
	} else if defaults, err := DefaultPaths(); err == nil && filepath.Clean(defaults.Config) == configPath {
		return defaults, nil
	} else if configDirAcceptsCatalogStorage(configPath, dir) {
		return prepareCatalogPaths(configPath, filepath.Join(dir, "state"), filepath.Join(dir, "staging"))
	}
	root := defaultWritableCatalogRoot(configPath)
	return prepareCatalogPaths(configPath, filepath.Join(root, "state"), filepath.Join(root, "staging"))
}

type catalogStorageSettings struct {
	State   string `toml:"state"`
	Staging string `toml:"staging"`
}

func (s catalogStorageSettings) configured() bool {
	return strings.TrimSpace(s.State) != "" || strings.TrimSpace(s.Staging) != ""
}

func (s catalogStorageSettings) absoluteDirs() (string, string, error) {
	state := filepath.Clean(strings.TrimSpace(s.State))
	staging := filepath.Clean(strings.TrimSpace(s.Staging))
	if state == "." || staging == "." || !filepath.IsAbs(state) || !filepath.IsAbs(staging) {
		return "", "", fmt.Errorf("catalog state and staging must both be absolute paths")
	}
	return state, staging, nil
}

func readCatalogStorageSettings(configPath string) (catalogStorageSettings, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return catalogStorageSettings{}, err
	}
	defer file.Close()
	var settings catalogStorageSettings
	if err := toml.NewDecoder(file).Decode(&settings); err != nil {
		return catalogStorageSettings{}, err
	}
	return settings, nil
}

func prepareCatalogPaths(configPath, state, staging string) (Paths, error) {
	root := filepath.Dir(state)
	paths := Paths{
		Config:          configPath,
		Index:           filepath.Join(state, "library.sqlite3"),
		Staging:         staging,
		MetadataRoot:    filepath.Join(root, "metadata"),
		UserLibrary:     filepath.Join(state, "library-user.sqlite3"),
		LibrarySettings: filepath.Join(state, "library-settings.json"),
		MediaIndex:      filepath.Join(state, "library-media.sqlite3"),
		MediaCache:      filepath.Join(root, "media-cache"),
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

func defaultWritableCatalogRoot(configPath string) string {
	if directoryWritable("/run/fogcast") || (directoryExists("/run") && directoryWritable("/run")) {
		return kitCatalogRoot
	}
	sum := sha256.Sum256([]byte(configPath))
	return filepath.Join(os.TempDir(), "fogcast-catalog", hex.EncodeToString(sum[:8]))
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func storageOnSystemReadOnlyTree(path string) bool {
	clean := filepath.Clean(path)
	return clean == catalogSystemReadOnlyRoot || strings.HasPrefix(clean, catalogSystemReadOnlyRoot+"/")
}

func configDirAcceptsCatalogStorage(configPath, dir string) bool {
	if storageOnSystemReadOnlyTree(configPath) || storageOnSystemReadOnlyTree(dir) {
		return false
	}
	return directoryWritable(dir)
}

func directoryWritable(dir string) bool {
	file, err := os.CreateTemp(dir, ".fogcast-write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
