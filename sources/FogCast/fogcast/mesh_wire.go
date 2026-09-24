package fogcast

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

const meshDialBackoff = 5 * time.Second

// EnableMeshContent turns the ensure seam on when configuration asked
// for it. Open records that choice and does not dial. Production mains
// call this after Open. [mesh] ensure = false leaves the seam off.
func (s *Service) EnableMeshContent() {
	if s == nil || !s.meshEnsureConfig {
		return
	}
	s.meshMu.Lock()
	s.meshEnsure = true
	if s.meshHTTP == nil {
		s.meshHTTP = &http.Client{}
	}
	s.meshMu.Unlock()
}

// activateMeshExecutor dials the selected kit and installs its content
// store as the ensure executor. A failed dial leaves the seam off so
// Phase 0 and Phase 1 launch continue. Retries wait meshDialBackoff.
func (s *Service) activateMeshExecutor(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}
	s.meshMu.Lock()
	enabled := s.meshEnsure
	installed := s.meshExecute.Executor != nil
	recent := !s.meshDialAt.IsZero() && time.Since(s.meshDialAt) < meshDialBackoff
	client := s.meshHTTP
	if enabled && !installed && !recent {
		s.meshDialAt = time.Now()
	}
	s.meshMu.Unlock()
	if !enabled || installed || recent {
		return
	}
	if client == nil {
		client = &http.Client{}
	}
	s.targetMu.RLock()
	cfg := targetByName(s.targets, s.selectedTarget)
	s.targetMu.RUnlock()
	if !cfg.Enabled || strings.TrimSpace(cfg.Address) == "" || strings.TrimSpace(cfg.Agent) == "" {
		return
	}
	endpoint, err := url.Parse(cfg.Address)
	if err != nil {
		return
	}
	remote, err := kitcontent.Dial(ctx, endpoint, cfg.Agent, client)
	if err != nil || remote == nil {
		return
	}
	if want := cfg.NodeID(); want != "" && remote.NodeID() != want {
		return
	}
	s.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: remote.NodeID(),
		Executor:  remote,
		Entry:     s.meshCatalogEntry,
	})
}

// meshCatalogEntry projects one package-backed FPGA title. Host-only
// titles and unrecognized play ABIs return false so they stay on the
// existing launch path and omit ready_here. A title the projection
// cannot name returns false for the same reason.
func (s *Service) meshCatalogEntry(gameID string) (meshcontent.Entry, bool) {
	if s == nil || s.catalog == nil {
		return meshcontent.Entry{}, false
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return meshcontent.Entry{}, false
	}
	ctx := context.Background()
	entry, err := store.CoreEntry(ctx, gameID)
	if err != nil {
		return meshcontent.Entry{}, false
	}
	inspection, _, err := s.readInstalledCore(ctx, entry.PackageID)
	if err != nil {
		return meshcontent.Entry{}, false
	}
	abi := inspection.Descriptor.ABI
	if !RecognizedPlayABI(abi.ID, abi.Major, abi.Minor) {
		return meshcontent.Entry{}, false
	}
	game, err := s.catalog.Game(ctx, gameID)
	if err != nil {
		game = catalog.Game{ID: entry.GameID, Title: entry.Title, System: catalog.CorePlatform}
	}
	title := MeshTitle{
		Game:       game,
		Launchable: true,
		Execute:    ExecutionFPGANative,
		Core:       &entry,
		ABI:        abi,
	}
	if expansions, ok := s.catalog.(coreExpansionCatalog); ok {
		selected, err := expansions.CoreEntryExpansion(ctx, gameID)
		if err != nil {
			return meshcontent.Entry{}, false
		}
		if selected.ExpansionID != "" {
			asset, err := expansions.ReadCoreExpansion(ctx, selected.ExpansionID)
			if err != nil || strings.TrimSpace(asset.Manifest.Slot) == "" || asset.Manifest.CartSHA256 == "" {
				return meshcontent.Entry{}, false
			}
			title.Expansions = []MeshExpansion{{
				Name:   asset.Manifest.Slot,
				Digest: asset.Manifest.CartSHA256,
			}}
		}
	}
	var firmware catalog.CoreFirmware
	if entry.FirmwareRequired {
		slot, err := s.CoreFirmware(ctx, protocol.FirmwareRole)
		if err != nil || slot.MediaID == "" {
			return meshcontent.Entry{}, false
		}
		firmware = slot
	}
	entries, _ := ProjectMeshLibrary(MeshLibrary{Firmware: firmware, Titles: []MeshTitle{title}})
	if len(entries) != 1 {
		return meshcontent.Entry{}, false
	}
	return entries[0], true
}

// MeshContentAdvertises reports whether this host holds id as core-media
// or as an expansion cart payload. A library path is not a content-id.
func (s *Service) MeshContentAdvertises(ctx context.Context, id meshcontent.ContentID) bool {
	if s == nil || id.Validate() != nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.coreMediaHeld(ctx, id.Digest) {
		return true
	}
	return s.expansionCart(ctx, id.Digest) != nil
}

// OpenMeshContent streams id. The bytes are core-media whose media id is
// the digest, or the expansion cart payload whose CartSHA256 is the digest.
func (s *Service) OpenMeshContent(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if s == nil || id.Validate() != nil {
		return nil, os.ErrNotExist
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if store, ok := s.catalog.(coreMediaCatalog); ok {
		media, reader, err := store.OpenCoreMedia(ctx, id.Digest)
		if err == nil {
			if media.MediaID != id.Digest {
				reader.Close()
				return nil, os.ErrNotExist
			}
			return reader, nil
		}
	}
	cart := s.expansionCart(ctx, id.Digest)
	if cart == nil {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(cart)), nil
}

func (s *Service) coreMediaHeld(ctx context.Context, digest string) bool {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return false
	}
	_, err := store.CoreMediaInfo(ctx, digest)
	return err == nil
}

func (s *Service) expansionCart(ctx context.Context, digest string) []byte {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return nil
	}
	rows, err := store.CoreExpansions(ctx)
	if err != nil {
		return nil
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return nil
		}
		asset, err := store.ReadCoreExpansion(ctx, row.ExpansionID)
		if err != nil || asset.Manifest.CartSHA256 != digest || len(asset.Cart) == 0 {
			continue
		}
		return append([]byte(nil), asset.Cart...)
	}
	return nil
}
