package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestSchemaNineAddsFirmwareSlot(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatalf("user_version = %d, %v", version, err)
	}
	graphics, err := s.CreateCoreEntry(ctx, "Graphics I", "fes.coleco", strings.Repeat("a", 64))
	if err != nil || graphics.FirmwareRequired {
		t.Fatalf("graphics: %+v, %v", graphics, err)
	}
	frogger, err := s.CreateFirmwareRequiredEntry(ctx, "Frogger", "fes.coleco", strings.Repeat("a", 64), "", "")
	if err != nil || !frogger.FirmwareRequired {
		t.Fatalf("frogger: %+v, %v", frogger, err)
	}
	got, err := s.CoreEntry(ctx, frogger.GameID)
	if err != nil || got != frogger {
		t.Fatalf("reload frogger: %+v, %v", got, err)
	}
	filled, err := s.HouseholdFirmwareFilled(ctx)
	if err != nil || filled {
		t.Fatalf("empty household: %v filled=%v", err, filled)
	}
}

func TestHouseholdFirmwareIsContentAddressedAndSized(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	wrong, _, err := s.ImportCoreMedia(ctx, []byte("not-8kib"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, wrong.MediaID); !errors.Is(err, ErrInvalidCoreFirmware) {
		t.Fatalf("short firmware: %v", err)
	}
	data := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	media, created, err := s.ImportCoreMedia(ctx, data)
	if err != nil || !created || media.Size != protocol.FirmwareBytes || media.MediaID != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("import = %+v created=%v err=%v", media, created, err)
	}
	if _, err := s.SelectCoreFirmware(ctx, "bios", media.MediaID); !errors.Is(err, ErrInvalidCoreFirmware) {
		t.Fatalf("unknown slot: %v", err)
	}
	selected, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, media.MediaID)
	if err != nil || selected.MediaID != media.MediaID || selected.Size != protocol.FirmwareBytes {
		t.Fatalf("select = %+v, %v", selected, err)
	}
	filled, err := s.HouseholdFirmwareFilled(ctx)
	if err != nil || !filled {
		t.Fatalf("filled=%v err=%v", filled, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.CoreFirmware(ctx, protocol.FirmwareRole)
	if err != nil || got != selected {
		t.Fatalf("reopened = %+v, %v", got, err)
	}
	cleared, err := reopened.SelectCoreFirmware(ctx, protocol.FirmwareRole, "")
	if err != nil || cleared.MediaID != "" {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
	filled, err = reopened.HouseholdFirmwareFilled(ctx)
	if err != nil || filled {
		t.Fatalf("cleared filled=%v err=%v", filled, err)
	}
}
