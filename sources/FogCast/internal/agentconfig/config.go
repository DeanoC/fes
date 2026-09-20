package agentconfig

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/pelletier/go-toml/v2"
)

const DefaultCacheMaxBytes int64 = 2 << 30

type Config struct {
	TargetID           string
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
	CastBinary         string
	CastRTPAddress     string
	CastControlAddress string
	CastFramebuffer    string
	CastNativeCmd      string
	CastNativeMode     string
	CastTokenFile      string
	CastGeneration     uint64
}

type fileConfig struct {
	TargetID           string `toml:"target_id"`
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
	CastBinary         string `toml:"cast_binary"`
	CastRTPAddress     string `toml:"cast_rtp_address"`
	CastControlAddress string `toml:"cast_control_address"`
	CastFramebuffer    string `toml:"cast_framebuffer"`
	CastNativeCmd      string `toml:"cast_native_cmd"`
	CastNativeMode     string `toml:"cast_native_mode"`
	CastTokenFile      string `toml:"cast_token_file"`
	CastGeneration     uint64 `toml:"cast_generation"`
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
		TargetID:           raw.TargetID,
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
		CastBinary:         raw.CastBinary,
		CastRTPAddress:     raw.CastRTPAddress,
		CastControlAddress: raw.CastControlAddress,
		CastFramebuffer:    raw.CastFramebuffer,
		CastNativeCmd:      raw.CastNativeCmd,
		CastNativeMode:     raw.CastNativeMode,
		CastTokenFile:      raw.CastTokenFile,
		CastGeneration:     raw.CastGeneration,
	}
	if cfg.InputListenAddress == "" {
		cfg.InputListenAddress = "127.0.0.1:18183"
	}
	if cfg.InputUInputPath == "" {
		cfg.InputUInputPath = "/dev/uinput"
	}
	if cfg.CastBinary != "" {
		if cfg.CastRTPAddress == "" || cfg.CastControlAddress == "" || cfg.CastFramebuffer == "" || cfg.CastNativeCmd == "" || cfg.CastNativeMode == "" {
			return Config{}, fmt.Errorf("cast configuration is incomplete")
		}
		if !filepath.IsAbs(cfg.CastBinary) || !filepath.IsAbs(cfg.CastFramebuffer) || !filepath.IsAbs(cfg.CastNativeCmd) || (cfg.CastTokenFile != "" && !filepath.IsAbs(cfg.CastTokenFile)) {
			return Config{}, fmt.Errorf("cast paths must be absolute")
		}
		if _, _, err := net.SplitHostPort(cfg.CastRTPAddress); err != nil {
			return Config{}, fmt.Errorf("cast_rtp_address: %w", err)
		}
		if _, _, err := net.SplitHostPort(cfg.CastControlAddress); err != nil {
			return Config{}, fmt.Errorf("cast_control_address: %w", err)
		}
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
