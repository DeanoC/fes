package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/catalog"
)

const (
	// ShelfEmpty is the unfiltered library notice when configured roots
	// are online and contain no games.
	ShelfEmpty = "No games in this library yet."
	// ShelfMissing is the notice when every configured content root is
	// missing and the catalog has no saved rows.
	ShelfMissing = "The game files for this library can't be found."
	// ShelfOffline is the notice when saved rows remain but their files
	// are not on this machine. The kit footer uses the same sentence.
	ShelfOffline = "Offline, showing your saved list"
)

// BootLocalCatalog opens the existing FogCast service on paths, scans the
// configured library roots, and returns the shelf notice. It does not dial
// a remote target. Callers that only browse the catalog must not call
// Status or Health.
func BootLocalCatalog(ctx context.Context, paths Paths) (*Service, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	service, err := Open(ctx, paths, nil)
	if err != nil {
		return nil, "", err
	}
	if _, err := service.Scan(ctx); err != nil {
		_ = service.Close()
		return nil, "", err
	}
	return service, service.LocalShelfNotice(ctx), nil
}

// LocalShelfNotice explains an empty or offline local shelf from the last
// successful scan and the saved catalog rows. It returns an empty string
// when the shelf has playable local files, or when no scan has completed.
func (s *Service) LocalShelfNotice(ctx context.Context) string {
	if s == nil || ctx.Err() != nil {
		return ""
	}
	s.libraryMu.RLock()
	ok := s.lastScanOK
	report := s.lastScan
	s.libraryMu.RUnlock()
	if !ok {
		return ""
	}
	games, err := s.Games(ctx)
	if err != nil {
		return ""
	}
	return shelfNotice(report, games)
}

func shelfNotice(report catalog.ScanReport, games []catalog.Game) string {
	user := 0
	allUserOffline := true
	for _, game := range games {
		if game.LibraryID == catalog.BuiltinLibraryID || game.Kind == catalog.SourceKindBuiltin {
			continue
		}
		user++
		if game.RootOnline {
			allUserOffline = false
		}
	}
	roots := 0
	offlineRoots := 0
	for _, root := range report.Roots {
		roots++
		if root.Offline {
			offlineRoots++
		}
	}
	allRootsOffline := roots > 0 && offlineRoots == roots
	if user > 0 && (allRootsOffline || allUserOffline) {
		return ShelfOffline
	}
	// Built-in rows are a shelf. The empty and missing sentences are only
	// for a catalog that has no rows at all.
	if len(games) == 0 && allRootsOffline {
		return ShelfMissing
	}
	if len(games) == 0 {
		return ShelfEmpty
	}
	return ""
}
