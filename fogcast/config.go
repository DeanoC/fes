// Package fogcast contains the host-side FogCast configuration and services.
package fogcast

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/pelletier/go-toml/v2"
)

type Paths struct {
	Config       string
	Index        string
	Staging      string
	MetadataRoot string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}
	return Paths{
		Config:       filepath.Join(home, ".config", "fogcast", "config.toml"),
		Index:        filepath.Join(home, ".local", "share", "fogcast", "library.sqlite3"),
		Staging:      filepath.Join(home, ".cache", "fogcast", "staging"),
		MetadataRoot: filepath.Join(home, ".cache", "fogcast", "metadata"),
	}, nil
}

type Config struct {
	BaseURL        string
	Token          string
	MetadataRoot   string
	RequestTimeout time.Duration
	UploadTimeout  time.Duration
	Libraries      []catalog.Root
	RemoteInput    RemoteInputConfig
	HostEmulator   HostEmulatorConfig
	Media          MediaConfig
	Metadata       MetadataConfig
}

// MetadataConfig contains opt-in provider-scoped presentation enrichment.
// ClientSecret is retained only in memory after loading the private config.
type MetadataConfig struct {
	Configured   bool
	Enabled      bool
	Provider     string
	ClientID     string
	ClientSecret string
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
	Width          int
	Height         int
	FPSNumerator   int
	FPSDenominator int
	Bitrate        int
	GOP            int
	MTU            int
	Audio          remotemedia.AudioConfig
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
	Metadata              *fileMetadata    `toml:"metadata"`
}

type fileMetadata struct {
	Enabled      bool   `toml:"enabled"`
	Provider     string `toml:"provider"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
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
	Enabled        bool      `toml:"enabled"`
	Session        string    `toml:"session"`
	Generation     uint64    `toml:"generation"`
	SSRC           uint32    `toml:"ssrc"`
	RTPListen      string    `toml:"rtp_listen"`
	RTPDestination string    `toml:"rtp_destination"`
	ControlAddress string    `toml:"control_address"`
	Decoder        string    `toml:"decoder"`
	CaptureDevice  string    `toml:"capture_device"`
	Width          int       `toml:"width"`
	Height         int       `toml:"height"`
	FPSNumerator   int       `toml:"fps_numerator"`
	FPSDenominator int       `toml:"fps_denominator"`
	Bitrate        int       `toml:"bitrate"`
	GOP            int       `toml:"gop"`
	MTU            int       `toml:"mtu"`
	Audio          fileAudio `toml:"audio"`
}

type fileAudio struct {
	Enabled                  bool   `toml:"enabled"`
	Source                   string `toml:"source"`
	Device                   string `toml:"device"`
	DeviceUID                string `toml:"device_uid"`
	DeviceHash               string `toml:"device_hash"`
	DisplayDigest            string `toml:"display_digest"`
	DisplayHardwareUUID      string `toml:"display_hardware_uuid"`
	DisplayEDIDVendor        string `toml:"display_edid_vendor"`
	DisplayEDIDModel         string `toml:"display_edid_model"`
	DisplayEDIDSerial        string `toml:"display_edid_serial"`
	DisplayExplicitSelection bool   `toml:"display_explicit_selection"`
	DisplaySelectionContext  string `toml:"display_selection_context"`
	RTPDestination           string `toml:"rtp_destination"`
	ControlAddress           string `toml:"control_address"`
	SSRC                     uint32 `toml:"ssrc"`
	SampleRate               int    `toml:"sample_rate"`
	Channels                 int    `toml:"channels"`
	FrameSamples             int    `toml:"frame_samples"`
	MTU                      int    `toml:"mtu"`
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
	metadata, err := normalizeMetadata(raw.Metadata, path)
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
		Metadata:     metadata,
	}, nil
}

func normalizeMetadata(raw *fileMetadata, configPath string) (MetadataConfig, error) {
	if raw == nil {
		return MetadataConfig{}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(raw.Provider))
	if provider != "" && provider != "igdb" {
		return MetadataConfig{}, fmt.Errorf("metadata provider must be igdb")
	}
	if raw.Enabled && provider != "igdb" {
		return MetadataConfig{}, fmt.Errorf("metadata provider must be explicitly set to igdb when enabled")
	}
	if provider == "" {
		provider = "igdb"
	}
	if !raw.Enabled {
		return MetadataConfig{Configured: true, Provider: provider}, nil
	}
	if strings.TrimSpace(raw.ClientID) == "" || strings.TrimSpace(raw.ClientSecret) == "" {
		return MetadataConfig{}, fmt.Errorf("metadata credentials are required when enabled")
	}
	if err := validatePrivateConfigFile(configPath); err != nil {
		return MetadataConfig{}, fmt.Errorf("metadata config file: %w", err)
	}
	return MetadataConfig{Configured: true, Enabled: true, Provider: provider, ClientID: raw.ClientID, ClientSecret: raw.ClientSecret}, nil
}

func validatePrivateConfigFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("must be a regular file with mode 0600")
	}
	return nil
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
	if raw.Width < 0 || raw.Height < 0 || raw.FPSNumerator < 0 || raw.FPSDenominator < 0 || raw.Bitrate < 0 || raw.GOP < 0 || raw.MTU < 0 {
		return MediaConfig{}, fmt.Errorf("media dimensions, frame rate, bitrate, gop, and mtu must not be negative")
	}
	if (raw.FPSNumerator == 0) != (raw.FPSDenominator == 0) {
		return MediaConfig{}, fmt.Errorf("media frame rate numerator and denominator must both be set or unset")
	}
	audio, err := normalizeAudio(raw.Audio, raw)
	if err != nil {
		return MediaConfig{}, err
	}
	return MediaConfig{Enabled: true, Session: raw.Session, Generation: raw.Generation, SSRC: raw.SSRC,
		RTPListen: raw.RTPListen, RTPDestination: raw.RTPDestination, ControlAddress: raw.ControlAddress,
		Decoder: decoder, CaptureDevice: raw.CaptureDevice, Width: raw.Width, Height: raw.Height,
		FPSNumerator: raw.FPSNumerator, FPSDenominator: raw.FPSDenominator,
		Bitrate: raw.Bitrate, GOP: raw.GOP, MTU: raw.MTU, Audio: audio}, nil
}

func normalizeAudio(raw fileAudio, video fileMedia) (remotemedia.AudioConfig, error) {
	if !raw.Enabled {
		return remotemedia.AudioConfig{}, nil
	}
	if raw.SSRC == 0 || raw.SSRC == video.SSRC {
		return remotemedia.AudioConfig{}, fmt.Errorf("audio ssrc must be non-zero and differ from video")
	}
	rtpDestination, err := parsePrivateEndpoint("audio rtp_destination", raw.RTPDestination)
	if err != nil {
		return remotemedia.AudioConfig{}, err
	}
	controlAddress, err := parsePrivateEndpoint("audio control_address", raw.ControlAddress)
	if err != nil {
		return remotemedia.AudioConfig{}, err
	}
	if rtpDestination == controlAddress {
		return remotemedia.AudioConfig{}, fmt.Errorf("audio RTP and control addresses must differ")
	}
	for _, videoAddress := range []string{video.RTPListen, video.RTPDestination, video.ControlAddress} {
		if videoAddress == "" {
			continue
		}
		videoEndpoint, err := parseEndpoint(videoAddress)
		if err != nil {
			return remotemedia.AudioConfig{}, fmt.Errorf("video transport address is invalid")
		}
		if rtpDestination == videoEndpoint || controlAddress == videoEndpoint {
			return remotemedia.AudioConfig{}, fmt.Errorf("audio transport addresses must differ from video")
		}
	}
	source := remotemedia.AudioSourceConfig{
		Kind: remotemedia.AudioSourceKind(strings.ToLower(strings.TrimSpace(raw.Source))), Enabled: true,
		EndpointName: strings.TrimSpace(raw.Device), EndpointUID: raw.DeviceUID, EndpointDigest: strings.TrimSpace(raw.DeviceHash),
		Display:       remotemedia.AudioDisplayIdentity{HardwareUUID: raw.DisplayHardwareUUID, EDIDVendor: raw.DisplayEDIDVendor, EDIDModel: raw.DisplayEDIDModel, EDIDSerial: raw.DisplayEDIDSerial, ExplicitSelection: raw.DisplayExplicitSelection, SelectionContext: raw.DisplaySelectionContext},
		DisplayDigest: strings.TrimSpace(raw.DisplayDigest), SampleRate: raw.SampleRate, Channels: raw.Channels, FrameSamples: raw.FrameSamples,
	}
	if err := remotemedia.ValidateAudioFormat(remotemedia.AudioFormat{SampleRate: source.SampleRate, Channels: source.Channels, Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: source.FrameSamples}); err != nil {
		return remotemedia.AudioConfig{}, fmt.Errorf("audio format: %w", err)
	}
	switch source.Kind {
	case remotemedia.AudioSourceShadowCastUAC:
		digest, err := remotemedia.CanonicalAudioEndpointDigest(remotemedia.AudioEndpoint{UID: source.EndpointUID, DisplayName: source.EndpointName, SampleRate: source.SampleRate, Channels: source.Channels})
		if err != nil || source.EndpointDigest == "" || source.EndpointDigest != digest {
			return remotemedia.AudioConfig{}, fmt.Errorf("audio shadowcast endpoint identity is invalid")
		}
	case remotemedia.AudioSourceHostOutput:
		if strings.HasPrefix(source.EndpointName, "screen:") {
			selection := strings.TrimSpace(strings.TrimPrefix(source.EndpointName, "screen:"))
			if selection == "" {
				return remotemedia.AudioConfig{}, fmt.Errorf("audio host output requires screen selection or display identity")
			}
			source.Display.ExplicitSelection = true
			source.Display.SelectionContext = selection
			digest, err := remotemedia.CanonicalAudioDisplayDigest(source.Display)
			if err != nil {
				return remotemedia.AudioConfig{}, fmt.Errorf("audio host output requires screen selection or display identity")
			}
			if source.DisplayDigest != "" && source.DisplayDigest != digest {
				return remotemedia.AudioConfig{}, fmt.Errorf("audio host output display identity is invalid")
			}
			source.DisplayDigest = digest
		} else {
			if source.Display.HardwareUUID == "" && source.Display.EDIDSerial == "" {
				return remotemedia.AudioConfig{}, fmt.Errorf("audio host output requires screen selection or display identity")
			}
			digest, err := remotemedia.CanonicalAudioDisplayDigest(source.Display)
			if err != nil || source.DisplayDigest == "" || source.DisplayDigest != digest {
				return remotemedia.AudioConfig{}, fmt.Errorf("audio host output display identity is invalid")
			}
		}
	default:
		return remotemedia.AudioConfig{}, fmt.Errorf("audio source is invalid")
	}
	if raw.MTU <= 0 {
		return remotemedia.AudioConfig{}, fmt.Errorf("audio mtu must be positive")
	}
	config := remotemedia.AudioConfig{Enabled: true, Source: source, Transport: remotemedia.AudioTransportConfig{RTPDestination: rtpDestination, ControlAddress: controlAddress, SSRC: raw.SSRC, MTU: raw.MTU, FormatCapabilityVersion: 1}}
	if err := remotemedia.ValidateAudioConfig(config); err != nil {
		return remotemedia.AudioConfig{}, fmt.Errorf("audio configuration is invalid")
	}
	return config, nil
}

func parsePrivateEndpoint(name, address string) (string, error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.IsValid() || endpoint.Port() == 0 {
		return "", fmt.Errorf("%s must be a private host:port address", name)
	}
	endpoint = netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
	if !endpoint.Addr().IsPrivate() && !endpoint.Addr().IsLoopback() {
		return "", fmt.Errorf("%s must be a private host:port address", name)
	}
	return endpoint.String(), nil
}

func parseEndpoint(address string) (string, error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.IsValid() || endpoint.Port() == 0 {
		return "", fmt.Errorf("invalid endpoint")
	}
	endpoint = netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
	return endpoint.String(), nil
}

func validatePrivateAddress(name, address string) error {
	if _, err := parsePrivateEndpoint(name, address); err != nil {
		return fmt.Errorf("%s must be a private host:port address", name)
	}
	return nil
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
	if filepath.Clean(raw) != raw {
		return "", fmt.Errorf("must be a clean absolute path")
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
