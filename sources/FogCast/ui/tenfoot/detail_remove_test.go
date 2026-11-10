package tenfoot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func removeTestApp(client *Client) *App {
	app := NewApp(client, 1280, 720, 10)
	app.games = []hostclient.Game{{ID: "core-a", Title: "A", CoreID: "fes.zx81", PackageID: strings.Repeat("a", 64)}, {ID: "core-b", Title: "B", CoreID: "fes.zx81", PackageID: strings.Repeat("b", 64)}}
	app.grid.SetCount(len(app.games))
	app.detailOpen = true
	return app
}

func TestDetailRemoveRequiresConfirmationAndKeepsOtherSelection(t *testing.T) {
	started, release := make(chan string, 1), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			requests.Add(1)
			started <- r.URL.Path
			<-release
			fmt.Fprint(w, `{"game_id":"core-a"}`)
			return
		}
		if r.URL.Path == "/api/v1/games" {
			fmt.Fprint(w, `{"games":[]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := removeTestApp(NewClient(server.URL, server.Client()))
	app.ctx = ctx
	app.mu.Lock()
	if !app.handleDetailLocked(CmdSortCycle) || app.detailRemoveID != "core-a" {
		t.Fatal("remove did not open confirmation for displayed entry")
	}
	app.handleDetailLocked(CmdBack)
	if app.detailRemoveID != "" || requests.Load() != 0 {
		t.Fatal("cancel removed a title")
	}
	app.handleDetailLocked(CmdSortCycle)
	app.handleDetailLocked(CmdSelect)
	app.mu.Unlock()
	select {
	case path := <-started:
		if path != "/api/v1/library/core-entries/core-a" {
			t.Fatalf("wrong title %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	app.mu.Lock()
	app.closeDetailLocked()
	app.libraryDetail = app.games[1]
	app.detailOpen = true
	app.mu.Unlock()
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		busy, open, id := app.detailRemoveBusy, app.detailOpen, app.libraryDetail.ID
		app.mu.Unlock()
		if !busy {
			if !open || id != "core-b" {
				t.Fatalf("completion changed another detail: open=%v id=%q", open, id)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests=%d", requests.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("removal did not complete")
}

func TestDetailRemoveRejectsNonCoreAndObsoleteConfirmation(t *testing.T) {
	app := removeTestApp(NewClient("http://127.0.0.1:1", nil))
	app.games[0].PackageID = ""
	if app.startDetailRemovalLocked() {
		t.Fatal("offered removal for non-core title")
	}
	app.games[0].PackageID = strings.Repeat("a", 64)
	if !app.startDetailRemovalLocked() {
		t.Fatal("missing remove action")
	}
	app.grid.Focus = 1
	app.handleDetailRemoveConfirmLocked(CmdSelect)
	if app.detailRemoveBusy || app.detailRemoveID != "" {
		t.Fatal("accepted confirmation after target changed")
	}
}

func TestDetailRemovePointerCannotLaunchWhileConfirming(t *testing.T) {
	app := removeTestApp(NewClient("http://127.0.0.1:1", nil))
	if !app.startDetailRemovalLocked() {
		t.Fatal("missing remove action")
	}
	snap := app.Snapshot()
	action, cancel, ok := detailRemoveRects(snap)
	if !ok {
		t.Fatal("missing buttons")
	}
	if got := HitTest(snap, action.X+1, action.Y+1).Kind; got != PointerDetailRemove {
		t.Fatalf("remove hit %v", got)
	}
	if got := HitTest(snap, cancel.X+1, cancel.Y+1).Kind; got != PointerDetailRemoveCancel {
		t.Fatalf("cancel hit %v", got)
	}
	geom, _ := detailGeomOf(snap)
	if got := HitTest(snap, geom.Pane.X+5, geom.Pane.Y+5).Kind; got == PointerDetail || got == PointerCatalog {
		t.Fatalf("confirmation allowed launch via %v", got)
	}
}

func TestDetailRemoveBusyKeepsConfirmationForRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			calls.Add(1)
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"code":"BUSY","message":"title is running"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	app := removeTestApp(NewClient(server.URL, server.Client()))
	app.mu.Lock()
	app.startDetailRemovalLocked()
	app.handleDetailRemoveConfirmLocked(CmdSelect)
	app.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		busy, id, status, open := app.detailRemoveBusy, app.detailRemoveID, app.status, app.detailOpen
		app.mu.Unlock()
		if !busy && calls.Load() == 1 {
			if !open || id != "core-a" || !strings.Contains(status, "Stop play") {
				t.Fatalf("error lost retry: open=%v id=%q status=%q", open, id, status)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("busy response did not complete")
}

func TestDetailRemoveConfirmationRenders(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	app.games[0].PackageID = strings.Repeat("a", 64)
	app.detailOpen = true
	if !app.startDetailRemovalLocked() {
		t.Fatal("missing remove action")
	}
	snap := writeLibraryScreenshot(t, app, "remove-confirm")
	if !snap.Detail.RemoveConfirm || !snap.Detail.RemoveAvailable || !strings.Contains(snap.Detail.Hint, "remove") {
		t.Fatalf("confirmation %+v", snap.Detail)
	}
	app.detailRemoveError = "Can't check kits. Reconnect them and retry."
	errorSnap := writeLibraryScreenshot(t, app, "remove-unavailable")
	if !strings.Contains(errorSnap.Detail.RemoveError, "Reconnect") {
		t.Fatalf("unavailable message %+v", errorSnap.Detail)
	}
}

func TestRoomDetailRemoveConfirmationUsesDisplayedEntry(t *testing.T) {
	instance, err := rooms.New(testRoomPack(t, "remove-test", "function draw() end"), rooms.Options{Width: 1280, Height: 720})
	if err != nil {
		t.Fatal(err)
	}
	app := removeTestApp(NewClient("http://127.0.0.1:1", nil))
	app.room = instance
	app.roomDetail = app.games[1]
	if !app.handleRoomDetailLocked(CmdSortCycle) || app.detailRemoveID != "core-b" {
		t.Fatalf("confirmation chose %q rather than the room Details title", app.detailRemoveID)
	}
	if hint := app.Snapshot().HeaderHint(); !strings.Contains(hint, "remove") || !strings.Contains(hint, "cancel") {
		t.Fatalf("missing confirmation hint %q", hint)
	}
	app.handleRoomDetailLocked(CmdBack)
	if app.detailRemoveID != "" || !app.detailOpen {
		t.Fatal("cancel should retain room Details")
	}
}

func TestDetailRemoveSuccessVisibleAfterCatalogRefresh(t *testing.T) {
	refreshStarted, release := make(chan struct{}, 2), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/core-entries/core-a":
			fmt.Fprint(w, `{"game_id":"core-a"}`)
		case r.URL.Path == "/api/v1/games":
			select {
			case refreshStarted <- struct{}{}:
			default:
			}
			<-release
			fmt.Fprint(w, `{"games":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := removeTestApp(NewClient(server.URL, server.Client()))
	app.ctx = ctx
	app.mu.Lock()
	app.startDetailRemovalLocked()
	app.handleDetailRemoveConfirmLocked(CmdSelect)
	app.mu.Unlock()
	select {
	case <-refreshStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	if got := app.Snapshot().Status; got != "Title removed. Saved data was kept." {
		t.Fatalf("during refresh status=%q", got)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := app.Snapshot()
		if !snap.Loading && snap.Status == "Title removed. Saved data was kept." {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := app.Snapshot()
	t.Fatalf("success not retained after refresh: loading=%v status=%q", snap.Loading, snap.Status)
}

func TestDetailRemoveUnavailableShowsReconnectActionInConfirmation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"code":"MISTER_UNAVAILABLE","message":"REMOTE_SECRET_PATH"}}`)
	}))
	defer server.Close()
	app := removeTestApp(NewClient(server.URL, server.Client()))
	app.mu.Lock()
	app.startDetailRemovalLocked()
	app.handleDetailRemoveConfirmLocked(CmdSelect)
	app.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := app.Snapshot()
		if calls.Load() > 0 && !snap.Detail.RemoveBusy {
			if !snap.Detail.RemoveConfirm || !strings.Contains(snap.Detail.RemoveError, "Reconnect") || strings.Contains(snap.Detail.RemoveError, "REMOTE_SECRET_PATH") {
				t.Fatalf("unavailable removal: %+v", snap.Detail)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("unavailable response did not complete")
}
