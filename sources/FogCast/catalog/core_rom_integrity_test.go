package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreROMSelectionRejectsCorruptedDigest(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	entry, err := s.CreateCoreEntry(ctx, "ROM", "fes.pong", pkg)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte("ROM bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE core_media_chunks SET data=? WHERE media_id=?`, []byte("bad bytes"), media.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "machine", 9, "", media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("corrupt binary selected: %v", err)
	}
}
