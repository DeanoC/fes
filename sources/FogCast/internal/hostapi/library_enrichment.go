package hostapi

import (
	"context"
	"errors"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

func applyUserState(result *gameResult, state libraryuser.State) {
	if result == nil {
		return
	}
	result.Favorite = state.Favorite
	if state.PlayCount > 0 {
		result.PlayCount = state.PlayCount
	}
	if state.LastPlayedAt > 0 {
		result.LastPlayedAt = state.LastPlayedAt
	}
}

func enrichGameResult(ctx context.Context, service Service, result gameResult) (gameResult, error) {
	if users, ok := service.(favoriteService); ok {
		if state, err := users.LibraryState(ctx, result.ID); err == nil {
			applyUserState(&result, state)
		}
	}
	if collections, ok := service.(collectionService); ok {
		if ids, err := collections.GameCollectionIDs(ctx, result.ID); err == nil && len(ids) > 0 {
			result.Collections = ids
		}
	}
	if covers, ok := service.(coverService); ok {
		result.Cover = covers.CoverHandle(ctx, result.ID)
	}
	result = enrichLaunchable(ctx, service, result)
	games := []gameResult{result}
	if err := enrichCompositions(ctx, service, games); err != nil {
		return result, err
	}
	applyMeshReadiness(ctx, service, games)
	attachCoreIdentities(ctx, service, games)
	return games[0], nil
}

// coreIdentityService reads the catalog's core-entry rows. Games stay
// listable when a service has no core entries.
type coreIdentityService interface {
	CoreEntries(context.Context) ([]catalog.CoreEntry, error)
}

// stampedCore is the core entry identity a catalog row can show without a
// second library call.
type stampedCore struct {
	coreID    string
	packageID string
	mediaID   string
	mediaRole string
	mediaName string
	moreMedia []catalog.SetMedia
}

func stampOf(e catalog.CoreEntry) stampedCore {
	return stampedCore{
		coreID: strings.TrimSpace(e.CoreID), packageID: strings.TrimSpace(e.PackageID),
		mediaID: strings.TrimSpace(e.MediaID), mediaRole: strings.TrimSpace(e.MediaRole),
		mediaName: e.MediaName, moreMedia: e.MoreMedia,
	}
}

// attachFilteredCoreIdentities resolves exactly the returned rows in a batch.
// A missing or conflicting identity fails the filtered page closed.
func attachFilteredCoreIdentities(ctx context.Context, service Service, games []gameResult) error {
	if len(games) == 0 {
		return nil
	}
	reader, ok := service.(interface {
		CoreEntriesByIDs(context.Context, []string) ([]catalog.CoreEntry, error)
	})
	if !ok {
		return errors.New("filtered core identities are unavailable")
	}
	requested := make(map[string]bool)
	ids := make([]string, 0, len(games))
	collect := func(g gameResult) error {
		if !gameWantsCoreID(g) || strings.TrimSpace(g.ID) == "" {
			return errors.New("filtered row lacks a core identity")
		}
		if !requested[g.ID] {
			requested[g.ID] = true
			ids = append(ids, g.ID)
		}
		return nil
	}
	for _, g := range games {
		if err := collect(g); err != nil {
			return err
		}
		for _, v := range g.Variants {
			if err := collect(v); err != nil {
				return err
			}
		}
	}
	if len(ids) > catalog.MaxQueryLimit*(catalog.MaxVariantLimit+1) {
		return errors.New("filtered identity page exceeds its bound")
	}
	entries, err := reader.CoreEntriesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	byGame := make(map[string]stampedCore, len(entries))
	for _, e := range entries {
		if !requested[e.GameID] || strings.TrimSpace(e.CoreID) == "" {
			return errors.New("unexpected filtered core identity")
		}
		if _, ok := byGame[e.GameID]; ok {
			return errors.New("duplicate filtered core identity")
		}
		byGame[e.GameID] = stampOf(e)
	}
	if len(byGame) != len(requested) {
		return errors.New("filtered core identity is missing")
	}
	for i := range games {
		stampCoreID(&games[i], byGame)
		for j := range games[i].Variants {
			stampCoreID(&games[i].Variants[j], byGame)
		}
	}
	return nil
}

// attachCoreIdentities stamps core_id, package_id, and saved media from
// core_entries onto core-package rows, including grouped editions. A missing
// reader or a failed read leaves those fields empty and the catalog page intact.
func attachCoreIdentities(ctx context.Context, service Service, games []gameResult) {
	if !gamesNeedCoreIdentity(games) {
		return
	}
	reader, ok := service.(coreIdentityService)
	if !ok {
		return
	}
	entries, err := reader.CoreEntries(ctx)
	if err != nil {
		return
	}
	byGame := make(map[string]stampedCore, len(entries))
	for _, entry := range entries {
		gameID := strings.TrimSpace(entry.GameID)
		coreID := strings.TrimSpace(entry.CoreID)
		if gameID == "" || coreID == "" {
			continue
		}
		byGame[gameID] = stampOf(entry)
	}
	// Exact-ID detail can address a generated kit-host entry that is omitted
	// from the ordinary entry list. Preserve its launch identity on that read.
	if exact, ok := service.(interface {
		CoreEntry(context.Context, string) (catalog.CoreEntry, error)
	}); ok {
		add := func(game gameResult) {
			if !gameWantsCoreID(game) {
				return
			}
			if _, exists := byGame[game.ID]; exists {
				return
			}
			entry, err := exact.CoreEntry(ctx, game.ID)
			if err == nil && entry.GameID == game.ID {
				byGame[game.ID] = stampOf(entry)
			}
		}
		for _, game := range games {
			add(game)
			for _, variant := range game.Variants {
				add(variant)
			}
		}
	}
	for i := range games {
		stampCoreID(&games[i], byGame)
		for j := range games[i].Variants {
			stampCoreID(&games[i].Variants[j], byGame)
		}
	}
}

func gamesNeedCoreIdentity(games []gameResult) bool {
	for i := range games {
		if gameWantsCoreID(games[i]) {
			return true
		}
		for _, variant := range games[i].Variants {
			if gameWantsCoreID(variant) {
				return true
			}
		}
	}
	return false
}

func gameWantsCoreID(game gameResult) bool {
	return game.Kind == catalog.SourceKindCorePackage || game.System == catalog.CorePlatform
}

func stampCoreID(game *gameResult, byGame map[string]stampedCore) {
	if game == nil || !gameWantsCoreID(*game) {
		return
	}
	id, ok := byGame[game.ID]
	if !ok || id.coreID == "" {
		return
	}
	game.CoreID = id.coreID
	if id.packageID != "" {
		game.PackageID = id.packageID
	}
	if id.mediaID != "" {
		game.MediaID = id.mediaID
		game.MediaRole = id.mediaRole
		game.MediaName = id.mediaName
		game.MoreMedia = id.moreMedia
	}
}

type platformLaunchService interface {
	PlatformLaunchable(protocol.System) bool
}

func enrichLaunchable(ctx context.Context, service Service, result gameResult) gameResult {
	apply := func(game *gameResult) {
		if resolver, ok := service.(sessionExecutionService); ok {
			if execution, err := resolver.SessionExecution(ctx, game.ID); err == nil && execution != "" {
				game.Execution = execution
			}
		}
		game.Launchable = gameRowLaunchable(service, *game)
	}
	apply(&result)
	for i := range result.Variants {
		apply(&result.Variants[i])
	}
	return result
}

// gameRowLaunchable reports the catalog platform/target gate for one row.
// Installed FPGA packages stay launchable even when their browse system
// (Coleco, for example) is not launchable as a raw cartridge. Raw rows
// still follow PlatformLaunchable / catalog.Launchable.
func gameRowLaunchable(service Service, result gameResult) bool {
	if result.Kind == catalog.SourceKindCorePackage || result.System == catalog.CorePlatform {
		return true
	}
	if policy, ok := service.(platformLaunchService); ok {
		return policy.PlatformLaunchable(result.System)
	}
	return catalog.Launchable(result.System)
}

type compositionService interface {
	CoreCompositions(context.Context, []string) (map[string]protocol.CoreComposition, error)
}

type meshReadyService interface {
	GamesMeshReady(context.Context, []string) (map[string]fogcast.GameMeshReady, bool)
}

// applyMeshReadiness writes ReadyHere onto each row when a mesh execute
// session is installed, and writes placement when that view asked Place.
// A placement-only row, from a view with the ensure seam off, writes
// placement and omits ready_here, so composition Ready still decides the
// row. The ensure seam stays off when the service does not report one:
// launchable and composition fields stay as they are, and placement is
// omitted.
func applyMeshReadiness(ctx context.Context, service Service, games []gameResult) {
	provider, ok := service.(meshReadyService)
	if !ok || len(games) == 0 {
		return
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
		for _, variant := range game.Variants {
			ids = append(ids, variant.ID)
		}
	}
	decisions, on := provider.GamesMeshReady(ctx, ids)
	if !on {
		return
	}
	apply := func(game *gameResult) {
		decision, ok := decisions[game.ID]
		if !ok {
			return
		}
		if decision.Placement != "" {
			game.Placement = string(decision.Placement)
		}
		if decision.PlacementOnly {
			return
		}
		ready := decision.Ready
		game.ReadyHere = &ready
		if ready {
			return
		}
		game.ReadyBlock = string(decision.Block)
		game.NextAction = decision.NextAction
	}
	for i := range games {
		apply(&games[i])
		for j := range games[i].Variants {
			apply(&games[i].Variants[j])
		}
	}
}

func enrichCompositions(ctx context.Context, service Service, games []gameResult) error {
	composer, ok := service.(compositionService)
	if !ok || len(games) == 0 {
		return nil
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
		for _, variant := range game.Variants {
			ids = append(ids, variant.ID)
		}
	}
	comps, err := composer.CoreCompositions(ctx, ids)
	if err != nil {
		return err
	}
	if len(comps) == 0 {
		return nil
	}
	applyComposition := func(game *gameResult) {
		comp, ok := comps[game.ID]
		if !ok {
			return
		}
		game.FirmwareRequired = comp.FirmwareRequired
		game.PackageUnavailable = comp.PackageUnavailable
		if comp.FirmwareRequired {
			game.FirmwareReady = comp.FirmwareReady
		}
		game.ROMRequired = comp.ROMRequired
		game.ROMReady = comp.ROMReady
		game.VideoRequired = comp.VideoRequired
		game.VideoReady = comp.VideoReady
		game.ROMID = comp.ROMID
		game.ROMMediaID = comp.ROMMediaID
		game.ROMRole = comp.ROMRole
		game.ROMSourceSize = comp.ROMSourceSize
		game.ExpansionID = comp.ExpansionID
		game.ExpansionReady = comp.ExpansionReady
		game.SlotExpansions = append([]protocol.SlotExpansionStatus(nil), comp.SlotExpansions...)
	}
	for i := range games {
		applyComposition(&games[i])
		for j := range games[i].Variants {
			applyComposition(&games[i].Variants[j])
		}
	}
	return nil
}

type romCachedService interface {
	ROMCachePresence(context.Context) (map[string]bool, bool)
}

func romCachePresence(ctx context.Context, service Service) (map[string]bool, bool) {
	provider, ok := service.(romCachedService)
	if !ok {
		return nil, false
	}
	return provider.ROMCachePresence(ctx)
}

func applyROMCached(result *gameResult, game catalog.Game, presence map[string]bool, known bool) {
	if result == nil || !known {
		return
	}
	if game.Content == nil || strings.TrimSpace(game.Content.SHA256) == "" {
		return
	}
	cached := presence[string(game.System)+"/"+game.Content.SHA256]
	result.ROMCached = &cached
}
