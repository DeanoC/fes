package host

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type ConnectionConfig struct {
	BaseURL        *url.URL
	Token          string
	RequestTimeout time.Duration
	ManifestPath   string
}

type connectionFile struct {
	BaseURL              string `toml:"base_url"`
	Token                string `toml:"token"`
	RequestTimeoutSecond int64  `toml:"request_timeout_seconds"`
	ManifestPath         string `toml:"manifest_path"`
}

func LoadConnection(path string) (ConnectionConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return ConnectionConfig{}, err
	}
	defer f.Close()
	var raw connectionFile
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return ConnectionConfig{}, fmt.Errorf("decode host connection: %w", err)
	}
	baseURL, err := url.Parse(raw.BaseURL)
	if err != nil {
		return ConnectionConfig{}, fmt.Errorf("base_url: %w", err)
	}
	if baseURL.Scheme != "http" || baseURL.Hostname() == "" || baseURL.Port() != "8182" || baseURL.User != nil || baseURL.Fragment != "" || baseURL.RawQuery != "" || (baseURL.Path != "" && baseURL.Path != "/") {
		return ConnectionConfig{}, fmt.Errorf("base_url must be an HTTP origin on port 8182 without credentials, path, query, or fragment")
	}
	baseURL.Path = ""
	if strings.TrimSpace(raw.Token) == "" {
		return ConnectionConfig{}, fmt.Errorf("token must not be empty")
	}
	maxDurationSeconds := int64(time.Duration(1<<63-1) / time.Second)
	if raw.RequestTimeoutSecond <= 0 || raw.RequestTimeoutSecond > maxDurationSeconds {
		return ConnectionConfig{}, fmt.Errorf("request_timeout_seconds must fit a positive time.Duration")
	}
	if strings.TrimSpace(raw.ManifestPath) == "" {
		return ConnectionConfig{}, fmt.Errorf("manifest_path must not be empty")
	}
	manifestPath := raw.ManifestPath
	if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(filepath.Dir(path), manifestPath)
	}
	return ConnectionConfig{
		BaseURL:        baseURL,
		Token:          raw.Token,
		RequestTimeout: time.Duration(raw.RequestTimeoutSecond) * time.Second,
		ManifestPath:   filepath.Clean(manifestPath),
	}, nil
}
