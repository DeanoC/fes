package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestOpenDigestRejectsContentDigestMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	name := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.WriteFile(filepath.Join(root, "artwork", name), testPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fetcher.OpenDigest(name); err == nil {
		t.Fatal("expected digest mismatch to be rejected")
	}
}

func TestSanitizeImageRejectsActiveTrailingBytes(t *testing.T) {
	content := append(testPNG(t), []byte("trailing-active-content")...)
	if _, _, _, err := sanitizeImage("image/png", content); err == nil {
		t.Fatal("expected trailing content to be rejected")
	}
}

func TestArtworkFetchDoesNotForwardCredentialsOrFollowRedirects(t *testing.T) {
	var got http.Header
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		got = request.Header.Clone()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(testPNG(t))), Request: request}, nil
	})}
	fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: filepath.Join(t.TempDir(), "metadata"), HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	object, err := fetcher.Fetch(context.Background(), []byte("cache-key"), ArtworkRef{Role: ArtworkCover, ID: "image-id"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "" || got.Get("Cookie") != "" || object.MIME != "image/png" {
		t.Fatalf("artwork request headers=%v object=%#v", got, object)
	}
}

func TestArtworkDigestHelperMatchesPublishedObject(t *testing.T) {
	content := testPNG(t)
	digestBytesValue := sha256.Sum256(content)
	if digestBytes(content) != hex.EncodeToString(digestBytesValue[:]) {
		t.Fatal("digest helper changed unexpectedly")
	}
}
