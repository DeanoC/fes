package appliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	release "github.com/DeanoC/FogCast/appliance"
)

// ConfirmedContext is the watchdog's confirmation boundary. True means this
// exact boot/image is confirmed AND both state and its directory have synced.
// It can finish an earlier indeterminate post-rename confirmation safely.
// Unconfirmed, different or corrupt state returns false; I/O failures return
// an error and must never cause the watchdog to disarm.
//
// When confirmation also publishes a FAT splash, ConfirmedImage is recorded
// only after that publish. Store acceptance alone must not disarm the watchdog.
func (s *Store) ConfirmedContext(ctx context.Context, bootID, imageSHA string) (bool, error) {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return false, errors.New("invalid confirmation identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return false, err
	}
	if corrupt || st.BootID != bootID || st.ConfirmedImage != imageSHA || st.Good != imageSHA {
		return false, nil
	}
	if err = s.syncState(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// SplashPendingContext reports that this boot's image is known-good and the
// watchdog has not been shown confirmation yet. That is the recoverable gap
// between accepting the trial and publishing its FAT splash.
func (s *Store) SplashPendingContext(ctx context.Context, bootID, imageSHA string) (bool, error) {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return false, errors.New("invalid confirmation identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return false, err
	}
	if corrupt || st.BootID != bootID || st.Good != imageSHA || st.TrialImage != "" || st.ConfirmedImage != "" {
		return false, nil
	}
	return true, nil
}

// RejectTrialContext records an exact consumed trial rejected before watchdog
// startup or root switching. It preserves Good/Previous and the consumed boot
// ID, clears the failed trial, and never creates Pending or confirmation state.
// Failure is fatal to this boot attempt because persistence may be uncertain.
func (s *Store) RejectTrialContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid rejected trial identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return err
	}
	if corrupt {
		return ErrCorruptState
	}
	if st.BootID != bootID || st.TrialImage != imageSHA {
		return errors.New("rejected image is not this boot's consumed trial")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	st.TrialImage = ""
	return s.writeState(st)
}

// RecordFallbackContext binds Good to the verified nontrial image that the
// bootstrap has successfully prepared for this boot. The recorded Good, its
// verified Previous image, or stable factory may be selected. A consumed trial
// must first be rejected explicitly; this method cannot discard or accept a
// trial. When a fallback replaces an unusable Good, the unusable image is not
// retained as a rollback target.
// With corrupt state, only the factory or the image named by the boot ticket
// (and its explicit fallback for a nontrial ticket) is accepted. Corrupt-state
// boot preserves the damaged record as recovery evidence.
func (s *Store) RecordFallbackContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid fallback identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return err
	}
	if corrupt {
		if imageSHA == s.factory.ImageSHA256 {
			return ctx.Err()
		}
		ticket, ticketErr := s.readBootTicket()
		if ticketErr != nil {
			return ErrCorruptState
		}
		allowed := imageSHA == ticket.FallbackImageSHA256
		if !ticket.Trial {
			allowed = allowed || imageSHA == ticket.ImageSHA256
		}
		if !allowed {
			return ErrCorruptState
		}
		return ctx.Err()
	}
	if st.BootID != bootID {
		return errors.New("fallback boot does not match consumed boot")
	}
	if imageSHA != st.Good && imageSHA != st.Previous && imageSHA != s.factory.ImageSHA256 {
		return errors.New("fallback is neither known good, previous, nor factory")
	}
	if st.TrialImage != "" || st.Pending != "" {
		return errors.New("fallback cannot discard trial or pending selection")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if st.Good == imageSHA {
		return s.syncState(ctx)
	}
	st.Good = imageSHA
	st.Previous = ""
	st.ConfirmedImage = ""
	return s.writeState(st)
}

type bootTicket struct {
	BootID              string `json:"boot_id"`
	ImageSHA256         string `json:"image_sha256"`
	Trial               bool   `json:"trial"`
	FallbackImageSHA256 string `json:"fallback_image_sha256,omitempty"`
}

type bootFallbackRecord struct {
	BootID              string `json:"boot_id"`
	ImageSHA256         string `json:"image_sha256"`
	FallbackImageSHA256 string `json:"fallback_image_sha256"`
}

// WriteBootTicket writes recovery metadata first, then atomically publishes the
// legacy-compatible boot.json. Older target agents ignore the sidecar and keep
// accepting the unchanged three-field ticket schema.
func WriteBootTicket(path, bootID, imageSHA string, trial bool, fallbackImageSHA string) error {
	return writeBootTicketWithSync(path, bootID, imageSHA, trial, fallbackImageSHA, (*os.File).Sync, syncDir)
}

func writeBootTicketWithSync(path, bootID, imageSHA string, trial bool, fallbackImageSHA string, syncFile func(*os.File) error, syncDirectory func(string) error) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) || !release.ValidHash(fallbackImageSHA) {
		return errors.New("invalid boot ticket identity or fallback")
	}
	meta := bootFallbackRecord{BootID: bootID, ImageSHA256: imageSHA, FallbackImageSHA256: fallbackImageSHA}
	metaBody, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = atomicWriteWithSync(filepath.Join(dir, BootFallbackFilename), append(metaBody, '\n'), syncFile, syncDirectory); err != nil {
		return err
	}
	body, err := json.Marshal(BootTicket{BootID: bootID, ImageSHA256: imageSHA, Trial: trial})
	if err != nil {
		return err
	}
	return atomicWriteWithSync(path, append(body, '\n'), syncFile, syncDirectory)
}

// ReadBootFallback returns the sidecar fallback only when it belongs to the
// supplied ticket. A missing or stale sidecar is harmless for older tickets.
func ReadBootFallback(path, bootID, imageSHA string) (string, error) {
	f, err := regularOpen(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(body) > 4096 {
		return "", errUnreadableBootTicket
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var record bootFallbackRecord
	if decoder.Decode(&record) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!validBootID(record.BootID) || !release.ValidHash(record.ImageSHA256) || !release.ValidHash(record.FallbackImageSHA256) {
		return "", errUnreadableBootTicket
	}
	if record.BootID != bootID || record.ImageSHA256 != imageSHA {
		return "", nil
	}
	return record.FallbackImageSHA256, nil
}

func (s *Store) readBootTicket() (bootTicket, error) {
	var ticket bootTicket
	f, err := regularOpen(filepath.Join(s.root, "boot.json"))
	if err != nil {
		return ticket, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return ticket, err
	}
	if len(b) > 4096 {
		return ticket, errUnreadableBootTicket
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&ticket) != nil || !release.ValidHash(ticket.ImageSHA256) || ticket.FallbackImageSHA256 != "" && !release.ValidHash(ticket.FallbackImageSHA256) {
		return ticket, errUnreadableBootTicket
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return ticket, errUnreadableBootTicket
	}
	if _, ok := fields["boot_id"]; !ok || !validBootID(ticket.BootID) {
		return ticket, errUnreadableBootTicket
	}
	trialRaw, ok := fields["trial"]
	if !ok {
		return ticket, errUnreadableBootTicket
	}
	var trial *bool
	if json.Unmarshal(trialRaw, &trial) != nil || trial == nil {
		return ticket, errUnreadableBootTicket
	}
	ticket.Trial = *trial
	if raw, ok := fields["fallback_image_sha256"]; ok {
		var fallback *string
		if json.Unmarshal(raw, &fallback) != nil || fallback == nil || !release.ValidHash(*fallback) {
			return ticket, errUnreadableBootTicket
		}
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		return ticket, errUnreadableBootTicket
	}
	fallback, err := ReadBootFallback(filepath.Join(s.root, BootFallbackFilename), ticket.BootID, ticket.ImageSHA256)
	// The sidecar is optional recovery metadata. A malformed or unreadable
	// sidecar must not make a valid boot.json ticket unusable; retain any
	// fallback hash carried by the ticket itself.
	if err == nil && fallback != "" {
		ticket.FallbackImageSHA256 = fallback
	}
	return ticket, nil
}

// LastFallbackSelection identifies the last bootable release independently
// of damaged selection state. The ticket records a verified fallback alongside
// the last selected image. If that fallback is damaged, try the last selected
// image and then the immutable factory image.
func (s *Store) LastFallbackSelection(bootID string) (Selection, error) {
	if !validBootID(bootID) {
		return Selection{}, errors.New("invalid boot ID")
	}
	unlock, err := s.lock(context.Background())
	if err != nil {
		return Selection{}, err
	}
	defer unlock()
	ticket, err := s.readBootTicket()
	if err != nil {
		return Selection{}, err
	}
	type fallbackCandidate struct {
		hash     string
		fallback string
	}
	candidates := []fallbackCandidate{{hash: ticket.FallbackImageSHA256, fallback: s.factory.ImageSHA256}}
	if !ticket.Trial {
		fallback := ticket.FallbackImageSHA256
		if !release.ValidHash(fallback) {
			fallback = s.factory.ImageSHA256
		}
		candidates = []fallbackCandidate{
			{hash: ticket.ImageSHA256, fallback: fallback},
			{hash: ticket.FallbackImageSHA256, fallback: s.factory.ImageSHA256},
		}
	}
	candidates = append(candidates, fallbackCandidate{hash: s.factory.ImageSHA256, fallback: s.factory.ImageSHA256})
	var failures []error
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.hash == "" || seen[candidate.hash] {
			continue
		}
		seen[candidate.hash] = true
		manifest, verifyErr := s.verify(candidate.hash)
		if verifyErr != nil {
			failures = append(failures, verifyErr)
			continue
		}
		path, pathErr := s.ImagePath(candidate.hash)
		if pathErr != nil {
			return Selection{}, pathErr
		}
		return Selection{Manifest: manifest, Path: path, BootID: bootID, FallbackImageSHA256: candidate.fallback}, nil
	}
	return Selection{}, fmt.Errorf("no bootable image in last fallback ticket: %w", errors.Join(failures...))
}

// clearBootTrial rewrites a matching trial ticket to trial:false. The boot id
// and image stay as the bootstrap recorded them. A missing ticket, or a ticket
// for another boot or image, is left untouched. The caller holds the store lock.
func (s *Store) clearBootTrial(bootID, imageSHA, fallbackImageSHA string) error {
	ticket, err := s.readBootTicket()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if ticket.BootID != bootID || ticket.ImageSHA256 != imageSHA || !ticket.Trial {
		return nil
	}
	// Older boot writers may have omitted the fallback from the ticket. Once
	// confirmation records the previous Good image, preserve that recovery
	// target when clearing the trial flag.
	if ticket.FallbackImageSHA256 == "" {
		if release.ValidHash(fallbackImageSHA) {
			ticket.FallbackImageSHA256 = fallbackImageSHA
		} else {
			ticket.FallbackImageSHA256 = s.factory.ImageSHA256
		}
	}
	ticket.Trial = false
	return writeBootTicketWithSync(filepath.Join(s.root, "boot.json"), ticket.BootID, ticket.ImageSHA256, ticket.Trial, ticket.FallbackImageSHA256, s.syncFile, s.syncDirectory)
}

// clearConfirmedBootTicket clears a durable confirmation's trial ticket.
// It is only called after the release record and its directory are synced.
// A rewrite error is ErrBootTicketRewrite and does not undo that record.
func (s *Store) clearConfirmedBootTicket(bootID, imageSHA, fallbackImageSHA string) error {
	if err := s.clearBootTrial(bootID, imageSHA, fallbackImageSHA); err != nil {
		return fmt.Errorf("%w: %w", ErrBootTicketRewrite, err)
	}
	return nil
}

func (s *Store) activate(ctx context.Context, st diskState, hash string) error {
	if st.Pending == hash {
		return nil
	}
	if st.Pending != "" || st.TrialImage != "" {
		return ErrBusy
	}
	if hash == st.Good {
		return errors.New("image is already known good")
	}
	if _, e := s.verifyContext(ctx, hash); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	st.Pending = hash
	return s.writeState(st)
}

func (s *Store) Activate(hash string) error {
	return s.ActivateContext(context.Background(), hash)
}

func (s *Store) ActivateContext(ctx context.Context, hash string) error {
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	return s.activate(ctx, st, hash)
}

func (s *Store) Rollback() error {
	return s.RollbackContext(context.Background())
}

func (s *Store) RollbackContext(ctx context.Context) error {
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.Previous == "" {
		return errors.New("no previous known-good release")
	}
	return s.activate(ctx, st, st.Previous)
}

func (s *Store) selection(hash, bootID string, trial bool) (Selection, error) {
	m, e := s.verify(hash)
	if e != nil {
		return Selection{}, e
	}
	path, _ := s.ImagePath(hash)
	return Selection{Manifest: m, Path: path, Trial: trial, BootID: bootID, FallbackImageSHA256: hash}, nil
}

func (s *Store) goodSelection(st diskState, bootID string) (Selection, error) {
	sel, e := s.selection(st.Good, bootID, false)
	if e == nil {
		sel.FallbackImageSHA256 = st.Previous
		if !release.ValidHash(sel.FallbackImageSHA256) {
			sel.FallbackImageSHA256 = s.factory.ImageSHA256
		}
		return sel, nil
	}
	if release.ValidHash(st.Previous) {
		sel, e = s.selection(st.Previous, bootID, false)
		if e == nil {
			sel.FallbackImageSHA256 = s.factory.ImageSHA256
			return sel, nil
		}
	}
	return s.selection(s.factory.ImageSHA256, bootID, false)
}

// BeginBoot durably consumes a trial before returning it. Calling twice for one
// boot is an error. A subsequent boot clears an unconfirmed consumed trial.
// The caller recovers mount/init failures with verified Good, Previous, then factory.
func (s *Store) BeginBoot(bootID string) (Selection, error) {
	if !validBootID(bootID) {
		return Selection{}, errors.New("invalid boot ID")
	}
	unlock, e := s.lock(context.Background())
	if e != nil {
		return Selection{}, e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return Selection{}, e
	}
	if c {
		return s.goodSelection(s.fallbackState(), bootID)
	}
	if st.BootID == bootID {
		return Selection{}, errors.New("boot selection already consumed for this boot")
	}
	candidate := st.Pending
	st.Pending = ""
	st.TrialImage = ""
	st.ConfirmedImage = ""
	st.BootID = bootID
	if candidate != "" {
		sel, e := s.selection(candidate, bootID, true)
		if e == nil {
			st.TrialImage = candidate
			sel.FallbackImageSHA256 = st.Good
			if e = s.writeState(st); e != nil {
				return Selection{}, e
			}
			return sel, nil
		}
	}
	if e = s.writeState(st); e != nil {
		return Selection{}, e
	}
	return s.goodSelection(st, bootID)
}

func (s *Store) Confirm(bootID, imageSHA string) error {
	return s.ConfirmContext(context.Background(), bootID, imageSHA)
}

func (s *Store) ConfirmContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid confirmation identity")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.BootID != bootID {
		return errors.New("confirmation boot does not match")
	}
	if st.ConfirmedImage == imageSHA && st.Good == imageSHA {
		if e = s.syncState(ctx); e != nil {
			return e
		}
		// The release is already durable. Still clear a trial ticket left by an
		// interrupted confirm or an older writer, so this boot does not keep
		// reporting trial until the next reboot. A rewrite failure leaves the
		// durable record in place.
		if e = s.clearConfirmedBootTicket(bootID, imageSHA, st.Previous); e != nil {
			return e
		}
		if e = s.enforceRetention(ctx, ""); e != nil {
			return fmt.Errorf("%w: %w", ErrRetentionAfterConfirmation, e)
		}
		return nil
	}
	if st.TrialImage != imageSHA {
		return errors.New("confirmation image is not this boot's trial")
	}
	if _, e = s.verifyContext(ctx, imageSHA); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	st.Previous = st.Good
	st.Good = imageSHA
	st.TrialImage = ""
	st.ConfirmedImage = imageSHA
	if e = s.writeState(st); e != nil {
		return e
	}
	if e = s.clearConfirmedBootTicket(bootID, imageSHA, st.Previous); e != nil {
		return e
	}
	if e = s.enforceRetention(ctx, ""); e != nil {
		return fmt.Errorf("%w: %w", ErrRetentionAfterConfirmation, e)
	}
	return nil
}

// AcceptTrialContext promotes this boot's trial to known-good without recording
// the watchdog boundary. ConfirmedImage stays empty until CompleteSplashContext.
// A retry after promotion only finishes the state sync.
func (s *Store) AcceptTrialContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid confirmation identity")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.BootID != bootID {
		return errors.New("confirmation boot does not match")
	}
	if st.Good == imageSHA && st.TrialImage == "" && (st.ConfirmedImage == "" || st.ConfirmedImage == imageSHA) {
		return s.syncState(ctx)
	}
	if st.TrialImage != imageSHA {
		return errors.New("confirmation image is not this boot's trial")
	}
	if _, e = s.verifyContext(ctx, imageSHA); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if st.Good != imageSHA {
		st.Previous = st.Good
	}
	st.Good = imageSHA
	st.TrialImage = ""
	st.ConfirmedImage = ""
	return s.writeState(st)
}

// CompleteSplashContext records the watchdog confirmation for an image whose
// splash is already on FAT. It does not publish the splash itself.
func (s *Store) CompleteSplashContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid confirmation identity")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.BootID != bootID || st.Good != imageSHA || st.TrialImage != "" {
		return errors.New("splash confirmation is not this boot's accepted image")
	}
	if st.ConfirmedImage == imageSHA {
		return s.syncState(ctx)
	}
	if st.ConfirmedImage != "" {
		return errors.New("splash confirmation does not match")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	st.ConfirmedImage = imageSHA
	return s.writeState(st)
}
