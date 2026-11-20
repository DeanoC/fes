package hostapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type atariStShelfService interface {
	AtariStShelf(context.Context) (fogcast.AtariStShelf, error)
	NewAtariStShelfDisk(context.Context, fogcast.AtariStShelfNew) (fogcast.AtariStShelfDisk, error)
	CopyAtariStShelfDisk(context.Context, string, string) (fogcast.AtariStShelfDisk, error)
	RevertAtariStShelfDisk(context.Context, string, string) (fogcast.AtariStShelfDisk, error)
	RenameAtariStShelfDisk(context.Context, string, string, string) (fogcast.AtariStShelfDisk, error)
	DeleteAtariStShelfDisk(context.Context, string, string) error
}

// A shelf name is a library title. An expected revision is the
// kept_revision the shelf listed, or empty for a disk with no saved changes.
func validShelfTitle(title string) bool {
	return strings.TrimSpace(title) != "" && len(title) <= 120
}

func validShelfRevision(revision string) bool {
	return revision == "" || corePackageIDRE.MatchString(revision)
}

func registerAtariStShelf(mux *http.ServeMux, service Service) {
	shelf := func(w http.ResponseWriter) (atariStShelfService, bool) {
		s, ok := service.(atariStShelfService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "the Atari ST disk shelf is unavailable")
		}
		return s, ok
	}
	mux.HandleFunc("GET /api/v1/library/atari-st-shelf", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		s, ok := shelf(w)
		if !ok {
			return
		}
		result, err := s.AtariStShelf(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/library/atari-st-shelf", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Title     string `json:"title"`
			PackageID string `json:"package_id"`
			Tracks    int    `json:"tracks"`
			Heads     int    `json:"heads"`
			Sectors   int    `json:"sectors"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if !validShelfTitle(request.Title) || !corePackageIDRE.MatchString(request.PackageID) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "new shelf disk request is invalid")
			return
		}
		s, ok := shelf(w)
		if !ok {
			return
		}
		disk, err := s.NewAtariStShelfDisk(r.Context(), fogcast.AtariStShelfNew{
			Title: request.Title, PackageID: request.PackageID, Tracks: request.Tracks, Heads: request.Heads, Sectors: request.Sectors,
		})
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, disk)
	})
	for _, action := range []string{"copy", "revert", "rename", "delete"} {
		mux.HandleFunc("POST /api/v1/library/atari-st-shelf/{game_id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Title            *string `json:"title"`
				ExpectedRevision *string `json:"expected_revision"`
			}
			if decodeSingleJSON(w, r, &request) != nil {
				return
			}
			gameID := r.PathValue("game_id")
			// Each action takes exactly its own fields: copy a title, revert
			// and delete a revision, rename both.
			wantTitle := action == "copy" || action == "rename"
			wantRevision := action != "copy"
			if protocol.ValidateGameID(gameID) != nil ||
				(request.Title != nil) != wantTitle || (wantTitle && !validShelfTitle(*request.Title)) ||
				(request.ExpectedRevision != nil) != wantRevision || (wantRevision && !validShelfRevision(*request.ExpectedRevision)) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "shelf disk request is invalid")
				return
			}
			s, ok := shelf(w)
			if !ok {
				return
			}
			var disk fogcast.AtariStShelfDisk
			var err error
			code := http.StatusOK
			switch action {
			case "copy":
				code = http.StatusCreated
				disk, err = s.CopyAtariStShelfDisk(r.Context(), gameID, *request.Title)
			case "revert":
				disk, err = s.RevertAtariStShelfDisk(r.Context(), gameID, *request.ExpectedRevision)
			case "rename":
				disk, err = s.RenameAtariStShelfDisk(r.Context(), gameID, *request.ExpectedRevision, *request.Title)
			case "delete":
				err = s.DeleteAtariStShelfDisk(r.Context(), gameID, *request.ExpectedRevision)
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
