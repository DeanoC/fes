// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func programExport(t *testing.T, id string, request packagestore.Request, files map[string][]byte) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for name, data := range files {
		result[name] = bytes.Clone(data)
	}
	choice := programChoice{Path: request.Path, PackageID: request.PackageID, MetadataPath: request.MetadataPath, MetadataSHA256: request.MetadataSHA256, ROMReceiptSHA256: request.ReceiptSHA256}
	for _, source := range request.Sources {
		choice.Sources = append(choice.Sources, programROMSource{ID: source.ID, Role: source.Role, Path: source.Path, SHA256: source.SHA256})
	}
	if request.Tape != (packagestore.TapeRequest{}) {
		tape := request.Tape
		choice.Tape = &tape
	}
	result["FES/CORESEL.JSN"] = programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{choice}})
	result["FES/UI.JSON"] = []byte(fmt.Sprintf("{\"entries\":[{\"id\":%q,\"program\":0,\"title\":\"Downloaded core\"}],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n", id))
	return result
}

func TestProgramDownloadHTTPThenCatalogInstallLaunchAndReturn(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	request := b.entries[0].Request
	files := programUpload(f.r.f.files, request)
	// A valid filename containing URL syntax remains one file under the origin.
	old := request.Path
	request.Path = "FES/Core %? #.FCP"
	files[request.Path] = files[old]
	delete(files, old)
	export := programExport(t, "downloaded", request, files)
	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "artifact.test" || r.URL.RawQuery != "" || r.Header.Get("Accept-Encoding") != "identity" {
			http.Error(w, "bad request framing", 400)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/export/")
		data, ok := export[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hits[name]++
		mu.Unlock()
		w.Write(data)
	}))
	defer server.Close()
	source, err := NewProgramSource(server.Client(), server.URL+"/export/", "artifact.test")
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	plan, err := source.Download(context.Background(), b, "downloaded")
	if err != nil {
		t.Fatal(err)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("network preparation touched devices")
	}
	mu.Lock()
	for name := range export {
		if hits[name] != 1 {
			t.Errorf("%s fetched %d times", name, hits[name])
		}
	}
	mu.Unlock()
	server.Close() // Installation and activation must use only retained content.
	if err = b.AttachProgramInstall(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("catalog publication touched devices")
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "available" {
		t.Fatal(rows, err)
	}
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err != nil {
		t.Fatal(err)
	}
	setup, err := client.CoreSetup(context.Background(), rows[0].SourceID, rows[0].CoreID, rows[0].PackageID)
	if err != nil || len(setup.Entries) != 1 || setup.Entries[0].GameID != "downloaded" {
		t.Fatal(setup, err)
	}
	rows, err = client.AvailableCores(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "installed" {
		t.Fatal(rows, err)
	}
	play, err := client.Launch(context.Background(), "downloaded")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
		t.Fatal("downloaded core did not restore its menu")
	}
}

func TestProgramDownloadRefusesOriginChangesAndBadContentBeforeDevices(t *testing.T) {
	for _, kind := range []string{"redirect", "status", "oversize", "encoding", "truncated", "archive", "ROM", "metadata", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			f, b, _, _ := newProgramInstallFixture(t)
			request := b.entries[0].Request
			export := programExport(t, "remote", request, programUpload(f.r.f.files, request))
			var redirected atomic.Int64
			other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
			defer other.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				name := strings.TrimPrefix(r.URL.Path, "/")
				data, ok := export[name]
				if !ok {
					http.NotFound(w, r)
					return
				}
				if name == "FES/CORESEL.JSN" {
					switch kind {
					case "redirect":
						http.Redirect(w, r, other.URL, http.StatusFound)
						return
					case "status":
						http.Error(w, "unavailable", 503)
						return
					case "oversize":
						data = bytes.Repeat([]byte("x"), 8193)
					case "encoding":
						w.Header().Set("Content-Encoding", "gzip")
					case "truncated":
						w.Header().Set("Content-Length", fmt.Sprint(len(data)+1))
					}
				}
				if kind == "archive" && name == request.Path || kind == "ROM" && name == request.Sources[0].Path || kind == "metadata" && name == request.MetadataPath {
					data = bytes.Clone(data)
					data[0] ^= 1
				}
				w.Write(data)
			}))
			defer server.Close()
			source, err := NewProgramSource(server.Client(), server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			id := "remote"
			if kind == "unknown" {
				id = "not-listed"
			}
			calls, opens := f.r.cpu.calls, f.r.f.files.opens
			if _, err = source.Download(context.Background(), b, id); err == nil {
				t.Fatal("invalid transfer prepared an install")
			}
			if redirected.Load() != 0 || calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
				t.Fatal("failed transfer followed origin or touched devices")
			}
		})
	}
}

type programDownloadTransport func(*http.Request) (*http.Response, error)

func (f programDownloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type stalledDownload struct {
	closed   int
	closeErr error
}

func (*stalledDownload) Read([]byte) (int, error) { return 0, nil }
func (s *stalledDownload) Close() error           { s.closed++; return s.closeErr }

func TestProgramDownloadBoundsStalledResponsesAndCloseFailure(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	body := &stalledDownload{closeErr: errors.New("close failed")}
	client := &http.Client{Transport: programDownloadTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, ContentLength: -1, Request: r}, nil
	})}
	source, err := NewProgramSource(client, "http://192.0.2.1/export", "")
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	if _, err = source.Download(context.Background(), b, "remote"); !errors.Is(err, io.ErrNoProgress) || !errors.Is(err, body.closeErr) || body.closed != 1 {
		t.Fatal(err, body.closed)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens {
		t.Fatal("stalled HTTP touched devices")
	}
}

func TestProgramDownloadCancellationStopsPartialTransfer(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	source, err := NewProgramSource(server.Client(), server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := source.Download(ctx, b, "remote"); done <- err }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled transfer did not stop")
	}
	if len(f.r.f.files.installOperations) != 0 {
		t.Fatal("partial network transfer wrote storage")
	}
}

func TestProgramSourceRequiresExplicitFixedHTTPOrigin(t *testing.T) {
	for _, raw := range []string{"https://192.0.2.1", "http://example.test", "http://user@192.0.2.1", "http://192.0.2.1/path?query=1", "http://192.0.2.1/#fragment", "http://192.0.2.1/a/../b", "http://192.0.2.1//a//", "http://0.0.0.0"} {
		if _, err := NewProgramSource(&http.Client{}, raw, ""); err == nil {
			t.Fatal("invalid source", raw)
		}
	}
	if _, err := NewProgramSource(nil, "http://192.0.2.1", ""); err == nil {
		t.Fatal("missing client accepted")
	}
	if _, err := NewProgramSource(&http.Client{}, "http://192.0.2.1", "host\r\nX: invalid"); err == nil {
		t.Fatal("invalid Host accepted")
	}
}

func TestProgramDownloadRealFATInstallAndFreshBoot(t *testing.T) {
	ownedInstallRealFATCycle(t, func(b *ProgramBackend, request packagestore.Request, files map[string][]byte) (*ProgramInstall, error) {
		export := programExport(t, "persisted", request, files)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, ok := export[strings.TrimPrefix(r.URL.Path, "/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(data)
		}))
		defer server.Close()
		source, err := NewProgramSource(server.Client(), server.URL, "")
		if err != nil {
			return nil, err
		}
		return source.Download(context.Background(), b, "persisted")
	})
}

func TestProgramRetainedCatalogAttachmentRefusesWithoutDeviceIO(t *testing.T) {
	for _, kind := range []string{"nil-backend", "nil-context", "nil-plan", "foreign-plan", "cancelled", "running", "duplicate", "existing-id"} {
		t.Run(kind, func(t *testing.T) {
			f, b, plan, _ := newProgramInstallFixture(t)
			ctx := context.Background()
			target := b
			switch kind {
			case "nil-backend":
				target = nil
			case "nil-context":
				ctx = nil
			case "nil-plan":
				plan = nil
			case "foreign-plan":
				_, _, plan, _ = newProgramInstallFixture(t)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "running":
				client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
				if _, err := client.Launch(ctx, "existing"); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				if err := b.AttachProgramInstall(ctx, plan); err != nil {
					t.Fatal(err)
				}
			case "existing-id":
				var err error
				plan, err = b.prepareInstallContent(ctx, "existing", "Conflicting title", plan.content)
				if err != nil {
					t.Fatal(err)
				}
			}
			calls, opens := f.r.cpu.calls, f.r.f.files.opens
			writes, retained := len(f.r.f.files.installOperations), len(b.catalog)
			if err := target.AttachProgramInstall(ctx, plan); err == nil {
				t.Fatal("invalid catalog publication accepted")
			}
			if calls != f.r.cpu.calls || opens != f.r.f.files.opens || writes != len(f.r.f.files.installOperations) || retained != len(b.catalog) {
				t.Fatal("rejected attachment mutated devices or catalog")
			}
		})
	}
}
