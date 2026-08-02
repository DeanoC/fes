// Package fogcast contains the host-side FogCast configuration and services.
package fogcast

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/pelletier/go-toml/v2"
)

type Paths struct {
	Config  string
	Index   string
	Staging string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}
	return Paths{
		Config:  filepath.Join(home, ".config", "fogcast", "config.toml"),
		Index:   filepath.Join(home, ".local", "share", "fogcast", "library.sqlite3"),
		Staging: filepath.Join(home, ".cache", "fogcast", "staging"),
	}, nil
}

type Config struct {
	BaseURL        string
	Token          string
	RequestTimeout time.Duration
	UploadTimeout  time.Duration
	Libraries      []catalog.Root
}

type fileConfig struct {
	BaseURL               string        `toml:"base_url"`
	Token                 string        `toml:"token"`
	RequestTimeoutSeconds int64         `toml:"request_timeout_seconds"`
	UploadTimeoutSeconds  int64         `toml:"upload_timeout_seconds"`
	Libraries             []fileLibrary `toml:"libraries"`
}

type fileLibrary struct {
	ID     string          `toml:"id"`
	System protocol.System `toml:"system"`
	Root   string          `toml:"root"`
}

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var raw fileConfig
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode FogCast config: %w", err)
	}

	baseURL, err := normalizeHTTPOrigin(raw.BaseURL)
	if err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(raw.Token) == "" {
		return Config{}, fmt.Errorf("token must not be empty")
	}
	requestTimeout, err := positiveDuration("request_timeout_seconds", raw.RequestTimeoutSeconds)
	if err != nil {
		return Config{}, err
	}
	uploadTimeout, err := positiveDuration("upload_timeout_seconds", raw.UploadTimeoutSeconds)
	if err != nil {
		return Config{}, err
	}
	libraries, err := normalizeLibraries(raw.Libraries)
	if err != nil {
		return Config{}, err
	}

	return Config{
		BaseURL:        baseURL,
		Token:          raw.Token,
		RequestTimeout: requestTimeout,
		UploadTimeout:  uploadTimeout,
		Libraries:      libraries,
	}, nil
}

func normalizeHTTPOrigin(raw string) (string, error) {
	baseURL, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("base_url: %w", err)
	}
	if baseURL.Scheme != "http" || baseURL.Hostname() == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" || strings.Contains(raw, "#") || (baseURL.Path != "" && baseURL.Path != "/") {
		return "", fmt.Errorf("base_url must be an HTTP origin without credentials, path, query, or fragment")
	}
	baseURL.Path = ""
	return baseURL.String(), nil
}

func positiveDuration(name string, seconds int64) (time.Duration, error) {
	maxSeconds := int64(time.Duration(1<<63-1) / time.Second)
	if seconds <= 0 || seconds > maxSeconds {
		return 0, fmt.Errorf("%s must fit a positive time.Duration", name)
	}
	return time.Duration(seconds) * time.Second, nil
}

func normalizeLibraries(raw []fileLibrary) ([]catalog.Root, error) {
	libraries := make([]catalog.Root, 0, len(raw))
	ids := make(map[string]struct{}, len(raw))
	roots := make(map[string]struct{}, len(raw))
	for _, library := range raw {
		if err := protocol.ValidateGameID(library.ID); err != nil {
			return nil, fmt.Errorf("library id: %w", err)
		}
		if _, duplicate := ids[library.ID]; duplicate {
			return nil, fmt.Errorf("duplicate library id %q", library.ID)
		}
		if err := protocol.ValidateSystem(library.System); err != nil {
			return nil, fmt.Errorf("library %q: %w", library.ID, err)
		}
		root, err := normalizeRoot(library.Root)
		if err != nil {
			return nil, fmt.Errorf("library %q root: %w", library.ID, err)
		}
		if _, duplicate := roots[root]; duplicate {
			return nil, fmt.Errorf("duplicate library root %q", root)
		}
		ids[library.ID] = struct{}{}
		roots[root] = struct{}{}
		libraries = append(libraries, catalog.Root{ID: library.ID, System: library.System, Path: root})
	}
	return libraries, nil
}

func normalizeRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("must be absolute")
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err == nil {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}
	return root, nil
}
