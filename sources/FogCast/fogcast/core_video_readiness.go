package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

// videoReadiness is scoped to one catalog listing. It snapshots the selected
// profile and video rows once and verifies each selected part at most once,
// rather than re-reading the same archive for every title using that shell.
// It never reads or composes the shell; launch remains authoritative for the
// combined video/CPU/card configuration.
type videoReadiness struct {
	store     coreVideoCatalog
	profile   string
	loaded    bool
	rows      map[string]map[string]catalog.CoreVideoPart
	duplicate map[string]bool
	validated map[string]videoReadinessPart
}

type videoReadinessPart struct {
	manifest expansion.Manifest
	valid    bool
}

func (v *videoReadiness) ready(ctx context.Context, inspection corepackage.Inspection) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := libraryVideoAdmission(inspection); err != nil {
		return false, nil
	}
	if !v.loaded {
		v.loaded = true
		v.rows = make(map[string]map[string]catalog.CoreVideoPart)
		v.duplicate = make(map[string]bool)
		v.validated = make(map[string]videoReadinessPart)
		if v.store != nil {
			parts, err := v.store.CoreVideoParts(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return false, err
				}
				return false, videoPartError(err)
			}
			for _, part := range parts {
				if v.rows[part.PackageID] == nil {
					v.rows[part.PackageID] = make(map[string]catalog.CoreVideoPart)
				}
				if _, duplicate := v.rows[part.PackageID][part.Profile]; duplicate {
					v.duplicate[part.PackageID] = true
				}
				v.rows[part.PackageID][part.Profile] = part
			}
		}
	}
	if v.duplicate[inspection.PackageID] {
		return false, videoPartUnavailable()
	}
	candidate, found := chooseVideoCandidate(v.rows[inspection.PackageID], v.profile)
	if !found {
		return !requiresVideoPart(inspection), nil
	}
	part, checked := v.validated[candidate.PartID]
	if !checked {
		asset, err := v.store.ReadCoreVideoPart(ctx, candidate.PartID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return false, err
			}
			if errors.Is(err, catalog.ErrInvalidCoreVideoPart) || errors.Is(err, catalog.ErrCoreVideoPartNotFound) {
				v.validated[candidate.PartID] = part
				return false, nil
			}
			return false, videoPartError(err)
		}
		part = videoReadinessPart{manifest: asset.Manifest, valid: true}
		v.validated[candidate.PartID] = part
	}
	if !part.valid {
		return false, nil
	}
	wantSlot := expansion.VideoSlot
	if requiresVideoPart(inspection) {
		wantSlot = expansion.NativeVideoSlot
	}
	m := part.manifest
	return m.Slot == wantSlot && m.ShellPackageID == inspection.PackageID &&
		m.ShellBuildID == inspection.Descriptor.Build.ID && m.ShellSHA256 == inspection.Descriptor.Payload.SHA256, nil
}
