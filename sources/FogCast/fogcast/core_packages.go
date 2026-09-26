package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"regexp"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
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
	CoreFirmware(context.Context, string) (catalog.CoreFirmware, error)
	SelectCoreFirmware(context.Context, string, string) (catalog.CoreFirmware, error)
	HouseholdFirmwareFilled(context.Context) (bool, error)
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
	if s.protocolAdmissionEnabled() {
		if _, err = s.refreshTargetAdmission(ctx); err != nil {
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
	result, err := inspectInstalledPackageForClient(ctx, value, data, inspector)
	if err != nil {
		return CoreCompatibility{}, err
	}
	state := "incompatible"
	if result.Compatible {
		state = "compatible"
	}
	return CoreCompatibility{CoreInspection: result, Target: s.selectedTarget, TargetID: health.TargetID, State: state}, nil
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
	return s.writeCoreEntry(parent, "", title, "", id, "", "", false)
}
func (s *Service) CreateCoreMediaEntry(parent context.Context, title, id, role, mediaID string) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id, role, mediaID, false)
}
func (s *Service) CreateCoreEntryWithFirmware(parent context.Context, title, id, role, mediaID string, firmwareRequired bool) (catalog.CoreEntry, error) {
	return s.writeCoreEntry(parent, "", title, "", id, role, mediaID, firmwareRequired)
}
func (s *Service) SelectCoreEntry(parent context.Context, gameID, expected, id string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if !packageIDPattern.MatchString(expected) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.writeCoreEntry(parent, gameID, "", expected, id, "", "", false)
}
func (s *Service) writeCoreEntry(parent context.Context, gameID, title, expected, id, role, mediaID string, firmwareRequired bool) (catalog.CoreEntry, error) {
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
		role, mediaID = entry.MediaRole, entry.MediaID
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
	if err := s.validateCoreEntryMedia(ctx, check.Descriptor, role, mediaID); err != nil {
		return catalog.CoreEntry{}, err
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
		if firmwareRequired {
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

func (s *Service) launchCoreEntry(parent context.Context, gameID string, snap launchSnapshot) (result protocol.CachedLaunchResponse, resultErr error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.CachedLaunchResponse{}, corePackageRequestFailure(err)
	}
	defer release()
	if err := s.revalidateLaunchSnapshot(snap); err != nil {
		if errors.Is(err, ErrLaunchSnapshot) || errors.Is(err, meshcontent.ErrUnboundNode) {
			return protocol.CachedLaunchResponse{}, err
		}
		return protocol.CachedLaunchResponse{}, corePackageRequestFailure(err)
	}
	defer s.clearUnstartedSessionTarget()
	var entry catalog.CoreEntry
	var media *coreEntryMedia
	var firmware *coreEntryMedia
	var imageSHA string
	defer func() {
		if media != nil {
			resultErr = errors.Join(resultErr, media.Close())
		}
		if firmware != nil {
			resultErr = errors.Join(resultErr, firmware.Close())
		}
	}()
	status, err := s.loadCoreLocked(ctx, parent, func(ctx context.Context) (coreLoadSource, error) {
		selected, err := s.CoreEntry(ctx, gameID)
		if err != nil {
			return coreLoadSource{}, err
		}
		entry = selected
		inspection, data, err := s.readInstalledCore(ctx, entry.PackageID)
		if err != nil {
			return coreLoadSource{}, err
		}
		if inspection.Descriptor.Core.ID != entry.CoreID {
			return coreLoadSource{}, canonicalError(protocol.CodeInvalidArchive, nil)
		}
		if err := validateROMMediaContract(inspection.Descriptor); err != nil {
			return coreLoadSource{}, err
		}
		if protocol.RequiresCoreMedia(inspection.Descriptor) && !(inspection.Descriptor.ROM != nil && inspection.Descriptor.ROM.Role == "cartridge") && entry.MediaID == "" {
			return coreLoadSource{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
				Message: "application requires selected library media before launch"}
		}
		if entry.FirmwareRequired && inspection.Descriptor.Format != 4 {
			if !protocol.DeclaresFirmwareSlot(inspection.Descriptor) {
				return coreLoadSource{}, protocol.FirmwareAdmissionError("required firmware cannot be bound; package does not declare fes.firmware.blob 1.0")
			}
			store, ok := s.catalog.(coreFirmwareCatalog)
			if !ok {
				return coreLoadSource{}, protocol.FirmwareAdmissionError("")
			}
			slot, err := store.CoreFirmware(ctx, protocol.FirmwareRole)
			if err != nil {
				return coreLoadSource{}, mapCoreFirmwareError(err)
			}
			if slot.MediaID == "" {
				return coreLoadSource{}, protocol.FirmwareAdmissionError("")
			}
			firmware, err = s.readCoreFirmware(ctx, inspection.Descriptor, slot.MediaID)
			if err != nil {
				return coreLoadSource{}, err
			}
		}
		if inspection.Descriptor.ROM != nil || inspection.Descriptor.Format == 4 {
			// Cartridge bytes are consumed by linking, never delivered again as media.
			if inspection.Descriptor.Format != 4 && inspection.Descriptor.ROM.Role != "cartridge" {
				media, err = s.readCoreEntryMedia(ctx, inspection.Descriptor, entry.MediaRole, entry.MediaID)
				if err != nil {
					return coreLoadSource{}, err
				}
			}
			source, err := s.romLaunchSource(ctx, entry, inspection.Descriptor, data)
			if err != nil {
				return coreLoadSource{}, err
			}
			if snap.romSources != nil && (entry.PackageID != snap.romSources.packageID ||
				source.biosMediaID != snap.romSources.biosID || source.romMediaID != snap.romSources.cartID) {
				return coreLoadSource{}, snapshotMismatch(launchSnapshotROMSourcesChanged)
			}
			return source, nil
		}
		media, err = s.readCoreEntryMedia(ctx, inspection.Descriptor, entry.MediaRole, entry.MediaID)
		if err != nil {
			return coreLoadSource{}, err
		}
		bundle, err := s.composeCoreEntry(ctx, entry, data)
		if err != nil {
			return coreLoadSource{}, err
		}
		initialized, sha, err := s.applyZX81MachineROM(ctx, inspection.Descriptor.Core.ID, data, bundle)
		if err != nil {
			return coreLoadSource{}, err
		}
		if initialized != nil {
			imageSHA = sha
			return initializedLaunchSource(entry, initialized, bundle), nil
		}
		if bundle != nil {
			var transport bytes.Buffer
			if err = bundle.Write(&transport); err != nil {
				return coreLoadSource{}, expansionError(err)
			}
			return coreLoadSource{size: int64(transport.Len()), body: bytes.NewReader(transport.Bytes()), entry: &entry, composition: &bundle.Composition}, nil
		}
		return coreLoadSource{size: int64(len(data)), body: bytes.NewReader(data), entry: &entry}, nil
	})
	status = retainImageSHA(status, imageSHA)
	response := protocol.CachedLaunchResponse{Status: status}
	if err != nil {
		return response, err
	}
	// The core load recorded execution. Keep a placement claim through
	// later media delivery. A failure before this return releases it.
	s.settlePlacementClaim(snap)
	if firmware == nil && media == nil {
		return response, nil
	}
	if status.CorePackage == nil {
		return response, canonicalError(protocol.CodeInternal, nil)
	}
	if firmware != nil {
		fwBinding := s.libraryDevelopmentMediaBinding(*status.CorePackage)
		fwBinding.Role = protocol.FirmwareRole
		fwStatus, fwErr := s.loadDevelopmentMediaReaderLocked(ctx, firmware.size, firmware.ReadCloser, fwBinding)
		fwErr = errors.Join(fwErr, firmware.Close())
		firmware = nil
		if fwErr != nil {
			return s.recoverLibrarySlot(parent, fwStatus, fwErr)
		}
		status = retainImageSHA(fwStatus, imageSHA)
		response.Status = status
	}
	if media == nil {
		return response, nil
	}
	binding := s.libraryDevelopmentMediaBinding(*status.CorePackage)
	binding.Stream = media.stream
	mediaCtx := ctx
	if media.stream {
		// Staging/package activation use the normal upload deadline. Stream
		// delivery additionally covers the daemon's 120s owned media worker.
		var mediaCancel context.CancelFunc
		mediaCtx, mediaCancel = context.WithTimeout(parent, max(s.uploadTimeout, 150*time.Second))
		defer mediaCancel()
	}
	mediaStatus, mediaErr := s.loadDevelopmentMediaReaderLocked(mediaCtx, media.size, media.ReadCloser, binding)
	mediaErr = errors.Join(mediaErr, media.Close())
	media = nil
	if mediaErr != nil {
		return s.recoverLibrarySlot(parent, mediaStatus, mediaErr)
	}
	return protocol.CachedLaunchResponse{Status: retainImageSHA(mediaStatus, imageSHA)}, nil
}

func initializedLaunchSource(entry catalog.CoreEntry, body []byte, bundle *corepackage.CompositionBundle) coreLoadSource {
	source := coreLoadSource{size: int64(len(body)), body: bytes.NewReader(body), entry: &entry}
	if bundle != nil {
		source.composition = &bundle.Composition
	}
	return source
}

func retainImageSHA(status protocol.Status, imageSHA string) protocol.Status {
	if imageSHA != "" && status.CorePackage != nil {
		status.CorePackage.ImageSHA256 = imageSHA
	}
	return status
}

func (s *Service) recoverLibrarySlot(parent context.Context, mediaStatus protocol.Status, mediaErr error) (protocol.CachedLaunchResponse, error) {
	cleanupParent := context.WithoutCancel(parent)
	cleanupCtx, cleanupCancel := serviceTimeout(cleanupParent, s.uploadTimeout)
	stopStatus, stopErr := s.stopLocked(cleanupCtx, cleanupParent, s.uploadTimeout)
	cleanupCancel()
	if stopErr != nil {
		recoveryErr := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "library core media cleanup is not confirmed", Phase: "recovery"}
		s.executionMu.Lock()
		s.packageRejection = recoveryErr
		s.activeGameID, s.activeSystem = "", ""
		s.activePackageID, s.activePackageGeneration = "", 0
		s.executionMu.Unlock()
		if stopStatus.State != "" {
			mediaStatus = stopStatus
		}
		mediaStatus.LastError = recoveryErr
		mediaStatus.GameID, mediaStatus.System = nil, nil
		return protocol.CachedLaunchResponse{Status: mediaStatus}, errors.Join(recoveryErr, mediaErr, stopErr)
	}
	return protocol.CachedLaunchResponse{Status: stopStatus}, postMutationCoreMediaError(mediaErr)
}

func postMutationCoreMediaError(err error) error {
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	classified := *apiErr
	classified.Phase = "recovery"
	return &classified
}

func (s *Service) libraryDevelopmentMediaBinding(packageStatus protocol.CorePackageStatus) protocol.DevelopmentMediaBinding {
	s.executionMu.Lock()
	target := s.activeTarget
	if target == "" {
		target = s.selectedTarget
	}
	s.executionMu.Unlock()
	s.targetMu.RLock()
	targetID := targetByName(s.targets, target).TargetID
	s.targetMu.RUnlock()
	return protocol.DevelopmentMediaBinding{
		PackageID:  packageStatus.PackageID,
		Generation: packageStatus.Generation,
		Target:     target,
		TargetID:   targetID,
	}
}

type coreLoadSource struct {
	biosID         string
	biosMediaID    string
	biosSourceSize int64
	romID          string
	romMediaID     string
	romMapSHA256   string
	romSourceSize  int64
	expansionID    string
	composition    *expansion.Composition
	size           int64
	body           io.Reader
	entry          *catalog.CoreEntry
}

// stopRejectedCore owns recovery when package activation cannot be accepted,
// including an earlier host executor whose cleanup failed after target success.
// Caller holds lifecycle admission.
func (s *Service) stopRejectedCore(ctx context.Context) (protocol.Status, error) {
	return s.stopRejectedCoreWithAdmission(ctx, false)
}

// Activation cleanup retains ordinary admission; only explicit Stop ignores the
// observation backoff timer. All target validation and recovery steps are shared.
func (s *Service) stopRejectedCoreWithAdmission(ctx context.Context, explicitStop bool) (protocol.Status, error) {
	if s.protocolAdmissionEnabled() {
		admit := s.refreshTargetAdmission
		if explicitStop {
			admit = s.refreshStopAdmission
		}
		if _, err := admit(ctx); err != nil {
			if explicitStop {
				return protocol.Status{}, stopAdmissionError(err)
			}
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
