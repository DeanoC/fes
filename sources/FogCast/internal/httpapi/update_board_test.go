package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/httpapi"
)

type observedReleaseBody struct {
	io.Reader
	read bool
}

func (r *observedReleaseBody) Read(p []byte) (int, error) { r.read = true; return r.Reader.Read(p) }

func TestSelectedBoardUpdateAPI(t *testing.T) {
	for _, board := range []string{release.Board, release.BoardDE25} {
		t.Run(board, func(t *testing.T) {
			h, store, candidate, token, image := updateFixtureForBoard(t, board, false, func(context.Context) error { return nil }, nil)
			stage := func(m release.Manifest) (*httptest.ResponseRecorder, bool) {
				raw, _ := json.Marshal(m)
				body := &observedReleaseBody{Reader: bytes.NewReader(image)}
				req := httptest.NewRequest(http.MethodPost, "/v1/update/stage", body)
				req.ContentLength = int64(len(image))
				req.Header.Set("Authorization", "Bearer bearer")
				req.Header.Set("X-FogCast-Kit-Lease", token)
				req.Header.Set("Content-Type", "application/octet-stream")
				req.Header.Set(httpapi.ReleaseManifestHeader, base64.StdEncoding.EncodeToString(raw))
				res := httptest.NewRecorder()
				h.ServeHTTP(res, req)
				return res, body.read
			}
			foreign := candidate
			foreign.Board = release.BoardDE25
			if board == release.BoardDE25 {
				foreign.Board = release.Board
			}
			if res, read := stage(foreign); res.Code != http.StatusBadRequest || read {
				t.Fatalf("foreign upload read=%t status=%d body=%s", read, res.Code, res.Body)
			}
			if _, err := store.Verify(candidate.ImageSHA256); err == nil {
				t.Fatal("foreign upload published an image")
			}
			if res, read := stage(candidate); res.Code != http.StatusCreated || !read {
				t.Fatalf("selected upload read=%t status=%d body=%s", read, res.Code, res.Body)
			}
			if got, err := store.Verify(candidate.ImageSHA256); err != nil || got != candidate {
				t.Fatalf("selected upload was not retained: %+v %v", got, err)
			}
			req := httptest.NewRequest(http.MethodGet, "/v1/update", nil)
			req.Header.Set("Authorization", "Bearer bearer")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			var status struct {
				Board string `json:"board"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &status); err != nil || res.Code != http.StatusOK || status.Board != board {
				t.Fatalf("selected board status: %s %v", res.Body, err)
			}
		})
	}
}
