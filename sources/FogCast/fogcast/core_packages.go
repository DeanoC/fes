package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"reflect"
	"regexp"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

var packageIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type InstalledCorePackage struct {
	corepackage.Inspection
	Entries       []catalog.CoreEntry `json:"entries"`
	Compatibility string              `json:"compatibility"`
}

type CoreCompatibility struct {
	protocol.CoreInspection
	Target   string `json:"target"`
	TargetID string `json:"target_id,omitempty"`
	State    string `json:"state"`
}

type coreEntryCatalog interface {
	CreateCoreEntry(context.Context, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error)
	CoreEntry(context.Context, string) (catalog.CoreEntry, error)
	CoreEntries(context.Context) ([]catalog.CoreEntry, error)
}

type coreFirmwareCatalog interface {
	coreMediaCatalog
	CreateFirmwareRequiredEntry(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

// corePackageStore holds the installed core packages. A desktop host uses its
// own corepackage.Store. A kit host ([core_packages] kit = true) uses
// kitpackages.View: the kit's durable store, then its factory packages.
type corePackageStore interface {
	Import(context.Context, int64, io.Reader) (corepackage.Inspection, bool, error)
	List(context.Context) ([]corepackage.Inspection, error)
	Inspect(context.Context, string) (corepackage.Inspection, error)
	Read(context.Context, string) (corepackage.Inspection, []byte, error)
}

const coreStoreUnavailableMessage = "core package storage is unavailable"

var kitCorePackagesLogf = log.Printf

// openKitCorePackages opens the kit's durable store and logs the outcome,
// because callers only see CORE_PACKAGE_STORE_UNAVAILABLE.
func openKitCorePackages(ctx context.Context) (*kitpackages.Store, error) {
	started := time.Now()
	store, err := kitpackages.OpenPersistentForHost(ctx, kitpackages.DefaultRoot)
	if err != nil {
		kitCorePackagesLogf("fogcast: kit core packages unavailable after %s: %v", time.Since(started).Round(time.Millisecond), err)
		return nil, err
	}
	kitCorePackagesLogf("fogcast: kit core packages open after %s", time.Since(started).Round(time.Millisecond))
	return store, nil
}

type coreInspectionClient interface {
	InspectCore(context.Context, int64, io.Reader) (protocol.CoreInspection, error)
}

func (s *Service) ImportCorePackage(ctx context.Context, size int64, body io.Reader) (corepackage.Inspection, bool, error) {
	if s.corePackages == nil {
		return corepackage.Inspection{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	inspection, created, err := s.corePackages.Import(ctx, size, body)
	if err != nil {
		code := protocol.CodeInternal
		if errors.Is(err, corepackage.ErrInvalidPackage) {
			code = protocol.CodeInvalidArchive
		}
		if errors.Is(err, kitpackages.ErrStoreUnavailable) {
			code = protocol.CodeCorePackageStoreUnavailable
		}
		return corepackage.Inspection{}, false, canonicalError(code, safeContextError(err))
	}
	return inspection, created, nil
}

func (s *Service) CorePackages(ctx context.Context) ([]InstalledCorePackage, error) {
	if s.corePackages == nil {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	values, err := s.corePackages.List(ctx)
	if errors.Is(err, kitpackages.ErrStoreUnavailable) {
		return nil, canonicalError(protocol.CodeCorePackageStoreUnavailable, safeContextError(err))
	}
	if errors.Is(err, kitpackages.ErrInvalidPackage) || errors.Is(err, kitpackages.ErrInvalidPackageID) {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	entries, err := s.CoreEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]InstalledCorePackage, 0, len(values))
	for _, value := range values {
		item := InstalledCorePackage{Inspection: value, Entries: []catalog.CoreEntry{}, Compatibility: "unknown"}
		for _, entry := range entries {
			if entry.PackageID == value.PackageID {
				item.Entries = append(item.Entries, entry)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) CorePackage(ctx context.Context, id string) (corepackage.Inspection, error) {
	return s.installedCoreInspection(ctx, id)
}

// installedCoreInspection is readInstalledCore for callers that need only
// the descriptor; an unchanged archive is not read or hashed again.
func (s *Service) installedCoreInspection(ctx context.Context, id string) (corepackage.Inspection, error) {
	if err := s.installedCoreRequest(id); err != nil {
		return corepackage.Inspection{}, err
	}
	value, err := s.corePackages.Inspect(ctx, id)
	if err != nil {
		return corepackage.Inspection{}, installedCoreError(err)
	}
	return value, nil
}

func (s *Service) readInstalledCore(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	if err := s.installedCoreRequest(id); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	value, data, err := s.corePackages.Read(ctx, id)
	if err != nil {
		return corepackage.Inspection{}, nil, installedCoreError(err)
	}
	return value, data, nil
}

func (s *Service) installedCoreRequest(id string) error {
	if !packageIDPattern.MatchString(id) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.corePackages == nil {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return nil
}

func installedCoreError(err error) error {
	if errors.Is(err, corepackage.ErrPackageNotFound) {
		return canonicalError(protocol.CodeROMNotFound, nil)
	}
	if errors.Is(err, kitpackages.ErrStoreUnavailable) {
		return canonicalError(protocol.CodeCorePackageStoreUnavailable, safeContextError(err))
	}
	if errors.Is(err, kitpackages.ErrInvalidPackage) || errors.Is(err, kitpackages.ErrInvalidPackageID) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	return canonicalError(protocol.CodeInternal, safeContextError(err))
}

func (s *Service) CorePackageCompatibility(parent context.Context, id string) (CoreCompatibility, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return CoreCompatibility{}, err
	}
	defer release()
	return s.inspectInstalledCore(ctx, id)
}

// Caller holds lifecycle admission, excluding a target switch during inspection.
func (s *Service) inspectInstalledCore(ctx context.Context, id string) (CoreCompatibility, error) {
	value, data, err := s.readInstalledCore(ctx, id)
	if err != nil {
		return CoreCompatibility{}, err
	}
	if SessionTargetFromContext(ctx) != "" || s.protocolAdmissionEnabled() {
		if _, err = s.refreshTargetAdmission(ctx); err != nil {
			return CoreCompatibility{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.clientForSessionContextLocked(ctx)
	if !ok {
		return CoreCompatibility{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	inspector, ok := client.(coreInspectionClient)
	if !ok {
		return CoreCompatibility{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	health, err := client.Health(ctx)
	if err != nil {
		return CoreCompatibility{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	result, err := inspectInstalledPackageForClient(ctx, value, data, inspector)
	if err != nil {
		return CoreCompatibility{}, err
	}
	state := "incompatible"
	if result.Compatible {
		state = "compatible"
	}
	target := SessionTargetFromContext(ctx)
	if target == "" {
		target = s.selectedTarget
	}
	return CoreCompatibility{CoreInspection: result, Target: target, TargetID: health.TargetID, State: state}, nil
}

func (s *Service) inspectInstalledPackageForClient(ctx context.Context, id string, client coreInspectionClient) (protocol.CoreInspection, error) {
	value, data, err := s.readInstalledCore(ctx, id)
	if err != nil {
		return protocol.CoreInspection{}, err
	}
	return inspectInstalledPackageForClient(ctx, value, data, client)
}

func inspectInstalledPackageForClient(ctx context.Context, value corepackage.Inspection, data []byte, client coreInspectionClient) (protocol.CoreInspection, error) {
	result, err := client.InspectCore(ctx, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		return protocol.CoreInspection{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if result.PackageID != value.PackageID || !reflect.DeepEqual(result.Descriptor, value.Descriptor) || result.Compatible != (result.CompatibilityError == nil) || !validPersistenceLayout(result.Descriptor, result.PersistenceLayout) {
		return protocol.CoreInspection{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return result, nil
}

func (s *Service) CoreEntries(ctx context.Context) ([]catalog.CoreEntry, error) {
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	result, err := store.CoreEntries(ctx)
	return result, mapCoreEntryError(err)
}

// CoreEntriesByIDs resolves the returned page without enumerating the catalog.
func (s *Service) CoreEntriesByIDs(ctx context.Context, ids []string) ([]catalog.CoreEntry, error) {
	for _, id := range ids {
		if protocol.ValidateGameID(id) != nil {
			return nil, canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	store, ok := s.catalog.(interface {
		CoreEntriesByIDs(context.Context, []string) ([]catalog.CoreEntry, error)
	})
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entries, err := store.CoreEntriesByIDs(ctx, ids)
	if err != nil {
		return nil, mapCoreEntryError(err)
	}
	return entries, nil
}

func (s *Service) CoreEntry(ctx context.Context, id string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(id) != nil {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	result, err := store.CoreEntry(ctx, id)
	return result, mapCoreEntryError(err)
}

type removableCoreEntryCatalog interface {
	RemoveCoreEntry(context.Context, string) error
	RetainedCoreEntry(context.Context, string) (bool, error)
	CoreEntry(context.Context, string) (catalog.CoreEntry, error)
}

// checkRemovalTargets observes configured kits even when the host has just
// restarted and has not yet reconstructed its local play records. The caller
// holds lifecycle admission, so this host cannot launch during the check.
func (s *Service) checkRemovalTargets(parent context.Context, gameID string) error {
	var targets []serviceClient
	s.targetMu.Lock()
	for _, cfg := range s.targets {
		if !cfg.Enabled {
			continue
		}
		client, ok := s.targetClientLocked(cfg.Name)
		if !ok {
			s.targetMu.Unlock()
			return canonicalError(protocol.CodeMiSTerUnavailable, nil)
		}
		targets = append(targets, client)
	}
	s.targetMu.Unlock()
	timeout := s.requestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	for _, target := range targets {
		status, err := target.Status(ctx)
		if err != nil {
			return canonicalError(protocol.CodeMiSTerUnavailable, nil)
		}
		if validRecoveredDevelopmentStatus(status) && status.CorePackage == nil {
			continue
		}
		if status.State == protocol.StateActive && status.Recovery == "" && status.LastError == nil &&
			status.GameID != nil && protocol.ValidateGameID(*status.GameID) == nil && *status.GameID != gameID {
			continue
		}
		return canonicalError(protocol.CodeBusy, nil)
	}
	return nil
}

func (s *Service) localCoreEntryID(ctx context.Context, title, coreID, packageID string) (string, error) {
	if store, ok := s.catalog.(interface {
		ResolveCoreEntryID(context.Context, string, string, string) (string, error)
	}); ok {
		return store.ResolveCoreEntryID(ctx, title, coreID, packageID)
	}
	return catalog.CoreEntryID(title, coreID, packageID)
}

// RemoveCoreEntry removes a library title, retaining saved media and core data.
// A retry also completes user-state cleanup if the earlier cleanup failed.
func (s *Service) RemoveCoreEntry(ctx context.Context, gameID string) error {
	if protocol.ValidateGameID(gameID) != nil {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	store, ok := s.catalog.(removableCoreEntryCatalog)
	if !ok {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if s.users == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.removeCoreEntryLocked(ctx, store, gameID)
}

// Caller holds lifecycle admission.
func (s *Service) removeCoreEntryLocked(ctx context.Context, store removableCoreEntryCatalog, gameID string) error {
	if _, err := store.CoreEntry(ctx, gameID); err != nil {
		if !errors.Is(err, catalog.ErrCoreEntryNotFound) {
			return mapCoreEntryError(err)
		}
		retained, err := store.RetainedCoreEntry(ctx, gameID)
		if err != nil {
			return mapCoreEntryError(err)
		}
		if !retained {
			return mapCoreEntryError(catalog.ErrCoreEntryNotFound)
		}
	}
	s.executionMu.Lock()
	busy := s.activeGameID == gameID
	for _, play := range s.plays {
		if play.gameID == gameID {
			busy = true
			break
		}
	}
	s.executionMu.Unlock()
	if busy {
		return canonicalError(protocol.CodeBusy, nil)
	}
	if err := s.checkRemovalTargets(ctx, gameID); err != nil {
		return err
	}
	err := s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if err := store.RemoveCoreEntry(ctx, gameID); err != nil {
			return mapCoreEntryError(err)
		}
		if err := m.RemoveGameReferences(ctx, gameID); err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		return nil
	})
	if err != nil {
		var apiErr *protocol.APIError
		if errors.As(err, &apiErr) {
			return err
		}
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

// cleanupRetainedEntry clears an interrupted removal before the same stable
// identity is made visible again.
func (s *Service) cleanupRetainedEntry(ctx context.Context, gameID string) error {
	store, ok := s.catalog.(interface {
		RetainedCoreEntry(context.Context, string) (bool, error)
	})
	if !ok {
		return nil
	}
	if s.users == nil {
		retained, err := store.RetainedCoreEntry(ctx, gameID)
		if err != nil {
			return mapCoreEntryError(err)
		}
		if retained {
			return canonicalError(protocol.CodeInternal, nil)
		}
		return nil
	}
	if err := s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		retained, err := store.RetainedCoreEntry(ctx, gameID)
		if err != nil {
			return err
		}
		if retained {
			return m.RemoveGameReferences(ctx, gameID)
		}
		return nil
	}); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) CreateCoreEntry(parent context.Context, title, id string) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id, "", "", false, nil)
}

func (s *Service) CreateCoreMediaEntry(parent context.Context, title, id, role, mediaID string) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id, role, mediaID, false, nil)
}

func (s *Service) CreateCoreEntryWithFirmware(parent context.Context, title, id, role, mediaID string, firmwareRequired bool) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id, role, mediaID, firmwareRequired, nil)
}

func (s *Service) SelectCoreEntry(parent context.Context, gameID, expected, id string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if !packageIDPattern.MatchString(expected) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.writeCoreEntry(parent, gameID, "", expected, id, "", "", false, nil)
}

func (s *Service) writeCoreEntry(parent context.Context, gameID, title, expected, id, role, mediaID string, firmwareRequired bool, kit *catalog.KitHostEntryIdentity) (catalog.CoreEntry, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	return s.writeCoreEntryLocked(ctx, gameID, title, expected, id, role, mediaID, firmwareRequired, kit)
}

// Caller holds lifecycle admission.
func (s *Service) writeCoreEntryLocked(ctx context.Context, gameID, title, expected, id, role, mediaID string, firmwareRequired bool, kit *catalog.KitHostEntryIdentity) (catalog.CoreEntry, error) {
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	var current *CoreCompatibility
	var more []catalog.SetMedia
	if gameID != "" {
		entry, err := store.CoreEntry(ctx, gameID)
		if err != nil {
			return catalog.CoreEntry{}, mapCoreEntryError(err)
		}
		if entry.PackageID != expected {
			return catalog.CoreEntry{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
		}
		role, mediaID, more = entry.MediaRole, entry.MediaID, entry.MoreMedia
		old, err := s.inspectInstalledCore(ctx, entry.PackageID)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		current = &old
	}
	check, err := s.inspectInstalledCore(ctx, id)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if !check.Compatible {
		return catalog.CoreEntry{}, check.CompatibilityError
	}
	// Format-4 requires all named sources even when the caller omits the flag.
	if check.Descriptor.Format == 4 {
		firmwareRequired = true
	}
	if firmwareRequired && check.Descriptor.Format == 3 && check.Descriptor.ROM != nil && check.Descriptor.ROM.Role == "cartridge" {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	var cartridge *corepackage.ROM
	if gameID == "" && kit == nil && role == "cartridge" {
		if protocol.ValidateDigest(mediaID) != nil || !linkedCartridge(check.Descriptor) {
			return catalog.CoreEntry{}, romAdmissionError()
		}
		if err := validateROMMediaContract(check.Descriptor); err != nil {
			return catalog.CoreEntry{}, err
		}
		if check.Descriptor.Format == 3 {
			cartridge = check.Descriptor.ROM
		} else {
			rom := check.Descriptor.ROMs[1]
			cartridge = &corepackage.ROM{ID: rom.ID, Role: rom.Role, SourceSize: rom.SourceSize}
		}
		mediaStore, ok := s.catalog.(coreMediaCatalog)
		if !ok {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
		}
		media, reader, err := mediaStore.OpenCoreMedia(ctx, mediaID)
		if err != nil {
			return catalog.CoreEntry{}, romSelectionError(err)
		}
		if err := reader.Close(); err != nil {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		if media.MediaID != mediaID || media.Size != cartridge.SourceSize {
			return catalog.CoreEntry{}, romAdmissionError()
		}
	} else if err := s.validateCoreEntryMedia(ctx, check.Descriptor, role, mediaID); err != nil {
		return catalog.CoreEntry{}, err
	}
	// The new package must still have a compatible drive for each disk.
	for _, m := range more {
		if err := s.validateCoreMediaForEntry(ctx, check.Descriptor, role, m.MediaID, false); err != nil {
			return catalog.CoreEntry{}, err
		}
	}
	if current != nil && current.PersistenceLayout != nil && !reflect.DeepEqual(current.PersistenceLayout, check.PersistenceLayout) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeIncompatibleData, nil)
	}
	// A pc386 record is per library game: a new entry has none to read yet,
	// and its launch reads the layout from the kit.
	if !protocol.DeclaresPerGameMachine(check.Descriptor) || gameID != "" {
		data, err := s.inspectInstalledCoreData(ctx, id, gameID, nil)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		if !reflect.DeepEqual(data.Layout, check.PersistenceLayout) {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
		}
	}
	var entry catalog.CoreEntry
	if gameID == "" {
		if kit == nil {
			newID, err := s.localCoreEntryID(ctx, title, check.Descriptor.Core.ID, id)
			if err != nil {
				return catalog.CoreEntry{}, mapCoreEntryError(err)
			}
			if err := s.cleanupRetainedEntry(ctx, newID); err != nil {
				return catalog.CoreEntry{}, err
			}
		}
		if kit != nil {
			kitStore, ok := s.catalog.(interface {
				CreateKitHostEntry(context.Context, string, string, string, string, string, catalog.KitHostEntryIdentity) (catalog.CoreEntry, error)
			})
			if !ok {
				return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
			}
			if kit.ROMID != "" && (check.Descriptor.ROM == nil || check.Descriptor.ROM.ID != kit.ROMID || check.Descriptor.ROM.Role != "cartridge" || check.Descriptor.ROM.SourceSize != kit.ROMSourceSize) {
				return catalog.CoreEntry{}, romAdmissionError()
			}
			entry, err = kitStore.CreateKitHostEntry(ctx, title, check.Descriptor.Core.ID, id, role, mediaID, *kit)
		} else if cartridge != nil {
			romStore, ok := s.catalog.(interface {
				CreateCoreROMEntry(context.Context, string, string, string, string, int64, string, bool) (catalog.CoreEntry, error)
			})
			if !ok {
				return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
			}
			entry, err = romStore.CreateCoreROMEntry(ctx, title, check.Descriptor.Core.ID, id, cartridge.ID, cartridge.SourceSize, mediaID, firmwareRequired)
			if err != nil {
				return catalog.CoreEntry{}, romSelectionError(err)
			}
		} else if firmwareRequired {
			if mediaStore, ok := s.catalog.(coreFirmwareCatalog); ok {
				entry, err = mediaStore.CreateFirmwareRequiredEntry(ctx, title, check.Descriptor.Core.ID, id, role, mediaID)
			} else {
				return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
			}
		} else if role == "" && mediaID == "" {
			entry, err = store.CreateCoreEntry(ctx, title, check.Descriptor.Core.ID, id)
		} else if mediaStore, ok := s.catalog.(coreMediaCatalog); ok {
			entry, err = mediaStore.CreateCoreMediaEntry(ctx, title, check.Descriptor.Core.ID, id, role, mediaID)
		} else {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
		}
	} else {
		entry, err = store.SelectCoreEntry(ctx, gameID, check.Descriptor.Core.ID, expected, id)
	}
	return entry, mapCoreEntryError(err)
}

func mapCoreEntryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalog.ErrCoreEntryNotFound):
		return canonicalError(protocol.CodeROMNotFound, nil)
	case errors.Is(err, catalog.ErrCoreEntryConflict):
		return &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "core entry selection changed; refresh and retry"}
	case errors.Is(err, catalog.ErrInvalidCoreEntry), errors.Is(err, catalog.ErrCoreEntryCoreMismatch):
		return canonicalError(protocol.CodeBadRequest, nil)
	default:
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
}
