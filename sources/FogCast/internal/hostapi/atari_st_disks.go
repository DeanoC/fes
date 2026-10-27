package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type blankAtariStService interface {
	CreateBlankAtariStDisk(context.Context, fogcast.BlankAtariStRequest) (fogcast.BlankAtariStDisk, error)
}

type atariStLayout struct {
	Tracks  int   `json:"tracks"`
	Heads   int   `json:"heads"`
	Sectors int   `json:"sectors"`
	Bytes   int64 `json:"bytes"`
}

type blankAtariStDiskResult struct {
	MediaID string             `json:"media_id"`
	Size    int64              `json:"size"`
	Name    string             `json:"name"`
	Tracks  int                `json:"tracks"`
	Heads   int                `json:"heads"`
	Sectors int                `json:"sectors"`
	Created bool               `json:"created"`
	Entry   *catalog.CoreEntry `json:"entry,omitempty"`
}

func registerAtariStDisks(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/library/atari-st-disks", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		layouts := atarist.Layouts()
		out := make([]atariStLayout, 0, len(layouts))
		for _, layout := range layouts {
			out = append(out, atariStLayout{
				Tracks: layout.Tracks, Heads: layout.Heads, Sectors: layout.Sectors,
				Bytes: int64(layout.Tracks) * int64(layout.Heads) * int64(layout.Sectors) * 512,
			})
		}
		writeJSON(w, http.StatusOK, struct {
			Layouts []atariStLayout `json:"layouts"`
		}{out})
	})
	mux.HandleFunc("POST /api/v1/library/atari-st-disks", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name              string  `json:"name"`
			Tracks            int     `json:"tracks"`
			Heads             int     `json:"heads"`
			Sectors           int     `json:"sectors"`
			Bind              bool    `json:"bind"`
			GameID            string  `json:"game_id"`
			ExpectedPackageID string  `json:"expected_package_id"`
			ExpectedMediaID   *string `json:"expected_media_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.Name == "" || !blankAtariStBinding(request.Bind, request.GameID, request.ExpectedPackageID, request.ExpectedMediaID) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "blank ST disk request is invalid")
			return
		}
		creator, ok := service.(blankAtariStService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "blank ST disks are unavailable")
			return
		}
		expected := ""
		if request.ExpectedMediaID != nil {
			expected = *request.ExpectedMediaID
		}
		disk, err := creator.CreateBlankAtariStDisk(r.Context(), fogcast.BlankAtariStRequest{
			Name: request.Name, Tracks: request.Tracks, Heads: request.Heads, Sectors: request.Sectors,
			GameID: request.GameID, ExpectedPackageID: request.ExpectedPackageID, ExpectedMediaID: expected, Bind: request.Bind,
		})
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		code := http.StatusOK
		if disk.Created {
			code = http.StatusCreated
		}
		writeJSON(w, code, blankAtariStDiskResult{
			MediaID: disk.MediaID, Size: disk.Size, Name: disk.Name,
			Tracks: disk.Tracks, Heads: disk.Heads, Sectors: disk.Sectors, Created: disk.Created, Entry: disk.Entry,
		})
	})
}

func blankAtariStBinding(bind bool, game, pkg string, media *string) bool {
	if !bind {
		return game == "" && pkg == "" && media == nil
	}
	if protocol.ValidateGameID(game) != nil || !corePackageIDRE.MatchString(pkg) || media == nil {
		return false
	}
	return *media == "" || corePackageIDRE.MatchString(*media)
}
