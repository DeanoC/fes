package agentconfig

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/pelletier/go-toml/v2"
)

const DefaultCacheMaxBytes int64 = 2 << 30

type Config struct {
	TargetID           string
	ListenAddress      string
	Token              string
	CacheMaxBytes      int64
	InputListenAddress string
	InputUInputPath    string
	// MeshContent installs the launcher-backed content source and the
	// installed-package ABI list. It defaults on. false keeps a nil
	// source and an empty ABI list.
	MeshContent bool
}

type fileConfig struct {
	TargetID           string `toml:"target_id"`
	ListenAddress      string `toml:"listen_address"`
	Token              string `toml:"token"`
	CacheMaxBytes      *int64 `toml:"cache_max_bytes"`
	InputListenAddress string `toml:"input_listen_address"`
	InputUInputPath    string `toml:"input_uinput_path"`
	MeshContent        *bool  `toml:"mesh_content"`
}

// retiredSettingsError contains only allowlisted public setting names, never
// parser diagnostics, values, or filesystem paths.
type retiredSettingsError struct{ keys []string }

func (e *retiredSettingsError) Error() string {
	return "remove retired Main settings from the agent configuration: " + strings.Join(e.keys, ", ") + "; FES packages use the native runtime"
}

// RetiredSettingsMessage exposes only the safe actionable migration error.
func RetiredSettingsMessage(err error) (string, bool) {
	var retired *retiredSettingsError
	if errors.As(err, &retired) {
		return retired.Error(), true
	}
	return "", false
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var raw fileConfig
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		var unknown *toml.StrictMissingError
		if errors.As(err, &unknown) {
			keys := map[string]bool{}
			for _, field := range unknown.Errors {
				key := field.Key()
				if len(key) != 1 {
					continue
				}
				switch key[0] {
				case "mister_process_comm", "command_pipe", "core_name_file", "menu_rbf", "mgl_directory":
					keys[key[0]] = true
				}
			}
			if len(keys) != 0 {
				names := make([]string, 0, len(keys))
				for key := range keys {
					names = append(names, key)
				}
				sort.Strings(names)
				return Config{}, &retiredSettingsError{keys: names}
			}
		}
		return Config{}, fmt.Errorf("decode target config: %w", err)
	}
	cacheMaxBytes := DefaultCacheMaxBytes
	if raw.CacheMaxBytes != nil {
		cacheMaxBytes = *raw.CacheMaxBytes
	}
	meshContent := true
	if raw.MeshContent != nil {
		meshContent = *raw.MeshContent
	}
	cfg := Config{
		TargetID:           raw.TargetID,
		ListenAddress:      raw.ListenAddress,
		Token:              raw.Token,
		CacheMaxBytes:      cacheMaxBytes,
		InputListenAddress: raw.InputListenAddress,
		InputUInputPath:    raw.InputUInputPath,
		MeshContent:        meshContent,
	}
	if cfg.InputListenAddress == "" {
		cfg.InputListenAddress = "127.0.0.1:18183"
	}
	if cfg.InputUInputPath == "" {
		cfg.InputUInputPath = "/dev/uinput"
	}
	if _, _, err := net.SplitHostPort(cfg.InputListenAddress); err != nil {
		return Config{}, fmt.Errorf("input_listen_address: %w", err)
	}
	if !filepath.IsAbs(cfg.InputUInputPath) {
		return Config{}, fmt.Errorf("input_uinput_path must be absolute")
	}
	if cfg.CacheMaxBytes <= 0 {
		return Config{}, fmt.Errorf("cache_max_bytes must be positive")
	}
	if cfg.TargetID != "" && !discovery.ValidID(cfg.TargetID) {
		return Config{}, fmt.Errorf("target_id must be a canonical lowercase UUID")
	}
	_, _, err = net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("listen_address: %w", err)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return Config{}, fmt.Errorf("token must not be empty")
	}
	return cfg, nil
}
