package targetclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resumeStub is an agent with no resumable local grant. It answers local
// resume with a fixed refusal, reports the configured ownership, and counts
// resume requests.
type resumeStub struct {
	mu          sync.Mutex
	ownership   KitOwnership
	plainText   bool
	resumes     atomic.Int32
	statusReads atomic.Int32
}

func (s *resumeStub) setOwnership(o KitOwnership) {
	s.mu.Lock()
	s.ownership = o
	s.mu.Unlock()
}

func (s *resumeStub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/kit/local/resume":
			s.resumes.Add(1)
			if s.plainText {
				http.Error(w, "local access required", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_REQUIRED","message":"KIT_LEASE_REQUIRED"}}`)
		case "GET /v1/kit/lease":
			s.mu.Lock()
			o := s.ownership
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(o)
		case "GET /v1/status":
			s.statusReads.Add(1)
			fmt.Fprint(w, `{"state":"idle"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newLocalObserver(t *testing.T, server *httptest.Server) (*Client, *KitLease, *url.URL) {
	t.Helper()
	base, _ := url.Parse(server.URL)
	lease := NewLocalKitLease(base, "bearer", server.Client())
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	return NewClient(base, "bearer", server.Client()).WithKitLease(lease), lease, base
}

// observeLocal runs n refresh cycles the way the kit-local host does: one
// endpoint adoption and one status read each.
func observeLocal(t *testing.T, client *Client, base *url.URL, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := client.AdoptEndpoint(context.Background(), base, false); err != nil {
			t.Fatalf("adopt endpoint %d: %v", i, err)
		}
		if status, err := client.Status(context.Background()); err != nil || status.State != "idle" {
			t.Fatalf("status %d = %+v, %v", i, status, err)
		}
	}
}

func TestLocalResumeLatchesNothingToResumeOnIdleAgent(t *testing.T) {
	stub := &resumeStub{ownership: KitOwnership{State: "free"}}
	server := stub.serve(t)
	client, _, base := newLocalObserver(t, server)

	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 1 {
		t.Fatalf("resume requests = %d, want 1 for an idle agent", got)
	}
	if got := stub.statusReads.Load(); got != 20 {
		t.Fatalf("status reads = %d, want 20", got)
	}

	// A host restart builds a new KitLease, which starts unlatched.
	restarted, _, _ := newLocalObserver(t, server)
	observeLocal(t, restarted, base, 20)
	if got := stub.resumes.Load(); got != 2 {
		t.Fatalf("resume requests after host restart = %d, want 2", got)
	}
}

func TestLocalResumeRearmsOncePerLocalOwnerGeneration(t *testing.T) {
	// A grant held under the local owner name that the agent will not
	// resume, such as a remote claim (not marked local by the agent).
	stub := &resumeStub{ownership: KitOwnership{State: "held", Owner: "fogcast@kit", Generation: "generation-1"}}
	server := stub.serve(t)
	client, _, base := newLocalObserver(t, server)

	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 1 {
		t.Fatalf("resume requests = %d, want 1 for generation-1", got)
	}
	// Another owner's generation does not re-arm resume.
	stub.setOwnership(KitOwnership{State: "held", Owner: "fogcast@host", Generation: "generation-2"})
	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 1 {
		t.Fatalf("resume requests = %d, want 1 after a foreign generation", got)
	}
	// A new local-owner generation re-arms resume exactly once.
	stub.setOwnership(KitOwnership{State: "held", Owner: "fogcast@kit", Generation: "generation-3"})
	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 2 {
		t.Fatalf("resume requests = %d, want one more for generation-3", got)
	}
	stub.setOwnership(KitOwnership{State: "free", Generation: "generation-3"})
	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 2 {
		t.Fatalf("resume requests = %d, want none once the kit is free", got)
	}
}

func TestLocalResumePlainTextForbiddenDoesNotLatch(t *testing.T) {
	stub := &resumeStub{plainText: true, ownership: KitOwnership{State: "free"}}
	server := stub.serve(t)
	client, _, base := newLocalObserver(t, server)

	observeLocal(t, client, base, 20)
	if got := stub.resumes.Load(); got != 40 {
		t.Fatalf("resume requests = %d, want 40: an untyped 403 must not latch", got)
	}
}

func TestLocalResumeRearmsWhenHeldGrantIsDropped(t *testing.T) {
	stub := &resumeStub{ownership: KitOwnership{State: "free"}}
	server := stub.serve(t)
	client, lease, base := newLocalObserver(t, server)

	observeLocal(t, client, base, 5)
	if got := stub.resumes.Load(); got != 1 {
		t.Fatalf("resume requests = %d, want 1", got)
	}
	lease.mu.Lock()
	lease.grant = kitLeaseGrant{Token: "held-token", Status: kitLeaseStatus{State: "held", Generation: "generation-1"}}
	lease.localExpiry = time.Now().Add(time.Minute)
	lease.mu.Unlock()
	// The agent reports the kit free, so adoption drops the held grant and
	// the next observation may resume once.
	observeLocal(t, client, base, 5)
	if got := stub.resumes.Load(); got != 2 {
		t.Fatalf("resume requests = %d, want one resume after dropping a held grant", got)
	}
}
