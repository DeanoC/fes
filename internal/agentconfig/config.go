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
	BuildProfile       string
	DevelopmentProfile bool
	HardwareOwnerPath  string
	HardwareOwnerLock  string
	DesignationPath    string
	TargetIdentityPath string

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

	// FPGADevBootDispatcher and FPGADevStartSources are protected operator /
	// package authority for the private development profile. They are never
	// inferred from the filesystem because choosing a different launcher would
	// change the recovery ordering contract.
	FPGADevBootDispatcher string
	FPGADevStartSources   []string

	developmentFieldsPresent bool
}

type fileConfig struct {
	BuildProfile       *string `toml:"build_profile"`
	DevelopmentProfile *bool   `toml:"development_profile"`
	HardwareOwnerPath  *string `toml:"hardware_owner_path"`
	HardwareOwnerLock  *string `toml:"hardware_owner_lock"`
	DesignationPath    *string `toml:"designation_path"`
	TargetIdentityPath *string `toml:"target_identity_path"`

	ListenAddress         string    `toml:"listen_address"`
	Token                 string    `toml:"token"`
	MiSTerProcessComm     string    `toml:"mister_process_comm"`
	CommandPipe           string    `toml:"command_pipe"`
	CoreNameFile          string    `toml:"core_name_file"`
	MenuRBF               string    `toml:"menu_rbf"`
	MGLDirectory          string    `toml:"mgl_directory"`
	CacheMaxBytes         *int64    `toml:"cache_max_bytes"`
	InputListenAddress    string    `toml:"input_listen_address"`
	InputUInputPath       string    `toml:"input_uinput_path"`
	CastBinary            string    `toml:"cast_binary"`
	CastRTPAddress        string    `toml:"cast_rtp_address"`
	CastControlAddress    string    `toml:"cast_control_address"`
	CastFramebuffer       string    `toml:"cast_framebuffer"`
	CastNativeCmd         string    `toml:"cast_native_cmd"`
	CastNativeMode        string    `toml:"cast_native_mode"`
	CastTokenFile         string    `toml:"cast_token_file"`
	CastGeneration        uint64    `toml:"cast_generation"`
	FPGADevBootDispatcher *string   `toml:"fpgadev_boot_dispatcher"`
	FPGADevStartSources   *[]string `toml:"fpgadev_start_sources"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(raw)
}

// Parse decodes one complete protected configuration snapshot. Callers that
// have already opened and identity-checked a descriptor use this entry point
// so a pathname cannot be re-resolved between metadata validation and parse.
func Parse(data []byte) (Config, error) {
	var raw fileConfig
	decoder := toml.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode target config: %w", err)
	}
	cacheMaxBytes := DefaultCacheMaxBytes
	if raw.CacheMaxBytes != nil {
		cacheMaxBytes = *raw.CacheMaxBytes
	}
	buildProfile := "production"
	if raw.BuildProfile != nil {
		buildProfile = *raw.BuildProfile
	}
	developmentProfile := false
	if raw.DevelopmentProfile != nil {
		developmentProfile = *raw.DevelopmentProfile
	}
	devFieldsPresent := raw.DevelopmentProfile != nil || raw.HardwareOwnerPath != nil || raw.HardwareOwnerLock != nil || raw.DesignationPath != nil || raw.TargetIdentityPath != nil
	cfg := Config{
		BuildProfile:       buildProfile,
		DevelopmentProfile: developmentProfile,
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
	if raw.HardwareOwnerPath != nil {
		cfg.HardwareOwnerPath = *raw.HardwareOwnerPath
	}
	if raw.HardwareOwnerLock != nil {
		cfg.HardwareOwnerLock = *raw.HardwareOwnerLock
	}
	if raw.DesignationPath != nil {
		cfg.DesignationPath = *raw.DesignationPath
	}
	if raw.TargetIdentityPath != nil {
		cfg.TargetIdentityPath = *raw.TargetIdentityPath
	}
	if raw.FPGADevBootDispatcher != nil {
		cfg.FPGADevBootDispatcher = *raw.FPGADevBootDispatcher
	}
	if raw.FPGADevStartSources != nil {
		cfg.FPGADevStartSources = append([]string(nil), (*raw.FPGADevStartSources)...)
	}
	cfg.developmentFieldsPresent = devFieldsPresent || raw.FPGADevBootDispatcher != nil || raw.FPGADevStartSources != nil
	if err := cfg.validateProfileShape(); err != nil {
		return Config{}, err
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
	if cfg.IsDevelopmentProfile() {
		if err := cfg.ValidateFPGABootAuthority(); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

const (
	BuildProfileProduction  = "production"
	BuildProfileDevelopment = "development"
)

// ValidateProfile checks the runtime profile against the compile-time
// capability. The capability is supplied by cmd/mister-agent's build-tagged
// package so an untagged binary cannot activate development behavior.
func (c Config) ValidateProfile(fpgadevCapability bool) error {
	if err := c.validateProfileShape(); err != nil {
		return err
	}
	if c.BuildProfile == BuildProfileDevelopment {
		if !c.DevelopmentProfile {
			return fmt.Errorf("development_profile must be true for development build profile")
		}
		if !fpgadevCapability {
			return fmt.Errorf("development profile requires fpgadev capability")
		}
		if err := c.ValidateDevelopmentInventory(); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// ValidateDevelopmentInventory checks the closed set of configuration fields
// that Task 8 can attest into InventoryV1. A partially configured auxiliary
// facility is ambiguous and therefore rejected instead of being silently
// omitted from the install journal.
func (c Config) ValidateDevelopmentInventory() error {
	if !c.IsDevelopmentProfile() {
		return fmt.Errorf("development inventory requires the development profile")
	}
	if err := c.ValidateFPGABootAuthority(); err != nil {
		return err
	}
	if c.InputUInputPath != "" && !filepath.IsAbs(c.InputUInputPath) {
		return fmt.Errorf("input_uinput_path must be absolute")
	}
	castFields := map[string]string{
		"cast_rtp_address":     c.CastRTPAddress,
		"cast_control_address": c.CastControlAddress,
		"cast_framebuffer":     c.CastFramebuffer,
		"cast_native_cmd":      c.CastNativeCmd,
		"cast_native_mode":     c.CastNativeMode,
		"cast_token_file":      c.CastTokenFile,
	}
	if c.CastBinary == "" {
		for name, value := range castFields {
			if value != "" {
				return fmt.Errorf("%s requires cast_binary", name)
			}
		}
		if c.CastGeneration != 0 {
			return fmt.Errorf("cast_generation requires cast_binary")
		}
		return nil
	}
	if !filepath.IsAbs(c.CastBinary) || !filepath.IsAbs(c.CastFramebuffer) || !filepath.IsAbs(c.CastNativeCmd) || (c.CastTokenFile != "" && !filepath.IsAbs(c.CastTokenFile)) {
		return fmt.Errorf("cast inventory paths must be absolute")
	}
	for name, value := range castFields {
		if value == "" {
			return fmt.Errorf("%s is required when cast_binary is configured", name)
		}
	}
	if _, _, err := net.SplitHostPort(c.CastRTPAddress); err != nil {
		return fmt.Errorf("cast_rtp_address: %w", err)
	}
	if _, _, err := net.SplitHostPort(c.CastControlAddress); err != nil {
		return fmt.Errorf("cast_control_address: %w", err)
	}
	return nil
}

// ValidateFPGABootAuthority validates the exact private-profile launch
// authority. A sorted list is required so the canonical journal and all
// callers agree on one source ordering; the dispatcher must be one member.
func (c Config) ValidateFPGABootAuthority() error {
	if c.FPGADevBootDispatcher == "" {
		return fmt.Errorf("fpgadev_boot_dispatcher is required")
	}
	if !filepath.IsAbs(c.FPGADevBootDispatcher) || filepath.Clean(c.FPGADevBootDispatcher) != c.FPGADevBootDispatcher || c.FPGADevBootDispatcher == string(filepath.Separator) {
		return fmt.Errorf("fpgadev_boot_dispatcher must be an absolute canonical path")
	}
	if len(c.FPGADevStartSources) == 0 || len(c.FPGADevStartSources) > 16 {
		return fmt.Errorf("fpgadev_start_sources must contain 1..16 paths")
	}
	seen := make(map[string]struct{}, len(c.FPGADevStartSources))
	containsDispatcher := false
	for index, path := range c.FPGADevStartSources {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
			return fmt.Errorf("fpgadev_start_sources[%d] must be an absolute canonical path", index)
		}
		if index > 0 && c.FPGADevStartSources[index-1] >= path {
			return fmt.Errorf("fpgadev_start_sources must be path-sorted and duplicate-free")
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("fpgadev_start_sources contains a duplicate path")
		}
		seen[path] = struct{}{}
		if path == c.FPGADevBootDispatcher {
			containsDispatcher = true
		}
	}
	if !containsDispatcher {
		return fmt.Errorf("fpgadev_boot_dispatcher is not in fpgadev_start_sources")
	}
	return nil
}

// IsDevelopmentProfile reports the explicit configuration opt-in. It does
// not imply that the compile-time fpgadev capability is present; callers that
// execute the profile must call ValidateProfile first.
func (c Config) IsDevelopmentProfile() bool {
	return c.BuildProfile == BuildProfileDevelopment && c.DevelopmentProfile
}

func (c Config) validateProfileShape() error {
	switch c.BuildProfile {
	case BuildProfileProduction:
		if c.developmentFieldsPresent || c.DevelopmentProfile || c.HardwareOwnerPath != "" || c.HardwareOwnerLock != "" || c.DesignationPath != "" || c.TargetIdentityPath != "" {
			return fmt.Errorf("development-only configuration fields require the development build profile")
		}
	case BuildProfileDevelopment:
		if !c.DevelopmentProfile {
			return fmt.Errorf("development_profile must be true for development build profile")
		}
		for name, value := range map[string]string{
			"hardware_owner_path":  c.HardwareOwnerPath,
			"hardware_owner_lock":  c.HardwareOwnerLock,
			"designation_path":     c.DesignationPath,
			"target_identity_path": c.TargetIdentityPath,
		} {
			if value == "" {
				return fmt.Errorf("%s is required for development profile", name)
			}
			if !filepath.IsAbs(value) {
				return fmt.Errorf("%s must be absolute", name)
			}
		}
	default:
		return fmt.Errorf("build_profile must be production or development")
	}
	return nil
}
