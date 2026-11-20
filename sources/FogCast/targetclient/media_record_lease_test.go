package targetclient_test

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
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// A discard borrows the kit lease when the caller holds none. Successful and
// CAS refusal answers give it back. A busy answer may overlap another discard
// using the shared grant, and a lost reply may have removed the record, so
// both keep the lease for recovery.
func TestDiscardMediaRecordReleasesBorrowedLeaseOnlyAfterSafeAnswer(t *testing.T) {
	d := protocol.MediaRecordDiscard{
		MediaRecordIdentity: protocol.MediaRecordIdentity{CoreID: "fes.atari-st", GameID: "work-disk", BaseMediaID: strings.Repeat("b", 64)},
		ExpectedRevision:    strings.Repeat("c", 64),
	}
	for _, tc := range []struct {
		kind        string
		wantRelease int32
		wantErr     bool
	}{
		{"discarded", 1, false},
		{"busy", 0, true},
		{"stale revision", 1, true},
		{"lost reply", 0, true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var claims, releases atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/kit/claim":
					claims.Add(1)
					fmt.Fprint(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"discard-token"}`)
				case "/v1/kit/release":
					releases.Add(1)
					io.WriteString(w, `{"state":"free"}`)
				case "/v1/library/media/record/discard":
					switch tc.kind {
					case "busy":
						http.Error(w, `{"error":{"code":"BUSY","message":"a core is running"}}`, 409)
					case "stale revision":
						http.Error(w, `{"error":{"code":"STALE_REVISION","message":"stale","phase":"core_data"}}`, 409)
					case "lost reply":
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
					default:
						io.WriteString(w, `{"state":"absent","revision":"absent","bytes":0,"changed":false}`)
					}
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "discard")
			defer lease.Close(context.Background())
			client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := client.DiscardMediaRecord(ctx, d)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if claims.Load() != 1 || releases.Load() != tc.wantRelease {
				t.Fatalf("claims=%d releases=%d, want 1 and %d", claims.Load(), releases.Load(), tc.wantRelease)
			}
		})
	}
}
