// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"context"
	"io/fs"
	"testing"
	"time"
)

func TestVolumeTimestampsAndDefaultClock(t *testing.T) {
	_, v := newSparseFAT32(t, 8)
	ctx := context.Background()

	// Without SetClock, entries get the legacy 1980-01-01 00:00:00 UTC date.
	if err := v.CreateFile(ctx, "DEFAULT.TXT", []byte("a")); err != nil {
		t.Fatalf("CreateFile default: %v", err)
	}
	fi, err := fs.Stat(v, "DEFAULT.TXT")
	if err != nil {
		t.Fatalf("Stat default: %v", err)
	}
	wantDefault := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	if !fi.ModTime().Equal(wantDefault) {
		t.Fatalf("default ModTime=%v, want %v", fi.ModTime(), wantDefault)
	}

	// With SetClock, CreateFile, Mkdir, and WriteFile stamp UTC DOS timestamps
	// (2-second resolution in write time).
	createdAt := time.Date(2026, time.November, 22, 19, 45, 37, 500_000_000, time.UTC)
	current := createdAt
	v.SetClock(func() time.Time { return current })

	if err := v.Mkdir(ctx, "SAVES"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := v.CreateFile(ctx, "SAVES/GAME.SAV", []byte("hello world")); err != nil {
		t.Fatalf("CreateFile stamped: %v", err)
	}
	dirInfo, err := fs.Stat(v, "SAVES")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	wantCreated := time.Date(2026, time.November, 22, 19, 45, 36, 0, time.UTC)
	if !dirInfo.ModTime().Equal(wantCreated) {
		t.Fatalf("dir ModTime=%v, want %v", dirInfo.ModTime(), wantCreated)
	}

	fileInfo, err := fs.Stat(v, "SAVES/GAME.SAV")
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if !fileInfo.ModTime().Equal(wantCreated) {
		t.Fatalf("file ModTime=%v, want %v", fileInfo.ModTime(), wantCreated)
	}

	// WriteFile updates write timestamp when clock is set.
	updatedAt := time.Date(2026, time.December, 1, 8, 12, 14, 0, time.UTC)
	current = updatedAt
	if err := v.WriteFile("SAVES/GAME.SAV", []byte("updated!")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fileInfo, err = fs.Stat(v, "SAVES/GAME.SAV")
	if err != nil {
		t.Fatalf("Stat updated: %v", err)
	}
	if !fileInfo.ModTime().Equal(updatedAt) {
		t.Fatalf("updated ModTime=%v, want %v", fileInfo.ModTime(), updatedAt)
	}

	// Clearing clock restores default behavior.
	v.SetClock(nil)
	if err := v.CreateFile(ctx, "SAVES/AFTER.TXT", []byte("x")); err != nil {
		t.Fatalf("CreateFile after nil clock: %v", err)
	}
	afterInfo, err := fs.Stat(v, "SAVES/AFTER.TXT")
	if err != nil {
		t.Fatalf("Stat after: %v", err)
	}
	if !afterInfo.ModTime().Equal(wantDefault) {
		t.Fatalf("after nil clock ModTime=%v, want %v", afterInfo.ModTime(), wantDefault)
	}
}

func TestFATTimestampClampingAndInvalidDecode(t *testing.T) {
	// Pre-1980 clamps to 1980-01-01 00:00:00.
	d, ct, tenths := encodeFATTimestamp(time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC))
	if d != 0x0021 || ct != 0 || tenths != 0 {
		t.Fatalf("pre-1980=(%#x, %#x, %d), want (0x21, 0, 0)", d, ct, tenths)
	}
	// Post-2107 clamps to 2107-12-31 23:59:58 (plus 199 tenths).
	d, ct, tenths = encodeFATTimestamp(time.Date(2200, time.June, 15, 10, 0, 0, 0, time.UTC))
	gotMax := decodeFATTimestamp(d, ct)
	wantMax := time.Date(2107, time.December, 31, 23, 59, 58, 0, time.UTC)
	if !gotMax.Equal(wantMax) || tenths != 199 {
		t.Fatalf("post-2107=(%v, %d), want (%v, 199)", gotMax, tenths, wantMax)
	}
	// Invalid dates decode to zero time.
	if !decodeFATTimestamp(0, 0).IsZero() {
		t.Fatal("zero date should decode to zero time")
	}
	if !decodeFATTimestamp(uint16(13<<5|1), 0).IsZero() {
		t.Fatal("month 13 should decode to zero time")
	}
	// Feb 31 rolls over in time.Date and must decode to zero time.
	if !decodeFATTimestamp(uint16((2026-1980)<<9|2<<5|31), 0).IsZero() {
		t.Fatal("Feb 31 should decode to zero time")
	}
}
