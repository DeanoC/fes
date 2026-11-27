package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/misteross/expansion"
)

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
	// The last play DisplaySink changes only when the whole launch
	// succeeded. A firmware or media failure stops the core and leaves
	// the remembered sink in place. This runs after the media cleanup
	// below has joined its errors.
	defer func() {
		if resultErr == nil {
			s.notePlayDisplaySink()
		}
	}()
	var entry catalog.CoreEntry
	var media *coreEntryMedia
	var hardDisk *hardDiskSource
	var floppy *floppySource
	var imageSHA string
	defer func() {
		resultErr = errors.Join(resultErr, hardDisk.Close(), floppy.Close())
		if media != nil {
			resultErr = errors.Join(resultErr, media.Close())
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
		if err := libraryVideoAdmission(inspection); err != nil {
			return coreLoadSource{}, err
		}
		if err := validateROMMediaContract(inspection.Descriptor); err != nil {
			return coreLoadSource{}, err
		}
		if (protocol.DeclaresAtaDisk(inspection.Descriptor) || protocol.DeclaresPc386Machine(inspection.Descriptor)) &&
			(inspection.Descriptor.ROM != nil || inspection.Descriptor.Format == 4) {
			// Only the pc386 launch below sends the game and stages the disk;
			// a ROM-linked launch would send neither.
			return coreLoadSource{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
				Message: "a package with a hard disk or a pc386 machine record cannot link a ROM"}
		}
		if protocol.RequiresCoreMedia(inspection.Descriptor) && !(inspection.Descriptor.ROM != nil && inspection.Descriptor.ROM.Role == "cartridge") && entry.MediaID == "" {
			return coreLoadSource{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
				Message: "application requires selected library media before launch"}
		}
		if inspection.Descriptor.Format == 5 && entry.FirmwareRequired {
			return coreLoadSource{}, protocol.FirmwareAdmissionError("static packages contain sealed firmware; a later firmware upload is unsupported")
		}
		if entry.FirmwareRequired && inspection.Descriptor.Format != 4 && inspection.Descriptor.ROM == nil {
			return coreLoadSource{}, protocol.FirmwareAdmissionError("this package requires the retired household firmware slot")
		}
		if inspection.Descriptor.ROM != nil || inspection.Descriptor.Format == 4 {
			// Cartridge bytes are consumed by linking, never delivered again as media.
			// A PC/XT with uploaded firmware starts from its own disk or the
			// bundled FreeDOS disk, as a format-2 PC/XT does below.
			disk := entry
			if inspection.Descriptor.Format == 3 {
				disk = s.pcxtStartDisk(inspection.Descriptor, entry)
			}
			if inspection.Descriptor.Format != 4 && inspection.Descriptor.ROM.Role != "cartridge" {
				media, err = s.readCoreEntryMedia(ctx, inspection.Descriptor, disk.MediaRole, disk.MediaID)
				if err != nil {
					return coreLoadSource{}, err
				}
			}
			var initial *corepackage.InitialMedia
			if media != nil && (initialSTDisk(inspection.Descriptor, media) || initialPcxtDisk(inspection.Descriptor, media)) {
				if initialPcxtDisk(inspection.Descriptor, media) {
					initial, err = snapshotInitialPcxtDisk(disk, media)
				} else {
					initial, err = snapshotInitialSTDisk(entry, media)
				}
				closeErr := media.Close()
				media = nil
				if err = errors.Join(err, closeErr); err != nil {
					return coreLoadSource{}, err
				}
			}
			source, err := s.romLaunchSource(ctx, entry, inspection.Descriptor, data, initial)
			if err != nil {
				return coreLoadSource{}, err
			}
			expectedCartID := ""
			if snap.romSources != nil {
				expectedCartID = snap.romSources.cartID
				// The saved pair was revalidated before loading; a swap changes
				// only the cartridge expected for this one load.
				if replacementID, swapped := CartridgeFromContext(ctx, gameID); swapped {
					expectedCartID = replacementID
				}
			}
			if snap.romSources != nil && (entry.PackageID != snap.romSources.packageID ||
				source.biosMediaID != snap.romSources.biosID || source.romMediaID != expectedCartID) {
				return coreLoadSource{}, snapshotMismatch(launchSnapshotROMSourcesChanged)
			}
			return source, nil
		}
		if protocol.DeclaresAtaDisk(inspection.Descriptor) && !protocol.DeclaresPc386Machine(inspection.Descriptor) && entry.MediaID != "" {
			// The runtime binds a disk delta to the game of a per-game
			// record, and takes a game only from a package that has one.
			return coreLoadSource{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
				Message: "this package has no per-game machine record, so a hard-disk image cannot be bound to it"}
		}
		if protocol.DeclaresPc386Machine(inspection.Descriptor) {
			// The game keys the machine record and disk delta; the disk is
			// staged on the kit by digest and written by the runtime before
			// release, so nothing is inserted after the core starts.
			// A floppy entry boots its disk 1 with the hard disk empty.
			if entry.MediaRole == protocol.FloppyRole && protocol.DeclaresPcFloppy(inspection.Descriptor) {
				floppy, err = s.readLaunchFloppy(ctx, entry)
			} else if protocol.DeclaresAtaDisk(inspection.Descriptor) {
				hardDisk, err = s.readHardDisk(ctx, entry)
			} else if entry.MediaID != "" {
				err = protocol.HardDiskRequestError()
			}
			if err != nil {
				return coreLoadSource{}, err
			}
			machine := &protocol.LibraryMachineBinding{GameID: entry.GameID}
			if hardDisk != nil {
				disk := hardDisk.disk
				machine.Disk = &disk
			}
			if floppy != nil {
				machine.Floppies = []protocol.StagedFloppy{floppy.floppy}
			}
			return coreLoadSource{activationBudget: hardDiskActivationBudget, size: int64(len(data)), body: bytes.NewReader(data),
				entry: &entry, machine: machine, hardDisk: hardDisk, floppy: floppy}, nil
		}
		disk := s.pcxtStartDisk(inspection.Descriptor, entry)
		media, err = s.readCoreEntryMedia(ctx, inspection.Descriptor, disk.MediaRole, disk.MediaID)
		if err != nil {
			return coreLoadSource{}, err
		}
		if initialPcxtDisk(inspection.Descriptor, media) {
			initial, diskErr := snapshotInitialPcxtDisk(disk, media)
			closeErr := media.Close()
			media = nil
			if err = errors.Join(diskErr, closeErr); err != nil {
				return coreLoadSource{}, err
			}
			if transport, err := corepackage.WritePcxtInitialInput(corepackage.PcxtInitialInput{Package: data, Media: *initial}); err != nil {
				return coreLoadSource{}, canonicalError(protocol.CodeInvalidArchive, nil)
			} else {
				var machine *protocol.LibraryMachineBinding
				if protocol.DeclaresPcxtMachine(inspection.Descriptor) {
					machine = &protocol.LibraryMachineBinding{GameID: entry.GameID}
				}
				return coreLoadSource{activationBudget: 300 * time.Second, size: int64(len(transport)), body: bytes.NewReader(transport), entry: &entry, initialMedia: initial, machine: machine}, nil
			}
		}
		if inspection.Descriptor.Format == 5 {
			return s.staticCoreLaunchSource(ctx, entry, inspection, data)
		}
		parts, err := s.composeVideoEntry(ctx, entry, inspection, data)
		if err != nil {
			return coreLoadSource{}, err
		}
		if parts != nil {
			transport, err := parts.Write(ctx)
			if err != nil {
				return coreLoadSource{}, videoPartUnavailable()
			}
			return coreLoadSource{size: int64(len(transport)), body: bytes.NewReader(transport), entry: &entry, partsComposition: &parts.Composition}, nil
		}
		slots, err := s.composeSlotEntry(ctx, entry, inspection, data)
		if err != nil {
			return coreLoadSource{}, err
		}
		if slots != nil {
			transport, err := slots.Write()
			if err != nil {
				return coreLoadSource{}, slotExpansionUnavailable()
			}
			return coreLoadSource{size: int64(len(transport)), body: bytes.NewReader(transport), entry: &entry, slotComposition: &slots.Composition}, nil
		}
		bundle, err := s.composeCoreEntry(ctx, entry, data)
		if err != nil {
			return coreLoadSource{}, err
		}
		initialized, sha, err := s.applyZX81MachineROM(ctx, inspection.Descriptor, data, bundle)
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
		var machine *protocol.LibraryMachineBinding
		if protocol.DeclaresPcxtMachine(inspection.Descriptor) {
			machine = &protocol.LibraryMachineBinding{GameID: entry.GameID}
		}
		return coreLoadSource{size: int64(len(data)), body: bytes.NewReader(data), entry: &entry, machine: machine}, nil
	})
	status = retainImageSHA(status, imageSHA)
	response := protocol.CachedLaunchResponse{Status: status}
	if err != nil {
		return response, err
	}
	// The core load recorded execution. Keep a placement claim through
	// later media delivery. A failure before this return releases it.
	s.settlePlacementClaim(snap)
	if media == nil {
		return response, nil
	}
	if status.CorePackage == nil {
		return response, canonicalError(protocol.CodeInternal, nil)
	}
	if media.unit != nil {
		// Other removable-media contracts insert after Start. Writable format-3
		// ST disks have already traveled with ROM activation before CPU release.
		dev := s.libraryDevelopmentMediaBinding(*status.CorePackage)
		unitBinding := protocol.MediaUnitBinding{PackageID: dev.PackageID, Generation: dev.Generation, Unit: *media.unit, Target: dev.Target, TargetID: dev.TargetID}
		mediaBudget := 150 * time.Second
		if u, ok := protocol.MediaUnit(status.CorePackage, *media.unit); ok && u.Interface == protocol.AtariStFloppyInterface() {
			mediaBudget = 300 * time.Second
		}
		mediaCtx, mediaCancel := context.WithTimeout(parent, max(s.uploadTimeout, mediaBudget))
		defer mediaCancel()
		var unitStatus protocol.Status
		var unitErr error
		if u, ok := protocol.MediaUnit(status.CorePackage, *media.unit); ok && (u.Interface == protocol.AtariStFloppyInterface() || (u.Interface == protocol.PcxtFloppyInterface() && protocol.MediaWriteCapable(status.CorePackage))) {
			library := protocol.LibraryMediaBinding{MediaUnitBinding: unitBinding, GameID: entry.GameID, BaseMediaID: entry.MediaID}
			unitStatus, unitErr = s.insertBoundMediaUnitLocked(mediaCtx, media.size, media.ReadCloser, unitBinding, &library)
		} else {
			unitStatus, unitErr = s.insertMediaUnitLocked(mediaCtx, media.size, media.ReadCloser, unitBinding)
		}
		unitErr = errors.Join(unitErr, media.Close())
		media = nil
		if unitErr != nil {
			return s.recoverLibrarySlot(parent, unitStatus, unitErr)
		}
		unitStatus = s.retainKitSessionIdentity(unitBinding.Target, s.retainMediaUnitSessionIdentity(unitStatus, unitBinding))
		return protocol.CachedLaunchResponse{Status: retainImageSHA(unitStatus, imageSHA)}, nil
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
	return protocol.CachedLaunchResponse{Status: retainImageSHA(s.retainKitSessionIdentity(binding.Target, mediaStatus), imageSHA)}, nil
}

func (s *Service) retainKitSessionIdentity(target string, status protocol.Status) protocol.Status {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	play, ok := s.plays[target]
	if !ok || status.CorePackage == nil || play.packageID != status.CorePackage.PackageID || play.packageGeneration != status.CorePackage.Generation {
		return status
	}
	if status.GameID == nil && play.gameID != "" {
		status.GameID = stringPtr(play.gameID)
	}
	if status.System == nil && play.system != "" {
		status.System = systemPtr(play.system)
	}
	if status.LastError == nil {
		status.LastError = play.packageRejection
	}
	return status
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
	stopStatus, stopErr := s.stopLocked(cleanupCtx, cleanupParent, s.uploadTimeout, "")
	cleanupCancel()
	if stopErr != nil {
		recoveryErr := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "library core media cleanup is not confirmed", Phase: "recovery"}
		s.executionMu.Lock()
		target, play := s.selectedKitPlayLocked()
		play.packageRejection = recoveryErr
		if play.execution == "" {
			play.execution = ExecutionFPGADevelopment
		}
		s.plays[target] = play
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
	target, _ := s.selectedKitPlayLocked()
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
	// activationBudget is a minimum target-load budget chosen from validated sources.
	activationBudget time.Duration
	initialMedia     *corepackage.InitialMedia
	biosID           string
	biosMediaID      string
	biosSourceSize   int64
	romID            string
	romMediaID       string
	romMapSHA256     string
	romSourceSize    int64
	expansionID      string
	composition      *expansion.Composition
	partsComposition *expansion.PartsComposition
	// slotComposition is the v2 tuple the host linked for a multi-socket
	// shell; the target's independent composition must equal it.
	slotComposition   *expansion.SlotComposition
	staticComposition *expansion.StaticComposition
	size              int64
	body              io.Reader
	entry             *catalog.CoreEntry
	// machine binds a pc386 library launch to its game and staged disk;
	// hardDisk uploads that disk when the kit does not hold it yet.
	machine  *protocol.LibraryMachineBinding
	hardDisk *hardDiskSource
	// floppy uploads a floppy entry's disk 1 for its drive.
	floppy *floppySource
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
	return s.stopRejectedKitCoreWithAdmission(ctx, explicitStop, "")
}

// stopRejectedKitCoreWithAdmission resolves only the named kit's retained
// rejection.
func (s *Service) stopRejectedKitCore(ctx context.Context, target string) (protocol.Status, error) {
	return s.stopRejectedKitCoreWithAdmission(ctx, false, target)
}

func (s *Service) stopRejectedKitCoreWithAdmission(ctx context.Context, explicitStop bool, target string) (protocol.Status, error) {
	if target == "" {
		s.executionMu.Lock()
		target, _ = s.selectedKitPlayLocked()
		s.executionMu.Unlock()
	}
	scopedOtherKit := target != s.SessionTargetName()
	if scopedOtherKit {
		ctx = WithSessionTarget(ctx, target)
	}
	s.targetMu.RLock()
	_, protocolClient := s.targetClients[target].(*targetclient.Client)
	s.targetMu.RUnlock()
	if protocolClient {
		admit := s.refreshTargetAdmission
		if explicitStop && !scopedOtherKit {
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
	client := s.targetClients[target]
	if client == nil {
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
	play := s.plays[target]
	rejection := play.packageRejection
	s.executionMu.Unlock()
	if err != nil || !validRecoveredDevelopmentStatus(status) || status.CorePackage != nil {
		status.LastError = rejection
		return status, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package recovery is not confirmed", Phase: "recovery"}
	}
	s.executionMu.Lock()
	delete(s.plays, target)
	if target == s.selectedTarget {
		s.selectedTargetReconciled = true
		s.selectedTargetRepairAllowed = false
	}
	s.executionMu.Unlock()
	return status, nil
}
