package fogcast

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func (s *Service) libraryRootsSnapshot() []catalog.Root {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	return append([]catalog.Root(nil), s.roots...)
}

func (s *Service) libraryRoot(id string) (catalog.Root, bool) {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	root, ok := s.rootsByID[id]
	return root, ok
}

func (s *Service) catalogCloseTimeout() time.Duration {
	if s.catalogCloseWait > 0 {
		return s.catalogCloseWait
	}
	return catalogCloseScanTimeout
}

func (s *Service) beginCatalogScan() bool {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if s.closing {
		return false
	}
	s.scanWG.Add(1)
	return true
}

func (s *Service) endCatalogScan() {
	s.scanWG.Done()
}

func (s *Service) closeCatalog() error {
	s.catalogCloseOnce.Do(func() {
		s.catalogCloseErr = s.catalog.Close()
	})
	return s.catalogCloseErr
}

func (s *Service) waitForCatalogScan() bool {
	done := make(chan struct{})
	go func() {
		s.scanWG.Wait()
		close(done)
	}()
	timer := time.NewTimer(s.catalogCloseTimeout())
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *Service) Scan(ctx context.Context) (catalog.ScanReport, error) {
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return catalog.ScanReport{}, err
	}
	defer releaseSettings()
	return s.scanLocked(ctx)
}

// scanLocked scans the published library roots while library settings admission is held.
func (s *Service) scanLocked(ctx context.Context) (catalog.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return catalog.ScanReport{}, err
	}
	if !s.beginCatalogScan() {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errCatalogClosing)
	}
	defer s.endCatalogScan()
	if err := s.retireSupersededLibrariesWithAdmission(ctx); err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	report, err := s.scanner.Scan(ctx, s.libraryRootsSnapshot())
	if err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	s.rememberScan(report)
	_ = s.ScanMedia(ctx)
	return report, nil
}

// FolderWatchRoot returns the configured SMB share source of truth.
func (s *Service) FolderWatchRoot() string {
	return s.watchRoot
}

func (s *Service) folderWatchRoots() []catalog.Root {
	return FolderWatchRoots(s.libraryRootsSnapshot())
}

// ReconcileFolderWatch updates the host catalog from every configured root
// mapped by the system table. It does not scan unmapped systems or media.
func (s *Service) ReconcileFolderWatch(ctx context.Context) (catalog.ScanReport, error) {
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return catalog.ScanReport{}, err
	}
	defer releaseSettings()
	return s.reconcileFolderWatchLocked(ctx)
}

// reconcileFolderWatchLocked scans mapped roots while library settings admission is held.
func (s *Service) reconcileFolderWatchLocked(ctx context.Context) (catalog.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return catalog.ScanReport{}, err
	}
	roots := s.folderWatchRoots()
	if !s.beginCatalogScan() {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errCatalogClosing)
	}
	defer s.endCatalogScan()
	if err := s.retireSupersededLibrariesWithAdmission(ctx); err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if len(roots) == 0 {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errFolderWatchRootUnresolved)
	}
	report, err := s.scanner.Scan(ctx, roots)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	s.rememberScan(report)
	return report, nil
}

func (s *Service) rememberScan(report catalog.ScanReport) {
	copied := catalog.ScanReport{Roots: append([]catalog.RootReport(nil), report.Roots...)}
	s.libraryMu.Lock()
	s.lastScan = copied
	s.lastScanOK = true
	s.libraryMu.Unlock()
}

type catalogLibraryRetirer interface {
	Libraries(context.Context) ([]catalog.Root, error)
	RebindLibrary(context.Context, catalog.Root) error
	RetireLibrary(context.Context, string) error
}

type catalogLibraryRootReleaser interface {
	ReleaseLibraryRoot(context.Context, catalog.Root) error
}

func (s *Service) retireSupersededLibraries(ctx context.Context) error {
	retirer, ok := s.catalog.(catalogLibraryRetirer)
	if !ok {
		return nil
	}
	keep := make(map[string]catalog.Root)
	keepByID := make(map[string]catalog.Root)
	keepByPath := make(map[string]catalog.Root)
	for _, root := range s.libraryRootsSnapshot() {
		keepByPath[root.Path] = root
		if strings.TrimSpace(root.ID) != "" {
			keepByID[root.ID] = root
			keep[root.ID] = root
		}
	}
	libraries, err := retirer.Libraries(ctx)
	if err != nil {
		return err
	}
	releaser, supportsRelease := retirer.(catalogLibraryRootReleaser)
	for _, library := range libraries {
		root, adopted := keepByPath[library.Path]
		if !adopted || library.ID == root.ID && library.System == root.System {
			root, adopted = keepByID[library.ID]
			adopted = adopted && library.System != root.System
		}
		if !adopted {
			continue
		}
		if !supportsRelease {
			return errors.New("catalog library does not support releasing an adopted root")
		}
		if err := releaser.ReleaseLibraryRoot(ctx, root); err != nil {
			return err
		}
	}
	for _, library := range libraries {
		root, ok := keep[library.ID]
		if !ok || library.Path == root.Path {
			continue
		}
		if err := retirer.RebindLibrary(ctx, root); err != nil {
			return err
		}
	}
	for _, library := range libraries {
		if _, ok := keep[library.ID]; ok {
			continue
		}
		if _, adopted := keepByPath[library.Path]; adopted {
			continue
		}
		if err := retirer.RetireLibrary(ctx, library.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) retireSupersededLibrariesWithAdmission(ctx context.Context) error {
	if _, ok := s.catalog.(catalogLibraryRetirer); !ok {
		return nil
	}
	release, err := s.acquireCatalogAdmission(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.retireSupersededLibraries(ctx)
}

// RunFolderWatch reconciles every configured table-mapped root immediately
// and then on a poll interval until ctx is cancelled. Polling is SMB-safe.
// With the watch off ([library] folder_watch_seconds = 0) it only waits for
// ctx.
func (s *Service) RunFolderWatch(ctx context.Context) error {
	if s.folderWatchOff {
		<-ctx.Done()
		return nil
	}
	interval := s.folderWatchInterval
	if interval <= 0 {
		interval = catalog.DefaultFolderWatchInterval
	}
	watcher := catalog.FolderWatcher{
		Scan: func(ctx context.Context, _ []catalog.Root) (catalog.ScanReport, error) {
			return s.ReconcileFolderWatch(ctx)
		},
		Roots:    s.folderWatchRoots,
		Interval: interval,
		OnError:  s.noteFolderWatchReconcileFailure,
	}
	return watcher.Run(ctx)
}

func (s *Service) noteFolderWatchReconcileFailure() {
	s.folderWatchFailureMu.Lock()
	s.folderWatchFailureCount++
	s.folderWatchFailureMu.Unlock()
}

func (s *Service) acquireCatalogAdmission(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.catalogAdmission:
		return func() { s.catalogAdmission <- struct{}{} }, nil
	}
}

func (s *Service) acquireLibrarySettings(ctx context.Context) (func(), error) {
	s.librarySettingsOnce.Do(func() {
		s.librarySettingsAdmission = make(chan struct{}, 1)
		s.librarySettingsAdmission <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.librarySettingsAdmission:
		return func() { s.librarySettingsAdmission <- struct{}{} }, nil
	}
}

// FolderWatchReconcileFailures returns how many non-cancel reconcile
// failures the poller has seen. Transient SMB errors increment this and
// the watcher keeps retrying.
func (s *Service) FolderWatchReconcileFailures() int {
	s.folderWatchFailureMu.Lock()
	defer s.folderWatchFailureMu.Unlock()
	return s.folderWatchFailureCount
}

func (s *Service) Games(ctx context.Context) ([]catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	games, err := s.catalog.Games(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return games, nil
}

func (s *Service) Game(ctx context.Context, gameID string) (catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Game{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return catalog.Game{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	game, err := s.catalog.Game(ctx, gameID)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.Game{}, ctx.Err()
		}
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Game{}, canonicalError(protocol.CodeROMNotFound, nil)
		}
		return catalog.Game{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return game, nil
}

func (s *Service) Search(ctx context.Context, query string) ([]catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	games, err := s.catalog.Search(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return games, nil
}

func (s *Service) QueryGames(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Page{}, err
	}
	if query.CoreEntryKind != "" && query.Collection != "" {
		return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if len(query.PreferredRegions) == 0 {
		query.PreferredRegions = s.currentPreferredRegions()
	}
	switch query.Collection {
	case "favorites":
		ids, err := s.favoriteIDs(ctx)
		if err != nil {
			return catalog.Page{}, err
		}
		query.Restrict = true
		query.RestrictIDs = ids
	case "recents":
		return s.queryRecents(ctx, query)
	case "continue":
		return s.queryContinue(ctx, query)
	case "unplayed":
		ids, err := s.playedIDs(ctx)
		if err != nil {
			return catalog.Page{}, err
		}
		query.ExcludeIDs = ids
	case "recently_added":
		if query.Sort == "" || query.Sort == catalog.SortTitle {
			query.Sort = catalog.SortAdded
		}
	case "":
	default:
		return s.queryCustomCollection(ctx, query)
	}
	page, err := s.catalog.QueryGames(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.Page{}, ctx.Err()
		}
		if errors.Is(err, catalog.ErrInvalidQuery) {
			return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return page, nil
}

func (s *Service) PlatformLaunchable(system protocol.System) bool {
	return system == catalog.CorePlatform
}

func (s *Service) Platforms(ctx context.Context) ([]catalog.PlatformInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	platforms, err := s.catalog.Platforms(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	for index := range platforms {
		platforms[index].Launchable = s.PlatformLaunchable(platforms[index].ID)
	}
	return platforms, nil
}
