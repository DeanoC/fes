package hostapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

// diskShelfService is one shelf per computer family, named by its core ID.
type diskShelfService interface {
	DiskShelf(ctx context.Context, coreID string) (fogcast.DiskShelf, error)
	NewShelfDisk(ctx context.Context, coreID string, req fogcast.ShelfNewDisk) (fogcast.ShelfDisk, error)
	CopyShelfDisk(ctx context.Context, coreID, gameID, title string) (fogcast.ShelfDisk, error)
	RevertShelfDisk(ctx context.Context, coreID, gameID, expectedRevision string) (fogcast.ShelfDisk, error)
	RenameShelfDisk(ctx context.Context, coreID, gameID, expectedRevision, title string) (fogcast.ShelfDisk, error)
	DeleteShelfDisk(ctx context.Context, coreID, gameID, expectedRevision string) error
}

// A shelf name is a library title. An expected revision is the
// kept_revision the shelf listed, or empty for a disk with no saved changes.
func validShelfTitle(title string) bool {
	return strings.TrimSpace(title) != "" && len(title) <= 120
}

func validShelfRevision(revision string) bool {
	return revision == "" || corePackageIDRE.MatchString(revision)
}

// shelfCoreRE is a core ID in a shelf route, as media records spell one.
// The service decides which cores have a shelf.
var shelfCoreRE = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

func validShelfCore(coreID string) bool {
	return shelfCoreRE.MatchString(coreID)
}

func registerDiskShelf(mux *http.ServeMux, service Service) {
	shelf := func(w http.ResponseWriter) (diskShelfService, bool) {
		s, ok := service.(diskShelfService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "the disk shelf is unavailable")
		}
		return s, ok
	}
	mux.HandleFunc("GET /api/v1/library/disk-shelves/{core_id}", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		coreID := r.PathValue("core_id")
		if !validShelfCore(coreID) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "shelf request is invalid")
			return
		}
		s, ok := shelf(w)
		if !ok {
			return
		}
		result, err := s.DiskShelf(r.Context(), coreID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/library/disk-shelves/{core_id}", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Title     string `json:"title"`
			PackageID string `json:"package_id"`
			Contents  string `json:"contents"`
			Tracks    int    `json:"tracks"`
			Heads     int    `json:"heads"`
			Sectors   int    `json:"sectors"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		coreID := r.PathValue("core_id")
		if !validShelfCore(coreID) || !validShelfTitle(request.Title) || !corePackageIDRE.MatchString(request.PackageID) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "new shelf disk request is invalid")
			return
		}
		s, ok := shelf(w)
		if !ok {
			return
		}
		disk, err := s.NewShelfDisk(r.Context(), coreID, fogcast.ShelfNewDisk{
			Title: request.Title, PackageID: request.PackageID, Contents: request.Contents,
			Tracks: request.Tracks, Heads: request.Heads, Sectors: request.Sectors,
		})
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, disk)
	})
	for _, action := range []string{"copy", "revert", "rename", "delete"} {
		mux.HandleFunc("POST /api/v1/library/disk-shelves/{core_id}/{game_id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Title            *string `json:"title"`
				ExpectedRevision *string `json:"expected_revision"`
			}
			if decodeSingleJSON(w, r, &request) != nil {
				return
			}
			coreID, gameID := r.PathValue("core_id"), r.PathValue("game_id")
			// Each action takes exactly its own fields: copy a title, revert
			// and delete a revision, rename both.
			wantTitle := action == "copy" || action == "rename"
			wantRevision := action != "copy"
			if !validShelfCore(coreID) || protocol.ValidateGameID(gameID) != nil ||
				(request.Title != nil) != wantTitle || (wantTitle && !validShelfTitle(*request.Title)) ||
				(request.ExpectedRevision != nil) != wantRevision || (wantRevision && !validShelfRevision(*request.ExpectedRevision)) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "shelf disk request is invalid")
				return
			}
			s, ok := shelf(w)
			if !ok {
				return
			}
			var disk fogcast.ShelfDisk
			var err error
			code := http.StatusOK
			switch action {
			case "copy":
				code = http.StatusCreated
				disk, err = s.CopyShelfDisk(r.Context(), coreID, gameID, *request.Title)
			case "revert":
				disk, err = s.RevertShelfDisk(r.Context(), coreID, gameID, *request.ExpectedRevision)
			case "rename":
				disk, err = s.RenameShelfDisk(r.Context(), coreID, gameID, *request.ExpectedRevision, *request.Title)
			case "delete":
				err = s.DeleteShelfDisk(r.Context(), coreID, gameID, *request.ExpectedRevision)
			}
			if err != nil {
				writeCoreLibraryError(w, err)
				return
			}
			if action == "delete" {
				writeJSON(w, http.StatusOK, map[string]string{"game_id": gameID})
				return
			}
			writeJSON(w, code, disk)
		})
	}
}
