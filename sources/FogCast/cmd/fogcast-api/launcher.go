package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"

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
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return config, errors.New("launcher configuration must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
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
	if err != nil || net.ParseIP(host) == nil || portErr != nil || number < 1 || number > 65535 || config.LauncherConfig.Validate() != nil {
		return config, errors.New("invalid launcher configuration")
	}
	return config, nil
}
