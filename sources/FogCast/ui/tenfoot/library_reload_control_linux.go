//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// An abstract socket has process lifetime and cannot leave a stale filesystem
// entry after a launcher crash. It is local to the kit's network namespace.
const libraryReloadSocket = "@fogcast-tenfoot-library-reload"

// startLibraryReloadControl exposes one supported kit-local diagnostic. A
// successful reply means the requested generation finished successfully; it
// does not claim that a frame has subsequently reached the display.
func startLibraryReloadControl(ctx context.Context, app *App, address string) (func(), error) {
	listener, err := net.Listen("unix", address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/reload" {
			http.NotFound(w, r)
			return
		}
		if err := app.reloadLibraryAndWait(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() { <-ctx.Done(); _ = server.Close() }()
	return func() { cancel(); _ = server.Close() }, nil
}

func (a *App) reloadLibraryAndWait(ctx context.Context) error {
	a.mu.Lock()
	if a.ctx == nil || a.ctx.Err() != nil {
		a.mu.Unlock()
		return errors.New("launcher is not running")
	}
	a.reloadCatalogLocked()
	gen := a.loadGen
	appCtx := a.ctx
	a.mu.Unlock()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.mu.Lock()
		superseded := a.loadGen != gen
		finished := a.loadFinishedGen == gen
		ok := a.loadFinishedOK
		a.mu.Unlock()
		if superseded {
			return errors.New("library reload superseded")
		}
		if finished {
			if !ok {
				return errors.New("library reload failed")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-appCtx.Done():
			return errors.New("launcher stopped")
		case <-ticker.C:
		}
	}
}

// RequestLibraryReload waits for the kit launcher's current library reload.
func RequestLibraryReload(ctx context.Context) error {
	return requestLibraryReload(ctx, libraryReloadSocket)
}

func requestLibraryReload(ctx context.Context, address string) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://tenfoot/reload", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s: %s", resp.Status, message)
	}
	return nil
}
