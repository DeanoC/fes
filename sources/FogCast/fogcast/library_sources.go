package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
)

// LibrarySourceConfig names a read-only content source, never an executor.
type LibrarySourceConfig struct {
	ID    string `json:"id" toml:"id"`
	URL   string `json:"url" toml:"url"`
	Token string `json:"token,omitempty" toml:"token,omitempty"`
}

// ValidLibrarySourceToken accepts a private shared bearer token suitable for
// both listener configuration and client source entries.
func ValidLibrarySourceToken(token string) bool {
	if len(token) < 32 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

type LibrarySourceRefresh struct {
	SourceID string              `json:"source_id"`
	Entries  []catalog.CoreEntry `json:"entries"`
	Skipped  int                 `json:"skipped"`
}

type remoteCoreEntryCatalog interface {
	CreateRemoteCoreEntry(context.Context, string, string, string, catalog.RemoteCoreEntryOrigin) (catalog.CoreEntry, error)
	RemoteCoreEntry(context.Context, string) (catalog.RemoteCoreEntryOrigin, bool, error)
	RemoteCoreEntryBySource(context.Context, string, string) (catalog.CoreEntry, catalog.RemoteCoreEntryOrigin, bool, error)
	BindRemoteCoreEntryMedia(context.Context, string, catalog.RemoteCoreEntryOrigin) (catalog.CoreEntry, error)
}

type remoteCassetteImporter interface {
	ImportRemoteCassetteStream(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
}

func normalizeLibrarySources(raw []LibrarySourceConfig) ([]LibrarySourceConfig, error) {
	if len(raw) > 16 {
		return nil, errors.New("at most 16 library sources are supported")
	}
	result := make([]LibrarySourceConfig, 0, len(raw))
	seen := map[string]bool{}
	for _, source := range raw {
		source.ID, source.URL = strings.TrimSpace(source.ID), strings.TrimSpace(source.URL)
		u, err := url.Parse(source.URL)
		if !validLibrarySourceID(source.ID) || seen[source.ID] || (source.Token != "" && !ValidLibrarySourceToken(source.Token)) || err != nil ||
			(u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return nil, errors.New("library sources require unique ids and HTTP origins")
		}
		u.Path = ""
		source.URL = u.String()
		seen[source.ID] = true
		result = append(result, source)
	}
	return result, nil
}

func (s *Service) LibrarySources() []LibrarySourceConfig {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	return append([]LibrarySourceConfig{}, s.librarySources...)
}

// SetLibrarySources uses the same canonical config and writer admission as
// other library settings. The kit host preserves this field on regeneration.
func (s *Service) SetLibrarySources(ctx context.Context, sources []LibrarySourceConfig) error {
	next, err := normalizeLibrarySources(sources)
	if err != nil || s.configPath == "" {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	release, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return err
	}
	defer release()
	s.libraryMu.Lock()
	defer s.libraryMu.Unlock()
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	// An omitted token in a PUT keeps the stored credential for the same id,
	// including when that source's URL changes.
	for i := range next {
		if next[i].Token != "" {
			continue
		}
		for _, existing := range s.librarySources {
			if existing.ID == next[i].ID {
				next[i].Token = existing.Token
				break
			}
		}
	}
	file, err := openConfigSource(s.configPath)
	if err != nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	var raw fileConfig
	previous, readErr := io.ReadAll(file)
	closeErr := file.Close()
	decoder := toml.NewDecoder(bytes.NewReader(previous))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&raw)
	if err != nil || readErr != nil || closeErr != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return canonicalError(protocol.CodeInternal, nil)
	}
	raw.LibrarySources = next
	if err := writeConfigFile(s.configPath, raw); err != nil {
		if err := restorePrivateFile(s.configPath, previous, true); err != nil {
			return canonicalError(protocol.CodeInternal, nil)
		}
		return canonicalError(protocol.CodeInternal, nil)
	}
	s.librarySources = next
	return nil
}

func (s *Service) librarySource(id string) (LibrarySourceConfig, bool) {
	for _, source := range s.LibrarySources() {
		if source.ID == id {
			return source, true
		}
	}
	return LibrarySourceConfig{}, false
}

func sourceUnavailable() error {
	return &protocol.APIError{Code: protocol.CodeSourceUnavailable, Phase: "prepare", Message: "library source is unavailable"}
}

func librarySourceGET(ctx context.Context, source LibrarySourceConfig, path string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL+path, nil)
	if err != nil {
		return nil, sourceUnavailable()
	}
	if source.Token != "" {
		request.Header.Set("Authorization", "Bearer "+source.Token)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, sourceUnavailable()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, sourceUnavailable()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, sourceUnavailable()
	}
	return body, nil
}

// RefreshLibrarySource indexes a snapshot without downloading media. Existing
// mappings keep their selected digest and every kit-side user choice.
func (s *Service) RefreshLibrarySource(ctx context.Context, id string) (LibrarySourceRefresh, error) {
	result := LibrarySourceRefresh{SourceID: id, Entries: []catalog.CoreEntry{}}
	source, ok := s.librarySource(id)
	store, supported := s.catalog.(remoteCoreEntryCatalog)
	if !ok || !supported {
		return result, canonicalError(protocol.CodeBadRequest, nil)
	}
	body, err := librarySourceGET(ctx, source, "/api/v1/library/core-entries", 1<<20)
	if err != nil {
		return result, err
	}
	var document struct {
		Entries []catalog.CoreEntry `json:"entries"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Entries == nil || len(document.Entries) > 1000 {
		return result, sourceUnavailable()
	}
	seen := map[string]bool{}
	for _, entry := range document.Entries {
		if seen[entry.GameID] || protocol.ValidateGameID(entry.GameID) != nil ||
			protocol.ValidateDigest(entry.MediaID) != nil || protocol.ValidateDigest(entry.PackageID) != nil ||
			strings.TrimSpace(entry.Title) == "" || len(entry.Title) > 512 {
			return result, sourceUnavailable()
		}
		seen[entry.GameID] = true
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	for _, entry := range document.Entries {
		if entry.CoreID != "fes.zx81" || entry.MediaRole != "cassette" {
			result.Skipped++
			continue
		}
		local, _, found, err := store.RemoteCoreEntryBySource(ctx, id, entry.GameID)
		if err != nil {
			return result, mapCoreEntryError(err)
		}
		if !found {
			inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
			if err != nil || inspection.Descriptor.Core.ID != "fes.zx81" || !sourceCassetteCapable(inspection.Descriptor) {
				result.Skipped++
				continue
			}
			remoteID := catalog.RemoteCoreEntryID(id, entry.GameID)
			if err = s.cleanupRetainedEntry(ctx, remoteID); err != nil {
				return result, err
			}
			local, err = store.CreateRemoteCoreEntry(ctx, entry.Title, entry.CoreID, entry.PackageID,
				catalog.RemoteCoreEntryOrigin{SourceID: id, SourceGameID: entry.GameID, PackageID: entry.PackageID, MediaID: entry.MediaID, MediaName: entry.MediaName})
			if err != nil {
				return result, mapCoreEntryError(err)
			}
		}
		result.Entries = append(result.Entries, local)
	}
	return result, nil
}

func sourceCassetteCapable(descriptor corepackage.Descriptor) bool {
	for _, media := range protocol.DeclaredCoreMediaCapabilities(descriptor) {
		if media.Role == "cassette" && media.MinBytes <= 16<<10 && media.MaxBytes >= 1 {
			return true
		}
	}
	return false
}

func (s *Service) remoteMediaPending(ctx context.Context, entry catalog.CoreEntry) (bool, bool) {
	store, ok := s.catalog.(remoteCoreEntryCatalog)
	if !ok {
		return false, false
	}
	origin, found, err := store.RemoteCoreEntry(ctx, entry.GameID)
	_, configured := s.librarySource(origin.SourceID)
	if err != nil {
		return true, false
	}
	return found && origin.Pending, found && origin.Pending && configured && entry.CoreID == "fes.zx81" &&
		entry.PackageID == origin.PackageID && entry.MediaID == "" && protocol.ValidateDigest(origin.MediaID) == nil
}

// remoteSelectionSnapshot protects the local package, firmware and expansion
// selections while the remote cassette is in flight. ROM is separate because
// the mesh projection uses its primary slot for the ROM before cassette bind.
type remoteSelectionSnapshot struct {
	entry     catalog.CoreEntry
	rom       catalog.CoreEntryROM
	expansion catalog.CoreEntryExpansion
	slots     []catalog.CoreEntrySlotExpansion
}

type remoteSelectionCatalog interface {
	CoreEntryROM(context.Context, string) (catalog.CoreEntryROM, error)
	CoreEntryExpansion(context.Context, string) (catalog.CoreEntryExpansion, error)
	CoreEntrySlotExpansions(context.Context, string) ([]catalog.CoreEntrySlotExpansion, error)
}

func (s *Service) captureRemoteSelection(ctx context.Context, gameID string) (remoteSelectionSnapshot, error) {
	var selection remoteSelectionSnapshot
	store, ok := s.catalog.(remoteSelectionCatalog)
	if !ok {
		return selection, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	var err error
	if selection.entry, err = s.CoreEntry(ctx, gameID); err != nil {
		return selection, err
	}
	if selection.rom, err = store.CoreEntryROM(ctx, gameID); err != nil {
		return selection, mapCoreEntryError(err)
	}
	if selection.expansion, err = store.CoreEntryExpansion(ctx, gameID); err != nil {
		return selection, mapCoreEntryError(err)
	}
	if selection.slots, err = store.CoreEntrySlotExpansions(ctx, gameID); err != nil {
		return selection, mapCoreEntryError(err)
	}
	return selection, nil
}

func (s *Service) snapshotRemoteSelectionDrift(snap launchSnapshot) error {
	if snap.remoteSelection == nil {
		return nil
	}
	current, err := s.captureRemoteSelection(context.Background(), snap.gameID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(*snap.remoteSelection, current) {
		return snapshotMismatch(launchSnapshotCompositionChanged)
	}
	return nil
}

// remoteSnapshotSelectMedia changes only the primary content slot; all other
// fields remain as captured before the source read.
func remoteSnapshotSelectMedia(snap *launchSnapshot, digest string) error {
	if !snap.entryOK {
		return nil
	}
	id, err := meshcontent.FromSHA256(digest)
	if err != nil {
		return snapshotMismatch(launchSnapshotCompositionChanged)
	}
	slots := make([]meshcontent.Slot, 0, len(snap.entry.Slots)+1)
	inserted := false
	for _, slot := range snap.entry.Slots {
		if slot.Kind == meshcontent.SlotPrimaryMedia {
			if inserted {
				return snapshotMismatch(launchSnapshotCompositionChanged)
			}
			slots = append(slots, meshcontent.PrimaryMediaSlot(id))
			inserted = true
			continue
		}
		if !inserted && slot.Kind == meshcontent.SlotExpansion {
			slots = append(slots, meshcontent.PrimaryMediaSlot(id))
			inserted = true
		}
		slots = append(slots, slot)
	}
	if !inserted {
		slots = append(slots, meshcontent.PrimaryMediaSlot(id))
	}
	snap.entry.Slots = slots
	return nil
}

// prepareRemoteLibraryLaunch materializes media before the launch snapshot is
// projected for mesh Ensure. It never stops or programs a running core.
func (s *Service) prepareRemoteLibraryLaunch(ctx context.Context, gameID string, snap *launchSnapshot) error {
	store, ok := s.catalog.(remoteCoreEntryCatalog)
	if !ok {
		return nil
	}
	origin, found, err := store.RemoteCoreEntry(ctx, gameID)
	if err != nil {
		return mapCoreEntryError(err)
	}
	if !found || !origin.Pending {
		return nil
	}
	if s.launchUsesForeignKit(*snap, ExecutionFPGANative) {
		return canonicalError(protocol.CodeKitLeaseDenied, nil)
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return err
	}
	defer release()
	snap.remoteTargetFrozen = true
	if !snap.frozen {
		// An entry-only placement session can exist without an executor.
		// Freeze its projection too, without enabling mesh Ensure.
		s.meshMu.Lock()
		session := s.meshExecute
		s.meshMu.Unlock()
		if session.BoundNode != snap.boundNode || session.Executor != snap.executor {
			return snapshotMismatch(launchSnapshotBoundNodeChanged)
		}
		if session.Entry != nil {
			snap.entry, snap.entryOK = session.Entry(gameID)
		}
	}
	if err := s.validateLaunchSnapshot(*snap); err != nil {
		return err
	}
	selection, err := s.captureRemoteSelection(ctx, gameID)
	if err != nil {
		return err
	}
	snap.remoteSelection = &selection
	entry := selection.entry
	if entry.MediaID != "" || entry.PackageID != origin.PackageID {
		return nil
	}
	inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
	if err != nil {
		return err
	}
	if entry.CoreID != "fes.zx81" || !sourceCassetteCapable(inspection.Descriptor) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	source, configured := s.librarySource(origin.SourceID)
	if !configured {
		return sourceUnavailable()
	}
	mediaStore, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, err := mediaStore.CoreMediaInfo(ctx, origin.MediaID)
	if errors.Is(err, catalog.ErrCoreMediaNotFound) {
		body, fetchErr := librarySourceGET(ctx, source, "/api/v1/mesh/content/object?id=sha256:"+origin.MediaID, 16<<10)
		if fetchErr != nil {
			return fetchErr
		}
		digest := sha256.Sum256(body)
		if len(body) == 0 || hex.EncodeToString(digest[:]) != origin.MediaID {
			return sourceUnavailable()
		}
		if err := s.validateLaunchSnapshot(*snap); err != nil {
			return err
		}
		importer, ok := s.catalog.(remoteCassetteImporter)
		if !ok {
			return canonicalError(protocol.CodeUnsupportedOperation, nil)
		}
		media, _, err = importer.ImportRemoteCassetteStream(ctx, int64(len(body)), bytes.NewReader(body))
	}
	if err != nil {
		return mapCoreMediaError(err)
	}
	if media.MediaID != origin.MediaID || media.Size < 1 || media.Size > 16<<10 {
		return sourceUnavailable()
	}
	if err := s.validateCoreEntryMedia(ctx, inspection.Descriptor, "cassette", origin.MediaID); err != nil {
		return err
	}
	if err := s.validateLaunchSnapshot(*snap); err != nil {
		return err
	}
	bound, err := store.BindRemoteCoreEntryMedia(ctx, gameID, origin)
	if err != nil {
		return mapCoreEntryError(err)
	}
	snap.remoteSelection.entry = bound
	if err := remoteSnapshotSelectMedia(snap, origin.MediaID); err != nil {
		return err
	}
	if err := s.validateLaunchSnapshot(*snap); err != nil {
		return err
	}
	return nil
}
