package metadata

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 42, A: 255})
		}
	}
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestArtworkFetcherConstructsFixedURLAndSanitizesPNGAtomically(t *testing.T) {
	root := t.TempDir()
	body := pngFixture(t, 2, 3)
	var request *http.Request
	client := &http.Client{Transport: roundTripFunc(func(got *http.Request) (*http.Response, error) {
		request = got
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: got}, nil
	})}
	fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: root, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	object, err := fetcher.Fetch(context.Background(), []byte("cache-key"), ArtworkRef{Role: ArtworkCover, ID: "image_01"})
	if err != nil {
		t.Fatal(err)
	}
	if object.MIME != "image/png" || object.Width != 2 || object.Height != 3 || len(object.Handle) != 64 || len(object.Digest) != 64 {
		t.Fatalf("object = %#v", object)
	}
	if request.URL.String() != "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/image_01.jpg" {
		t.Fatalf("artwork URL = %s", request.URL.Redacted())
	}
	if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		t.Fatalf("artwork request forwarded credentials: %v", request.Header)
	}
	path := filepath.Join(root, "artwork", object.Digest)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("stored object mode/type = %s", info.Mode())
	}
	opened, err := fetcher.OpenDigest(object.Digest)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Reader.Close()
	stored, _ := io.ReadAll(opened.Reader)
	if !bytes.Equal(stored, body) {
		t.Fatalf("stored bytes changed unexpectedly: got %d want %d", len(stored), len(body))
	}
	entries, _ := os.ReadDir(filepath.Join(root, "artwork"))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary artwork file remained: %s", entry.Name())
		}
	}
}

func TestArtworkFetcherRejectsUnsafeIDsMIMEAndDimensions(t *testing.T) {
	for _, id := range []string{"../escape", "a/b", "", strings.Repeat("a", 129), "image?id=1"} {
		if _, err := ArtworkURL(ArtworkCover, id); err == nil {
			t.Fatalf("unsafe image ID accepted: %q", id)
		}
	}
	root := t.TempDir()
	for name, response := range map[string]struct {
		mime string
		body []byte
	}{
		"wrong mime": {mime: "text/html", body: pngFixture(t, 2, 2)},
		"mismatch":   {mime: "image/jpeg", body: pngFixture(t, 2, 2)},
		"oversize":   {mime: "image/png", body: append(bytes.Repeat([]byte{0}, maxArtworkCompressed), 1)},
		"invalid":    {mime: "image/png", body: []byte("not an image")},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{response.mime}}, Body: io.NopCloser(bytes.NewReader(response.body)), Request: request}, nil
			})}
			fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: filepath.Join(root, name), HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = fetcher.Fetch(context.Background(), []byte("key"), ArtworkRef{Role: ArtworkBackdrop, ID: "safe-id"})
			if err == nil || opCode(err) != ErrInvalidResponse {
				t.Fatalf("err = %v code=%s", err, opCode(err))
			}
		})
	}
}
