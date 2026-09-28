package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

type launcherListenerConfig struct {
	Listen string `json:"listen"`
	hostapi.LauncherConfig
}

func loadLauncherConfig(path string) (launcherListenerConfig, error) {
	var config launcherListenerConfig
	file, err := os.Open(path)
	if err != nil {
		return config, errors.New("launcher configuration is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return config, errors.New("launcher configuration must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return config, errors.New("invalid launcher configuration")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return config, errors.New("invalid launcher configuration")
	}
	host, port, err := net.SplitHostPort(config.Listen)
	number, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(host) == nil || portErr != nil || number < 1 || number > 65535 {
		return config, errors.New("invalid launcher configuration")
	}
	if err := config.LauncherConfig.Validate(); err != nil {
		if errors.Is(err, hostapi.ErrLauncherSharedToken) {
			return config, err
		}
		return config, errors.New("invalid launcher configuration")
	}
	return config, nil
}

// reusesAgentToken reports whether any launcher bearer equals the
// host-to-agent token of any configured target, enabled or not, or the
// selected target's compatibility token. Every candidate is compared in
// constant time and the loop does not stop at the first match.
func (c launcherListenerConfig) reusesAgentToken(config fogcast.Config) bool {
	candidates := make([]string, 0, len(config.Targets)+1)
	candidates = append(candidates, config.Token)
	for _, target := range config.Targets {
		candidates = append(candidates, target.Agent)
	}
	reused := false
	for _, candidate := range candidates {
		if c.UsesToken(candidate) {
			reused = true
		}
	}
	return reused
}
