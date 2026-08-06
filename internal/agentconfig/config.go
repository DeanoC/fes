package agentconfig

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const DefaultCacheMaxBytes int64 = 2 << 30

type Config struct {
	ListenAddress      string
	Token              string
	MiSTerProcessComm  string
	CommandPipe        string
	CoreNameFile       string
	MenuRBF            string
	MGLDirectory       string
	CacheMaxBytes      int64
	InputListenAddress string
	InputUInputPath    string
}

type fileConfig struct {
	ListenAddress      string `toml:"listen_address"`
	Token              string `toml:"token"`
	MiSTerProcessComm  string `toml:"mister_process_comm"`
	CommandPipe        string `toml:"command_pipe"`
	CoreNameFile       string `toml:"core_name_file"`
	MenuRBF            string `toml:"menu_rbf"`
	MGLDirectory       string `toml:"mgl_directory"`
	CacheMaxBytes      *int64 `toml:"cache_max_bytes"`
	InputListenAddress string `toml:"input_listen_address"`
	InputUInputPath    string `toml:"input_uinput_path"`
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
		return Config{}, fmt.Errorf("decode target config: %w", err)
	}
	cacheMaxBytes := DefaultCacheMaxBytes
	if raw.CacheMaxBytes != nil {
		cacheMaxBytes = *raw.CacheMaxBytes
	}
	cfg := Config{
		ListenAddress:      raw.ListenAddress,
		Token:              raw.Token,
		MiSTerProcessComm:  raw.MiSTerProcessComm,
		CommandPipe:        raw.CommandPipe,
		CoreNameFile:       raw.CoreNameFile,
		MenuRBF:            raw.MenuRBF,
		MGLDirectory:       raw.MGLDirectory,
		CacheMaxBytes:      cacheMaxBytes,
		InputListenAddress: raw.InputListenAddress,
		InputUInputPath:    raw.InputUInputPath,
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
	_, port, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("listen_address: %w", err)
	}
	if port != "8182" {
		return Config{}, fmt.Errorf("listen_address must use port 8182")
	}
	if strings.TrimSpace(cfg.Token) == "" || strings.TrimSpace(cfg.MiSTerProcessComm) == "" {
		return Config{}, fmt.Errorf("token and mister_process_comm must not be empty")
	}
	for name, value := range map[string]string{
		"command_pipe":   cfg.CommandPipe,
		"core_name_file": cfg.CoreNameFile,
		"menu_rbf":       cfg.MenuRBF,
		"mgl_directory":  cfg.MGLDirectory,
	} {
		if !filepath.IsAbs(value) {
			return Config{}, fmt.Errorf("%s must be absolute", name)
		}
	}
	return cfg, nil
}
