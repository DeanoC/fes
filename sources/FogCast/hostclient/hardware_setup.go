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

// HardwareTape is an attributed starter cassette or a saved program. Starter
// bytes are imported on explicit selection; saved programs already have a
// MediaID. Simply opening the shelf changes nothing.
type HardwareTape struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Filename  string `json:"filename"`
	License   string `json:"license"`
	SourceURL string `json:"source_url"`
	Controls  string `json:"controls"`
	SHA256    string `json:"sha256"`
	RAMKB     int    `json:"ram_kb"`
	// MediaID is set when the tape is already stored. Selection must not import it again.
	MediaID string `json:"media_id,omitempty"`
}

func (c *Client) ZX81Tapes(ctx context.Context) ([]HardwareTape, error) {
	return c.ZX81TapesForGame(ctx, "")
}

// ZX81TapesForGame returns compatible starter cassettes plus programs saved by
// this machine. The host filters starters against the catalogued core ID.
func (c *Client) ZX81TapesForGame(ctx context.Context, gameID string) ([]HardwareTape, error) {
	path := "/api/v1/library/zx81-tapes"
	if gameID != "" {
		if protocol.ValidateGameID(gameID) != nil {
			return nil, fmt.Errorf("invalid tape game")
		}
		path += "?game_id=" + url.QueryEscape(gameID)
	}
	var result struct {
		Tapes []HardwareTape `json:"tapes"`
	}
	err := c.getJSON(ctx, path, &result)
	return result.Tapes, err
}

// ImportZX81Tape copies the host's embedded starter into its media store. It
// uses the ordinary bounded client: it does not fetch SourceURL or touch the
// FPGA. The later media insert owns the longer physical-operation deadline.
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

// PackageMediaCapabilities reads the installed package's declared media roles.
func (c *Client) PackageMediaCapabilities(ctx context.Context, pkg string) (protocol.CoreMediaCapabilities, error) {
	if protocol.ValidateDigest(pkg) != nil {
		return protocol.CoreMediaCapabilities{}, fmt.Errorf("invalid package identity")
	}
	var result protocol.CoreMediaCapabilities
	if err := c.getJSON(ctx, "/api/v1/core-packages/"+url.PathEscape(pkg)+"/media-capabilities", &result); err != nil {
		return protocol.CoreMediaCapabilities{}, err
	}
	if result.PackageID != pkg {
		return protocol.CoreMediaCapabilities{}, fmt.Errorf("media capabilities identity mismatch")
	}
	return result, nil
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

func (c *Client) CoreEntry(ctx context.Context, game string) (CoreEntry, error) {
	if protocol.ValidateGameID(game) != nil {
		return CoreEntry{}, fmt.Errorf("invalid core entry identity")
	}
	var result CoreEntry
	err := c.getJSON(ctx, "/api/v1/library/core-entries/"+url.PathEscape(game), &result)
	if err == nil && (result.GameID != game || protocol.ValidateDigest(result.PackageID) != nil) {
		err = fmt.Errorf("core entry identity mismatch")
	}
	return result, err
}

// SelectCoreEntryPackage changes the saved package only if it still matches
// the package shown to the user. The host rechecks core, media and persistence
// compatibility before saving the selection.
func (c *Client) SelectCoreEntryPackage(ctx context.Context, game, expected, pkg string) (CoreEntry, error) {
	if protocol.ValidateGameID(game) != nil || protocol.ValidateDigest(expected) != nil || protocol.ValidateDigest(pkg) != nil {
		return CoreEntry{}, fmt.Errorf("invalid package selection identity")
	}
	var result CoreEntry
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(game), map[string]string{
		"expected_package_id": expected, "package_id": pkg,
	}, &result)
	if err == nil && (result.GameID != game || result.PackageID != pkg) {
		err = fmt.Errorf("host returned a different package selection")
	}
	return result, err
}

func (c *Client) SelectCoreEntryROM(ctx context.Context, game, pkg, rom, expected, id string) (CoreEntryROMSelection, error) {
	if protocol.ValidateGameID(game) != nil || protocol.ValidateDigest(pkg) != nil || strings.TrimSpace(rom) == "" || expected != "" && protocol.ValidateDigest(expected) != nil || id != "" && protocol.ValidateDigest(id) != nil {
		return CoreEntryROMSelection{}, fmt.Errorf("invalid ROM selection identity")
	}
	var result CoreEntryROMSelection
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(game)+"/rom", map[string]string{
		"package_id": pkg, "rom_id": rom, "expected_media_id": expected, "media_id": id,
	}, &result)
	if err == nil && (result.GameID != game || result.PackageID != pkg || result.ROMID != rom || result.MediaID != id) {
		err = fmt.Errorf("selected ROM identity mismatch")
	}
	return result, err
}

type CoreEntryROMSelection struct {
	GameID    string `json:"game_id"`
	PackageID string `json:"package_id"`
	ROMID     string `json:"rom_id"`
	MediaID   string `json:"media_id"`
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

// InstalledCoreSetup reads the local package's requirements without a published
// catalog. ROM and media choices still come from the installed manifest.
func (c *Client) InstalledCoreSetup(ctx context.Context, pkg string) (CoreSetup, error) {
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
	descriptor := installed.Descriptor
	if installed.PackageID != pkg || descriptor.Core.ID == "" || descriptor.Core.ID == "fes.menu" || !installedPlayableABI(descriptor.ABI) {
		return CoreSetup{}, fmt.Errorf("choose an installed playable core package")
	}
	library, err := c.CoreLibrary(ctx)
	if err != nil {
		return CoreSetup{}, err
	}
	result := CoreSetup{CoreReference: CoreReference{CoreID: descriptor.Core.ID, PackageID: pkg}, Descriptor: descriptor, ROMs: []SetupROM{}, Entries: []CoreEntry{}}
	if r := descriptor.ROM; r != nil {
		result.ROMs = append(result.ROMs, SetupROM{ID: r.ID, Role: r.Role, SourceSize: r.SourceSize, Binding: "entry"})
	}
	for i, r := range descriptor.ROMs {
		binding := "entry"
		if i == 0 && r.Role == protocol.FirmwareRole {
			binding = "household-firmware"
		}
		result.ROMs = append(result.ROMs, SetupROM{ID: r.ID, Role: r.Role, SourceSize: r.SourceSize, Binding: binding})
	}
	for _, entry := range library.Entries {
		if entry.CoreID == descriptor.Core.ID {
			result.Entries = append(result.Entries, entry)
		}
	}
	result.Capabilities, err = c.PackageMediaCapabilities(ctx, pkg)
	if err != nil {
		return CoreSetup{}, err
	}
	if len(descriptor.ROMs) > 0 {
		firmware, err := c.HouseholdFirmware(ctx)
		if err != nil {
			return CoreSetup{}, err
		}
		result.FirmwareMediaID = firmware.MediaID
	}
	return result, nil
}

func installedPlayableABI(abi corepackage.Contract) bool {
	if abi.Major != 1 || abi.Minor != 0 {
		return false
	}
	switch abi.ID {
	case "fes.application", "fes.simple-game", "fes.simple-computer", "fes.computer":
		return true
	default:
		return false
	}
}

// CreateInstalledCoreEntry preserves existing package, ROM and media choices.
// A retry can finish an empty ROM binding but cannot replace a saved selection.
func (c *Client) CreateInstalledCoreEntry(ctx context.Context, setup CoreSetup, title string, roms map[string]string, mediaRole, mediaID string) (CoreEntry, error) {
	if setup.CoreID == "" || setup.CoreID == "fes.menu" || !installedPlayableABI(setup.Descriptor.ABI) || protocol.ValidateDigest(setup.PackageID) != nil {
		return CoreEntry{}, fmt.Errorf("invalid installed core setup")
	}
	if (mediaRole == "") != (mediaID == "") || mediaID != "" && protocol.ValidateDigest(mediaID) != nil {
		return CoreEntry{}, fmt.Errorf("invalid media selection")
	}
	for _, rom := range setup.ROMs {
		id := roms[rom.ID]
		if protocol.ValidateDigest(id) != nil {
			return CoreEntry{}, fmt.Errorf("choose the required %s ROM", rom.ID)
		}
		if rom.Binding == "household-firmware" && id != setup.FirmwareMediaID {
			return CoreEntry{}, fmt.Errorf("select the chosen household BIOS before adding the game")
		}
	}
	firmwareRequired := len(setup.Descriptor.ROMs) > 0
	library, err := c.CoreLibrary(ctx)
	if err != nil {
		return CoreEntry{}, err
	}
	var entry CoreEntry
	for _, candidate := range library.Entries {
		if candidate.CoreID == setup.CoreID && candidate.Title == strings.TrimSpace(title) {
			if candidate.PackageID != setup.PackageID || candidate.MediaID != mediaID || candidate.MediaRole != mediaRole || candidate.FirmwareRequired != firmwareRequired {
				return entry, fmt.Errorf("this title already has different selections; choose another title")
			}
			entry = candidate
			break
		}
	}
	if entry.GameID == "" {
		request := struct {
			Title            string `json:"title"`
			PackageID        string `json:"package_id"`
			MediaRole        string `json:"media_role,omitempty"`
			MediaID          string `json:"media_id,omitempty"`
			FirmwareRequired bool   `json:"firmware_required,omitempty"`
		}{title, setup.PackageID, mediaRole, mediaID, firmwareRequired}
		err = c.doJSONStatuses(ctx, http.MethodPost, "/api/v1/library/core-entries", request, &entry, http.StatusOK, http.StatusCreated)
		if err != nil {
			return entry, err
		}
	}
	if protocol.ValidateGameID(entry.GameID) != nil || entry.PackageID != setup.PackageID || entry.CoreID != setup.CoreID || entry.MediaID != mediaID || entry.MediaRole != mediaRole || entry.FirmwareRequired != firmwareRequired {
		return entry, fmt.Errorf("created setup identity mismatch")
	}
	for _, rom := range setup.ROMs {
		if rom.Binding != "entry" {
			continue
		}
		var binding CoreEntryROMSelection
		path := "/api/v1/library/core-entries/" + url.PathEscape(entry.GameID) + "/rom"
		if err = c.getJSON(ctx, path, &binding); err != nil {
			return entry, err
		}
		if binding.GameID != entry.GameID || binding.PackageID != "" && binding.PackageID != entry.PackageID {
			return entry, fmt.Errorf("ROM selection identity changed")
		}
		id := roms[rom.ID]
		if binding.MediaID != "" {
			if binding.ROMID != rom.ID || binding.MediaID != id {
				return entry, fmt.Errorf("the existing ROM differs; choose another title")
			}
			continue
		}
		if _, err = c.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, rom.ID, "", id); err != nil {
			return entry, err
		}
	}
	return entry, nil
}
