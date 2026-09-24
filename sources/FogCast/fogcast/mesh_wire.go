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
// call this after Open. [mesh] ensure defaults off, so an unset table
// leaves the seam off. ensure = true turns it on.
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

// meshTargetIdentity is the selected-target configuration a dial used.
// Name, address, agent token, node id, and the enabled flag are the
// fields that choose the endpoint. A later call whose identity differs
// drops the installed executor instead of keeping the previous kit.
type meshTargetIdentity struct {
	name    string
	address string
	agent   string
	nodeID  string
	enabled bool
}

func meshTargetIdentityOf(selected string, cfg TargetConfig) meshTargetIdentity {
	return meshTargetIdentity{
		name:    strings.TrimSpace(selected),
		address: strings.TrimSpace(cfg.Address),
		agent:   cfg.Agent,
		nodeID:  strings.TrimSpace(cfg.NodeID()),
		enabled: cfg.Enabled,
	}
}

func (id meshTargetIdentity) dialable() bool {
	return id.enabled && id.address != "" && strings.TrimSpace(id.agent) != ""
}

// activateMeshExecutor dials the selected kit and installs its content
// store as the ensure executor. A failed dial leaves the seam off so
// Phase 0 and Phase 1 launch continue. Retries for the same target
// wait meshDialBackoff. A selected target whose configuration no longer
// matches the installed dial drops that session first, so readiness is
// not taken from the previous kit, and dials the new endpoint without
// waiting out the previous backoff.
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
	s.targetMu.RLock()
	want := meshTargetIdentityOf(s.selectedTarget, targetByName(s.targets, s.selectedTarget))
	s.meshMu.Lock()
	enabled := s.meshEnsure
	matches := s.meshExecute.Executor != nil && s.meshInstalled == want
	recent := s.meshDialID == want && !s.meshDialAt.IsZero() && time.Since(s.meshDialAt) < meshDialBackoff
	client := s.meshHTTP
	if enabled && !matches && !recent {
		if s.meshExecute.Executor != nil {
			s.meshExecute = MeshExecuteSession{}
			s.meshInstalled = meshTargetIdentity{}
		}
		s.meshDialAt = time.Now()
		s.meshDialID = want
	}
	s.meshMu.Unlock()
	s.targetMu.RUnlock()
	if !enabled || matches || recent {
		return
	}
	if client == nil {
		client = &http.Client{}
	}
	if !want.dialable() {
		return
	}
	endpoint, err := url.Parse(want.address)
	if err != nil {
		return
	}
	remote, err := kitcontent.Dial(ctx, endpoint, want.agent, client)
	if err != nil || remote == nil {
		return
	}
	if want.nodeID != "" && remote.NodeID() != want.nodeID {
		return
	}
	s.installDialedMeshSession(want, remote)
}

// dialNamedMeshExecutor dials one configured kit and returns its content
// store. It does not install or replace the selected-target session.
// The caller uses the remote for one explicit launch snapshot.
func (s *Service) dialNamedMeshExecutor(ctx context.Context, name string) *kitcontent.Remote {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	s.targetMu.RLock()
	want := meshTargetIdentityOf(name, targetByName(s.targets, name))
	s.meshMu.Lock()
	client := s.meshHTTP
	s.meshMu.Unlock()
	s.targetMu.RUnlock()
	if !want.dialable() {
		return nil
	}
	if client == nil {
		client = &http.Client{}
	}
	endpoint, err := url.Parse(want.address)
	if err != nil {
		return nil
	}
	remote, err := kitcontent.Dial(ctx, endpoint, want.agent, client)
	if err != nil || remote == nil {
		return nil
	}
	if want.nodeID != "" && remote.NodeID() != want.nodeID {
		return nil
	}
	return remote
}

// installDialedMeshSession installs remote when the selected target and
// the in-flight dial are still want. A target change that landed during
// the dial leaves the seam off for the next call to bind.
func (s *Service) installDialedMeshSession(want meshTargetIdentity, remote *kitcontent.Remote) {
	if s == nil || remote == nil {
		return
	}
	session := MeshExecuteSession{
		BoundNode: remote.NodeID(),
		Executor:  remote,
		Entry:     s.meshCatalogEntry,
	}
	s.targetMu.RLock()
	live := meshTargetIdentityOf(s.selectedTarget, targetByName(s.targets, s.selectedTarget))
	s.meshMu.Lock()
	ok := live == want && s.meshDialID == want
	if ok {
		s.meshExecute = session
		s.meshInstalled = want
	}
	s.meshMu.Unlock()
	s.targetMu.RUnlock()
	if ok {
		s.attachMeshAuthorizer(session)
	}
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
