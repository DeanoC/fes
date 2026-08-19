package librarymedia_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/librarymedia"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestScanIndexesGameIDAndStemAndIgnoresSymlinks(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	cache := t.TempDir()
	outside := t.TempDir()
	pngBytes := tinyPNG(t)
	gameDir := filepath.Join(mediaRoot, "snes", "snes-mario-test")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "cover.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	stemDir := filepath.Join(mediaRoot, "snes", "mario")
	if err := os.MkdirAll(stemDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stemDir, "marquee.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(gameDir, "backdrop.png")); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), cache, []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	media, err := index.GameMedia(ctx, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if media.Cover == "" || media.Marquee == "" {
		t.Fatalf("media = %+v", media)
	}
	if media.Backdrop != "" {
		t.Fatal("symlink backdrop was indexed")
	}
	opened, err := index.Open(ctx, media.Cover)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Reader.Close()
	if opened.MIME != librarymedia.MIMEPNG && opened.MIME != librarymedia.MIMEJPEG {
		t.Fatalf("mime = %s", opened.MIME)
	}
	response := httptest.NewRecorder()
	librarymedia.Serve(response, httptest.NewRequest(http.MethodGet, "/media", nil), opened)
	if response.Code != http.StatusOK {
		t.Fatalf("serve = %d", response.Code)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
}

func TestScanAssignsIDPathOverCollidingStem(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	pngBytes := tinyPNG(t)
	idDir := filepath.Join(mediaRoot, "snes", "mario")
	if err := os.MkdirAll(idDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idDir, "cover.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), t.TempDir(), []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	idGame := catalog.Game{ID: "mario", Title: "Mario ID", System: protocol.SystemSNES, RelativePath: "mario-world.sfc"}
	stemGame := catalog.Game{ID: "snes-other-test", Title: "Other", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{idGame, stemGame}); err != nil {
		t.Fatal(err)
	}
	idMedia, err := index.GameMedia(ctx, idGame.ID)
	if err != nil || idMedia.Cover == "" {
		t.Fatalf("id game media = %+v, %v", idMedia, err)
	}
	stemMedia, err := index.GameMedia(ctx, stemGame.ID)
	if err != nil || stemMedia.Cover != "" {
		t.Fatalf("stem game must not take colliding ID path: %+v, %v", stemMedia, err)
	}
}

func TestScanFailureDoesNotWipeExistingIndex(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	cache := t.TempDir()
	pngBytes := tinyPNG(t)
	gameDir := filepath.Join(mediaRoot, "snes", "snes-mario-test")
	if err := os.MkdirAll(gameDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "cover.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), cache, []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	before, err := index.GameMedia(ctx, game.ID)
	if err != nil || before.Cover == "" {
		t.Fatalf("before = %+v, %v", before, err)
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := index.Scan(ctx, []catalog.Game{game}); err == nil {
		t.Fatal("expected cache write failure")
	}
	after, err := index.GameMedia(ctx, game.ID)
	if err != nil || after.Cover != before.Cover {
		t.Fatalf("failed scan wiped index: before=%+v after=%+v err=%v", before, after, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := index.Scan(canceled, []catalog.Game{game}); err == nil {
		t.Fatal("expected canceled scan to fail")
	}
	still, err := index.GameMedia(ctx, game.ID)
	if err != nil || still.Cover != before.Cover {
		t.Fatalf("canceled scan wiped index: %+v, %v", still, err)
	}
}

func TestScanRejectsOversizedAndUnknownRoles(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	dir := filepath.Join(mediaRoot, "snes", "snes-mario-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manual.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.png"), bytes.Repeat([]byte("x"), librarymedia.MaxStillBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), t.TempDir(), []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	media, err := index.GameMedia(ctx, game.ID)
	if err != nil || media.Cover != "" {
		t.Fatalf("media = %+v, %v", media, err)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestScanRejectsWrongMIMEAndServesVideoRange(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	dir := filepath.Join(mediaRoot, "snes", "snes-mario-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("not-an-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	video := make([]byte, 64)
	copy(video[4:8], "ftyp")
	if err := os.WriteFile(filepath.Join(dir, "video.mp4"), video, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), t.TempDir(), []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	media, err := index.GameMedia(ctx, game.ID)
	if err != nil || media.Cover != "" || media.Video == "" {
		t.Fatalf("media = %+v, %v", media, err)
	}
	opened, err := index.Open(ctx, media.Video)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Reader.Close()
	request := httptest.NewRequest(http.MethodGet, "/media", nil)
	request.Header.Set("Range", "bytes=0-7")
	response := httptest.NewRecorder()
	librarymedia.Serve(response, request, opened)
	if response.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d body=%s", response.Code, response.Body.String())
	}
	if response.Body.Len() != 8 {
		t.Fatalf("range body length = %d", response.Body.Len())
	}
}

func TestGameMediaPrefersGameIDPathThenEarlierRoot(t *testing.T) {
	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	pngA := tinyPNG(t)
	pngB := tinyPNGShifted(t)
	if err := os.MkdirAll(filepath.Join(rootA, "snes", "mario"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "snes", "mario", "cover.png"), pngA, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootB, "snes", "snes-mario-test"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "snes", "snes-mario-test", "cover.png"), pngB, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootA, "snes", "snes-mario-test"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "snes", "snes-mario-test", "marquee.png"), pngA, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "snes", "mario", "marquee.png"), pngB, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), t.TempDir(), []librarymedia.Root{
		{ID: "media-a", Path: rootA},
		{ID: "media-b", Path: rootB},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	media, err := index.GameMedia(ctx, game.ID)
	if err != nil || media.Cover == "" || media.Marquee == "" {
		t.Fatalf("media = %+v, %v", media, err)
	}
	cover, err := index.Lookup(ctx, media.Cover)
	if err != nil {
		t.Fatal(err)
	}
	if cover.RootID != "media-b" || cover.Rel != "snes/snes-mario-test/cover.png" {
		t.Fatalf("cover first-hit = %+v", cover)
	}
	marquee, err := index.Lookup(ctx, media.Marquee)
	if err != nil {
		t.Fatal(err)
	}
	if marquee.Rel != "snes/snes-mario-test/marquee.png" {
		t.Fatalf("marquee first-hit = %+v", marquee)
	}
}

func TestOpenRevalidatesIndexedVideo(t *testing.T) {
	ctx := context.Background()
	mediaRoot := t.TempDir()
	dir := filepath.Join(mediaRoot, "snes", "snes-mario-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	video := make([]byte, 64)
	copy(video[4:8], "ftyp")
	path := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(path, video, 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := librarymedia.Open(ctx, filepath.Join(t.TempDir(), "media.sqlite3"), t.TempDir(), []librarymedia.Root{{ID: "media-main", Path: mediaRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	game := catalog.Game{ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES, RelativePath: "mario.sfc"}
	if err := index.Scan(ctx, []catalog.Game{game}); err != nil {
		t.Fatal(err)
	}
	media, err := index.GameMedia(ctx, game.ID)
	if err != nil || media.Video == "" {
		t.Fatalf("media = %+v, %v", media, err)
	}
	changed := make([]byte, 64)
	copy(changed[4:8], "ftyp")
	changed[15] = 0x7F
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Open(ctx, media.Video); err == nil {
		t.Fatal("mutated video was served")
	}
}

func tinyPNGShifted(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
