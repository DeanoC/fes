package fogcast

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type zx81TapeCapturer interface {
	CaptureZX81Tape(context.Context, string, uint64) ([]byte, error)
}

type zx81SaveCatalog interface {
	ImportCoreMedia(context.Context, []byte) (catalog.CoreMedia, bool, error)
	InsertZX81Save(context.Context, string, string, string) error
	ListZX81Saves(context.Context, string) ([]catalog.ZX81Save, error)
}

// ListZX81Saves is the cassette picker's view of programs captured by SAVE.
func (s *Service) ListZX81Saves(ctx context.Context, gameID string) ([]catalog.ZX81Save, error) {
	if s == nil {
		return nil, nil
	}
	store, ok := s.catalog.(interface {
		ListZX81Saves(context.Context, string) ([]catalog.ZX81Save, error)
	})
	if !ok {
		return nil, nil
	}
	return store.ListZX81Saves(ctx, gameID)
}

// rememberZX81Program copies a completed SAVE into the host library before Stop
// tears the machine down. A machine with nothing saved, or a capture that
// fails, still stops.
func (s *Service) rememberZX81Program(ctx context.Context, client serviceClient, play targetPlay) {
	if s == nil || client == nil || play.gameID == "" || play.packageID == "" || play.packageGeneration == 0 {
		return
	}
	capturer, ok := client.(zx81TapeCapturer)
	if !ok {
		return
	}
	status, err := client.Status(ctx)
	if err != nil || status.CorePackage == nil || status.CorePackage.PackageID != play.packageID || status.CorePackage.Generation != play.packageGeneration || !protocol.Zx81TapeWriteCapable(status.CorePackage) {
		return
	}
	data, err := capturer.CaptureZX81Tape(ctx, play.packageID, play.packageGeneration)
	if err != nil || !protocol.AdmitZx81TapeSize(int64(len(data))) {
		return
	}
	store, ok := s.catalog.(zx81SaveCatalog)
	if !ok {
		return
	}
	media, _, err := store.ImportCoreMedia(ctx, data)
	if err != nil || media.MediaID == "" {
		return
	}
	_ = store.InsertZX81Save(ctx, play.gameID, media.MediaID, zx81SaveName(time.Now()))
}

func zx81SaveName(now time.Time) string {
	return now.UTC().Format("Saved program 2006-01-02 15:04")
}
