package hostclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestImportCoreMediaAndSelectHouseholdFirmware(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	wantID := fmt.Sprintf("%x", sha256.Sum256(payload))
	var puts int
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/core-media":
			if r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength != protocol.FirmwareBytes {
				t.Errorf("import headers = %q len=%d", r.Header.Get("Content-Type"), r.ContentLength)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			gotBody = body
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(CoreMedia{MediaID: wantID, Size: int64(len(body))})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/firmware":
			_ = json.NewEncoder(w).Encode(CoreFirmware{Slot: protocol.FirmwareRole})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/firmware":
			puts++
			var req struct {
				Slot    string `json:"slot"`
				MediaID string `json:"media_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode put: %v", err)
			}
			if req.Slot != protocol.FirmwareRole || req.MediaID != wantID {
				t.Errorf("put = %+v", req)
			}
			_ = json.NewEncoder(w).Encode(CoreFirmware{Slot: protocol.FirmwareRole, MediaID: wantID, Size: protocol.FirmwareBytes})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	media, err := client.ImportCoreMedia(context.Background(), protocol.FirmwareBytes, bytes.NewReader(payload))
	if err != nil || media.MediaID != wantID || media.Size != protocol.FirmwareBytes {
		t.Fatalf("import = %+v err=%v", media, err)
	}
	if !bytes.Equal(gotBody, payload) {
		t.Fatalf("stored %d bytes", len(gotBody))
	}
	empty, err := client.HouseholdFirmware(context.Background())
	if err != nil || empty.MediaID != "" || empty.Slot != protocol.FirmwareRole {
		t.Fatalf("get empty = %+v err=%v", empty, err)
	}
	slot, err := client.SelectHouseholdFirmware(context.Background(), wantID)
	if err != nil || slot.MediaID != wantID || slot.Size != protocol.FirmwareBytes || puts != 1 {
		t.Fatalf("select = %+v puts=%d err=%v", slot, puts, err)
	}
}

func TestImportCoreMediaRejectsEmptyAndMismatchedIdentity(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1", nil)
	if _, err := client.ImportCoreMedia(context.Background(), 0, strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty err = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(CoreMedia{MediaID: strings.Repeat("a", 64), Size: 3})
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).ImportCoreMedia(context.Background(), 4, strings.NewReader("abcd")); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("mismatch err = %v", err)
	}
}
