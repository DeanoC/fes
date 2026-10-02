package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// HardwareTape is an attributed default tape, served by the host. Its bytes are
// imported on explicit selection; simply opening the shelf changes nothing.
type HardwareTape struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Filename  string `json:"filename"`
	License   string `json:"license"`
	SourceURL string `json:"source_url"`
	Controls  string `json:"controls"`
	SHA256    string `json:"sha256"`
	RAMKB     int    `json:"ram_kb"`
}

func (c *Client) ZX81Tapes(ctx context.Context) ([]HardwareTape, error) {
	var result struct {
		Tapes []HardwareTape `json:"tapes"`
	}
	err := c.getJSON(ctx, "/api/v1/library/zx81-tapes", &result)
	return result.Tapes, err
}

func (c *Client) ImportZX81Tape(ctx context.Context, tape HardwareTape) (CoreMedia, error) {
	if tape.ID == "" || protocol.ValidateDigest(tape.SHA256) != nil {
		return CoreMedia{}, fmt.Errorf("invalid tape identity")
	}
	var result CoreMedia
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/library/zx81-tapes/"+url.PathEscape(tape.ID)+"/import", nil, &result)
	if err == nil && (result.MediaID != tape.SHA256 || !protocol.AdmitTapeMediaSize(result.Size)) {
		err = fmt.Errorf("imported tape identity mismatch")
	}
	return result, err
}

func (c *Client) SelectCoreEntryMedia(ctx context.Context, game, pkg, expected, role, id string) (CoreEntry, error) {
	if protocol.ValidateGameID(game) != nil || protocol.ValidateDigest(pkg) != nil || (expected != "" && protocol.ValidateDigest(expected) != nil) || (id != "" && protocol.ValidateDigest(id) != nil) {
		return CoreEntry{}, fmt.Errorf("invalid media selection identity")
	}
	var result CoreEntry
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(game)+"/media", map[string]string{"expected_package_id": pkg, "expected_media_id": expected, "media_role": role, "media_id": id}, &result)
	if err == nil && (result.GameID != game || result.PackageID != pkg || result.MediaID != id || result.MediaRole != role) {
		err = fmt.Errorf("host returned a different tape selection")
	}
	return result, err
}

func (c *Client) ImportCoreExpansion(ctx context.Context, size int64, content io.Reader) (CoreEntryExpansionAsset, error) {
	if size < 1 || size > expansion.MaxArchiveBytes || content == nil {
		return CoreEntryExpansionAsset{}, fmt.Errorf("invalid expansion archive size")
	}
	data, err := io.ReadAll(io.LimitReader(content, size+1))
	if err != nil || int64(len(data)) != size {
		return CoreEntryExpansionAsset{}, fmt.Errorf("expansion archive length differs")
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(data))
	if err != nil {
		return CoreEntryExpansionAsset{}, err
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/v1/core-expansions", bytes.NewReader(data))
	if err != nil {
		return CoreEntryExpansionAsset{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return CoreEntryExpansionAsset{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return CoreEntryExpansionAsset{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CoreEntryExpansionAsset{}, APIStatusError(resp.StatusCode, body)
	}
	var result CoreEntryExpansionAsset
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	if result.ExpansionID != asset.ID || result.PackageID != asset.Manifest.ShellPackageID || result.Slot != asset.Manifest.SlotIndex {
		return result, fmt.Errorf("imported expansion identity mismatch")
	}
	return result, nil
}

type CoreEntryExpansionAsset struct {
	ExpansionID string `json:"expansion_id"`
	PackageID   string `json:"package_id"`
	Slot        int    `json:"slot,omitempty"`
}

func (c *Client) DescribeCoreExpansion(ctx context.Context, id, label, description string, inProgress bool) error {
	if protocol.ValidateDigest(id) != nil {
		return fmt.Errorf("invalid expansion identity")
	}
	var result struct {
		CoreExpansionPresentation
		InProgress bool `json:"in_progress"`
	}
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/core-expansions/"+id+"/presentation", map[string]any{"label": label, "description": description, "in_progress": inProgress}, &result)
	if err == nil && (result.ExpansionID != id || result.Label != label || result.Description != description || result.InProgress != inProgress) {
		return fmt.Errorf("expansion description mismatch")
	}
	return err
}

// InstalledZX81Setup uses the local package library without a publication
// catalogue. Firmware still comes from the sealed manifest's ROM requirement.
func (c *Client) InstalledZX81Setup(ctx context.Context, pkg string) (CoreSetup, error) {
	if protocol.ValidateDigest(pkg) != nil {
		return CoreSetup{}, fmt.Errorf("invalid package identity")
	}
	var installed struct {
		PackageID  string                 `json:"package_id"`
		Descriptor corepackage.Descriptor `json:"descriptor"`
	}
	if err := c.getJSON(ctx, "/api/v1/core-packages/"+pkg, &installed); err != nil {
		return CoreSetup{}, err
	}
	if installed.PackageID != pkg || installed.Descriptor.Core.ID != "fes.zx81" || installed.Descriptor.ROM == nil {
		return CoreSetup{}, fmt.Errorf("choose a Sinclair ZX81 package with a sealed machine ROM requirement")
	}
	r := installed.Descriptor.ROM
	library, err := c.CoreLibrary(ctx)
	if err != nil {
		return CoreSetup{}, err
	}
	result := CoreSetup{CoreReference: CoreReference{CoreID: "fes.zx81", PackageID: pkg}, Descriptor: installed.Descriptor, ROMs: []SetupROM{{ID: r.ID, Role: r.Role, SourceSize: r.SourceSize, Binding: "entry"}}}
	for _, entry := range library.Entries {
		if entry.CoreID == "fes.zx81" {
			result.Entries = append(result.Entries, entry)
		}
	}
	err = c.getJSON(ctx, "/api/v1/core-packages/"+pkg+"/media-capabilities", &result.Capabilities)
	return result, err
}

// CreateInstalledZX81Entry preserves an existing title's package/media/ROM
// selections. Explicit retries can finish an empty ROM binding, never replace it.
func (c *Client) CreateInstalledZX81Entry(ctx context.Context, setup CoreSetup, title string, roms map[string]string) (CoreEntry, error) {
	if setup.CoreID != "fes.zx81" || len(setup.ROMs) != 1 || protocol.ValidateDigest(setup.PackageID) != nil {
		return CoreEntry{}, fmt.Errorf("invalid Sinclair ZX81 setup")
	}
	rom := setup.ROMs[0]
	mediaID := roms[rom.ID]
	if protocol.ValidateDigest(mediaID) != nil {
		return CoreEntry{}, fmt.Errorf("choose the required machine ROM")
	}
	library, err := c.CoreLibrary(ctx)
	if err != nil {
		return CoreEntry{}, err
	}
	var entry CoreEntry
	for _, candidate := range library.Entries {
		if candidate.CoreID == setup.CoreID && candidate.Title == strings.TrimSpace(title) {
			if candidate.PackageID != setup.PackageID || candidate.MediaID != "" || candidate.FirmwareRequired {
				return entry, fmt.Errorf("this title already has different selections; choose another title")
			}
			entry = candidate
			break
		}
	}
	if entry.GameID == "" {
		err = c.doJSON(ctx, http.MethodPost, "/api/v1/library/core-entries", map[string]string{"title": title, "package_id": setup.PackageID}, &entry)
		if err != nil {
			return entry, err
		}
	}
	if protocol.ValidateGameID(entry.GameID) != nil || entry.PackageID != setup.PackageID || entry.CoreID != setup.CoreID {
		return entry, fmt.Errorf("created setup identity mismatch")
	}
	var binding struct {
		PackageID string `json:"package_id"`
		ROMID     string `json:"rom_id"`
		MediaID   string `json:"media_id"`
		GameID    string `json:"game_id"`
	}
	path := "/api/v1/library/core-entries/" + url.PathEscape(entry.GameID) + "/rom"
	if err = c.getJSON(ctx, path, &binding); err != nil {
		return entry, err
	}
	if binding.GameID != entry.GameID || (binding.PackageID != "" && binding.PackageID != entry.PackageID) {
		return entry, fmt.Errorf("ROM selection identity changed")
	}
	if binding.MediaID != "" {
		if binding.ROMID != rom.ID || binding.MediaID != mediaID {
			return entry, fmt.Errorf("the existing machine ROM differs; choose another title")
		}
		return entry, nil
	}
	err = c.doJSON(ctx, http.MethodPut, path, map[string]string{"package_id": entry.PackageID, "rom_id": rom.ID, "expected_media_id": "", "media_id": mediaID}, &binding)
	if err == nil && (binding.GameID != entry.GameID || binding.PackageID != entry.PackageID || binding.ROMID != rom.ID || binding.MediaID != mediaID) {
		err = fmt.Errorf("selected ROM identity mismatch")
	}
	return entry, err
}
