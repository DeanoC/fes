package fogcast

import (
	"context"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type zx81TapeCapturer interface {
	CaptureZX81Tape(context.Context, string, uint64) ([]byte, error)
}

type zx81TapeAcknowledger interface {
	AcknowledgeZX81Tape(context.Context, string, uint64) error
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

// rememberZX81Program stores a completed SAVE before Stop tears the machine
// down. The runtime retains the frozen dirty snapshot until the catalog row is
// durable and this generation-bound acknowledgement succeeds.
func (s *Service) rememberZX81Program(ctx context.Context, client serviceClient, play targetPlay) error {
	if s == nil || client == nil || play.gameID == "" || play.packageID == "" || play.packageGeneration == 0 {
		return nil
	}
	status, err := client.Status(ctx)
	if err != nil || status.CorePackage == nil || status.CorePackage.PackageID != play.packageID ||
		status.CorePackage.Generation != play.packageGeneration || !protocol.Zx81TapeWriteCapable(status.CorePackage) {
		// Runtime Stop independently refuses to retire an unacknowledged SAVE.
		return nil
	}
	failure := func(cause error) error {
		apiErr := &protocol.APIError{Code: protocol.CodeSaveFailed,
			Message: "ZX81 SAVE could not be stored; Stop is blocked so it can be retried.", Phase: "save"}
		if cause == nil {
			return apiErr
		}
		return fmt.Errorf("%w: %v", apiErr, cause)
	}
	capturer, ok := client.(zx81TapeCapturer)
	if !ok {
		return failure(nil)
	}
	data, err := capturer.CaptureZX81Tape(ctx, play.packageID, play.packageGeneration)
	if err != nil {
		return failure(err)
	}
	if len(data) == 0 {
		return nil
	}
	if !protocol.AdmitZx81TapeSize(int64(len(data))) {
		return failure(nil)
	}
	store, ok := s.catalog.(zx81SaveCatalog)
	if !ok {
		return failure(nil)
	}
	media, _, err := store.ImportCoreMedia(ctx, data)
	if err != nil || media.MediaID == "" {
		return failure(err)
	}
	if err := store.InsertZX81Save(ctx, play.gameID, media.MediaID, zx81SaveName(time.Now())); err != nil {
		return failure(err)
	}
	acknowledger, ok := client.(zx81TapeAcknowledger)
	if !ok {
		return failure(nil)
	}
	if err := acknowledger.AcknowledgeZX81Tape(ctx, play.packageID, play.packageGeneration); err != nil {
		return failure(err)
	}
	return nil
}

func zx81SaveName(now time.Time) string {
	return now.UTC().Format("Saved program 2006-01-02 15:04")
}
