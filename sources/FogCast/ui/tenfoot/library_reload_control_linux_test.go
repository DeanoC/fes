//go:build linux

package tenfoot

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLibraryReloadControlWaitsAndKeepsBusyWarning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		wantError string
	}{
		{"success", 0, ""},
		{"failure", http.StatusInternalServerError, "library reload failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := newRoomHost(t)
			host.mu.Lock()
			host.state = "launching"
			host.mu.Unlock()
			app := startFirstRunWithHost(t, host, t.TempDir(), 1280, 720)
			waitRoom(t, app, "example.welcome")
			waitFor(t, app, "initial library and busy session", func(s Snapshot) bool { return !s.Loading && s.Session.State == "launching" })
			app.mu.Lock()
			app.runSetupActionLocked("setup_continue")
			app.mu.Unlock()
			if got := renderRoomLabelSource(t, app.Snapshot(), "nopanel-status"); got != setupBlockedStatus {
				t.Fatalf("before reload: footer = %q", got)
			}

			gate := make(chan struct{})
			defer func() { close(gate) }()
			arrived := make(chan struct{}, 1)
			host.mu.Lock()
			host.gamesGate, host.gamesArrived, host.gamesStatus = gate, arrived, tc.status
			host.mu.Unlock()
			address := fmt.Sprintf("@testrld-%d-%d", os.Getpid(), time.Now().UnixNano())
			stop, err := startLibraryReloadControl(t.Context(), app, address)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			result := make(chan error, 1)
			go func() { result <- requestLibraryReload(t.Context(), address) }()
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("reload did not reach host")
			}
			select {
			case err := <-result:
				t.Fatalf("returned before library response: %v", err)
			default:
			}
			close(gate)
			gate = make(chan struct{})
			select {
			case err := <-result:
				if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
					t.Fatalf("reload result = %v, want %q", err, tc.wantError)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("reload did not finish")
			}
			snap := app.Snapshot()
			if snap.Session.State != "launching" {
				t.Fatalf("session no longer busy: %s", snap.Session.State)
			}
			if got := renderRoomLabelSource(t, snap, "nopanel-status"); got != setupBlockedStatus {
				t.Fatalf("after reload: footer = %q", got)
			}
		})
	}
}

func TestLibraryReloadSupersededDoesNotReportSuccess(t *testing.T) {
	host := newRoomHost(t)
	app := startFirstRunWithHost(t, host, t.TempDir(), 1280, 720)
	waitFor(t, app, "initial library", func(s Snapshot) bool { return !s.Loading })
	gate := make(chan struct{})
	defer close(gate)
	arrived := make(chan struct{}, 1)
	host.mu.Lock()
	host.gamesGate, host.gamesArrived = gate, arrived
	host.mu.Unlock()
	result := make(chan error, 1)
	go func() { result <- app.reloadLibraryAndWait(context.Background()) }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("reload did not reach host")
	}
	app.mu.Lock()
	app.reloadCatalogLocked()
	app.mu.Unlock()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "superseded") {
			t.Fatalf("result = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superseded reload did not finish")
	}
}

func TestLibraryReloadControlConcurrentAndDeadline(t *testing.T) {
	host := newRoomHost(t)
	app := startFirstRunWithHost(t, host, t.TempDir(), 1280, 720)
	waitFor(t, app, "initial library", func(s Snapshot) bool { return !s.Loading })
	gate := make(chan struct{})
	defer func() { close(gate) }()
	arrived := make(chan struct{}, 2)
	host.mu.Lock()
	host.gamesGate, host.gamesArrived = gate, arrived
	host.mu.Unlock()
	address := fmt.Sprintf("@testrld-%d-%d", os.Getpid(), time.Now().UnixNano())
	stop, err := startLibraryReloadControl(t.Context(), app, address)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- requestLibraryReload(t.Context(), address) }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("first reload did not reach host")
	}
	go func() { second <- requestLibraryReload(t.Context(), address) }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("second reload did not reach host")
	}
	select {
	case err := <-first:
		if err == nil || !strings.Contains(err.Error(), "superseded") {
			t.Fatalf("first result = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first reload was not superseded")
	}
	select {
	case err := <-second:
		t.Fatalf("second reload completed before response: %v", err)
	default:
	}
	close(gate)
	gate = make(chan struct{})
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("second result = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second reload did not complete")
	}

	arrived = make(chan struct{}, 1)
	host.mu.Lock()
	host.gamesGate, host.gamesArrived = gate, arrived
	host.mu.Unlock()
	deadline, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := requestLibraryReload(deadline, address); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("deadline returned %v", err)
	}
	select {
	case <-arrived:
	default:
		t.Fatal("deadline did not exercise an in-flight load")
	}
}
