// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"io/fs"

	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// ProgramStartup is a RAM snapshot read before the menu contains storage.
// Neither catalogue records nor preferences grant hardware or write authority.
type ProgramStartup struct {
	Entries         []ProgramEntry
	Menu            packagestore.Request
	Preferences     *ProgramPreferences
	Staged          []*StagedProgram
	StagedError     error
	PreferencesMode string
	Agent           *agentapi.Provisioning
}

// ReadProgramStartup uses only the current active store. writable and paired
// are compiled platform selections, never values read from an SD manifest.
// Persistent preferences require both: writes then happen only in the existing
// owned launch/return/install/shutdown checkpoints, never during menu scanout.
func ReadProgramStartup(ctx context.Context, store *packagestore.Store, writable, paired bool) (*ProgramStartup, error) {
	if ctx == nil || store == nil {
		return nil, ErrProgramSession
	}
	var entries []ProgramEntry
	var err error
	if writable {
		entries, err = ReadInstallerBootstrap(ctx, store)
	} else {
		var raw []byte
		raw, err = store.ReadStagingRecord(ctx, "FES/CORESEL.JSN")
		if err == nil {
			var choices []packagestore.Request
			choices, err = decodeProgramSelections(ctx, raw)
			if err == nil {
				raw, err = store.ReadStagingRecord(ctx, "FES/UI.JSON")
				if err == nil {
					entries, err = decodeProgramCatalog(raw, choices)
				}
			}
		}
		if err == nil {
			entries, err = ReadInstalledPrograms(ctx, store, entries)
		}
	}
	if err != nil {
		return nil, err
	}
	startup := &ProgramStartup{Entries: entries, PreferencesMode: "ram"}
	if paired {
		raw, err := store.ReadStagingRecord(ctx, agentapi.ProvisioningPath)
		if err == nil {
			provision, err := agentapi.ParseProvisioning(raw)
			clear(raw)
			if err != nil {
				return nil, err
			}
			startup.Agent = &provision
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		// ReadStagingRecord may join a missing file with a lease failure.
		if _, err := store.ActiveReceipt(); err != nil {
			return nil, err
		}
		raw, err = store.ReadStagingRecord(ctx, "FES/MENUSEL.JSN")
		if err != nil {
			return nil, err
		}
		menus, err := decodeProgramSelections(ctx, raw)
		if err != nil {
			return nil, err
		}
		if len(menus) != 1 {
			return nil, errors.New("native menu requires exactly one startup selection")
		}
		startup.Menu = menus[0]
	}
	startup.Preferences, err = LoadProgramPreferences(ctx, store, writable && paired)
	if err != nil {
		return nil, err
	}
	if writable && paired {
		startup.PreferencesMode = "sd-checkpoint"
	}
	if writable {
		startup.Staged, err = ReadStagedPrograms(ctx, store, "FES/INSTALL.JSN")
		if err != nil {
			// An invalid optional install source cannot hide an ownership failure.
			if _, activeErr := store.ActiveReceipt(); activeErr != nil {
				return nil, errors.Join(err, activeErr)
			}
			if !errors.Is(err, ErrStagedCatalogAbsent) {
				startup.StagedError = err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := store.ActiveReceipt(); err != nil {
		return nil, err
	}
	return startup, nil
}
