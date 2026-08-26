// Package fogcast contains the host-side FogCast configuration and services.
package fogcast

import (
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/DeanoC/FogCast-POC/internal/systems"
	"github.com/DeanoC/FogCast-POC/librarymedia"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/pelletier/go-toml/v2"
)

type Paths struct {
	Config          string
	Index           string
	Staging         string
	MetadataRoot    string
	UserLibrary     string
	LibrarySettings string
	MediaIndex      string
	MediaCache      string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}
	share := filepath.Join(home, ".local", "share", "fogcast")
	cache := filepath.Join(home, ".cache", "fogcast")
	return Paths{
		Config:          filepath.Join(home, ".config", "fogcast", "config.toml"),
		Index:           filepath.Join(share, "library.sqlite3"),
		Staging:         filepath.Join(cache, "staging"),
		MetadataRoot:    filepath.Join(cache, "metadata"),
		UserLibrary:     filepath.Join(share, "library-user.sqlite3"),
		LibrarySettings: filepath.Join(share, "library-settings.json"),
		MediaIndex:      filepath.Join(share, "library-media.sqlite3"),
		MediaCache:      filepath.Join(cache, "library-media"),
	}, nil
}

const (
	// DefaultAgentBaseURL is the mister-remote agent origin when base_url is
	// omitted. The operator runs the host where that tunnel already exists.
	DefaultAgentBaseURL = "http://127.0.0.1:18182"
	// DefaultFPGAROMGameID is the seeded v1 allowlist key for ActRaiser.
	DefaultFPGAROMGameID = "actraiser"
	// DefaultActRaiserROMPath is the on-kit ROM path seeded for ActRaiser.
	DefaultActRaiserROMPath = "/media/fat/games/SNES/ActRaiser.smc"
	onKitROMRoot            = "/media/fat"
)

type Config struct {
	BaseURL        string
	Token          string
	Targets        []TargetConfig
	SelectedTarget string
	MetadataRoot   string
	RequestTimeout time.Duration
	UploadTimeout  time.Duration
	Libraries      []catalog.Root
	RemoteInput    RemoteInputConfig
	HostEmulator   HostEmulatorConfig
	Media          MediaConfig
	Metadata       MetadataConfig
	LibraryMedia   []librarymedia.Root
	Library        LibraryConfig
	// FPGAROMPaths maps catalog game_id (or the seeded ActRaiser alias) to an
	// on-kit rom_path. It is the v1 FPGA remote-start allowlist.
	FPGAROMPaths map[string]string
}

// TargetConfig describes one named MiSTer agent. Disabled targets may omit
// Address and Agent so an operator can add them before they are online.
// AgentSet and PreviousName are in-memory write markers and are never persisted.
type TargetConfig struct {
	Name         string
	Enabled      bool
	Address      string
	Agent        string
	AgentSet     bool
	PreviousName string
}

type LibraryConfig struct {
	AttractIdleSeconds int
	PreferredRegions   []string
	Libraries          []catalog.Root
	Targets            []TargetConfig
	SelectedTarget     string
	// WatchRoot is the SMB share root containing table-mapped library folders.
	// The scanner uses only operator-configured local [[libraries]] mounts.
	WatchRoot string
}

type LibraryConfigPatch struct {
	AttractIdleSeconds *int
	PreferredRegions   *[]string
	Libraries          *[]catalog.Root
	Targets            *[]TargetConfig
	SelectedTarget     *string
}

// MetadataConfig contains opt-in provider-scoped presentation enrichment.
// ClientSecret is retained only in memory after loading the private config.
type MetadataConfig struct {
	Configured   bool
	Enabled      bool
	Provider     string
	ClientID     string
	ClientSecret string
	Archive      string
}

type HostEmulatorConfig struct {
	Binary  string
	Core    string
	Systems []protocol.System
	Cores   []HostEmulatorCore
}

type HostEmulatorCore struct {
	Platform protocol.System
	Core     string
}

func (c HostEmulatorConfig) CoreFor(system protocol.System) string {
	for _, entry := range c.Cores {
		if entry.Platform == system {
			return entry.Core
		}
	}
	if c.Core == "" {
		return ""
	}
	for _, candidate := range c.Systems {
		if candidate == system {
			return c.Core
		}
	}
	return ""
}

func (c HostEmulatorConfig) LaunchPlatforms() []protocol.System {
	if len(c.Cores) > 0 {
		platforms := make([]protocol.System, 0, len(c.Cores))
		for _, entry := range c.Cores {
			platforms = append(platforms, entry.Platform)
		}
		return platforms
	}
	return append([]protocol.System(nil), c.Systems...)
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
	BaseURL               string               `toml:"base_url"`
	Token                 string               `toml:"token"`
	SelectedTarget        string               `toml:"selected_target"`
	Targets               []fileTarget         `toml:"targets"`
	RequestTimeoutSeconds int64                `toml:"request_timeout_seconds"`
	UploadTimeoutSeconds  int64                `toml:"upload_timeout_seconds"`
	Libraries             []fileLibrary        `toml:"libraries"`
	RemoteInput           fileRemoteInput      `toml:"remote_input"`
	HostEmulator          fileHostEmulator     `toml:"host_emulator"`
	Media                 fileMedia            `toml:"media"`
	Metadata              *fileMetadata        `toml:"metadata"`
	LibraryMedia          []fileLibraryMedia   `toml:"library_media"`
	Library               *fileLibrarySettings `toml:"library"`
	FPGAROMPaths          map[string]string    `toml:"fpga_rom_paths"`
}

type fileTarget struct {
	Name    string `toml:"name"`
	Enabled bool   `toml:"enabled"`
	Address string `toml:"address"`
	Agent   string `toml:"agent"`
}

type fileLibraryMedia struct {
	ID   string `toml:"id"`
	Root string `toml:"root"`
}

type fileLibrarySettings struct {
	AttractIdleSeconds int64    `toml:"attract_idle_seconds"`
	PreferredRegions   []string `toml:"preferred_regions"`
	WatchRoot          string   `toml:"watch_root"`
}

type fileMetadata struct {
	Enabled      bool   `toml:"enabled"`
	Provider     string `toml:"provider"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	Archive      string `toml:"archive"`
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
	Binary  string                 `toml:"binary"`
	Core    string                 `toml:"core"`
	Systems []protocol.System      `toml:"systems"`
	Cores   []fileHostEmulatorCore `toml:"cores"`
}

type fileHostEmulatorCore struct {
	Platform protocol.System `toml:"platform"`
	Core     string          `toml:"core"`
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
	file, err := openConfigSource(path)
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

	targets, selectedTarget, err := normalizeLoadedTargets(raw)
	if err != nil {
		return Config{}, err
	}
	selected := targetByName(targets, selectedTarget)
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
	if (raw.RemoteInput.Enabled || media.Enabled) && !selected.Enabled {
		return Config{}, fmt.Errorf("selected target %q must be enabled when remote input or media is enabled", selectedTarget)
	}
	sourceInfo, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("inspect FogCast config: %w", err)
	}
	metadata, err := normalizeMetadata(raw.Metadata, sourceInfo)
	if err != nil {
		return Config{}, err
	}
	libraryMedia, err := normalizeLibraryMedia(raw.LibraryMedia)
	if err != nil {
		return Config{}, err
	}
	library, err := normalizeLibrarySettings(raw.Library)
	if err != nil {
		return Config{}, err
	}
	watchRoot, legacyRoot, err := resolveFolderWatchRoot(raw.Library, libraries)
	if err != nil {
		return Config{}, fmt.Errorf("library watch_root: %w", err)
	}
	if legacyRoot != nil {
		libraries = append(libraries, *legacyRoot)
	}
	library.WatchRoot = watchRoot
	library.Libraries = append([]catalog.Root(nil), libraries...)
	library.Targets = append([]TargetConfig(nil), targets...)
	library.SelectedTarget = selectedTarget
	fpgaROMPaths, err := normalizeFPGAROMPaths(raw.FPGAROMPaths)
	if err != nil {
		return Config{}, err
	}

	return Config{
		BaseURL:        selected.Address,
		Token:          selected.Agent,
		Targets:        targets,
		SelectedTarget: selectedTarget,
		RequestTimeout: requestTimeout,
		UploadTimeout:  uploadTimeout,
		Libraries:      libraries,
		RemoteInput: RemoteInputConfig{
			Enabled: raw.RemoteInput.Enabled,
		},
		HostEmulator: hostEmulator,
		Media:        media,
		Metadata:     metadata,
		LibraryMedia: libraryMedia,
		Library:      library,
		FPGAROMPaths: fpgaROMPaths,
	}, nil
}

func normalizeLoadedTargets(raw fileConfig) ([]TargetConfig, string, error) {
	if len(raw.Targets) == 0 {
		address, err := normalizeHTTPOrigin(defaultedAgentBaseURL(raw.BaseURL))
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(raw.Token) == "" {
			return nil, "", fmt.Errorf("token must not be empty")
		}
		return []TargetConfig{{Name: "dev", Enabled: true, Address: address, Agent: raw.Token}}, "dev", nil
	}
	targets := make([]TargetConfig, 0, len(raw.Targets))
	for _, target := range raw.Targets {
		targets = append(targets, TargetConfig{
			Name: target.Name, Enabled: target.Enabled, Address: target.Address, Agent: target.Agent,
		})
	}
	return normalizeTargets(targets, raw.SelectedTarget)
}

func normalizeTargets(raw []TargetConfig, selected string) ([]TargetConfig, string, error) {
	targets := make([]TargetConfig, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, target := range raw {
		name := strings.TrimSpace(target.Name)
		if err := protocol.ValidateGameID(name); err != nil {
			return nil, "", fmt.Errorf("target name: %w", err)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, "", fmt.Errorf("duplicate target name %q", name)
		}
		address := strings.TrimSpace(target.Address)
		agent := target.Agent
		hasAddress := address != ""
		hasAgent := strings.TrimSpace(agent) != ""
		if hasAddress != hasAgent {
			return nil, "", fmt.Errorf("target %q address and agent must both be set or both be empty", name)
		}
		if target.Enabled && !hasAddress {
			return nil, "", fmt.Errorf("enabled target %q requires address and agent", name)
		}
		if hasAddress {
			var err error
			address, err = normalizeHTTPOrigin(address)
			if err != nil {
				return nil, "", fmt.Errorf("target %q address: %w", name, err)
			}
		}
		seen[name] = struct{}{}
		targets = append(targets, TargetConfig{Name: name, Enabled: target.Enabled, Address: address, Agent: agent})
	}
	selected = strings.TrimSpace(selected)
	if len(targets) == 0 {
		if selected != "" {
			return nil, "", fmt.Errorf("selected target %q does not exist", selected)
		}
		return targets, "", nil
	}
	if _, ok := seen[selected]; !ok {
		return nil, "", fmt.Errorf("selected target %q does not exist", selected)
	}
	return targets, selected, nil
}

func targetByName(targets []TargetConfig, name string) TargetConfig {
	for _, target := range targets {
		if target.Name == name {
			return target
		}
	}
	return TargetConfig{}
}

// LoadMetadataConfig reads only the metadata section from a FogCast config.
// Other recognized sections are decoded but not applied or validated.
func LoadMetadataConfig(path string) (MetadataConfig, error) {
	file, err := openConfigSource(path)
	if err != nil {
		return MetadataConfig{}, err
	}
	defer file.Close()

	var raw fileConfig
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return MetadataConfig{}, fmt.Errorf("decode FogCast config: %w", err)
	}
	if raw.Metadata == nil {
		return MetadataConfig{}, fmt.Errorf("decode FogCast config: metadata section is required")
	}
	sourceInfo, err := file.Stat()
	if err != nil {
		return MetadataConfig{}, fmt.Errorf("inspect FogCast config: %w", err)
	}
	return normalizeMetadata(raw.Metadata, sourceInfo)
}

func defaultedAgentBaseURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return DefaultAgentBaseURL
	}
	return raw
}

func normalizeFPGAROMPaths(raw map[string]string) (map[string]string, error) {
	if raw == nil {
		raw = map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath}
	}
	paths := make(map[string]string, len(raw))
	for gameID, romPath := range raw {
		if err := protocol.ValidateGameID(gameID); err != nil {
			return nil, fmt.Errorf("fpga_rom_paths key: %w", err)
		}
		cleaned, err := normalizeOnKitROMPath(romPath)
		if err != nil {
			return nil, fmt.Errorf("fpga_rom_paths %q: %w", gameID, err)
		}
		paths[gameID] = cleaned
	}
	return paths, nil
}

func normalizeOnKitROMPath(raw string) (string, error) {
	if strings.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("rom_path must not contain a NUL byte")
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("rom_path must not be empty")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("rom_path must be an absolute on-kit path")
	}
	cleaned := filepath.Clean(raw)
	relative, err := filepath.Rel(onKitROMRoot, cleaned)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("rom_path must stay under %s", onKitROMRoot)
	}
	if cleaned == onKitROMRoot {
		return "", fmt.Errorf("rom_path must identify a file under %s", onKitROMRoot)
	}
	return filepath.ToSlash(cleaned), nil
}

func normalizeMetadata(raw *fileMetadata, sourceInfo os.FileInfo) (MetadataConfig, error) {
	if raw == nil {
		return MetadataConfig{}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(raw.Provider))
	if provider != "" && provider != "igdb" && provider != "launchbox" {
		return MetadataConfig{}, fmt.Errorf("metadata provider must be igdb or launchbox")
	}
	if raw.Enabled && provider != "igdb" && provider != "launchbox" {
		return MetadataConfig{}, fmt.Errorf("metadata provider must be explicitly set when enabled")
	}
	if provider == "" {
		provider = "igdb"
	}
	if !raw.Enabled {
		return MetadataConfig{Configured: true, Provider: provider}, nil
	}
	if err := validatePrivateConfigFile(sourceInfo); err != nil {
		return MetadataConfig{}, fmt.Errorf("metadata config file: %w", err)
	}
	if provider == "launchbox" {
		archive := strings.TrimSpace(raw.Archive)
		if archive == "" || strings.TrimSpace(raw.ClientID) != "" || strings.TrimSpace(raw.ClientSecret) != "" {
			return MetadataConfig{}, fmt.Errorf("launchbox metadata requires an archive path and no credentials")
		}
		return MetadataConfig{Configured: true, Enabled: true, Provider: provider, Archive: archive}, nil
	}
	if strings.TrimSpace(raw.ClientID) == "" || strings.TrimSpace(raw.ClientSecret) == "" {
		return MetadataConfig{}, fmt.Errorf("metadata credentials are required when enabled")
	}
	return MetadataConfig{Configured: true, Enabled: true, Provider: provider, ClientID: raw.ClientID, ClientSecret: raw.ClientSecret}, nil
}

func validatePrivateConfigFile(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
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
	binary := strings.TrimSpace(raw.Binary)
	legacyCore := strings.TrimSpace(raw.Core)
	systems := append([]protocol.System(nil), raw.Systems...)
	if len(raw.Cores) > 0 && (legacyCore != "" || len(systems) > 0) {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator cores cannot mix with core and systems")
	}
	if len(raw.Cores) > 0 {
		if binary == "" {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator cores require binary")
		}
		if !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator binary must be a clean absolute path")
		}
		cores := make([]HostEmulatorCore, 0, len(raw.Cores))
		seen := make(map[protocol.System]struct{}, len(raw.Cores))
		for _, entry := range raw.Cores {
			if err := catalog.ValidatePlatform(entry.Platform); err != nil {
				return HostEmulatorConfig{}, fmt.Errorf("host_emulator cores: %w", err)
			}
			if _, ok := seen[entry.Platform]; ok {
				return HostEmulatorConfig{}, fmt.Errorf("host_emulator cores contains duplicate %q", entry.Platform)
			}
			core := strings.TrimSpace(entry.Core)
			if core == "" || !filepath.IsAbs(core) || filepath.Clean(core) != core {
				return HostEmulatorConfig{}, fmt.Errorf("host_emulator core for %q must be a clean absolute path", entry.Platform)
			}
			seen[entry.Platform] = struct{}{}
			cores = append(cores, HostEmulatorCore{Platform: entry.Platform, Core: core})
		}
		return HostEmulatorConfig{Binary: binary, Cores: cores}, nil
	}
	seen := make(map[protocol.System]struct{}, len(systems))
	for _, system := range systems {
		if err := catalog.ValidatePlatform(system); err != nil {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems: %w", err)
		}
		if _, ok := seen[system]; ok {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems contains duplicate %q", system)
		}
		seen[system] = struct{}{}
	}
	if binary == "" && legacyCore == "" {
		if len(systems) != 0 {
			return HostEmulatorConfig{}, fmt.Errorf("host_emulator systems require binary and core")
		}
		return HostEmulatorConfig{}, nil
	}
	if binary == "" || legacyCore == "" {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator requires binary and core")
	}
	if !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator binary must be a clean absolute path")
	}
	if !filepath.IsAbs(legacyCore) || filepath.Clean(legacyCore) != legacyCore {
		return HostEmulatorConfig{}, fmt.Errorf("host_emulator core must be a clean absolute path")
	}
	return HostEmulatorConfig{Binary: binary, Core: legacyCore, Systems: systems}, nil
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
		if err := catalog.ValidatePlatform(library.System); err != nil {
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

func normalizeLibraryMedia(raw []fileLibraryMedia) ([]librarymedia.Root, error) {
	roots := make([]librarymedia.Root, 0, len(raw))
	ids := make(map[string]struct{}, len(raw))
	paths := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		if err := protocol.ValidateGameID(entry.ID); err != nil {
			return nil, fmt.Errorf("library_media id: %w", err)
		}
		if _, duplicate := ids[entry.ID]; duplicate {
			return nil, fmt.Errorf("duplicate library_media id %q", entry.ID)
		}
		root, err := normalizeRoot(entry.Root)
		if err != nil {
			return nil, fmt.Errorf("library_media %q root: %w", entry.ID, err)
		}
		if _, duplicate := paths[root]; duplicate {
			return nil, fmt.Errorf("duplicate library_media root %q", root)
		}
		ids[entry.ID] = struct{}{}
		paths[root] = struct{}{}
		roots = append(roots, librarymedia.Root{ID: entry.ID, Path: root})
	}
	return roots, nil
}

func normalizeLibrarySettings(raw *fileLibrarySettings) (LibraryConfig, error) {
	if raw == nil {
		return NormalizeLibraryConfig(LibraryConfig{})
	}
	if raw.AttractIdleSeconds < 0 {
		return LibraryConfig{}, fmt.Errorf("library attract_idle_seconds must not be negative")
	}
	return NormalizeLibraryConfig(LibraryConfig{
		AttractIdleSeconds: int(raw.AttractIdleSeconds),
		PreferredRegions:   raw.PreferredRegions,
	})
}

// MaxAttractIdleSeconds is the largest idle that converts to a millisecond
// timer delay without overflowing a signed 32-bit setTimeout argument.
const MaxAttractIdleSeconds = 2147483

// NormalizeLibraryConfig applies the same attract-idle and preferred-region
// rules as config.toml [library], without reading or writing that file.
func NormalizeLibraryConfig(raw LibraryConfig) (LibraryConfig, error) {
	if raw.AttractIdleSeconds < 0 {
		return LibraryConfig{}, fmt.Errorf("library attract_idle_seconds must not be negative")
	}
	seconds := raw.AttractIdleSeconds
	if seconds == 0 {
		seconds = 60
	}
	if seconds > MaxAttractIdleSeconds {
		seconds = MaxAttractIdleSeconds
	}
	preferred := append([]string(nil), raw.PreferredRegions...)
	if len(preferred) == 0 {
		preferred = append([]string(nil), catalog.DefaultPreferredRegions...)
	}
	seen := make(map[string]struct{}, len(preferred))
	normalized := make([]string, 0, len(preferred))
	for _, region := range preferred {
		mapped := strings.TrimSpace(strings.ToLower(region))
		if mapped == "" {
			return LibraryConfig{}, fmt.Errorf("library preferred_regions must not contain empty values")
		}
		if _, ok := seen[mapped]; ok {
			return LibraryConfig{}, fmt.Errorf("library preferred_regions contains duplicate %q", mapped)
		}
		seen[mapped] = struct{}{}
		normalized = append(normalized, mapped)
	}
	libraries, err := normalizeCatalogRoots(raw.Libraries)
	if err != nil {
		return LibraryConfig{}, err
	}
	targets, selectedTarget, err := normalizeTargets(raw.Targets, raw.SelectedTarget)
	if err != nil {
		return LibraryConfig{}, err
	}
	return LibraryConfig{
		AttractIdleSeconds: seconds,
		PreferredRegions:   normalized,
		Libraries:          libraries,
		Targets:            targets,
		SelectedTarget:     selectedTarget,
		WatchRoot:          strings.TrimSpace(raw.WatchRoot),
	}, nil
}

func normalizeCatalogRoots(raw []catalog.Root) ([]catalog.Root, error) {
	encoded := make([]fileLibrary, 0, len(raw))
	for _, root := range raw {
		encoded = append(encoded, fileLibrary{ID: root.ID, System: root.System, Root: root.Path})
	}
	return normalizeLibraries(encoded)
}

const (
	// DefaultFolderWatchRoot is the confirmed SMB library share root.
	DefaultFolderWatchRoot = systems.DefaultSMBShareRoot
)

func resolveFolderWatchRoot(raw *fileLibrarySettings, libraries []catalog.Root) (string, *catalog.Root, error) {
	if raw != nil && strings.TrimSpace(raw.WatchRoot) != "" {
		root, err := normalizeWatchRoot(raw.WatchRoot)
		if err != nil {
			return "", nil, err
		}
		if parent, row, ok := mappedFolderParent(root); ok {
			if !isUNCWatchRoot(strings.ReplaceAll(root, `\`, "/")) {
				legacy := catalog.Root{ID: legacyFolderWatchLibraryID(root), System: row.PlatformID, Path: root}
				for _, library := range libraries {
					if library.Path == root {
						if library.System == row.PlatformID {
							return parent, nil, nil
						}
						return "", nil, fmt.Errorf("legacy mapped folder conflicts with [[libraries]] root %q", library.ID)
					}
					if library.ID == legacy.ID {
						return "", nil, fmt.Errorf("legacy mapped folder id conflicts with [[libraries]] root %q", library.ID)
					}
				}
				return parent, &legacy, nil
			}
			return parent, nil, nil
		}
		if !isUNCWatchRoot(strings.ReplaceAll(root, `\`, "/")) && len(libraries) == 0 {
			return "", nil, fmt.Errorf("local share root requires an explicit [[libraries]] mapping")
		}
		return root, nil, nil
	}
	root, err := normalizeWatchRoot(DefaultFolderWatchRoot)
	return root, nil, err
}

func mappedFolderParent(root string) (string, systems.Row, bool) {
	unified := strings.ReplaceAll(strings.TrimSpace(root), `\`, "/")
	leaf := filepath.Base(filepath.FromSlash(unified))
	for _, row := range systems.Rows() {
		if row.FolderAlias != "" && strings.EqualFold(leaf, row.FolderAlias) {
			separator := strings.LastIndex(unified, "/")
			if separator == 0 {
				return "/", row, true
			}
			if separator <= 1 {
				return "", systems.Row{}, false
			}
			return unified[:separator], row, true
		}
	}
	return "", systems.Row{}, false
}

func legacyFolderWatchLibraryID(root string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(root)))
	return "folder-watch-" + hex.EncodeToString(digest[:6])
}

func normalizeWatchRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if strings.IndexByte(trimmed, 0) >= 0 {
		return "", fmt.Errorf("must not contain a NUL byte")
	}
	unified := strings.ReplaceAll(trimmed, `\`, "/")
	if isUNCWatchRoot(unified) {
		return normalizeUNCWatchRoot(unified)
	}
	return normalizeRoot(trimmed)
}

func isUNCWatchRoot(path string) bool {
	if !strings.HasPrefix(path, "//") || strings.HasPrefix(path, "///") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "//"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func normalizeUNCWatchRoot(path string) (string, error) {
	rest := path[2:]
	for strings.Contains(rest, "//") {
		rest = strings.ReplaceAll(rest, "//", "/")
	}
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("must be a //server/share path")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("must be a clean UNC path")
		}
	}
	return "//" + strings.Join(parts, "/"), nil
}

// FolderWatchRoots returns every configured local root whose system row owns
// an SMB folder alias. Adding a future mapping is table data, not watcher code.
func FolderWatchRoots(libraries []catalog.Root) []catalog.Root {
	out := make([]catalog.Root, 0, len(libraries))
	for _, library := range libraries {
		if systems.Mapped(library.System) {
			out = append(out, library)
		}
	}
	return out
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
		return "", fmt.Errorf("must not be a symbolic link; configure the real absolute directory")
	}
	return root, nil
}
