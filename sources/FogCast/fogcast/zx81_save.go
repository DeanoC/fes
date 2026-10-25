package fogcast

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/zx81family"
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

type zx81CoreEntryCatalog interface {
	CoreEntry(context.Context, string) (catalog.CoreEntry, error)
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
	if s == nil {
		return zx81SaveFailure(ctx, play, "service_missing", nil)
	}
	if client == nil {
		return zx81SaveFailure(ctx, play, "target_client_missing", nil)
	}

	// Library core entries carry their selected core identity with the play, so
	// ordinary FPGA stops do not depend on a catalog read just to rule out ZX81.
	if play.system == catalog.CorePlatform {
		if play.coreID != "" {
			if !zx81family.Saves(play.coreID) {
				zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
					slog.String("reason", "core_does_not_save"),
					slog.String("core_id", play.coreID),
				)
				return nil
			}
			if play.gameID != "" {
				return s.rememberBoundZX81Program(ctx, client, play)
			}
			return s.rememberUnboundZX81Program(ctx, client, play)
		}

		// Status-adopted or otherwise unidentified FPGA plays may still have a
		// writable ZX81 package. Check their catalog binding when available, then
		// inspect the active package; only captured bytes make an unbound SAVE a
		// reason to hold Stop.
		if play.gameID != "" {
			entries, ok := s.catalog.(zx81CoreEntryCatalog)
			if ok {
				entry, err := entries.CoreEntry(ctx, play.gameID)
				if err == nil {
					if !zx81family.Saves(entry.CoreID) {
						zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
							slog.String("reason", "core_does_not_save"),
							slog.String("core_id", entry.CoreID),
						)
						return nil
					}
					return s.rememberBoundZX81Program(ctx, client, play)
				}
				zx81SaveLog(ctx, slog.LevelWarn, "ZX81 SAVE catalog identity unavailable; checking active package", play,
					slog.String("reason", "core_entry_lookup_failed"),
					slog.String("error", err.Error()),
				)
			} else {
				zx81SaveLog(ctx, slog.LevelWarn, "ZX81 SAVE catalog identity unavailable; checking active package", play,
					slog.String("reason", "core_entry_catalog_unavailable"),
				)
			}
		}
		return s.rememberUnboundZX81Program(ctx, client, play)
	}

	// A directly identified ZX81 play also needs the strict path. Unbound
	// development loads are inspected below because they have no catalog game
	// to which a pending program can be attached.
	if string(play.system) == "zx81" {
		return s.rememberBoundZX81Program(ctx, client, play)
	}
	if play.system != catalog.CorePlatform {
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "play_system_is_not_a_zx81_core_entry"),
			slog.String("observed_system", string(play.system)),
		)
		return nil
	}

	return s.rememberUnboundZX81Program(ctx, client, play)
}

func (s *Service) rememberBoundZX81Program(ctx context.Context, client serviceClient, play targetPlay) error {
	if play.gameID == "" || play.packageID == "" || play.packageGeneration == 0 {
		return zx81SaveFailure(ctx, play, "incomplete_save_binding", nil)
	}

	expectedWrite, requiredWrite, err := s.expectedZX81TapeWrite(ctx, play.packageID)
	if err != nil {
		return zx81SaveFailure(ctx, play, "expected_package_inspection_failed", err)
	}

	status, err := client.Status(ctx)
	if err != nil {
		return zx81SaveFailure(ctx, play, "status_read_failed", err,
			slog.String("observed_package_id", ""),
			slog.Uint64("observed_generation", 0),
		)
	}
	if status.CorePackage == nil {
		return zx81SaveFailure(ctx, play, "core_package_missing", nil,
			slog.String("observed_package_id", ""),
			slog.Uint64("observed_generation", 0),
			slog.Bool("observed_tape_write_capable", false),
		)
	}
	observed := status.CorePackage
	if observed.PackageID != play.packageID {
		return zx81SaveFailure(ctx, play, "package_id_mismatch", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}
	if observed.Generation != play.packageGeneration {
		return zx81SaveFailure(ctx, play, "generation_mismatch", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}

	activeWrite := protocol.Zx81TapeWriteCapable(observed)
	if activeWrite && !expectedWrite {
		return zx81SaveFailure(ctx, play, "unexpected_tape_write_capability", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Bool("expected_tape_write_declared", expectedWrite),
			slog.Bool("expected_tape_write_required", requiredWrite),
			slog.Bool("observed_tape_write_capable", activeWrite),
			slog.Any("observed_active_interfaces", observed.ActiveInterfaces),
		)
	}
	if requiredWrite && !activeWrite {
		return zx81SaveFailure(ctx, play, "required_tape_write_capability_missing", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Bool("expected_tape_write_declared", expectedWrite),
			slog.Bool("expected_tape_write_required", requiredWrite),
			slog.Bool("observed_tape_write_capable", activeWrite),
			slog.Any("observed_active_interfaces", observed.ActiveInterfaces),
		)
	}
	if !activeWrite {
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "tape_write_capability_not_active"),
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Bool("expected_tape_write_declared", expectedWrite),
			slog.Bool("expected_tape_write_required", requiredWrite),
			slog.Bool("observed_tape_write_capable", activeWrite),
			slog.Any("observed_active_interfaces", observed.ActiveInterfaces),
		)
		return nil
	}

	return s.captureAndPersistZX81Program(ctx, client, play, observed.PackageID, observed.Generation)
}

func (s *Service) rememberUnboundZX81Program(ctx context.Context, client serviceClient, play targetPlay) error {
	status, err := client.Status(ctx)
	if err != nil {
		// The host cannot confirm a pending SAVE without package status. Let
		// runtime Stop decide: it independently refuses to retire a dirty ZX81
		// snapshot, while non-ZX81 development plays remain stoppable.
		zx81SaveLog(ctx, slog.LevelWarn, "ZX81 SAVE status unavailable; allowing Stop without a confirmed pending save", play,
			slog.String("reason", "status_read_failed"),
			slog.String("error", err.Error()),
			slog.String("observed_package_id", ""),
			slog.Uint64("observed_generation", 0),
		)
		return nil
	}
	if status.CorePackage == nil {
		if play.packageID != "" || play.packageGeneration != 0 {
			return zx81SaveFailure(ctx, play, "core_package_missing", nil,
				slog.String("observed_package_id", ""),
				slog.Uint64("observed_generation", 0),
				slog.Bool("observed_tape_write_capable", false),
			)
		}
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "no_active_core_package"),
			slog.String("observed_package_id", ""),
			slog.Uint64("observed_generation", 0),
			slog.Bool("observed_tape_write_capable", false),
		)
		return nil
	}
	observed := status.CorePackage
	if play.packageID != "" && observed.PackageID != play.packageID {
		return zx81SaveFailure(ctx, play, "package_id_mismatch", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}
	if play.packageGeneration != 0 && observed.Generation != play.packageGeneration {
		return zx81SaveFailure(ctx, play, "generation_mismatch", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}
	if !protocol.Zx81TapeWriteCapable(observed) {
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "tape_write_capability_not_active"),
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Bool("observed_tape_write_capable", false),
			slog.Any("observed_active_interfaces", observed.ActiveInterfaces),
		)
		return nil
	}
	if observed.PackageID == "" || observed.Generation == 0 {
		return zx81SaveFailure(ctx, play, "invalid_observed_package_identity", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Bool("observed_tape_write_capable", true),
		)
	}

	// Capture even without a catalog game binding. An empty result proves the
	// session has no pending SAVE and can stop cleanly; bytes require blocking
	// Stop because this host cannot durably attach them to a game.
	capturer, ok := client.(zx81TapeCapturer)
	if !ok {
		return zx81SaveFailure(ctx, play, "tape_capture_unavailable", nil,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}
	data, err := capturer.CaptureZX81Tape(ctx, observed.PackageID, observed.Generation)
	if err != nil {
		return zx81SaveFailure(ctx, play, "tape_capture_failed", err,
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
		)
	}
	if len(data) == 0 {
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "no_pending_save"),
			slog.String("observed_package_id", observed.PackageID),
			slog.Uint64("observed_generation", observed.Generation),
			slog.Int("captured_bytes", 0),
		)
		return nil
	}
	return zx81SaveFailure(ctx, play, "pending_save_has_no_game_binding", nil,
		slog.String("observed_package_id", observed.PackageID),
		slog.Uint64("observed_generation", observed.Generation),
		slog.Int("captured_bytes", len(data)),
	)
}

func (s *Service) captureAndPersistZX81Program(ctx context.Context, client serviceClient, play targetPlay, packageID string, generation uint64) error {
	capturer, ok := client.(zx81TapeCapturer)
	if !ok {
		return zx81SaveFailure(ctx, play, "tape_capture_unavailable", nil,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
		)
	}
	data, err := capturer.CaptureZX81Tape(ctx, packageID, generation)
	if err != nil {
		return zx81SaveFailure(ctx, play, "tape_capture_failed", err,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
		)
	}
	if len(data) == 0 {
		zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE check skipped", play,
			slog.String("reason", "no_pending_save"),
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.Int("captured_bytes", 0),
		)
		return nil
	}
	if !protocol.AdmitZx81TapeSize(int64(len(data))) {
		return zx81SaveFailure(ctx, play, "captured_tape_size_invalid", nil,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.Int("captured_bytes", len(data)),
		)
	}
	store, ok := s.catalog.(zx81SaveCatalog)
	if !ok {
		return zx81SaveFailure(ctx, play, "save_catalog_unavailable", nil,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.Int("captured_bytes", len(data)),
		)
	}
	media, _, err := store.ImportCoreMedia(ctx, data)
	if err != nil {
		return zx81SaveFailure(ctx, play, "tape_media_import_failed", err,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.Int("captured_bytes", len(data)),
		)
	}
	if media.MediaID == "" {
		return zx81SaveFailure(ctx, play, "tape_media_import_returned_empty_id", nil,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.Int("captured_bytes", len(data)),
		)
	}
	if err := store.InsertZX81Save(ctx, play.gameID, media.MediaID, zx81SaveName(time.Now())); err != nil {
		return zx81SaveFailure(ctx, play, "save_catalog_insert_failed", err,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.String("media_id", media.MediaID),
			slog.Int("captured_bytes", len(data)),
		)
	}
	acknowledger, ok := client.(zx81TapeAcknowledger)
	if !ok {
		return zx81SaveFailure(ctx, play, "tape_acknowledgement_unavailable", nil,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.String("media_id", media.MediaID),
			slog.Int("captured_bytes", len(data)),
		)
	}
	if err := acknowledger.AcknowledgeZX81Tape(ctx, packageID, generation); err != nil {
		return zx81SaveFailure(ctx, play, "tape_acknowledgement_failed", err,
			slog.String("observed_package_id", packageID),
			slog.Uint64("observed_generation", generation),
			slog.String("media_id", media.MediaID),
			slog.Int("captured_bytes", len(data)),
		)
	}
	zx81SaveLog(ctx, slog.LevelInfo, "ZX81 SAVE persisted", play,
		slog.String("reason", "saved"),
		slog.String("observed_package_id", packageID),
		slog.Uint64("observed_generation", generation),
		slog.String("media_id", media.MediaID),
		slog.Int("captured_bytes", len(data)),
	)
	return nil
}

func (s *Service) expectedZX81TapeWrite(ctx context.Context, packageID string) (declared, required bool, err error) {
	if s.corePackages == nil {
		// An initialized service always has the verified package store. Unit
		// fixtures without one fail closed for a ZX81 entry.
		return true, true, nil
	}
	inspection, err := s.corePackages.Inspect(ctx, packageID)
	if err != nil {
		return false, false, err
	}
	for _, iface := range inspection.Descriptor.Interfaces {
		contract := protocol.Zx81TapeWriteInterface()
		if iface.ID == contract.ID {
			return true, iface.Required, nil
		}
	}
	return false, false, nil
}

func zx81SaveFailure(ctx context.Context, play targetPlay, reason string, cause error, extra ...slog.Attr) error {
	extra = append(extra, slog.String("reason", reason))
	if cause != nil {
		extra = append(extra, slog.String("error", cause.Error()))
	}
	zx81SaveLog(ctx, slog.LevelError, "ZX81 SAVE persistence blocked Stop", play, extra...)
	apiErr := &protocol.APIError{Code: protocol.CodeSaveFailed,
		Message: fmt.Sprintf("ZX81 SAVE could not be stored (%s); Stop is blocked so it can be retried.", reason), Phase: "save"}
	if cause == nil {
		return apiErr
	}
	return fmt.Errorf("%w: %v", apiErr, cause)
}

func zx81SaveLog(ctx context.Context, level slog.Level, message string, play targetPlay, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("game_id", play.gameID),
		slog.String("play_system", string(play.system)),
		slog.String("core_id", play.coreID),
		slog.String("play_execution", play.execution),
		slog.String("expected_package_id", play.packageID),
		slog.Uint64("expected_generation", play.packageGeneration),
	}
	attrs = append(attrs, extra...)
	slog.Default().LogAttrs(ctx, level, message, attrs...)
}

func zx81SaveName(now time.Time) string {
	return now.UTC().Format("Saved program 2006-01-02 15:04")
}
