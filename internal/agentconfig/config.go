package agentconfig

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ListenAddress     string `toml:"listen_address"`
	Token             string `toml:"token"`
	MiSTerProcessComm string `toml:"mister_process_comm"`
	CommandPipe       string `toml:"command_pipe"`
	CoreNameFile      string `toml:"core_name_file"`
	MenuRBF           string `toml:"menu_rbf"`
	MGLDirectory      string `toml:"mgl_directory"`
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var cfg Config
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode target config: %w", err)
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
