package fogcast

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/pelletier/go-toml/v2"
)

var canonicalConfigPublishErrorHook func() error

// writeCanonicalConfig atomically replaces the UI-managed portions of the
// canonical config.toml while preserving every other recognized setting.
func writeCanonicalConfig(path string, libraries []catalog.Root, targets []TargetConfig, selectedTarget string) error {
	if len(targets) == 0 {
		return fmt.Errorf("at least one named target is required")
	}
	normalizedLibraries, err := normalizeCatalogRoots(libraries)
	if err != nil {
		return err
	}
	normalizedTargets, normalizedSelected, err := normalizeTargets(targets, selectedTarget)
	if err != nil {
		return err
	}

	source, err := openConfigSource(path)
	if err != nil {
		return fmt.Errorf("open FogCast config for update: %w", err)
	}
	var raw fileConfig
	decoder := toml.NewDecoder(source)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&raw)
	closeErr := source.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode FogCast config for update: %w", decodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close FogCast config after update read: %w", closeErr)
	}

	raw.Libraries = make([]fileLibrary, 0, len(normalizedLibraries))
	for _, root := range normalizedLibraries {
		raw.Libraries = append(raw.Libraries, fileLibrary{ID: root.ID, System: root.System, Root: root.Path})
	}
	raw.Targets = make([]fileTarget, 0, len(normalizedTargets))
	for _, target := range normalizedTargets {
		raw.Targets = append(raw.Targets, fileTarget{
			Name: target.Name, Enabled: target.Enabled, Address: target.Address, Agent: target.Agent,
		})
	}
	raw.SelectedTarget = normalizedSelected
	// A local watch_root that names a mapped system folder is a legacy
	// singleton library declaration. Once the managed [[libraries]] list is
	// written, retaining it would recreate the removed legacy library on the
	// next load (or conflict with its generated ID after an edit).
	if raw.Library != nil && strings.TrimSpace(raw.Library.WatchRoot) != "" {
		watchRoot, normalizeErr := normalizeWatchRoot(raw.Library.WatchRoot)
		if normalizeErr != nil {
			return fmt.Errorf("normalize library watch_root for update: %w", normalizeErr)
		}
		_, _, mappedFolder := mappedFolderParent(watchRoot)
		if mappedFolder && !isUNCWatchRoot(strings.ReplaceAll(watchRoot, `\`, "/")) {
			raw.Library.WatchRoot = ""
		}
	}
	// Once named targets exist, the legacy singleton fields must not retain a
	// duplicate credential. LoadConfig still projects the selection into the
	// compatibility Config.BaseURL and Config.Token fields.
	raw.BaseURL = ""
	raw.Token = ""

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".fogcast-config-*")
	if err != nil {
		return fmt.Errorf("create private FogCast config update: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure FogCast config update: %w", err)
	}
	if err := toml.NewEncoder(temporary).Encode(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode FogCast config update: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync FogCast config update: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close FogCast config update: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish FogCast config update: %w", err)
	}
	if canonicalConfigPublishErrorHook != nil {
		if err := canonicalConfigPublishErrorHook(); err != nil {
			return fmt.Errorf("publish FogCast config update: %w", err)
		}
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open FogCast config directory after update: %w", err)
	}
	syncErr := directoryHandle.Sync()
	closeDirectoryErr := directoryHandle.Close()
	if syncErr != nil {
		return fmt.Errorf("sync FogCast config directory after update: %w", syncErr)
	}
	if closeDirectoryErr != nil {
		return fmt.Errorf("close FogCast config directory after update: %w", closeDirectoryErr)
	}
	return nil
}

func snapshotPrivateFile(path string) ([]byte, bool, error) {
	body, err := os.ReadFile(path)
	if err == nil {
		return body, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func restorePrivateFile(path string, body []byte, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".fogcast-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if err := ensurePrivateRegularFile(path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	return errors.Join(syncErr, closeErr)
}
