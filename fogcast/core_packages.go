package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"regexp"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/corepackage"
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
		return corepackage.Inspection{}, false, canonicalError(code, safeContextError(err))
	}
	return inspection, created, nil
}

func (s *Service) CorePackages(ctx context.Context) ([]InstalledCorePackage, error) {
	if s.corePackages == nil {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	values, err := s.corePackages.List(ctx)
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
	value, _, err := s.readInstalledCore(ctx, id)
	return value, err
}

func (s *Service) readInstalledCore(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	if !packageIDPattern.MatchString(id) {
		return corepackage.Inspection{}, nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.corePackages == nil {
		return corepackage.Inspection{}, nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, data, err := s.corePackages.Read(ctx, id)
	if errors.Is(err, corepackage.ErrPackageNotFound) {
		return corepackage.Inspection{}, nil, canonicalError(protocol.CodeROMNotFound, nil)
	}
	if err != nil {
		return corepackage.Inspection{}, nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return value, data, nil
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
	if s.discoveryEnabled() {
		if _, err = s.refreshTargetConnection(ctx); err != nil {
			return CoreCompatibility{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
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
	result, err := inspector.InspectCore(ctx, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		return CoreCompatibility{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if result.PackageID != value.PackageID || !reflect.DeepEqual(result.Descriptor, value.Descriptor) || result.Compatible != (result.CompatibilityError == nil) || !validPersistenceLayout(result.Descriptor, result.PersistenceLayout) {
		return CoreCompatibility{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	state := "incompatible"
	if result.Compatible {
		state = "compatible"
	}
	return CoreCompatibility{CoreInspection: result, Target: s.selectedTarget, TargetID: health.TargetID, State: state}, nil
}

func (s *Service) CoreEntries(ctx context.Context) ([]catalog.CoreEntry, error) {
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	result, err := store.CoreEntries(ctx)
	return result, mapCoreEntryError(err)
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
func (s *Service) CreateCoreEntry(parent context.Context, title, id string) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id)
}
func (s *Service) SelectCoreEntry(parent context.Context, gameID, expected, id string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if !packageIDPattern.MatchString(expected) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.writeCoreEntry(parent, gameID, "", expected, id)
}
func (s *Service) writeCoreEntry(parent context.Context, gameID, title, expected, id string) (catalog.CoreEntry, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	var current *CoreCompatibility
	if gameID != "" {
		entry, err := store.CoreEntry(ctx, gameID)
		if err != nil {
			return catalog.CoreEntry{}, mapCoreEntryError(err)
		}
		if entry.PackageID != expected {
			return catalog.CoreEntry{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
		}
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
	if current != nil && current.PersistenceLayout != nil && !reflect.DeepEqual(current.PersistenceLayout, check.PersistenceLayout) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeIncompatibleData, nil)
	}
	data, err := s.inspectInstalledCoreData(ctx, id, nil)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if !reflect.DeepEqual(data.Layout, check.PersistenceLayout) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	var entry catalog.CoreEntry
	if gameID == "" {
		entry, err = store.CreateCoreEntry(ctx, title, check.Descriptor.Core.ID, id)
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

func (s *Service) launchCoreEntry(ctx context.Context, gameID string) (protocol.CachedLaunchResponse, error) {
	status, err := s.loadCore(ctx, func(ctx context.Context) (coreLoadSource, error) {
		entry, err := s.CoreEntry(ctx, gameID)
		if err != nil {
			return coreLoadSource{}, err
		}
		inspection, data, err := s.readInstalledCore(ctx, entry.PackageID)
		if err != nil {
			return coreLoadSource{}, err
		}
		if inspection.Descriptor.Core.ID != entry.CoreID {
			return coreLoadSource{}, canonicalError(protocol.CodeInvalidArchive, nil)
		}
		return coreLoadSource{size: int64(len(data)), body: bytes.NewReader(data), entry: &entry}, nil
	})
	return protocol.CachedLaunchResponse{Status: status}, err
}

type coreLoadSource struct {
	size  int64
	body  io.Reader
	entry *catalog.CoreEntry
}

// stopRejectedCore owns recovery when package activation cannot be accepted,
// including an earlier host executor whose cleanup failed after target success.
// Caller holds lifecycle admission.
func (s *Service) stopRejectedCore(ctx context.Context) (protocol.Status, error) {
	if s.discoveryEnabled() {
		if _, err := s.refreshTargetConnection(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil || !validRecoveredDevelopmentStatus(status) || status.CorePackage != nil {
		status, err = client.Stop(ctx)
		if err != nil || !validRecoveredDevelopmentStatus(status) || status.CorePackage != nil {
			status, err = client.Status(ctx)
		}
	}
	s.executionMu.Lock()
	rejection := s.packageRejection
	s.executionMu.Unlock()
	if err != nil || !validRecoveredDevelopmentStatus(status) || status.CorePackage != nil {
		status.LastError = rejection
		return status, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package recovery is not confirmed", Phase: "recovery"}
	}
	if err := s.stopHostOnlyIfActive(ctx); err != nil {
		status.LastError = rejection
		return status, &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package rejection", Phase: "recovery"}
	}
	s.executionMu.Lock()
	s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
	s.activePackageID, s.activePackageGeneration = "", 0
	s.packageRejection = nil
	s.selectedTargetReconciled = true
	s.selectedTargetRepairAllowed = false
	s.executionMu.Unlock()
	return status, nil
}
