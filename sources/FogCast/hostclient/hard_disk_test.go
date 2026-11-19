package hostclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestImportHardDiskMediaRejectsInvalidBounds(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", nil)
	for _, size := range []int64{0, protocol.AtaDiskMinBytes - 512, protocol.AtaDiskMinBytes + 1, protocol.AtaDiskMaxBytes + 512} {
		if _, err := client.ImportHardDiskMedia(context.Background(), size, strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "whole 512-byte sectors") {
			t.Fatalf("invalid size %d: %v", size, err)
		}
	}
	if _, err := client.ImportCoreMedia(context.Background(), 64<<20, strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ordinary media limit widened: %v", err)
	}
}

func TestHardDiskImportUsesScopedDeadlineAndValidatesReply(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "stored", true: "size mismatch"}[mismatch], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/core-media/hard-disk" || r.Method != http.MethodPost {
					t.Errorf("request %s %s", r.Method, r.URL)
				}
				io.Copy(io.Discard, r.Body)
				time.Sleep(20 * time.Millisecond)
				size := protocol.AtaDiskMinBytes
				if mismatch {
					size--
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(CoreMedia{MediaID: strings.Repeat("a", 64), Size: size})
			}))
			defer server.Close()
			httpClient := server.Client()
			httpClient.Timeout = 5 * time.Millisecond
			client := NewClient(server.URL, httpClient)
			_, err := client.ImportHardDiskMedia(context.Background(), protocol.AtaDiskMinBytes, strings.NewReader(strings.Repeat("x", int(protocol.AtaDiskMinBytes))))
			if (err != nil) != mismatch {
				t.Fatalf("mismatch=%v err=%v", mismatch, err)
			}
			if client.HTTPClient().Timeout != 5*time.Millisecond || client.LaunchHTTPClient().Timeout != launchHTTPTimeout {
				t.Fatal("hard-disk import changed shared client timeouts")
			}
		})
	}
}
