package hostapi

import (
	"bytes"
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
	"github.com/DeanoC/FogCast/protocol"
)

// zx81TapeView is a starter cassette or a program captured by SAVE.
// MediaID is set only for a captured program, which is already in the library.
type zx81TapeView struct {
	zx81tapes.Entry
	MediaID string `json:"media_id,omitempty"`
}

type zx81SavedTapeLister interface {
	ListZX81Saves(context.Context, string) ([]catalog.ZX81Save, error)
}

type zx81TapeCoreEntryReader interface {
	CoreEntry(context.Context, string) (catalog.CoreEntry, error)
}

func registerZX81Tapes(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/library/zx81-tapes", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		tapes, ok := zx81TapeShelf(w, service, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Tapes []zx81TapeView `json:"tapes"`
		}{tapes})
	})
	mux.HandleFunc("POST /api/v1/library/zx81-tapes/{tape_id}/import", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		entry, ok := zx81tapes.Lookup(r.PathValue("tape_id"))
		if !ok {
			writeError(w, 404, "NOT_FOUND", "tape unavailable")
			return
		}
		importer, ok := service.(coreMediaLibraryService)
		if !ok {
			writeError(w, 501, "UNSUPPORTED_OPERATION", "core media library is unavailable")
			return
		}
		media, _, err := importer.ImportCoreMedia(r.Context(), int64(len(entry.Data)), bytes.NewReader(entry.Data))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, media)
	})
}

func zx81TapeShelf(w http.ResponseWriter, service Service, r *http.Request) ([]zx81TapeView, bool) {
	starters := zx81tapes.Entries()
	gameID := r.URL.Query().Get("game_id")
	if gameID != "" {
		if protocol.ValidateGameID(gameID) != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid game")
			return nil, false
		}
		reader, ok := service.(zx81TapeCoreEntryReader)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "machine identity is unavailable")
			return nil, false
		}
		entry, err := reader.CoreEntry(r.Context(), gameID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return nil, false
		}
		if entry.CoreID != "fes.zx81" {
			starters = nil
		}
	}
	tapes := make([]zx81TapeView, 0, len(starters))
	for _, entry := range starters {
		entry.Data = nil
		tapes = append(tapes, zx81TapeView{Entry: entry})
	}
	if gameID == "" {
		return tapes, true
	}
	lister, ok := service.(zx81SavedTapeLister)
	if !ok {
		return tapes, true
	}
	saves, err := lister.ListZX81Saves(r.Context(), gameID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "saved tapes are unavailable")
		return nil, false
	}
	for _, save := range saves {
		if protocol.ValidateDigest(save.MediaID) != nil || !protocol.AdmitZx81TapeSize(save.Size) || save.Name == "" {
			continue
		}
		ram := 1
		if save.Size > 1024 {
			ram = 16
		}
		tapes = append(tapes, zx81TapeView{Entry: zx81tapes.Entry{
			ID:       "saved-" + save.MediaID,
			Name:     save.Name,
			Filename: "saved.p",
			License:  "Saved",
			Controls: `After LOAD "", enter RUN.`,
			RAMKB:    ram,
			SHA256:   save.MediaID,
		}, MediaID: save.MediaID})
	}
	return tapes, true
}
