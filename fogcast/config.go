// Package fogcast contains the host-side FogCast configuration and services.
package fogcast

import (
	"fmt"
	"net"
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
	RemoteInput    RemoteInputConfig
	HostEmulator   HostEmulatorConfig
	Media          MediaConfig
}

type HostEmulatorConfig struct {
	Binary  string
	Core    string
	Systems []protocol.System
}

// RemoteInputConfig contains private remote-input composition settings. The
// target bridge is owned by mister-agent; its listen and device paths are
// target configuration, not host configuration.
type RemoteInputConfig struct {
	Enabled bool
}

// MediaConfig contains the opt-in host media transport. Zero values keep
// physical capture and all media sockets disabled.
type MediaConfig struct {
	Enabled        bool
	Session        string
	Generation     uint64
	SSRC           uint32
	RTPListen      string
	RTPDestination string
	ControlAddress string
	Decoder        string
	CaptureDevice  string
	Bitrate        int
	GOP            int
	MTU            int
}

type fileConfig struct {
	BaseURL               string           `toml:"base_url"`
	Token                 string           `toml:"token"`
	RequestTimeoutSeconds int64            `toml:"request_timeout_seconds"`
	UploadTimeoutSeconds  int64            `toml:"upload_timeout_seconds"`
	Libraries             []fileLibrary    `toml:"libraries"`
	RemoteInput           fileRemoteInput  `toml:"remote_input"`
	HostEmulator          fileHostEmulator `toml:"host_emulator"`
	Media                 fileMedia        `toml:"media"`
}

type fileLibrary struct {
	ID     string          `toml:"id"`
	System protocol.System `toml:"system"`
	Root   string          `toml:"root"`
}

type fileRemoteInput struct {
	Enabled bool `toml:"enabled"`
}

type fileHostEmulator struct {
	Binary  string            `toml:"binary"`
	Core    string            `toml:"core"`
	Systems []protocol.System `toml:"systems"`
}

type fileMedia struct {
	Enabled        bool   `toml:"enabled"`
	Session        string `toml:"session"`
	Generation     uint64 `toml:"generation"`
	SSRC           uint32 `toml:"ssrc"`
	RTPListen      string `toml:"rtp_listen"`
	RTPDestination string `toml:"rtp_destination"`
	ControlAddress string `toml:"control_address"`
	Decoder        string `toml:"decoder"`
	CaptureDevice  string `toml:"capture_device"`
	Bitrate        int    `toml:"bitrate"`
	GOP            int    `toml:"gop"`
	MTU            int    `toml:"mtu"`
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
	hostEmulator, err := normalizeHostEmulator(raw.HostEmulator)
	if err != nil {
		return Config{}, err
	}
	media, err := normalizeMedia(raw.Media)
	if err != nil {
		return Config{}, err
	}

	return Config{
		BaseURL:        baseURL,
		Token:          raw.Token,
		RequestTimeout: requestTimeout,
		UploadTimeout:  uploadTimeout,
		Libraries:      libraries,
		RemoteInput: RemoteInputConfig{
			Enabled: raw.RemoteInput.Enabled,
		},
		HostEmulator: hostEmulator,
		Media:        media,
	}, nil
}

func normalizeMedia(raw fileMedia) (MediaConfig, error) {
	if !raw.Enabled {
		return MediaConfig{}, nil
	}
	if strings.TrimSpace(raw.Session) == "" || raw.SSRC == 0 || strings.TrimSpace(raw.CaptureDevice) == "" {
		return MediaConfig{}, fmt.Errorf("media configuration is invalid")
	}
	for name, address := range map[string]string{"rtp_listen": raw.RTPListen, "rtp_destination": raw.RTPDestination} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return MediaConfig{}, fmt.Errorf("media %s must be a host:port address", name)
		}
	}
	if strings.TrimSpace(raw.ControlAddress) != "" {
		if _, _, err := net.SplitHostPort(raw.ControlAddress); err != nil {
			return MediaConfig{}, fmt.Errorf("media control_address must be a host:port address")
		}
	}
	decoder := strings.ToLower(strings.TrimSpace(raw.Decoder))
	if decoder == "" {
		decoder = "none"
	}
	if decoder != "none" && decoder != "ffplay" {
		return MediaConfig{}, fmt.Errorf("media decoder must be none or ffplay")
	}
	if raw.Bitrate < 0 || raw.GOP < 0 || raw.MTU < 0 {
		return MediaConfig{}, fmt.Errorf("media bitrate, gop, and mtu must not be negative")
	}
	return MediaConfig{Enabled: true, Session: raw.Session, Generation: raw.Generation, SSRC: raw.SSRC,
		RTPListen: raw.RTPListen, RTPDestination: raw.RTPDestination, ControlAddress: raw.ControlAddress,
		Decoder: decoder, CaptureDevice: raw.CaptureDevice, Bitrate: raw.Bitrate, GOP: raw.GOP, MTU: raw.MTU}, nil
}

func normalizeHostEmulator(raw fileHostEmulator) (HostEmulatorConfig, error) {
	systems := append([]protocol.System(nil), raw.Systems...)
	seen := make(map[protocol.System]struct{}, len(systems))
	for _, system := range systems {
		if err := protocol.ValidateSystem(system); err != nil {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems: %w", err)
		}
		if _, ok := seen[system]; ok {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems contains duplicate %q", system)
		}
		seen[system] = struct{}{}
	}
	if strings.TrimSpace(raw.Binary) == "" && strings.TrimSpace(raw.Core) == "" {
		if len(systems) != 0 {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems require binary and core")
		}
		return HostEmulatorConfig{}, nil
	}
	if strings.TrimSpace(raw.Binary) == "" || strings.TrimSpace(raw.Core) == "" {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator requires binary and core")
	}
	if !filepath.IsAbs(raw.Binary) || filepath.Clean(raw.Binary) != raw.Binary {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator binary must be a clean absolute path")
	}
	return HostEmulatorConfig{Binary: raw.Binary, Core: raw.Core, Systems: systems}, nil
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
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("must not be a symbolic link")
	}
	return root, nil
}
