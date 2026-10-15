package softwareagent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	contract "github.com/DeanoC/FogCast/kitlease"
)

// TestSupervisorFixture is a subprocess seam for the platform supervisor tests.
// It runs the production admission handler and real lease state machine without
// hardware. Ordinary test runs do not start a server or mutate persistent state.
func TestSupervisorFixture(t *testing.T) {
	socket := os.Getenv("FES_SOFTWARE_TEST_SOCKET")
	if socket == "" {
		return
	}
	root := os.Getenv("FES_SOFTWARE_TEST_ROOT")
	if root == "" {
		t.Fatal("FES_SOFTWARE_TEST_ROOT required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	manager := kitlease.New(time.Minute, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return nil
		}
	}, kitlease.WithMaintenanceFile(filepath.Join(root, "maintenance.json"), filepath.Join(root, "maintenance.lock"), strings.TrimSpace(string(boot))))
	defer manager.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	bundle := os.Getenv("FES_SOFTWARE_TEST_BUNDLE_ID")
	if bundle == "" {
		bundle = "fixture"
	}
	admission := &Admission{Leases: manager, Fence: NewFence(false), Ready: func(context.Context) bool { return true }, UpdateBlocked: func(context.Context) bool { return false }, BundleID: bundle}
	mux := http.NewServeMux()
	mux.Handle("/", admission.Handler())
	mux.HandleFunc("GET /test/lease", func(w http.ResponseWriter, r *http.Request) { write(w, manager.Status()) })
	mux.HandleFunc("POST /test/claim", func(w http.ResponseWriter, r *http.Request) {
		var request contract.ClaimRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad claim", 400)
			return
		}
		grant, err := manager.Claim(request)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, grant)
	})
	mux.HandleFunc("POST /test/release", func(w http.ResponseWriter, r *http.Request) {
		status, err := manager.Release(r.Header.Get("X-FogCast-Kit-Lease"))
		if err != nil {
			failure(w, err)
			return
		}
		write(w, status)
	})
	mux.HandleFunc("POST /test/renew", func(w http.ResponseWriter, r *http.Request) {
		grant, err := manager.Renew(r.Header.Get("X-FogCast-Kit-Lease"))
		if err != nil {
			failure(w, err)
			return
		}
		write(w, grant)
	})
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		_ = server.Close()
		if err := <-done; err != nil && err != http.ErrServerClosed {
			t.Fatal(err)
		}
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Fatal(err)
		}
	}
}
