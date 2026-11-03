package fogcast

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// KitHostTargetName is the one target a kit host drives: its own agent.
const KitHostTargetName = "kit"

// kitHostUploadTimeoutSeconds bounds kit-host uploads to the agent. Titling
// an ST package on a kit takes 29-48 s, close to the image template's 60 s.
const kitHostUploadTimeoutSeconds = 300

// KitHost is what kit mode manages in a kit host's config.toml (#863 slice 4).
type KitHost struct {
	// AgentAddress is the agent's loopback origin, such as http://127.0.0.1:8182.
	AgentAddress string
	AgentToken   string
	// TargetID is the agent's target ID, or empty when it is not known yet;
	// the host then learns it from the first authenticated health.
	TargetID string
	State    string
	Staging  string
}

// KitHostConfig renders a kit host's config.toml. A first start (existing
// empty) begins from template, the image's catalog config; a restart begins
// from existing, so libraries and settings changed through the API survive.
// Every start rewrites the managed keys: selected_target and the one
// loopback [[targets]] row, state and staging, [core_packages] kit, the
// [firmware] directory and the request timeout from template, the upload
// timeout, and [library] folder_watch_seconds = 0. Kit mode scans once at
// start instead of polling.
func KitHostConfig(template, existing []byte, kit KitHost) ([]byte, error) {
	if kit.AgentAddress == "" || kit.AgentToken == "" {
		return nil, errors.New("kit host config needs the agent address and token")
	}
	for _, path := range []string{kit.State, kit.Staging} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("kit host state and staging must be clean absolute paths")
		}
	}
	base, err := decodeKitHostConfig(template)
	if err != nil {
		return nil, fmt.Errorf("decode kit host template: %w", err)
	}
	raw := base
	if len(bytes.TrimSpace(existing)) != 0 {
		if raw, err = decodeKitHostConfig(existing); err != nil {
			return nil, fmt.Errorf("decode existing kit host config: %w", err)
		}
	}
	raw.TargetID, raw.BaseURL, raw.Token = "", "", ""
	raw.SelectedTarget = KitHostTargetName
	raw.Targets = []fileTarget{{
		TargetID: kit.TargetID, Name: KitHostTargetName, Enabled: true,
		Address: kit.AgentAddress, Agent: kit.AgentToken,
	}}
	raw.State, raw.Staging = kit.State, kit.Staging
	raw.CorePackages = &fileCorePackages{Kit: true}
	raw.Firmware = base.Firmware
	raw.RequestTimeoutSeconds = base.RequestTimeoutSeconds
	raw.UploadTimeoutSeconds = kitHostUploadTimeoutSeconds
	if raw.Library == nil {
		raw.Library = &fileLibrarySettings{}
	}
	off := int64(0)
	raw.Library.FolderWatchSeconds = &off
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(raw); err != nil {
		return nil, fmt.Errorf("encode kit host config: %w", err)
	}
	return out.Bytes(), nil
}

func decodeKitHostConfig(data []byte) (fileConfig, error) {
	var raw fileConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fileConfig{}, err
	}
	return raw, nil
}
