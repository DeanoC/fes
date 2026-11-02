package targetclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/appliance"
)

type selectedBoardUpload struct{ read atomic.Bool }

func (r *selectedBoardUpload) Read([]byte) (int, error) { r.read.Store(true); return 0, io.EOF }

func TestSelectedBoardRejectsForeignTargetBeforeUpload(t *testing.T) {
	for _, reported := range []string{"", appliance.Board, "unknown"} {
		t.Run("reported-"+reported, func(t *testing.T) {
			var posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts.Add(1)
				}
				switch r.URL.Path {
				case "/v1/health":
					fmt.Fprint(w, `{"target_id":"kit","boot_id":"old"}`)
				case "/v1/update":
					fmt.Fprintf(w, `{"board":%q,"boot_id":"old","image_sha256":%q,"raw_idle_ready":true}`, reported, strings.Repeat("f", 64))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			c := NewClient(u, "secret", srv.Client())
			m := updateManifest(strings.Repeat("x", 4096))
			m.Board = appliance.BoardDE25
			body := &selectedBoardUpload{}
			if _, err := c.UpdateApplianceForBoard(context.Background(), "kit", m, body, nil, appliance.BoardDE25); err == nil || !strings.Contains(err.Error(), "board differs") {
				t.Fatalf("wrong target accepted: %v", err)
			}
			if posts.Load() != 0 || body.read.Load() {
				t.Fatalf("foreign target mutated or consumed body: posts=%d read=%t", posts.Load(), body.read.Load())
			}
			if err := c.StageAppliance(context.Background(), m, body); err == nil || body.read.Load() {
				t.Fatal("DE10 default staging admitted DE25")
			}
		})
	}
}
