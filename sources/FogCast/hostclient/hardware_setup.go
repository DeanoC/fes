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
	"github.com/DeanoC/FogCast/internal/zx81family"
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
	coreID := installed.Descriptor.Core.ID
	if installed.PackageID != pkg || !zx81family.Known(coreID) || installed.Descriptor.ROM == nil {
		return CoreSetup{}, fmt.Errorf("choose a ZX81-family package with a sealed machine ROM requirement")
	}
	r := installed.Descriptor.ROM
	library, err := c.CoreLibrary(ctx)
	if err != nil {
		return CoreSetup{}, err
	}
	result := CoreSetup{CoreReference: CoreReference{CoreID: coreID, PackageID: pkg}, Descriptor: installed.Descriptor, ROMs: []SetupROM{{ID: r.ID, Role: r.Role, SourceSize: r.SourceSize, Binding: "entry"}}}
	for _, entry := range library.Entries {
		if entry.CoreID == coreID {
			result.Entries = append(result.Entries, entry)
		}
	}
	err = c.getJSON(ctx, "/api/v1/core-packages/"+pkg+"/media-capabilities", &result.Capabilities)
	return result, err
}

// CreateInstalledZX81Entry preserves an existing title's package/media/ROM
// selections. Explicit retries can finish an empty ROM binding, never replace it.
func (c *Client) CreateInstalledZX81Entry(ctx context.Context, setup CoreSetup, title string, roms map[string]string) (CoreEntry, error) {
	if !zx81family.Known(setup.CoreID) || len(setup.ROMs) != 1 || protocol.ValidateDigest(setup.PackageID) != nil {
		return CoreEntry{}, fmt.Errorf("invalid ZX81-family setup")
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
		// The library route returns 201. 200 remains success.
		err = c.doJSONStatuses(ctx, http.MethodPost, "/api/v1/library/core-entries", map[string]string{"title": title, "package_id": setup.PackageID}, &entry, http.StatusOK, http.StatusCreated)
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
