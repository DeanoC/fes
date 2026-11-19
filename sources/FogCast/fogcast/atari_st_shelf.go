package fogcast

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

const atariStCoreID = "fes.atari-st"

// What the kit holds for a shelf disk beside its imported image.
const (
	ShelfKeptNone    = "none"
	ShelfKeptChanges = "changes"
	ShelfKeptDamaged = "damaged"
	ShelfKeptUnknown = "unknown"
)

// AtariStShelfDisk is one library disk on the Atari ST shelf. Made is true
// for a disk a person made here, a new blank disk or a copy. KeptRevision
// names what the kit has saved for the disk, which a later change must
// still find. A kit saves a disk at Stop whether or not it was written to,
// so a disk can have a KeptRevision and Kept "none".
type AtariStShelfDisk struct {
	GameID       string `json:"game_id"`
	Title        string `json:"title"`
	PackageID    string `json:"package_id"`
	MediaID      string `json:"media_id"`
	Size         int64  `json:"size"`
	Made         bool   `json:"made"`
	Kept         string `json:"kept"`
	KeptRevision string `json:"kept_revision,omitempty"`
	InDrive      bool   `json:"in_drive"`
}

// AtariStShelf is the shelf as one kit sees it. Saved changes live on a
// kit, so KeptKnown is false when that kit could not be asked; the shelf
// then lists only the disks made here, each with Kept "unknown".
type AtariStShelf struct {
	Target    string             `json:"target"`
	TargetID  string             `json:"target_id"`
	KeptKnown bool               `json:"kept_known"`
	Disks     []AtariStShelfDisk `json:"disks"`
}

// AtariStShelfNew asks for a blank disk on the shelf. Zero geometry is the
// 720 KiB disk. Title is the disk's name in the library.
type AtariStShelfNew struct {
	Title     string
	PackageID string
	Tracks    int
	Heads     int
	Sectors   int
}

type mediaRecordClient interface {
	InspectMediaRecord(context.Context, protocol.MediaRecordIdentity) (protocol.MediaRecord, error)
	InspectMediaRecords(context.Context, []protocol.MediaRecordIdentity) ([]protocol.MediaRecord, error)
	ExportMediaRecord(context.Context, protocol.MediaRecordDiscard) ([]byte, error)
	DiscardMediaRecord(context.Context, protocol.MediaRecordDiscard) (protocol.MediaRecord, error)
}

type shelfDiskCatalog interface {
	MarkShelfDisk(context.Context, string) error
	ShelfDisks(context.Context, string) (map[string]struct{}, error)
}

type shelfKit struct {
	client   serviceClient
	records  mediaRecordClient
	target   string
	targetID string
}

var (
	errShelfDisk       = &protocol.APIError{Code: protocol.CodeBadRequest, Message: "that is not an Atari ST shelf disk", Phase: "request"}
	errShelfName       = &protocol.APIError{Code: protocol.CodeBadRequest, Message: "a game or disk already has that name", Phase: "request"}
	errShelfKitRunning = &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the running computer before discarding saved changes", Phase: "admission"}
	errShelfInDrive    = &protocol.APIError{Code: protocol.CodeBusy, Message: "the disk is in the drive; stop the computer first", Phase: "admission"}
	errShelfDamaged    = &protocol.APIError{Code: protocol.CodeIncompatibleData, Message: "the disk's saved changes are damaged and cannot be copied", Phase: "core_data"}
	errShelfChanged    = &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "the disk's saved changes are not the ones shown; refresh and retry", Phase: "core_data"}
	errShelfNotMade    = &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "only a disk made here can be renamed or deleted", Phase: "request"}
	errShelfNoChanges  = &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "the disk has no saved changes", Phase: "core_data"}
)

// shelfKit resolves the kit whose saved changes this request is about: the
// session's target, or the selected one.
func (s *Service) shelfKit(ctx context.Context) (shelfKit, error) {
	if SessionTargetFromContext(ctx) != "" || s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return shelfKit{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	client, ok := s.clientForSessionContextLocked(ctx)
	target := SessionTargetFromContext(ctx)
	if target == "" {
		target = s.selectedTarget
	}
	s.targetMu.RUnlock()
	if !ok {
		return shelfKit{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	records, ok := client.(mediaRecordClient)
	if !ok {
		return shelfKit{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	health, err := client.Health(ctx)
	if err != nil {
		return shelfKit{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return shelfKit{client: client, records: records, target: target, targetID: health.TargetID}, nil
}

func shelfIdentity(entry catalog.CoreEntry) protocol.MediaRecordIdentity {
	return protocol.MediaRecordIdentity{CoreID: entry.CoreID, GameID: entry.GameID, BaseMediaID: entry.MediaID, Unit: protocol.AtariStFloppyUnit}
}

func shelfKept(record protocol.MediaRecord) (string, string) {
	switch record.State {
	case protocol.MediaRecordPresent:
		if record.Changed {
			return ShelfKeptChanges, record.Revision
		}
		// Saved at Stop without a write: the image is still the original.
		return ShelfKeptNone, record.Revision
	case protocol.MediaRecordCorrupt:
		return ShelfKeptDamaged, record.Revision
	}
	return ShelfKeptNone, ""
}

func isAtariStDisk(entry catalog.CoreEntry) bool {
	return entry.CoreID == atariStCoreID && entry.MediaRole == protocol.DiskRole && entry.MediaID != ""
}

// hostPlaysGame reports a game this host has launched or is launching.
func (s *Service) hostPlaysGame(gameID string) bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activeGameID == gameID {
		return true
	}
	for _, play := range s.plays {
		if play.gameID == gameID {
			return true
		}
	}
	return false
}

// statusHoldsDisk reports a disk the kit has in a drive: the game it
// launched, or a library disk inserted into a running session, which binds
// the drive to that disk's game whatever the session's game is.
func statusHoldsDisk(status protocol.Status, gameID string) bool {
	if status.GameID != nil && *status.GameID == gameID {
		return true
	}
	if status.CorePackage == nil {
		return false
	}
	for _, unit := range status.CorePackage.MediaUnits {
		if unit.Persistence != nil && unit.Persistence.GameID == gameID {
			return true
		}
	}
	return false
}

// AtariStShelf lists the disks made here and the game disks with saved
// changes. It changes nothing. The kit is asked under lifecycle admission,
// like every other target read; when a launch or stop holds admission
// beyond the request timeout, the shelf answers with saved changes unknown.
func (s *Service) AtariStShelf(parent context.Context) (AtariStShelf, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	wait, stopWaiting := serviceTimeout(ctx, max(s.requestTimeout, time.Second))
	release, admitErr := s.acquireLifecycle(wait)
	stopWaiting()
	if admitErr == nil {
		defer release()
	}
	marks, ok := s.catalog.(shelfDiskCatalog)
	if !ok {
		return AtariStShelf{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	all, err := s.CoreEntries(ctx)
	if err != nil {
		return AtariStShelf{}, err
	}
	made, err := marks.ShelfDisks(ctx, atariStCoreID)
	if err != nil {
		return AtariStShelf{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	var entries []catalog.CoreEntry
	for _, entry := range all {
		if isAtariStDisk(entry) {
			entries = append(entries, entry)
		}
	}
	shelf := AtariStShelf{Disks: []AtariStShelfDisk{}}
	records := map[string]protocol.MediaRecord{}
	var running protocol.Status
	if kit, err := s.shelfKit(ctx); admitErr == nil && err == nil {
		shelf.Target, shelf.TargetID, shelf.KeptKnown = kit.target, kit.targetID, true
		var ids []protocol.MediaRecordIdentity
		for _, entry := range entries {
			if id := shelfIdentity(entry); id.Valid() {
				ids = append(ids, id)
			}
		}
		for start := 0; start < len(ids) && shelf.KeptKnown; start += protocol.MaxMediaRecordBatch {
			batch := ids[start:min(len(ids), start+protocol.MaxMediaRecordBatch)]
			got, err := kit.records.InspectMediaRecords(ctx, batch)
			if err != nil || len(got) != len(batch) {
				shelf.KeptKnown = false
				break
			}
			for i, record := range got {
				records[batch[i].GameID] = record
			}
		}
		// A status the kit cannot give leaves only this host's own plays.
		running, _ = kit.client.Status(ctx)
	}
	for _, entry := range entries {
		_, isMade := made[entry.GameID]
		disk := AtariStShelfDisk{
			GameID: entry.GameID, Title: entry.Title, PackageID: entry.PackageID, MediaID: entry.MediaID,
			Made: isMade, Kept: ShelfKeptUnknown,
			InDrive: s.hostPlaysGame(entry.GameID) || statusHoldsDisk(running, entry.GameID),
		}
		if shelf.KeptKnown {
			disk.Kept, disk.KeptRevision = shelfKept(records[entry.GameID])
		}
		if !isMade && disk.Kept != ShelfKeptChanges && disk.Kept != ShelfKeptDamaged {
			continue
		}
		media, err := s.CoreMedia(ctx, entry.MediaID)
		if err != nil {
			return AtariStShelf{}, err
		}
		disk.Size = media.Size
		shelf.Disks = append(shelf.Disks, disk)
	}
	sort.SliceStable(shelf.Disks, func(i, j int) bool {
		a, b := strings.ToLower(shelf.Disks[i].Title), strings.ToLower(shelf.Disks[j].Title)
		if a != b {
			return a < b
		}
		return shelf.Disks[i].GameID < shelf.Disks[j].GameID
	})
	return shelf, nil
}

// shelfVolumeLabel turns a disk's name into a GEMDOS volume label: the
// characters GEMDOS can store, at most eleven of them.
func shelfVolumeLabel(title string) string {
	var label strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(title)) {
		if label.Len() == 11 {
			break
		}
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '-' {
			label.WriteRune(r)
		}
	}
	if name := strings.TrimSpace(label.String()); name != "" {
		return name
	}
	return "DISK"
}

func atariStBlankSizeError(descriptor corepackage.Descriptor, size int64) error {
	if err := corepackage.ValidateWritableAtariStLibraryDiskPackage(descriptor); err != nil {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	for _, declared := range protocol.DeclaredCoreMediaCapabilities(descriptor) {
		if declared.Role == protocol.DiskRole && declared.Format == "atari-st-floppy" {
			if size < declared.MinBytes || size > declared.MaxBytes || !protocol.AdmitAtariStFloppySize(size) {
				return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk geometry is outside this package", Phase: "request"}
			}
			return nil
		}
	}
	return canonicalError(protocol.CodeUnsupportedOperation, nil)
}

// shelfNameFree refuses a name another title of this core already has, so
// the caller gets a plain reason instead of a selection conflict.
func (s *Service) shelfNameFree(ctx context.Context, title string) error {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 120 {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	entries, err := s.CoreEntries(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.CoreID == atariStCoreID && entry.Title == title {
			return errShelfName
		}
	}
	return nil
}

// Caller holds lifecycle admission.
func (s *Service) addShelfDiskLocked(ctx context.Context, title, packageID, mediaID string) (catalog.CoreEntry, error) {
	marks, ok := s.catalog.(shelfDiskCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.writeCoreEntryLocked(ctx, "", title, "", packageID, protocol.DiskRole, mediaID, false, nil)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	// The entry is written, so finish the pair even if the caller has gone;
	// an unmarked disk would show as a game and could not be managed here.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := marks.MarkShelfDisk(markCtx, entry.GameID); err != nil {
		if dropErr := s.dropNewShelfDiskLocked(markCtx, entry.GameID); dropErr != nil {
			return catalog.CoreEntry{}, dropErr
		}
		return catalog.CoreEntry{}, mapCoreEntryError(err)
	}
	return entry, nil
}

// dropNewShelfDiskLocked takes back a disk this operation has just added.
// The caller holds lifecycle admission, so no kit can have started it;
// the usual removal checks would refuse while any kit runs.
func (s *Service) dropNewShelfDiskLocked(ctx context.Context, gameID string) error {
	store, ok := s.catalog.(removableCoreEntryCatalog)
	if !ok {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if err := store.RemoveCoreEntry(ctx, gameID); err != nil {
			return mapCoreEntryError(err)
		}
		if err := m.RemoveGameReferences(ctx, gameID); err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		return nil
	})
}

// NewAtariStShelfDisk formats a blank disk and puts it on the shelf under
// its name. Each disk gets its own serial number, so it is its own library
// medium and never meets another disk's saved changes.
func (s *Service) NewAtariStShelfDisk(parent context.Context, req AtariStShelfNew) (AtariStShelfDisk, error) {
	if protocol.ValidateDigest(req.PackageID) != nil {
		return AtariStShelfDisk{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	var serial [3]byte
	if _, err := rand.Read(serial[:]); err != nil {
		return AtariStShelfDisk{}, canonicalError(protocol.CodeInternal, err)
	}
	disk, err := atarist.FormatSerial(shelfVolumeLabel(req.Title), req.Tracks, req.Heads, req.Sectors, serial)
	if err != nil {
		return AtariStShelfDisk{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk geometry is invalid", Phase: "request"}
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	inspection, err := s.installedCoreInspection(ctx, req.PackageID)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	if err := atariStBlankSizeError(inspection.Descriptor, int64(len(disk.Bytes))); err != nil {
		return AtariStShelfDisk{}, err
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	defer release()
	if err := s.shelfNameFree(ctx, req.Title); err != nil {
		return AtariStShelfDisk{}, err
	}
	media, _, err := s.ImportCoreMedia(ctx, int64(len(disk.Bytes)), bytes.NewReader(disk.Bytes))
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	entry, err := s.addShelfDiskLocked(ctx, req.Title, req.PackageID, media.MediaID)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	return AtariStShelfDisk{
		GameID: entry.GameID, Title: entry.Title, PackageID: entry.PackageID, MediaID: entry.MediaID,
		Size: media.Size, Made: true, Kept: ShelfKeptNone,
	}, nil
}

// shelfDisk is one disk and what its kit holds for it, read under
// lifecycle admission so no launch of this host moves it meanwhile.
type shelfDisk struct {
	entry  catalog.CoreEntry
	made   bool
	kit    shelfKit
	status protocol.Status
	record protocol.MediaRecord
}

// Caller holds lifecycle admission. A disk in the drive is refused: its
// saved record is older than what the running computer holds.
func (s *Service) shelfDiskLocked(ctx context.Context, gameID string) (shelfDisk, error) {
	marks, ok := s.catalog.(shelfDiskCatalog)
	if !ok {
		return shelfDisk{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return shelfDisk{}, err
	}
	id := shelfIdentity(entry)
	if !isAtariStDisk(entry) || !id.Valid() {
		return shelfDisk{}, errShelfDisk
	}
	made, err := marks.ShelfDisks(ctx, atariStCoreID)
	if err != nil {
		return shelfDisk{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	kit, err := s.shelfKit(ctx)
	if err != nil {
		return shelfDisk{}, err
	}
	status, err := kit.client.Status(ctx)
	if err != nil {
		return shelfDisk{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if s.hostPlaysGame(gameID) || statusHoldsDisk(status, gameID) {
		return shelfDisk{}, errShelfInDrive
	}
	record, err := kit.records.InspectMediaRecord(ctx, id)
	if err != nil {
		return shelfDisk{}, canonicalCoreDataError(err)
	}
	_, isMade := made[gameID]
	return shelfDisk{entry: entry, made: isMade, kit: kit, status: status, record: record}, nil
}

func (d shelfDisk) view(size int64) AtariStShelfDisk {
	kept, revision := shelfKept(d.record)
	return AtariStShelfDisk{
		GameID: d.entry.GameID, Title: d.entry.Title, PackageID: d.entry.PackageID, MediaID: d.entry.MediaID,
		Size: size, Made: d.made, Kept: kept, KeptRevision: revision,
	}
}

// revisionMatches holds a caller to the saved record it was shown. An
// empty expectation means the kit had saved nothing.
func (d shelfDisk) revisionMatches(expected string) bool {
	if d.record.State == protocol.MediaRecordAbsent {
		return expected == ""
	}
	return expected == d.record.Revision
}

// Caller holds lifecycle admission. The copy is a new library disk whose
// imported image is the source as it stands: its saved changes when it has
// any, otherwise the image it was made from.
func (s *Service) copyShelfDiskLocked(ctx context.Context, source shelfDisk, title string) (AtariStShelfDisk, error) {
	if err := s.shelfNameFree(ctx, title); err != nil {
		return AtariStShelfDisk{}, err
	}
	mediaID := source.entry.MediaID
	switch {
	case source.record.State == protocol.MediaRecordCorrupt:
		return AtariStShelfDisk{}, errShelfDamaged
	case source.record.State == protocol.MediaRecordPresent && source.record.Changed:
		image, err := source.kit.records.ExportMediaRecord(ctx, protocol.MediaRecordDiscard{
			MediaRecordIdentity: shelfIdentity(source.entry), ExpectedRevision: source.record.Revision,
		})
		if err != nil {
			return AtariStShelfDisk{}, canonicalCoreDataError(err)
		}
		media, _, err := s.ImportCoreMedia(ctx, int64(len(image)), bytes.NewReader(image))
		if err != nil {
			return AtariStShelfDisk{}, err
		}
		mediaID = media.MediaID
	}
	// A title removed from the library leaves its saved changes on the kit.
	// The same name and image would pick them up, so a copy starts by
	// clearing what an earlier disk of that name left behind.
	newID, err := s.localCoreEntryID(ctx, title, source.entry.CoreID, source.entry.PackageID)
	if err != nil {
		return AtariStShelfDisk{}, mapCoreEntryError(err)
	}
	stale := protocol.MediaRecordIdentity{CoreID: source.entry.CoreID, GameID: newID, BaseMediaID: mediaID, Unit: protocol.AtariStFloppyUnit}
	if !stale.Valid() {
		return AtariStShelfDisk{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	left, err := source.kit.records.InspectMediaRecord(ctx, stale)
	if err != nil {
		return AtariStShelfDisk{}, canonicalCoreDataError(err)
	}
	if left.State != protocol.MediaRecordAbsent {
		if _, err := source.kit.records.DiscardMediaRecord(ctx, protocol.MediaRecordDiscard{MediaRecordIdentity: stale, ExpectedRevision: left.Revision}); err != nil {
			return AtariStShelfDisk{}, canonicalCoreDataError(err)
		}
	}
	entry, err := s.addShelfDiskLocked(ctx, title, source.entry.PackageID, mediaID)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	media, err := s.CoreMedia(ctx, entry.MediaID)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	return AtariStShelfDisk{
		GameID: entry.GameID, Title: entry.Title, PackageID: entry.PackageID, MediaID: entry.MediaID,
		Size: media.Size, Made: true, Kept: ShelfKeptNone,
	}, nil
}

// deletableShelfDisk asks, before anything changes, what deleting the disk
// will need: an entry the library can remove, and an idle kit when there is
// a saved record to discard.
func (s *Service) deletableShelfDisk(ctx context.Context, disk shelfDisk) error {
	if _, ok := s.catalog.(removableCoreEntryCatalog); !ok || s.users == nil {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if err := s.checkRemovalTargets(ctx, disk.entry.GameID); err != nil {
		return err
	}
	if disk.record.State != protocol.MediaRecordAbsent && disk.status.State != protocol.StateIdle {
		return errShelfKitRunning
	}
	return nil
}

// Caller holds lifecycle admission. The kit's saved changes go first: an
// entry removed while they remain would leave them where nothing reaches.
func (s *Service) deleteShelfDiskLocked(ctx context.Context, disk shelfDisk) error {
	if err := s.deletableShelfDisk(ctx, disk); err != nil {
		return err
	}
	store := s.catalog.(removableCoreEntryCatalog)
	if disk.record.State != protocol.MediaRecordAbsent {
		if _, err := disk.kit.records.DiscardMediaRecord(ctx, protocol.MediaRecordDiscard{
			MediaRecordIdentity: shelfIdentity(disk.entry), ExpectedRevision: disk.record.Revision,
		}); err != nil {
			return canonicalCoreDataError(err)
		}
	}
	return s.removeCoreEntryLocked(ctx, store, disk.entry.GameID)
}

func (s *Service) withShelfDisk(parent context.Context, gameID string, act func(context.Context, shelfDisk) (AtariStShelfDisk, error)) (AtariStShelfDisk, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return AtariStShelfDisk{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	defer release()
	disk, err := s.shelfDiskLocked(ctx, gameID)
	if err != nil {
		return AtariStShelfDisk{}, err
	}
	return act(ctx, disk)
}

// CopyAtariStShelfDisk makes a new shelf disk from another disk as it
// stands. The source is unchanged.
func (s *Service) CopyAtariStShelfDisk(parent context.Context, gameID, title string) (AtariStShelfDisk, error) {
	return s.withShelfDisk(parent, gameID, func(ctx context.Context, disk shelfDisk) (AtariStShelfDisk, error) {
		return s.copyShelfDiskLocked(ctx, disk, title)
	})
}

// RevertAtariStShelfDisk discards a disk's saved changes, so it is again
// the image it was imported or made as. expectedRevision is the
// KeptRevision the caller was shown.
func (s *Service) RevertAtariStShelfDisk(parent context.Context, gameID, expectedRevision string) (AtariStShelfDisk, error) {
	return s.withShelfDisk(parent, gameID, func(ctx context.Context, disk shelfDisk) (AtariStShelfDisk, error) {
		if disk.record.State == protocol.MediaRecordAbsent {
			return AtariStShelfDisk{}, errShelfNoChanges
		}
		if !disk.revisionMatches(expectedRevision) {
			return AtariStShelfDisk{}, errShelfChanged
		}
		if disk.status.State != protocol.StateIdle {
			return AtariStShelfDisk{}, errShelfKitRunning
		}
		record, err := disk.kit.records.DiscardMediaRecord(ctx, protocol.MediaRecordDiscard{
			MediaRecordIdentity: shelfIdentity(disk.entry), ExpectedRevision: expectedRevision,
		})
		if err != nil {
			return AtariStShelfDisk{}, canonicalCoreDataError(err)
		}
		disk.record = record
		media, err := s.CoreMedia(ctx, disk.entry.MediaID)
		if err != nil {
			return AtariStShelfDisk{}, err
		}
		return disk.view(media.Size), nil
	})
}

// RenameAtariStShelfDisk gives a disk made here a new name. A library
// title's identity follows its name, and the kit keeps saved changes under
// that identity, so the disk is copied under the new name and the old one
// deleted. If the delete fails, both disks remain and nothing is lost.
func (s *Service) RenameAtariStShelfDisk(parent context.Context, gameID, expectedRevision, title string) (AtariStShelfDisk, error) {
	return s.withShelfDisk(parent, gameID, func(ctx context.Context, disk shelfDisk) (AtariStShelfDisk, error) {
		if !disk.made {
			return AtariStShelfDisk{}, errShelfNotMade
		}
		if !disk.revisionMatches(expectedRevision) {
			return AtariStShelfDisk{}, errShelfChanged
		}
		if strings.TrimSpace(title) == disk.entry.Title {
			media, err := s.CoreMedia(ctx, disk.entry.MediaID)
			if err != nil {
				return AtariStShelfDisk{}, err
			}
			return disk.view(media.Size), nil
		}
		// Nothing is copied unless the old disk can then be deleted.
		if err := s.deletableShelfDisk(ctx, disk); err != nil {
			return AtariStShelfDisk{}, err
		}
		renamed, err := s.copyShelfDiskLocked(ctx, disk, title)
		if err != nil {
			return AtariStShelfDisk{}, err
		}
		if err := s.deleteShelfDiskLocked(ctx, disk); err != nil {
			// Take the copy back, so a retry finds the name free again.
			dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if dropErr := s.dropNewShelfDiskLocked(dropCtx, renamed.GameID); dropErr != nil {
				return AtariStShelfDisk{}, errors.Join(err, dropErr)
			}
			return AtariStShelfDisk{}, err
		}
		return renamed, nil
	})
}

// DeleteAtariStShelfDisk removes a disk made here and its saved changes
// from the kit asked. A game's disk is not deleted here: revert it, or
// remove the game from the library.
func (s *Service) DeleteAtariStShelfDisk(parent context.Context, gameID, expectedRevision string) error {
	_, err := s.withShelfDisk(parent, gameID, func(ctx context.Context, disk shelfDisk) (AtariStShelfDisk, error) {
		if !disk.made {
			return AtariStShelfDisk{}, errShelfNotMade
		}
		if !disk.revisionMatches(expectedRevision) {
			return AtariStShelfDisk{}, errShelfChanged
		}
		return AtariStShelfDisk{}, s.deleteShelfDiskLocked(ctx, disk)
	})
	return err
}
