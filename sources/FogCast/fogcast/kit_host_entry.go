package fogcast

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/protocol"
)

// KitHostEntry resolves a kit-local package and optional scanned SMS cartridge
// into a durable host entry. The caller supplies identities, never file paths.
func (s *Service) KitHostEntry(ctx context.Context, packageID, gameID string) (catalog.CoreEntry, error) {
	if !s.kitHost || (packageID == "" && gameID == "") || (packageID != "" && protocol.ValidateDigest(packageID) != nil) || (gameID != "" && protocol.ValidateGameID(gameID) != nil) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	s.kitHostEntryMu.Lock()
	defer s.kitHostEntryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return catalog.CoreEntry{}, err
	}
	cores, err := s.KitHostCores(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if packageID == "" {
		packageID, err = selectedKitSMSPackage(cores)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
	}
	pkg, err := s.installedCoreInspection(ctx, packageID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if pkg.Descriptor.Core.ID == "" || pkg.Descriptor.Core.ID == "fes.menu" {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	selected := false
	for _, core := range cores {
		if core.CoreID == pkg.Descriptor.Core.ID && core.PackageID == packageID && core.Block != "Hardware unavailable" {
			selected = true
			break
		}
	}
	if !selected {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	title := pkg.Descriptor.Core.Name
	role, mediaID, romID := "", "", ""
	if gameID != "" {
		if pkg.Descriptor.Core.ID != "fes.sms" {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		game, err := s.Game(ctx, gameID)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		if game.System != protocol.SystemSMS || (game.Kind != catalog.SourceKindRaw && game.Kind != catalog.SourceKindZIP) || !game.RootOnline || game.State != catalog.SourceStateAvailable {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		if game.Fingerprint.SourceSize < 1 || (game.Kind == catalog.SourceKindRaw && game.Fingerprint.SourceSize > maxLocalCartridgeBytes) ||
			(game.Kind == catalog.SourceKindZIP && (game.Fingerprint.SourceSize > maxLocalCartridgeBytes || game.Fingerprint.ZIPSize < 1 || game.Fingerprint.ZIPSize > maxLocalCartridgeBytes)) {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		root, ok := s.libraryRoot(game.LibraryID)
		if !ok {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeROMNotFound, nil)
		}
		prepared, err := s.preparer.Prepare(ctx, root, game)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		defer prepared.Remove()
		if prepared.Content.Size < 1 || prepared.Content.Size > maxLocalCartridgeBytes {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		reader, err := prepared.Open()
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		media, _, err := s.ImportCoreMedia(ctx, prepared.Content.Size, reader)
		closeErr := reader.Close()
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		if closeErr != nil {
			return catalog.CoreEntry{}, closeErr
		}
		if media.MediaID != prepared.Content.SHA256 {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeInternal, nil)
		}
		title, mediaID = game.Title, media.MediaID
		if req := pkg.Descriptor.ROM; req != nil && req.Role == "cartridge" {
			if req.SourceSize != prepared.Content.Size {
				return catalog.CoreEntry{}, romAdmissionError()
			}
			romID = req.ID
		} else {
			role = "blob"
		}
	}
	title = kitHostEntryTitle(title, pkg.Descriptor.Core.ID)
	entryMediaID := mediaID
	if romID != "" {
		entryMediaID = ""
	}
	// Provenance distinguishes generated entries from independently configured
	// entries with the same visible title. Never rewrite a user's selection.
	store, ok := s.catalog.(interface {
		KitHostEntry(context.Context, string) (catalog.CoreEntry, bool, error)
		ForgetKitHostEntry(context.Context, string, string) error
		AllCoreEntries(context.Context) ([]catalog.CoreEntry, error)
	})
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	digest := sha256.Sum256([]byte(packageID + "\x00" + gameID + "\x00" + mediaID))
	identity := fmt.Sprintf("%x", digest)
	entry, found, err := store.KitHostEntry(ctx, identity)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if found {
		matches := entry.CoreID == pkg.Descriptor.Core.ID && entry.PackageID == packageID && entry.MediaRole == role && entry.MediaID == entryMediaID && !entry.FirmwareRequired
		if matches && pkg.Descriptor.ROM != nil {
			binding, err := s.CoreEntryROM(ctx, entry.GameID)
			if err != nil {
				return catalog.CoreEntry{}, err
			}
			if romID != "" {
				matches = binding.PackageID == packageID && binding.ROMID == romID && binding.MediaID == mediaID
			} else {
				matches = binding.MediaID == "" || binding.BundledDefault
			}
		}
		if matches {
			return entry, nil
		}
		// The user changed this entry. Keep it, and retire only our own
		// mapping before preparing a separate entry for the old identity.
		if err := store.ForgetKitHostEntry(ctx, identity, entry.GameID); err != nil {
			return catalog.CoreEntry{}, err
		}
	}
	for number := 1; number <= 10000; number++ {
		candidateTitle := title
		if number > 1 {
			candidateTitle = fmt.Sprintf("%s (%d)", title, number)
		}
		entries, err := store.AllCoreEntries(ctx)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		occupied := false
		for _, row := range entries {
			if row.CoreID == pkg.Descriptor.Core.ID && row.Title == candidateTitle {
				occupied = true
				break
			}
		}
		if occupied {
			continue
		}
		kit := &catalog.KitHostEntryIdentity{Identity: identity, SourceGameID: gameID}
		if romID != "" {
			kit.ROMID = romID
			kit.ROMMediaID = mediaID
			kit.ROMSourceSize = pkg.Descriptor.ROM.SourceSize
		}
		entry, err = s.writeCoreEntry(ctx, "", candidateTitle, "", packageID, role, entryMediaID, false, kit)
		if err != nil {
			// A concurrent user create may have taken the name.
			current, listErr := store.AllCoreEntries(ctx)
			if listErr != nil {
				return catalog.CoreEntry{}, err
			}
			collision := false
			for _, row := range current {
				if row.CoreID == pkg.Descriptor.Core.ID && row.Title == candidateTitle {
					collision = true
					break
				}
			}
			if collision {
				continue
			}
			return catalog.CoreEntry{}, err
		}

		return entry, nil
	}
	return catalog.CoreEntry{}, canonicalError(protocol.CodeStaleRevision, nil)
}

func kitHostEntryTitle(name, coreID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsControl(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > 240 {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		b.WriteString(coreID)
	}
	return b.String()
}

// selectedKitSMSPackage uses the agent's merged factory/durable selection,
// never an arbitrary retained version. The host inspects it again before use.
func selectedKitSMSPackage(cores []localcores.Core) (string, error) {
	selected := ""
	for _, core := range cores {
		if core.CoreID != "fes.sms" {
			continue
		}
		if protocol.ValidateDigest(core.PackageID) != nil || core.Block == "Hardware unavailable" || selected != "" {
			return "", canonicalError(protocol.CodeMiSTerUnavailable, nil)
		}
		selected = core.PackageID
	}
	if selected == "" {
		return "", canonicalError(protocol.CodeROMNotFound, nil)
	}
	return selected, nil
}

// KitHostCores reports the agent's verified selected factory and durable
// publications without rehashing them during launcher startup. Launch still
// inspects the selected package through the host package view.
func (s *Service) KitHostCores(ctx context.Context) ([]localcores.Core, error) {
	if !s.kitHost {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	list := s.kitHostInventory
	if list == nil {
		list = localcores.NewClient(localcores.DefaultSocket).List
	}
	cores, err := list(ctx)
	if err != nil {
		return nil, canonicalError(protocol.CodeMiSTerUnavailable, err)
	}
	return cores, nil
}
