package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type coreVideoCatalog interface {
	ImportCoreVideoPart(context.Context, expansion.Asset, string) (catalog.CoreVideoPart, error)
	CoreVideoParts(context.Context) ([]catalog.CoreVideoPart, error)
	ReadCoreVideoPart(context.Context, string) (expansion.Asset, error)
}

type CoreVideoChoice struct {
	Profile   string `json:"profile"`
	Label     string `json:"label"`
	PartID    string `json:"part_id,omitempty"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// CoreEntryVideo describes the next launch; changing the household preference
// does not modify the FPGA or the identity of an already active session.
type CoreEntryVideo struct {
	GameID           string            `json:"game_id"`
	PackageID        string            `json:"package_id"`
	PreferredProfile string            `json:"preferred_profile"`
	EffectiveProfile string            `json:"effective_profile"`
	PartID           string            `json:"part_id,omitempty"`
	Builtin          bool              `json:"builtin"`
	FallbackReason   string            `json:"fallback_reason,omitempty"`
	Choices          []CoreVideoChoice `json:"choices"`
}

func defaultVideoProfile(profile string) string {
	if profile == "" {
		return "direct"
	}
	return profile
}

func videoPartUnavailable() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "selected video part is unavailable or incompatible with the exact core package and CPU expansion"}
}

func videoPartError(err error) error {
	if errors.Is(err, catalog.ErrCoreVideoProfileConflict) {
		return &protocol.APIError{Code: protocol.CodeStaleRevision, Phase: "admission", Message: "this exact core package already has a different video part for that profile"}
	}
	if errors.Is(err, catalog.ErrInvalidCoreVideoPart) || errors.Is(err, catalog.ErrCoreVideoPartNotFound) {
		return videoPartUnavailable()
	}
	return mapCoreEntryError(err)
}

func (s *Service) ImportCoreVideoPart(ctx context.Context, size int64, body io.Reader, profile string) (catalog.CoreVideoPart, error) {
	store, ok := s.catalog.(coreVideoCatalog)
	if !ok {
		return catalog.CoreVideoPart{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if !catalog.ValidVideoProfile(profile) || size < 1 || size > expansion.MaxArchiveBytes || size > catalog.MaxCoreMediaBytes {
		return catalog.CoreVideoPart{}, videoPartUnavailable()
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil || int64(len(data)) != size {
		return catalog.CoreVideoPart{}, videoPartUnavailable()
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(data))
	if err != nil || (asset.Manifest.Slot != expansion.VideoSlot && asset.Manifest.Slot != expansion.NativeVideoSlot) {
		return catalog.CoreVideoPart{}, videoPartUnavailable()
	}
	_, base, err := s.readInstalledCore(ctx, asset.Manifest.ShellPackageID)
	if err != nil {
		return catalog.CoreVideoPart{}, err
	}
	// Decode and link the sealed FPGA bytes rather than estimating resource
	// availability from labels or a LUT budget.
	if _, err := corepackage.ComposePartsArchive(ctx, base, []expansion.Asset{asset}); err != nil {
		return catalog.CoreVideoPart{}, videoPartUnavailable()
	}
	value, err := store.ImportCoreVideoPart(ctx, asset, profile)
	return value, videoPartError(err)
}

func (s *Service) CoreVideoParts(ctx context.Context) ([]catalog.CoreVideoPart, error) {
	store, ok := s.catalog.(coreVideoCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	parts, err := store.CoreVideoParts(ctx)
	return parts, videoPartError(err)
}

func hasVideoSocket(inspection corepackage.Inspection) bool {
	for _, i := range inspection.Descriptor.Interfaces {
		if i.ID == expansion.VideoSlot || i.ID == expansion.NativeVideoSlot {
			return true
		}
	}
	return false
}

func requiresVideoPart(inspection corepackage.Inspection) bool {
	for _, i := range inspection.Descriptor.Interfaces {
		if i.ID == expansion.NativeVideoSlot {
			return true
		}
	}
	return false
}

// Validate the closed shell contract even when no candidate is installed.
// Unknown or mixed fabric markers must never select built-in direct output.
func libraryVideoAdmission(inspection corepackage.Inspection) error {
	if hasVideoSocket(inspection) {
		if _, err := corepackage.PartsShell(inspection, nil); err != nil {
			return videoPartUnavailable()
		}
	}
	return nil
}

func (s *Service) videoCandidates(ctx context.Context, inspection corepackage.Inspection) (map[string]catalog.CoreVideoPart, error) {
	candidates := make(map[string]catalog.CoreVideoPart)
	store, ok := s.catalog.(coreVideoCatalog)
	if !ok || !hasVideoSocket(inspection) {
		return candidates, nil
	}
	rows, err := store.CoreVideoParts(ctx)
	if err != nil {
		return nil, videoPartError(err)
	}
	for _, row := range rows {
		if row.PackageID == inspection.PackageID {
			if _, duplicate := candidates[row.Profile]; duplicate {
				return nil, videoPartUnavailable()
			}
			candidates[row.Profile] = row
		}
	}
	return candidates, nil
}

func (s *Service) composeVideoCandidate(ctx context.Context, entry catalog.CoreEntry, base []byte, candidate catalog.CoreVideoPart) (*corepackage.PartsBundle, error) {
	store, ok := s.catalog.(coreVideoCatalog)
	if !ok {
		return nil, videoPartUnavailable()
	}
	asset, err := store.ReadCoreVideoPart(ctx, candidate.PartID)
	if err != nil {
		return nil, videoPartError(err)
	}
	assets := []expansion.Asset{asset}
	if cpu, ok := s.catalog.(coreExpansionCatalog); ok {
		selected, err := cpu.CoreEntryExpansion(ctx, entry.GameID)
		if err != nil {
			return nil, expansionError(err)
		}
		if selected.ExpansionID != "" {
			part, err := cpu.ReadCoreExpansion(ctx, selected.ExpansionID)
			if err != nil {
				return nil, expansionError(err)
			}
			assets = append(assets, part)
		}
	}
	bundle, err := corepackage.ComposePartsArchive(ctx, base, assets)
	if err != nil {
		return nil, videoPartUnavailable()
	}
	return &bundle, nil
}

// A missing profile may fall back to direct. An installed selected part must
// pass admission: corruption or an incompatible CPU selection is an error.
func (s *Service) composeVideoEntry(ctx context.Context, entry catalog.CoreEntry, inspection corepackage.Inspection, base []byte) (*corepackage.PartsBundle, error) {
	candidates, err := s.videoCandidates(ctx, inspection)
	if err != nil {
		return nil, err
	}
	profile := s.LibrarySettings().VideoProfile
	candidate, found := candidates[profile]
	if !found {
		candidate, found = candidates["direct"]
	}
	if !found {
		if requiresVideoPart(inspection) {
			return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "native video shell requires a matching Direct video part for this exact core package"}
		}
		return nil, nil
	}
	return s.composeVideoCandidate(ctx, entry, base, candidate)
}

func (s *Service) CoreEntryVideo(ctx context.Context, gameID string) (CoreEntryVideo, error) {
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return CoreEntryVideo{}, err
	}
	inspection, base, err := s.readInstalledCore(ctx, entry.PackageID)
	if err != nil {
		return CoreEntryVideo{}, err
	}
	if err := libraryVideoAdmission(inspection); err != nil {
		return CoreEntryVideo{}, err
	}
	candidates, err := s.videoCandidates(ctx, inspection)
	if err != nil {
		return CoreEntryVideo{}, err
	}
	builtin := !requiresVideoPart(inspection)
	value := CoreEntryVideo{GameID: gameID, PackageID: entry.PackageID, PreferredProfile: s.LibrarySettings().VideoProfile,
		EffectiveProfile: "direct", Builtin: builtin, Choices: []CoreVideoChoice{}}
	for _, profile := range []string{"direct", "scanlines"} {
		label := "Direct"
		if profile == "scanlines" {
			label = "Scanlines"
		}
		choice := CoreVideoChoice{Profile: profile, Label: label, Available: profile == "direct" && builtin}
		if candidate, found := candidates[profile]; found {
			choice.PartID = candidate.PartID
			_, err := s.composeVideoCandidate(ctx, entry, base, candidate)
			choice.Available = err == nil
			if err != nil {
				choice.Reason = "Installed part failed compatibility or integrity checks; launch requires repair."
			}
		} else if !choice.Available {
			choice.Reason = "No matching part is installed for this exact core package."
			if !hasVideoSocket(inspection) {
				choice.Reason = "This core package uses built-in direct output."
			}
		}
		value.Choices = append(value.Choices, choice)
	}
	selected, found := candidates[value.PreferredProfile]
	if !found && value.PreferredProfile != "direct" {
		value.FallbackReason = value.Choices[1].Reason
		selected, found = candidates["direct"]
	}
	if found {
		value.EffectiveProfile, value.PartID, value.Builtin = selected.Profile, selected.PartID, false
	}
	return value, nil
}
